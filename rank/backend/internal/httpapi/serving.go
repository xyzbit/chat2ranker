package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type deploymentRequest struct {
	TenantID      string                  `json:"tenantId"`
	AgentFamilyID string                  `json:"agentFamilyId"`
	Name          string                  `json:"name"`
	PolicyDigest  string                  `json:"policyDigest"`
	Policy        domain.DeploymentPolicy `json:"policy"`
}

func (h *Handler) listDeployments(response http.ResponseWriter, request *http.Request) {
	if versionID := request.URL.Query().Get("agentVersionId"); versionID != "" {
		value, err := h.service.ListDeploymentsByAgentVersion(request.Context(), request.URL.Query().Get("tenantId"), versionID)
		writeResult(response, value, err, http.StatusOK)
		return
	}
	value, err := h.service.ListDeployments(request.Context(), request.URL.Query().Get("tenantId"))
	writeResult(response, value, err, http.StatusOK)
}

func (h *Handler) createDeployment(response http.ResponseWriter, request *http.Request) {
	var input deploymentRequest
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateDeployment(request.Context(), app.CreateDeploymentInput{
		TenantID: input.TenantID, AgentFamilyID: input.AgentFamilyID, Name: input.Name,
		PolicyDigest: input.PolicyDigest, Policy: input.Policy,
	})
	writeResult(response, value, err, http.StatusCreated)
}

func (h *Handler) getDeployment(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.GetDeployment(request.Context(), domain.DeploymentID(request.PathValue("id")))
	writeResult(response, value, err, http.StatusOK)
}

