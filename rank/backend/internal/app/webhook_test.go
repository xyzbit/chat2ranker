package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/sqlstore"
	"github.com/xyzbit/agentkiln/rank/backend/internal/testdb"
)

type memoryWebhookSecrets struct{ values map[string][]byte }

func (store *memoryWebhookSecrets) Save(_ context.Context, reference string, value []byte) error {
	if store.values == nil {
		store.values = map[string][]byte{}
	}
	if _, exists := store.values[reference]; exists {
		return errors.New("secret exists")
	}
	store.values[reference] = append([]byte(nil), value...)
	return nil
}

func (store *memoryWebhookSecrets) Load(_ context.Context, reference string) ([]byte, error) {
	value, exists := store.values[reference]
	if !exists {
		return nil, errors.New("secret not found")
	}
	return append([]byte(nil), value...), nil
}

func (store *memoryWebhookSecrets) Delete(_ context.Context, reference string) error {
	delete(store.values, reference)
	return nil
}

type capturedWebhookAttempt struct {
	body    []byte
	headers http.Header
}

type fakeWebhookSender struct {
	results  []WebhookSendResult
	errors   []error
	attempts []capturedWebhookAttempt
}

func (sender *fakeWebhookSender) Send(_ context.Context, _ string, body []byte, headers http.Header) (WebhookSendResult, error) {
	sender.attempts = append(sender.attempts, capturedWebhookAttempt{body: append([]byte(nil), body...), headers: headers.Clone()})
	index := len(sender.attempts) - 1
	var result WebhookSendResult
	if index < len(sender.results) {
		result = sender.results[index]
	}
	var err error
	if index < len(sender.errors) {
		err = sender.errors[index]
	}
	return result, err
}

