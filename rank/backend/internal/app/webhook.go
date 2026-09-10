package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

var supportedWebhookEvents = []string{
	domain.WebhookEventResponseCompleted,
	domain.WebhookEventResponseFailed,
	domain.WebhookEventResponseCancelled,
	domain.WebhookEventResponseRequiresAction,
}

// WebhookSecretStore retains signing keys outside repository records.
type WebhookSecretStore interface {
	Save(context.Context, string, []byte) error
	Load(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

// WebhookSendResult contains the receiver's delivery decision.
type WebhookSendResult struct {
	StatusCode int
	RetryAfter time.Duration
}

// WebhookSender performs one network attempt after applying destination policy.
type WebhookSender interface {
	Send(context.Context, string, []byte, http.Header) (WebhookSendResult, error)
}

type secureWebhookSender struct{ client *http.Client }

func newSecureWebhookSender(timeout time.Duration) WebhookSender {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, errors.New("webhook host has no addresses")
			}
			for _, address := range addresses {
				if !publicResponseInputIP(address.IP) {
					return nil, errors.New("webhook URL resolves to a non-public address")
				}
			}
			var lastErr error
			for _, address := range addresses {
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	return &secureWebhookSender{client: &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (sender *secureWebhookSender) Send(ctx context.Context, destination string, body []byte, headers http.Header) (WebhookSendResult, error) {
	parsed, err := url.Parse(destination)
	if err != nil || validateWebhookURL(parsed) != nil {
		return WebhookSendResult{}, errors.New("webhooks require a valid public HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return WebhookSendResult{}, err
	}
	request.Header = headers.Clone()
	response, err := sender.client.Do(request)
	if err != nil {
		return WebhookSendResult{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	return WebhookSendResult{StatusCode: response.StatusCode, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now())}, nil
}

func validateWebhookURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" || value.Hostname() == "" || value.User != nil || value.Fragment != "" {
		return errors.New("webhooks require an HTTPS URL without credentials or fragments")
	}
	return nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

type CreateWebhookEndpointInput struct {
	TenantID      string
	Name          string
	URL           string
	Events        []string
	DeploymentIDs []domain.DeploymentID
}

type CreatedWebhookEndpoint struct {
	domain.WebhookEndpoint
	SigningSecret string `json:"signingSecret"`
}

// CreateWebhookEndpoint creates a deployment-scoped event subscription and
// returns its signing secret once.
func (s *Service) CreateWebhookEndpoint(ctx context.Context, input CreateWebhookEndpointInput) (CreatedWebhookEndpoint, error) {
	if s.webhookSecrets == nil {
		return CreatedWebhookEndpoint{}, problem(503, "webhook_secret_store_unavailable", "Webhook signing secret storage is unavailable")
	}
	input.TenantID, input.Name, input.URL = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.Name), strings.TrimSpace(input.URL)
	if input.TenantID == "" {
		input.TenantID = s.tenantID
	}
	if input.Name == "" || len(input.Name) > 80 {
		return CreatedWebhookEndpoint{}, problem(400, "invalid_webhook_name", "Webhook endpoint name must contain 1 to 80 characters")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || validateWebhookURL(parsed) != nil {
		return CreatedWebhookEndpoint{}, problem(400, "invalid_webhook_url", "Webhook endpoint requires a valid HTTPS URL without credentials or fragments")
	}
	input.Events = uniqueWebhookStrings(input.Events)
	if len(input.Events) == 0 {
		return CreatedWebhookEndpoint{}, problem(400, "webhook_events_required", "Webhook endpoint requires at least one event type")
	}
	for _, event := range input.Events {
		if !slices.Contains(supportedWebhookEvents, event) {
			return CreatedWebhookEndpoint{}, problem(400, "unsupported_webhook_event", "Unsupported Webhook event type: "+event)
		}
	}
	input.DeploymentIDs = uniqueDeploymentIDs(input.DeploymentIDs)
	if len(input.DeploymentIDs) == 0 {
		return CreatedWebhookEndpoint{}, problem(400, "webhook_deployments_required", "Webhook endpoint requires at least one Deployment")
	}
	now := s.now().UTC()
	endpoint := domain.WebhookEndpoint{
		ID: domain.WebhookEndpointID(newServingID("wh")), TenantID: input.TenantID, Name: input.Name, URL: parsed.String(),
		Events: input.Events, DeploymentIDs: input.DeploymentIDs, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	endpoint.SecretRef = string(endpoint.ID)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return CreatedWebhookEndpoint{}, err
	}
	secret := "whsec_" + base64.StdEncoding.EncodeToString(secretBytes)
	if err := s.webhookSecrets.Save(ctx, endpoint.SecretRef, []byte(secret)); err != nil {
		return CreatedWebhookEndpoint{}, fmt.Errorf("save Webhook signing secret: %w", err)
	}
	err = s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		for _, deploymentID := range endpoint.DeploymentIDs {
			deployment, getErr := repo.GetDeployment(ctx, deploymentID)
			if getErr != nil {
				return getErr
			}
			if deployment.TenantID != endpoint.TenantID {
				return problem(400, "webhook_deployment_tenant_mismatch", "Webhook endpoint and Deployment must belong to the same tenant")
			}
		}
		return repo.CreateWebhookEndpoint(ctx, endpoint)
	})
	if err != nil {
		_ = s.webhookSecrets.Delete(context.Background(), endpoint.SecretRef)
		return CreatedWebhookEndpoint{}, err
	}
	return CreatedWebhookEndpoint{WebhookEndpoint: endpoint, SigningSecret: secret}, nil
}

func uniqueWebhookStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func uniqueDeploymentIDs(values []domain.DeploymentID) []domain.DeploymentID {
	result := make([]domain.DeploymentID, 0, len(values))
	for _, value := range values {
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func (s *Service) ListWebhookEndpoints(ctx context.Context, tenantID string) ([]domain.WebhookEndpoint, error) {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = s.tenantID
	}
	return s.repo.ListWebhookEndpoints(ctx, strings.TrimSpace(tenantID))
}

func (s *Service) ListWebhookDeliveries(ctx context.Context, id domain.WebhookEndpointID) ([]domain.WebhookDelivery, error) {
	_, err := s.repo.GetWebhookEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.repo.ListWebhookDeliveries(ctx, id)
}

func (s *Service) SetWebhookEndpointEnabled(ctx context.Context, id domain.WebhookEndpointID, enabled bool) (domain.WebhookEndpoint, error) {
	endpoint, err := s.repo.GetWebhookEndpoint(ctx, id)
	if err != nil {
		return endpoint, err
	}
	if err := s.repo.SetWebhookEndpointEnabled(ctx, id, enabled, s.now().UTC()); err != nil {
		return endpoint, err
	}
	return s.repo.GetWebhookEndpoint(ctx, id)
}

type webhookPayload struct {
	ID        domain.WebhookDeliveryID `json:"id"`
	Object    string                   `json:"object"`
	CreatedAt int64                    `json:"created_at"`
	Type      string                   `json:"type"`
	Data      webhookPayloadData       `json:"data"`
}

type webhookPayloadData struct {
	ID     domain.ResponseID `json:"id"`
	Object string            `json:"object"`
}

func webhookEventForStatus(status string) string {
	switch status {
	case domain.ResponseCompleted:
		return domain.WebhookEventResponseCompleted
	case domain.ResponseFailed:
		return domain.WebhookEventResponseFailed
	case domain.ResponseCancelled:
		return domain.WebhookEventResponseCancelled
	case domain.ResponseRequiresAction:
		return domain.WebhookEventResponseRequiresAction
	default:
		return ""
	}
}

func (s *Service) updateResponseWithWebhooks(ctx context.Context, response domain.ServingResponse, expectedStatus string, events []domain.ResponseEvent) error {
	eventType := webhookEventForStatus(response.Status)
	return s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		if err := repo.UpdateResponse(ctx, response, expectedStatus, events); err != nil {
			return err
		}
		if eventType == "" {
			return nil
		}
		endpoints, err := repo.ListWebhookEndpoints(ctx, response.TenantID)
		if err != nil {
			return err
		}
		for _, endpoint := range endpoints {
			if !endpoint.Enabled || !slices.Contains(endpoint.Events, eventType) || !slices.Contains(endpoint.DeploymentIDs, response.DeploymentID) {
				continue
			}
			deliveryID := domain.WebhookDeliveryID(newServingID("evt"))
			payload, err := json.Marshal(webhookPayload{
				ID: deliveryID, Object: "event", CreatedAt: events[len(events)-1].CreatedAt.Unix(), Type: eventType,
				Data: webhookPayloadData{ID: response.ID, Object: "response"},
			})
			if err != nil {
				return err
			}
			if err := repo.CreateWebhookDelivery(ctx, domain.WebhookDelivery{
				ID: deliveryID, EndpointID: endpoint.ID, ResponseID: response.ID, EventType: eventType,
				Payload: payload, Status: domain.WebhookDeliveryPending, NextAttemptAt: events[len(events)-1].CreatedAt, CreatedAt: events[len(events)-1].CreatedAt,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// RunWebhookDispatcher delivers due outbox records until its context ends.
func (s *Service) RunWebhookDispatcher(ctx context.Context) error {
	if s.webhookSecrets == nil {
		return nil
	}
	ticker := time.NewTicker(s.webhookPollInterval)
	defer ticker.Stop()
	for {
		if err := s.DeliverDueWebhooks(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// DeliverDueWebhooks claims and attempts the current due batch.
func (s *Service) DeliverDueWebhooks(ctx context.Context) error {
	now := s.now().UTC()
	deliveries, err := s.repo.ListDueWebhookDeliveries(ctx, now, 32)
	if err != nil {
		return err
	}
	for _, candidate := range deliveries {
		leaseToken := newServingID("lease")
		delivery, claimed, err := s.repo.ClaimWebhookDelivery(ctx, candidate.ID, now, now.Add(s.webhookAttemptTimeout), leaseToken)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := s.deliverWebhook(ctx, delivery, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deliverWebhook(ctx context.Context, delivery domain.WebhookDelivery, attemptedAt time.Time) error {
	endpoint, err := s.repo.GetWebhookEndpoint(ctx, delivery.EndpointID)
	if err != nil {
		return s.scheduleWebhookRetry(ctx, delivery, attemptedAt, 0, err.Error())
	}
	if !endpoint.Enabled {
		return ignoreStaleWebhookClaim(s.repo.RetryWebhookDelivery(ctx, delivery.ID, delivery.LeaseToken, attemptedAt, true, "Webhook endpoint is disabled"))
	}
	secret, err := s.webhookSecrets.Load(ctx, endpoint.SecretRef)
	if err != nil {
		return s.scheduleWebhookRetry(ctx, delivery, attemptedAt, 0, "load signing secret: "+err.Error())
	}
	headers, err := signedWebhookHeaders(delivery.ID, attemptedAt, delivery.Payload, secret)
	if err != nil {
		return s.scheduleWebhookRetry(ctx, delivery, attemptedAt, 0, err.Error())
	}
	result, sendErr := s.webhookSender.Send(ctx, endpoint.URL, delivery.Payload, headers)
	if sendErr == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
		return ignoreStaleWebhookClaim(s.repo.CompleteWebhookDelivery(ctx, delivery.ID, delivery.LeaseToken, attemptedAt))
	}
	if result.StatusCode == http.StatusGone {
		if err := s.repo.SetWebhookEndpointEnabled(ctx, endpoint.ID, false, attemptedAt); err != nil {
			return err
		}
		return ignoreStaleWebhookClaim(s.repo.RetryWebhookDelivery(ctx, delivery.ID, delivery.LeaseToken, attemptedAt, true, "receiver returned HTTP 410 Gone"))
	}
	message := "Webhook delivery failed"
	if sendErr != nil {
		message = sendErr.Error()
	} else if result.StatusCode != 0 {
		message = fmt.Sprintf("receiver returned HTTP %d", result.StatusCode)
	}
	return s.scheduleWebhookRetry(ctx, delivery, attemptedAt, result.RetryAfter, message)
}

func (s *Service) scheduleWebhookRetry(ctx context.Context, delivery domain.WebhookDelivery, attemptedAt time.Time, retryAfter time.Duration, message string) error {
	terminal := delivery.Attempts > len(s.webhookRetrySchedule)
	delay := time.Duration(0)
	if !terminal {
		delay = s.webhookRetrySchedule[delivery.Attempts-1]
		if retryAfter > delay {
			delay = retryAfter
		}
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	return ignoreStaleWebhookClaim(s.repo.RetryWebhookDelivery(ctx, delivery.ID, delivery.LeaseToken, attemptedAt.Add(delay), terminal, message))
}

func ignoreStaleWebhookClaim(err error) error {
	if errors.Is(err, domain.ErrConflict) {
		return nil
	}
	return err
}

func signedWebhookHeaders(id domain.WebhookDeliveryID, at time.Time, body, serializedSecret []byte) (http.Header, error) {
	secretText := strings.TrimSpace(string(serializedSecret))
	if !strings.HasPrefix(secretText, "whsec_") {
		return nil, errors.New("invalid Webhook signing secret")
	}
	secret, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secretText, "whsec_"))
	if err != nil || len(secret) < 24 || len(secret) > 64 {
		return nil, errors.New("invalid Webhook signing secret")
	}
	timestamp := strconv.FormatInt(at.Unix(), 10)
	message := string(id) + "." + timestamp + "." + string(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "Rank-Webhooks/1.0")
	headers.Set("webhook-id", string(id))
	headers.Set("webhook-timestamp", timestamp)
	headers.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return headers, nil
}