func (h *Handler) promoteDeploymentAlias(response http.ResponseWriter, request *http.Request) {
	var input struct {
		AgentVersionID            string                   `json:"agentVersionId"`
		ExpectedReleaseRevisionID domain.ReleaseRevisionID `json:"expectedReleaseRevisionId"`
		ExpectedPolicyRevision    int64                    `json:"expectedPolicyRevision"`
		EvidenceRunID             string                   `json:"evidenceRunId"`
		PolicyResult              string                   `json:"policyResult"`
		Actor                     string                   `json:"actor"`
		Reason                    string                   `json:"reason"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.PromoteDeploymentAlias(request.Context(), app.PromoteDeploymentInput{
		DeploymentID: domain.DeploymentID(request.PathValue("id")), Alias: request.PathValue("alias"),
		AgentVersionID: input.AgentVersionID, ExpectedReleaseRevisionID: input.ExpectedReleaseRevisionID,
		ExpectedPolicyRevision: input.ExpectedPolicyRevision,
		EvidenceRunID:          input.EvidenceRunID, PolicyResult: input.PolicyResult, Actor: requestActor(request, input.Actor), Reason: input.Reason,
	})
	writeResult(response, value, err, http.StatusCreated)
}

func (h *Handler) createServingCredential(response http.ResponseWriter, request *http.Request) {
	var input struct {
		TenantID      string                `json:"tenantId"`
		Name          string                `json:"name"`
		Scopes        []string              `json:"scopes"`
		DeploymentIDs []domain.DeploymentID `json:"deploymentIds"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateAPICredential(request.Context(), app.CreateAPICredentialInput{
		TenantID: input.TenantID, Name: input.Name, Scopes: input.Scopes, DeploymentIDs: input.DeploymentIDs,
	})
	writeResult(response, value, err, http.StatusCreated)
}

func (h *Handler) listWebhookEndpoints(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ListWebhookEndpoints(request.Context(), request.URL.Query().Get("tenantId"))
	writeResult(response, value, err, http.StatusOK)
}

func (h *Handler) createWebhookEndpoint(response http.ResponseWriter, request *http.Request) {
	var input struct {
		TenantID      string                `json:"tenantId"`
		Name          string                `json:"name"`
		URL           string                `json:"url"`
		Events        []string              `json:"events"`
		DeploymentIDs []domain.DeploymentID `json:"deploymentIds"`
	}
	if err := decode(request, &input); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.CreateWebhookEndpoint(request.Context(), app.CreateWebhookEndpointInput{
		TenantID: input.TenantID, Name: input.Name, URL: input.URL, Events: input.Events, DeploymentIDs: input.DeploymentIDs,
	})
	writeResult(response, value, err, http.StatusCreated)
}

func (h *Handler) listWebhookDeliveries(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.ListWebhookDeliveries(request.Context(), domain.WebhookEndpointID(request.PathValue("id")))
	writeResult(response, value, err, http.StatusOK)
}

func (h *Handler) enableWebhookEndpoint(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.SetWebhookEndpointEnabled(request.Context(), domain.WebhookEndpointID(request.PathValue("id")), true)
	writeResult(response, value, err, http.StatusOK)
}

func (h *Handler) disableWebhookEndpoint(response http.ResponseWriter, request *http.Request) {
	value, err := h.service.SetWebhookEndpointEnabled(request.Context(), domain.WebhookEndpointID(request.PathValue("id")), false)
	writeResult(response, value, err, http.StatusOK)
}

type responseRequest struct {
	Model              string                 `json:"model"`
	Input              json.RawMessage        `json:"input"`
	Instructions       string                 `json:"instructions"`
	Metadata           map[string]string      `json:"metadata"`
	Background         bool                   `json:"background"`
	Stream             bool                   `json:"stream"`
	Tools              json.RawMessage        `json:"tools"`
	Text               *app.ResponseTextInput `json:"text"`
	PreviousResponseID string                 `json:"previous_response_id"`
	MaxOutputTokens    *int                   `json:"max_output_tokens"`
	Store              *bool                  `json:"store"`
}

func (h *Handler) createServingResponse(response http.ResponseWriter, request *http.Request) {
	credential, err := h.authenticateServing(request, domain.ScopeResponsesCreate)
	if err != nil {
		writeServingError(response, err)
		return
	}
	var input responseRequest
	if err := decodeStrict(request, &input); err != nil {
		writeServingError(response, err)
		return
	}
	created, replayed, err := h.service.CreateServingResponse(request.Context(), credential, app.CreateServingResponseInput{
		Model: input.Model, Input: input.Input, Instructions: input.Instructions, Metadata: input.Metadata,
		Background: input.Background, IdempotencyKey: request.Header.Get("Idempotency-Key"), Tools: input.Tools,
		Text: input.Text, PreviousResponseID: input.PreviousResponseID, MaxOutputTokens: input.MaxOutputTokens, Store: input.Store,
	})
	if err != nil {
		writeServingError(response, err)
		return
	}
	if replayed {
		response.Header().Set("X-Idempotent-Replayed", "true")
	}
	if input.Stream {
		h.streamServingResponse(response, request, created.ID, 0)
		return
	}
	if !input.Background && !domain.IsResponseTerminal(created.Status) {
		created, err = h.service.WaitServingResponse(request.Context(), created.ID)
		if err != nil {
			writeServingError(response, err)
			return
		}
	}
	writeJSON(response, http.StatusOK, app.ResponseObjectFor(created))
}

func (h *Handler) getServingResponse(response http.ResponseWriter, request *http.Request) {
	credential, err := h.authenticateServing(request, domain.ScopeResponsesRead)
	if err != nil {
		writeServingError(response, err)
		return
	}
	id := domain.ResponseID(request.PathValue("id"))
	value, err := h.service.GetServingResponse(request.Context(), credential, id)
	if err != nil {
		writeServingError(response, err)
		return
	}
	if request.URL.Query().Get("stream") == "true" {
		after, parseErr := responseEventCursor(request)
		if parseErr != nil {
			writeServingError(response, parseErr)
			return
		}
		h.streamServingResponse(response, request, value.ID, after)
		return
	}
	writeJSON(response, http.StatusOK, app.ResponseObjectFor(value))
}

func (h *Handler) cancelServingResponse(response http.ResponseWriter, request *http.Request) {
	credential, err := h.authenticateServing(request, domain.ScopeResponsesCancel)
	if err != nil {
		writeServingError(response, err)
		return
	}
	value, err := h.service.CancelServingResponse(request.Context(), credential, domain.ResponseID(request.PathValue("id")))
	if err != nil {
		writeServingError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, app.ResponseObjectFor(value))
}

func (h *Handler) decideServingApproval(response http.ResponseWriter, request *http.Request) {
	credential, err := h.authenticateServing(request, domain.ScopeResponsesApprove)
	if err != nil {
		writeServingError(response, err)
		return
	}
	var input struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason"`
	}
	if err := decodeStrict(request, &input); err != nil {
		writeServingError(response, err)
		return
	}
	value, err := h.service.DecideServingApproval(request.Context(), credential, domain.ResponseID(request.PathValue("id")), input.Approved, input.Reason)
	if err != nil {
		writeServingError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, app.ResponseObjectFor(value))
}