func TestWebhookOutboxSignsRetriesAndRecoversExpiredLease(t *testing.T) {
	ctx := context.Background()
	store, err := testdb.Open(t, filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	secrets := &memoryWebhookSecrets{}
	sender := &fakeWebhookSender{results: []WebhookSendResult{{StatusCode: http.StatusInternalServerError}, {StatusCode: http.StatusNoContent}, {StatusCode: http.StatusNoContent}}}
	service := NewService(store, Options{
		Clock: func() time.Time { return now }, WebhookSecrets: secrets, WebhookSender: sender,
		WebhookAttemptTimeout: time.Second, WebhookRetrySchedule: []time.Duration{time.Second},
	})
	if err := service.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	deployments, err := service.ListDeployments(ctx, "local")
	if err != nil || len(deployments) != 1 {
		t.Fatalf("default Deployment: %v %#v", err, deployments)
	}
	createdEndpoint, err := service.CreateWebhookEndpoint(ctx, CreateWebhookEndpointInput{
		Name: "order events", URL: "https://events.example.com/rank",
		Events: []string{domain.WebhookEventResponseCompleted}, DeploymentIDs: []domain.DeploymentID{deployments[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(createdEndpoint.SigningSecret, "whsec_") || createdEndpoint.SecretRef == "" {
		t.Fatalf("signing secret was not created: %#v", createdEndpoint)
	}
	encodedEndpoint, err := json.Marshal(createdEndpoint.WebhookEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedEndpoint), "secretRef") || strings.Contains(string(encodedEndpoint), "whsec_") {
		t.Fatalf("endpoint projection leaked signing material: %s", encodedEndpoint)
	}
	credential, err := service.CreateAPICredential(ctx, CreateAPICredentialInput{
		TenantID: "local", Name: "webhook-test", Scopes: []string{domain.ScopeResponsesRead}, DeploymentIDs: []domain.DeploymentID{deployments[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := admitWebhookTestResponse(t, store, credential.APICredential, deployments[0], "resp_webhook_1", now)
	first.Status, first.CompletedAt = domain.ResponseCompleted, &now
	completed := domain.ResponseEvent{ResponseID: first.ID, Type: domain.WebhookEventResponseCompleted, Data: []byte(`{}`), CreatedAt: now}
	if err := service.updateResponseWithWebhooks(ctx, first, domain.ResponseQueued, []domain.ResponseEvent{completed}); err != nil {
		t.Fatal(err)
	}
	deliveries, err := service.ListWebhookDeliveries(ctx, createdEndpoint.ID)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != domain.WebhookDeliveryPending {
		t.Fatalf("transactional outbox: %v %#v", err, deliveries)
	}
	if err := service.DeliverDueWebhooks(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, _ = service.ListWebhookDeliveries(ctx, createdEndpoint.ID)
	if deliveries[0].Status != domain.WebhookDeliveryPending || deliveries[0].Attempts != 1 || !deliveries[0].NextAttemptAt.Equal(now.Add(time.Second)) {
		t.Fatalf("failed attempt was not scheduled: %#v", deliveries[0])
	}
	assertWebhookSignature(t, sender.attempts[0], createdEndpoint.SigningSecret)
	now = now.Add(2 * time.Second)
	if err := service.DeliverDueWebhooks(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, _ = service.ListWebhookDeliveries(ctx, createdEndpoint.ID)
	if deliveries[0].Status != domain.WebhookDeliveryDelivered || deliveries[0].Attempts != 2 {
		t.Fatalf("retry did not complete: %#v", deliveries[0])
	}

	second := admitWebhookTestResponse(t, store, credential.APICredential, deployments[0], "resp_webhook_2", now)
	second.Status, second.CompletedAt = domain.ResponseCompleted, &now
	if err := service.updateResponseWithWebhooks(ctx, second, domain.ResponseQueued, []domain.ResponseEvent{{ResponseID: second.ID, Type: domain.WebhookEventResponseCompleted, Data: []byte(`{}`), CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	deliveries, _ = service.ListWebhookDeliveries(ctx, createdEndpoint.ID)
	leased, claimed, err := store.ClaimWebhookDelivery(ctx, deliveries[0].ID, now, now.Add(time.Second), "lease-before-restart")
	if err != nil || !claimed || leased.Attempts != 1 {
		t.Fatalf("claim before restart: %v %v %#v", err, claimed, leased)
	}
	now = now.Add(2 * time.Second)
	restarted := NewService(store, Options{
		Clock: func() time.Time { return now }, WebhookSecrets: secrets, WebhookSender: sender,
		WebhookAttemptTimeout: time.Second, WebhookRetrySchedule: []time.Duration{time.Second},
	})
	if err := restarted.DeliverDueWebhooks(ctx); err != nil {
		t.Fatal(err)
	}
	deliveries, _ = restarted.ListWebhookDeliveries(ctx, createdEndpoint.ID)
	if deliveries[0].Status != domain.WebhookDeliveryDelivered || deliveries[0].Attempts != 2 {
		t.Fatalf("expired lease was not recovered after restart: %#v", deliveries[0])
	}
}

func admitWebhookTestResponse(t *testing.T, store *sqlstore.Store, credential domain.APICredential, deployment domain.Deployment, id string, now time.Time) domain.ServingResponse {
	t.Helper()
	response := domain.ServingResponse{
		ID: domain.ResponseID(id), TenantID: deployment.TenantID, CredentialID: credential.ID, DeploymentID: deployment.ID,
		DeploymentName: deployment.Name, RequestedReference: deployment.Name + "@v1", ResolutionKind: domain.ResolutionExact,
		AgentVersionID: "agent-research-demo-v1", ConversationID: domain.ConversationID("conv_" + id),
		IdempotencyKey: id, RequestHash: "sha256:" + id, Status: domain.ResponseQueued, OutputItemID: "msg_" + id, Metadata: map[string]string{}, CreatedAt: now,
	}
	conversation := domain.Conversation{
		ID: response.ConversationID, TenantID: response.TenantID, DeploymentID: response.DeploymentID,
		AgentVersionID: response.AgentVersionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.AdmitResponse(context.Background(), response, &conversation, domain.ResponseEvent{ResponseID: response.ID, Type: "response.created", Data: []byte(`{}`), CreatedAt: now}, 100, 4); err != nil {
		t.Fatal(err)
	}
	return response
}

func assertWebhookSignature(t *testing.T, attempt capturedWebhookAttempt, secretText string) {
	t.Helper()
	secret, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secretText, "whsec_"))
	if err != nil {
		t.Fatal(err)
	}
	id, timestamp := attempt.headers.Get("webhook-id"), attempt.headers.Get("webhook-timestamp")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(id + "." + timestamp + "." + string(attempt.body)))
	expected := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if attempt.headers.Get("webhook-signature") != expected {
		t.Fatalf("signature mismatch: got %q want %q", attempt.headers.Get("webhook-signature"), expected)
	}
	var payload webhookPayload
	if err := json.Unmarshal(attempt.body, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.ID) != id || payload.Type != domain.WebhookEventResponseCompleted || payload.Data.Object != "response" {
		t.Fatalf("unexpected thin payload: %#v", payload)
	}
}

func TestSecureWebhookSenderRejectsPrivateDestination(t *testing.T) {
	sender := newSecureWebhookSender(time.Second)
	_, err := sender.Send(context.Background(), "https://127.0.0.1/hook", []byte(`{}`), make(http.Header))
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private destination was not rejected: %v", err)
	}
}