func (h *Handler) authenticateServing(request *http.Request, scope string) (domain.APICredential, error) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return domain.APICredential{}, &app.Error{Status: 401, Code: "invalid_api_key", Message: "Authorization must use a bearer API key"}
	}
	return h.service.AuthenticateServingCredential(request.Context(), parts[1], scope)
}

func responseEventCursor(request *http.Request) (int64, error) {
	value := strings.TrimSpace(request.URL.Query().Get("starting_after"))
	if value == "" {
		value = strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	}
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, &app.Error{Status: 400, Code: "invalid_event_cursor", Message: "starting_after must be a non-negative event sequence"}
	}
	return cursor, nil
}

func (h *Handler) streamServingResponse(response http.ResponseWriter, request *http.Request, id domain.ResponseID, after int64) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeServingError(response, &app.Error{Status: 500, Code: "stream_unsupported", Message: "SSE is not supported"})
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()
	for {
		events, terminal, err := h.service.WaitResponseEvents(request.Context(), id, after, 15*time.Second)
		if err != nil {
			if errors.Is(err, request.Context().Err()) {
				return
			}
			payload, _ := json.Marshal(map[string]any{"error": map[string]any{"code": "stream_error", "message": err.Error()}})
			_, _ = response.Write([]byte("event: error\ndata: " + string(payload) + "\n\n"))
			flusher.Flush()
			return
		}
		if len(events) == 0 {
			if terminal {
				return
			}
			_, _ = io.WriteString(response, ": keepalive\n\n")
			flusher.Flush()
			continue
		}
		for _, event := range events {
			payload := map[string]any{}
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				return
			}
			payload["type"] = event.Type
			payload["sequence_number"] = event.Sequence
			encoded, _ := json.Marshal(payload)
			_, _ = response.Write([]byte("id: " + strconv.FormatInt(event.Sequence, 10) + "\nevent: " + event.Type + "\ndata: " + string(encoded) + "\n\n"))
			after = event.Sequence
		}
		flusher.Flush()
	}
}

func decodeStrict(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 32<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &app.Error{Status: 400, Code: "invalid_request", Message: "request JSON contains invalid or unsupported fields"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return &app.Error{Status: 400, Code: "invalid_request", Message: "request body must contain one JSON object"}
	}
	return nil
}

func writeServingError(response http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", err.Error()
	var problem *app.Error
	if errors.As(err, &problem) {
		status, code, message = problem.Status, problem.Code, problem.Message
	} else if errors.Is(err, context.DeadlineExceeded) {
		status, code, message = http.StatusServiceUnavailable, "dependency_timeout", "A service dependency timed out; check the existing request before retrying"
	} else if errors.Is(err, domain.ErrNotFound) {
		status, code, message = http.StatusNotFound, "not_found", "Response or Deployment not found"
	} else if errors.Is(err, domain.ErrConflict) {
		status, code, message = http.StatusConflict, "conflict", "resource state conflict"
	}
	errorType := "invalid_request_error"
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		errorType = "authentication_error"
	} else if status == http.StatusTooManyRequests {
		errorType = "rate_limit_error"
	} else if status >= 500 {
		errorType = "server_error"
	}
	writeJSON(response, status, map[string]any{"error": map[string]any{"message": message, "type": errorType, "param": nil, "code": code}})
}
