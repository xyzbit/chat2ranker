package httpapi_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/httpapi"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/webhooksecret"
)

type fakeServingExecution struct {
	mu           sync.Mutex
	autoComplete bool
	watches      int
	next         int
	byKey        map[string]string
	executions   map[string]contract.Execution
	done         map[string]chan struct{}
}

type fakeInputFetcher struct {
	calls int
	url   string
	data  []byte
	mime  string
}

func (fetcher *fakeInputFetcher) Fetch(_ context.Context, source string, _ int64) ([]byte, string, error) {
	fetcher.calls++
	fetcher.url = source
	return append([]byte(nil), fetcher.data...), fetcher.mime, nil
}

func newFakeServingExecution(autoComplete bool) *fakeServingExecution {
	return &fakeServingExecution{autoComplete: autoComplete, byKey: map[string]string{}, executions: map[string]contract.Execution{}, done: map[string]chan struct{}{}}
}

func (fake *fakeServingExecution) Submit(_ context.Context, request contract.SubmitRequest) (contract.Execution, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if id := fake.byKey[request.IdempotencyKey]; id != "" {
		return fake.executions[id], nil
	}
	fake.next++
	id := fmt.Sprintf("exec-serving-%d", fake.next)
	execution := contract.Execution{ID: id, Status: contract.StatusQueued, Spec: request.Spec, CreatedAt: time.Now()}
	fake.byKey[request.IdempotencyKey] = id
	fake.executions[id] = execution
	fake.done[id] = make(chan struct{})
	return execution, nil
}

func (fake *fakeServingExecution) Get(_ context.Context, id string) (contract.Execution, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, ok := fake.executions[id]
	if !ok {
		return contract.Execution{}, errors.New("execution not found")
	}
	return value, nil
}

func (fake *fakeServingExecution) Watch(ctx context.Context, id string, _ int64, _ func(contract.Event) error) (int64, error) {
	fake.mu.Lock()
	fake.watches++
	done, ok := fake.done[id]
	if !ok {
		fake.mu.Unlock()
		return 0, errors.New("execution not found")
	}
	if fake.autoComplete {
		fake.completeLocked(id)
	}
	fake.mu.Unlock()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-done:
		return 1, nil
	}
}

func (fake *fakeServingExecution) Cancel(_ context.Context, id string) (contract.Execution, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, ok := fake.executions[id]
	if !ok {
		return contract.Execution{}, errors.New("execution not found")
	}
	if !contract.IsTerminal(value.Status) {
		value.Status = contract.StatusCancelled
		fake.executions[id] = value
		close(fake.done[id])
	}
	return value, nil
}

func (fake *fakeServingExecution) completeLocked(id string) {
	value := fake.executions[id]
	if contract.IsTerminal(value.Status) {
		return
	}
	value.Status = contract.StatusCompleted
	output := "served: " + value.Spec.Prompt
	var structured []byte
	if value.Spec.StructuredOutput != nil {
		output = `{"status":"ok"}`
		structured = []byte(output)
	} else if strings.Contains(value.Spec.Prompt, "Available caller functions (JSON)") && !strings.Contains(value.Spec.Prompt, "function_call_output") {
		output = `{"type":"function_call","name":"lookup_order","arguments":{"order_id":"123"}}`
	}
	value.Result = &contract.Result{
		Output: output, Structured: structured, Cost: 0.002, CostKnown: true,
		Usage: contract.Usage{InputTokens: 3, OutputTokens: 2},
	}
	fake.executions[id] = value
	close(fake.done[id])
}

func TestResponsesHTTPEnforcesStructuredOutput(t *testing.T) {
	execution := newFakeServingExecution(true)
	server, client, _, apiKey, _ := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"status": map[string]any{"type": "string", "const": "ok"}},
		"required":   []string{"status"}, "additionalProperties": false,
	}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "return status",
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "status_result", "description": "A successful status",
			"schema": schema, "strict": true,
		}},
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-structured-1"})
	if status != http.StatusOK {
		t.Fatalf("create structured Response %d: %s", status, payload)
	}
	created := decodeBody[app.ResponseObject](t, payload)
	if created.Text.Format.Type != "json_schema" || created.Text.Format.Name != "status_result" || created.Text.Format.Strict == nil || !*created.Text.Format.Strict {
		t.Fatalf("structured format was not retained: %#v", created.Text)
	}
	if len(created.Output) != 1 || created.Output[0].Content[0].Text != `{"status":"ok"}` {
		t.Fatalf("structured output was not returned: %#v", created.Output)
	}
	execution.mu.Lock()
	structured := execution.executions["exec-serving-1"].Spec.StructuredOutput
	execution.mu.Unlock()
	if structured == nil || structured.Name != "status_result" || !strings.Contains(string(structured.Schema), `"additionalProperties":false`) {
		t.Fatalf("Execution Spec omitted structured output: %#v", structured)
	}

	for name, textConfig := range map[string]any{
		"strict false":  map[string]any{"format": map[string]any{"type": "json_schema", "name": "status_result", "schema": schema, "strict": false}},
		"remote ref":    map[string]any{"format": map[string]any{"type": "json_schema", "name": "status_result", "schema": map[string]any{"$ref": "https://example.com/schema.json"}}},
		"unknown field": map[string]any{"format": map[string]any{"type": "text", "verbosity": "low"}},
	} {
		t.Run(name, func(t *testing.T) {
			status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
				"model": "research-public@production", "input": "return status", "text": textConfig,
			}, map[string]string{"Authorization": "Bearer " + apiKey})
			if status != http.StatusBadRequest {
				t.Fatalf("invalid structured format returned %d: %s", status, payload)
			}
		})
	}
}

func TestWebhookEndpointHTTPReturnsSecretOnceAndCreatesOutbox(t *testing.T) {
	secretDirectory := t.TempDir()
	secretStore, err := webhooksecret.NewFileStore(secretDirectory)
	if err != nil {
		t.Fatal(err)
	}
	server, client, deployment, apiKey, store := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	}, func(options *app.Options) { options.WebhookSecrets = secretStore })
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/api/webhook-endpoints", map[string]any{
		"tenantId": "tenant-serving", "name": "CRM", "url": "https://events.example.com/rank",
		"events": []string{domain.WebhookEventResponseCompleted}, "deploymentIds": []domain.DeploymentID{deployment.ID},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create Webhook endpoint %d: %s", status, payload)
	}
	created := decodeBody[app.CreatedWebhookEndpoint](t, payload)
	if !strings.HasPrefix(created.SigningSecret, "whsec_") {
		t.Fatalf("signing secret missing: %#v", created)
	}
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/webhook-endpoints?tenantId=tenant-serving", nil, nil)
	if status != http.StatusOK || strings.Contains(string(payload), "signingSecret") || strings.Contains(string(payload), "secretRef") || strings.Contains(string(payload), "whsec_") {
		t.Fatalf("list leaked signing material %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "webhook me",
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-webhook-http-1"})
	if status != http.StatusOK {
		t.Fatalf("create Response %d: %s", status, payload)
	}
	deliveries, err := store.ListWebhookDeliveries(context.Background(), created.ID)
	if err != nil || len(deliveries) != 1 || deliveries[0].EventType != domain.WebhookEventResponseCompleted {
		t.Fatalf("Webhook outbox: %v %#v", err, deliveries)
	}
	status, _ = requestJSON(t, client, http.MethodPost, server.URL+"/api/webhook-endpoints", map[string]any{
		"tenantId": "tenant-serving", "name": "unsafe", "url": "http://127.0.0.1/hook",
		"events": []string{domain.WebhookEventResponseCompleted}, "deploymentIds": []domain.DeploymentID{deployment.ID},
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("unsafe Webhook URL returned %d", status)
	}
}

func setupServingHTTP(t *testing.T, execution *fakeServingExecution, policy domain.DeploymentPolicy, configure ...func(*app.Options)) (*httptest.Server, *http.Client, domain.Deployment, string, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	options := app.Options{Execution: execution}
	for _, apply := range configure {
		apply(&options)
	}
	service := app.NewService(store, options)
	if err := service.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service))
	t.Cleanup(server.Close)
	client := server.Client()
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/api/deployments", map[string]any{
		"tenantId": "tenant-serving", "agentFamilyId": "agent-research-demo", "name": "research-public", "policy": policy,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create Deployment %d: %s", status, payload)
	}
	deployment := decodeBody[domain.Deployment](t, payload)
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/deployments/"+string(deployment.ID)+"/aliases/production/releases", map[string]any{
		"agentVersionId": "agent-research-demo-v1", "policyResult": domain.ReleasePolicyManualOverride,
		"actor": "e2e", "reason": "initial release",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("promote Deployment %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/serving-credentials", map[string]any{
		"tenantId": "tenant-serving", "name": "E2E", "deploymentIds": []domain.DeploymentID{deployment.ID},
		"scopes": []string{domain.ScopeResponsesCreate, domain.ScopeResponsesRead, domain.ScopeResponsesCancel, domain.ScopeResponsesApprove},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create credential %d: %s", status, payload)
	}
	credential := decodeBody[app.CreatedAPICredential](t, payload)
	return server, client, deployment, credential.APIKey, store
}

func TestResponsesHTTPFreezesImageAndFileInputs(t *testing.T) {
	execution := newFakeServingExecution(true)
	server, client, _, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	imageBytes := []byte("\x89PNG\r\n\x1a\nfixture")
	fileBytes := []byte("immutable document")
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production",
		"input": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "input_text", "text": "inspect both inputs"},
			{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes), "detail": "auto"},
			{"type": "input_file", "filename": "brief.txt", "file_data": base64.StdEncoding.EncodeToString(fileBytes)},
		}}},
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "rich-input-1"})
	if status != http.StatusOK {
		t.Fatalf("create rich-input Response %d: %s", status, payload)
	}
	created := decodeBody[app.ResponseObject](t, payload)
	execution.mu.Lock()
	attachments := append([]contract.InputAttachment(nil), execution.executions["exec-serving-1"].Spec.InputAttachments...)
	execution.mu.Unlock()
	if len(attachments) != 2 || string(attachments[0].Data) != string(imageBytes) || string(attachments[1].Data) != string(fileBytes) {
		t.Fatalf("Execution Spec did not receive frozen inputs: %#v", attachments)
	}
	if attachments[0].Type != "input_image" || attachments[0].MediaType != "image/png" || attachments[1].Name != "brief.txt" {
		t.Fatalf("Execution Spec lost input metadata: %#v", attachments)
	}
	persisted, err := store.GetResponse(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.InputItems) != 3 || string(persisted.InputItems[2].Data) != string(fileBytes) {
		t.Fatalf("Response did not persist frozen inputs: %#v", persisted.InputItems)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "previous_response_id": created.ID,
		"input": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "input_text", "text": "compare with this"},
			{"type": "input_file", "filename": "second.txt", "file_data": base64.StdEncoding.EncodeToString([]byte("second document"))},
		}}},
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "rich-input-2"})
	if status != http.StatusOK {
		t.Fatalf("continue rich-input Response %d: %s", status, payload)
	}
	execution.mu.Lock()
	secondSpec := execution.executions["exec-serving-2"].Spec
	execution.mu.Unlock()
	if len(secondSpec.InputAttachments) != 3 {
		t.Fatalf("continuation received %d attachments, want prior and current inputs", len(secondSpec.InputAttachments))
	}
	for _, fragment := range []string{"[input_image: image-2.png, sha256:", "[input_file: brief.txt, sha256:", "compare with this"} {
		if !strings.Contains(secondSpec.Prompt, fragment) {
			t.Fatalf("continuation prompt omitted %q: %s", fragment, secondSpec.Prompt)
		}
	}
}

func TestResponsesHTTPCallerFunctionRoundTrip(t *testing.T) {
	execution := newFakeServingExecution(true)
	server, client, _, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000, AllowCallerTools: true,
	})
	tool := map[string]any{
		"type": "function", "name": "lookup_order", "description": "Look up one order", "strict": true,
		"require_approval": true,
		"parameters": map[string]any{
			"type": "object", "properties": map[string]any{"order_id": map[string]any{"type": "string"}},
			"required": []string{"order_id"}, "additionalProperties": false,
		},
	}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "look up order 123", "tools": []any{tool},
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "function-call-1"})
	if status != http.StatusOK {
		t.Fatalf("create function-call Response %d: %s", status, payload)
	}
	called := decodeBody[app.ResponseObject](t, payload)
	if called.Status != domain.ResponseRequiresAction || len(called.Output) != 1 || called.Output[0].Type != "function_call" || called.Output[0].Name != "lookup_order" || called.Output[0].CallID == "" || called.Output[0].Arguments != `{"order_id":"123"}` || !called.Output[0].ApprovalRequired || called.Output[0].ApprovalStatus != "pending" {
		t.Fatalf("unexpected function call: %#v", called.Output)
	}
	stored, err := store.GetResponse(context.Background(), called.ID)
	if err != nil || stored.ToolCall == nil || len(stored.CallerTools) != 1 {
		t.Fatalf("function call was not durable: response=%#v err=%v", stored, err)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses/"+string(called.ID)+"/approval", map[string]any{
		"approved": true, "reason": "operator verified read-only lookup",
	}, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusOK {
		t.Fatalf("approve function call %d: %s", status, payload)
	}
	approved := decodeBody[app.ResponseObject](t, payload)
	if approved.Status != domain.ResponseCompleted || approved.Output[0].ApprovalStatus != "approved" {
		t.Fatalf("approval was not retained: %#v", approved)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "previous_response_id": called.ID,
		"input": []map[string]any{{"type": "function_call_output", "call_id": called.Output[0].CallID, "output": `{"status":"shipped"}`}},
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "function-call-2"})
	if status != http.StatusOK {
		t.Fatalf("continue with function output %d: %s", status, payload)
	}
	continued := decodeBody[app.ResponseObject](t, payload)
	if len(continued.Output) != 1 || continued.Output[0].Type != "message" || !strings.Contains(continued.Output[0].Content[0].Text, "function_call_output") || len(continued.Tools) != 1 {
		t.Fatalf("function output did not reach continued Agent: %#v", continued)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "previous_response_id": continued.ID,
		"input": []map[string]any{{"type": "function_call_output", "call_id": "call_wrong", "output": "bad"}},
	}, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected function output returned %d: %s", status, payload)
	}
}

func TestResponsesHTTPFreezesRemoteInputOnceWithoutPersistingURL(t *testing.T) {
	inputFetcher := &fakeInputFetcher{data: []byte("remote-image"), mime: "image/png"}
	execution := newFakeServingExecution(true)
	server, client, _, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	}, func(options *app.Options) { options.InputFetcher = inputFetcher })
	headers := map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "remote-input-1"}
	requestBody := map[string]any{
		"model": "research-public@production",
		"input": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "input_image", "image_url": "https://cdn.example.test/signed/input.png?token=secret"},
		}}},
	}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", requestBody, headers)
	if status != http.StatusOK {
		t.Fatalf("create remote-input Response %d: %s", status, payload)
	}
	created := decodeBody[app.ResponseObject](t, payload)
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", requestBody, headers)
	if status != http.StatusOK || decodeBody[app.ResponseObject](t, payload).ID != created.ID || inputFetcher.calls != 1 {
		t.Fatalf("idempotent replay fetched remote input again: status=%d calls=%d body=%s", status, inputFetcher.calls, payload)
	}
	stored, err := store.GetResponse(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.InputItems) != 1 || stored.InputItems[0].SourceDigest == "" || stored.InputItems[0].ContentDigest == "" {
		t.Fatalf("stored remote input omitted digests: %#v", stored.InputItems)
	}
	if strings.Contains(fmt.Sprintf("%#v", stored.InputItems), "token=secret") {
		t.Fatalf("stored remote input retained the signed URL: %#v", stored.InputItems)
	}
	if inputFetcher.url != "https://cdn.example.test/signed/input.png?token=secret" {
		t.Fatalf("fetcher received %q", inputFetcher.url)
	}
}

func TestResponsesHTTPRejectsUnsupportedRichInputs(t *testing.T) {
	server, client, _, apiKey, _ := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	for name, item := range map[string]map[string]any{
		"file id":      {"type": "input_file", "file_id": "file_123"},
		"image detail": {"type": "input_image", "image_url": "data:image/png;base64,aW1hZ2U=", "detail": "high"},
		"plain http":   {"type": "input_image", "image_url": "http://example.test/input.png"},
		"private host": {"type": "input_image", "image_url": "https://127.0.0.1/input.png"},
		"invalid data": {"type": "input_file", "filename": "bad.txt", "file_data": "%%%"},
	} {
		t.Run(name, func(t *testing.T) {
			status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
				"model": "research-public@production",
				"input": []map[string]any{{"role": "user", "content": []map[string]any{item}}},
			}, map[string]string{"Authorization": "Bearer " + apiKey})
			if status != http.StatusBadRequest {
				t.Fatalf("unsupported rich input returned %d: %s", status, payload)
			}
		})
	}
}

func TestResponsesCompatibleHTTPPinsVersionAndStreamsDurableEvents(t *testing.T) {
	execution := newFakeServingExecution(true)
	server, client, _, apiKey, _ := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	headers := map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-e2e-1"}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "hello", "metadata": map[string]string{"case": "sync"},
	}, headers)
	if status != http.StatusOK {
		t.Fatalf("create Response %d: %s", status, payload)
	}
	created := decodeBody[app.ResponseObject](t, payload)
	if created.Status != domain.ResponseCompleted || created.Model != "research-public@production" || len(created.Output) != 1 || created.Output[0].Content[0].Text != "served: hello" {
		t.Fatalf("unexpected Response: %#v", created)
	}
	if created.Deployment.ReleaseRevisionID == "" || created.Deployment.AgentVersionID != "agent-research-demo-v1" {
		t.Fatalf("Response did not pin release and Agent version: %#v", created.Deployment)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "hello", "metadata": map[string]string{"case": "sync"},
	}, headers)
	if status != http.StatusOK || decodeBody[app.ResponseObject](t, payload).ID != created.ID || execution.next != 1 {
		t.Fatalf("idempotent replay created another execution: %d %s", status, payload)
	}
	status, _ = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "different",
	}, headers)
	if status != http.StatusConflict {
		t.Fatalf("idempotency conflict returned %d", status)
	}
	status, _ = requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/"+string(created.ID), nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated retrieval returned %d", status)
	}
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/"+string(created.ID), nil, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusOK || decodeBody[app.ResponseObject](t, payload).Status != domain.ResponseCompleted {
		t.Fatalf("retrieve Response %d: %s", status, payload)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"research-public@v1","input":[{"role":"user","content":[{"type":"input_text","text":"stream me"}]}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Idempotency-Key", "response-stream-1")
	stream, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	streamPayload, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(streamPayload)
	if strings.Contains(text, "retry:") {
		t.Fatalf("stream emitted a control-only frame that OpenAI SDKs parse as JSON: %s", text)
	}
	for _, eventType := range []string{"response.created", "response.in_progress", "response.output_text.delta", "response.completed"} {
		if !strings.Contains(text, "event: "+eventType) {
			t.Fatalf("stream omitted %s: %s", eventType, text)
		}
	}
	if strings.Contains(text, `release_revision_id`) {
		t.Fatalf("exact-version Response invented a release revision: %s", text)
	}
}

func TestResponsesHTTPContinuesConversationWithoutVersionDrift(t *testing.T) {
	execution := newFakeServingExecution(true)
	server, client, deployment, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	headers := func(idempotencyKey string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": idempotencyKey}
	}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "remember alpha",
	}, headers("conversation-first"))
	if status != http.StatusOK {
		t.Fatalf("create first Response %d: %s", status, payload)
	}
	first := decodeBody[app.ResponseObject](t, payload)
	if first.Conversation.ID == "" || first.PreviousResponseID != nil {
		t.Fatalf("first Response omitted Conversation identity: %#v", first)
	}

	v1, err := store.GetAgentVersion(context.Background(), "agent-research-demo-v1")
	if err != nil {
		t.Fatal(err)
	}
	v2 := v1
	v2.ID, v2.Version, v2.Description, v2.CreatedAt = "agent-research-demo-v2", 2, "v2", time.Now().UTC()
	if err := store.CreateAgentVersion(context.Background(), v2); err != nil {
		t.Fatal(err)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/deployments/"+string(deployment.ID)+"/aliases/production/releases", map[string]any{
		"agentVersionId": v2.ID, "expectedReleaseRevisionId": first.Deployment.ReleaseRevisionID,
		"policyResult": domain.ReleasePolicyManualOverride, "actor": "e2e", "reason": "advance production",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("advance production %d: %s", status, payload)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "what did I ask you to remember?",
		"previous_response_id": first.ID,
	}, headers("conversation-second"))
	if status != http.StatusOK {
		t.Fatalf("continue Response %d: %s", status, payload)
	}
	second := decodeBody[app.ResponseObject](t, payload)
	if second.Conversation.ID != first.Conversation.ID || second.Deployment.AgentVersionID != "agent-research-demo-v1" || second.Deployment.ResolutionKind != domain.ResolutionConversation {
		t.Fatalf("continuation drifted from pinned Conversation: %#v", second)
	}
	if second.PreviousResponseID != string(first.ID) || second.Deployment.ReleaseRevisionID != first.Deployment.ReleaseRevisionID {
		t.Fatalf("continuation lost predecessor or release lineage: %#v", second)
	}
	execution.mu.Lock()
	prompt := execution.executions["exec-serving-2"].Spec.Prompt
	execution.mu.Unlock()
	for _, fragment := range []string{`"role":"user","content":"remember alpha"`, `"role":"assistant"`, `"content":"what did I ask you to remember?"`} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("conversation prompt omitted %q: %s", fragment, prompt)
		}
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "what did I ask you to remember?",
		"previous_response_id": first.ID,
	}, headers("conversation-second"))
	if status != http.StatusOK || decodeBody[app.ResponseObject](t, payload).ID != second.ID || execution.next != 2 {
		t.Fatalf("continuation idempotency replay created another execution: %d %s", status, payload)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "fork stale head", "previous_response_id": first.ID,
	}, headers("conversation-stale"))
	if status != http.StatusConflict || !strings.Contains(string(payload), `"code":"conversation_head_changed"`) {
		t.Fatalf("stale continuation returned %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@v2", "input": "change version", "previous_response_id": second.ID,
	}, headers("conversation-version-drift"))
	if status != http.StatusBadRequest || !strings.Contains(string(payload), `"code":"conversation_version_mismatch"`) {
		t.Fatalf("version-changing continuation returned %d: %s", status, payload)
	}

	type concurrentResult struct {
		status  int
		payload []byte
		err     error
	}
	start := make(chan struct{})
	results := make(chan concurrentResult, 2)
	for range 2 {
		go func() {
			<-start
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(fmt.Sprintf(
				`{"model":"research-public@production","input":"retry together","previous_response_id":%q}`, second.ID,
			)))
			if err != nil {
				results <- concurrentResult{err: err}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+apiKey)
			request.Header.Set("Idempotency-Key", "conversation-concurrent-retry")
			response, err := client.Do(request)
			if err != nil {
				results <- concurrentResult{err: err}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			results <- concurrentResult{status: response.StatusCode, payload: body, err: err}
		}()
	}
	close(start)
	var concurrentID domain.ResponseID
	for range 2 {
		result := <-results
		if result.err != nil || result.status != http.StatusOK {
			t.Fatalf("concurrent idempotent continuation failed: status=%d body=%s err=%v", result.status, result.payload, result.err)
		}
		current := decodeBody[app.ResponseObject](t, result.payload)
		if concurrentID != "" && current.ID != concurrentID {
			t.Fatalf("concurrent idempotent continuation returned %s and %s", concurrentID, current.ID)
		}
		concurrentID = current.ID
	}
	execution.mu.Lock()
	executionCount := execution.next
	execution.mu.Unlock()
	if executionCount != 3 {
		t.Fatalf("concurrent idempotent continuation created %d executions, want 3 total", executionCount)
	}
}

func TestResponsesHTTPEnforcesConcurrencyAndCancels(t *testing.T) {
	execution := newFakeServingExecution(false)
	server, client, _, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 1, ExecutionTimeoutMs: 30_000,
	})
	headers := map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-blocked-1"}
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "wait", "background": true,
	}, headers)
	if status != http.StatusOK {
		t.Fatalf("create background Response %d: %s", status, payload)
	}
	first := decodeBody[app.ResponseObject](t, payload)
	headers["Idempotency-Key"] = "response-blocked-2"
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "second", "background": true,
	}, headers)
	if status != http.StatusTooManyRequests {
		t.Fatalf("concurrency limit returned %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses/"+string(first.ID)+"/cancel", map[string]any{}, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusOK || decodeBody[app.ResponseObject](t, payload).Status != domain.ResponseCancelled {
		t.Fatalf("cancel Response %d: %s", status, payload)
	}
	events, err := store.ListResponseEvents(context.Background(), first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Type != "response.cancelled" {
		t.Fatalf("cancellation emitted %s", events[len(events)-1].Type)
	}
}

func TestResponsesHTTPRejectsUnsupportedFields(t *testing.T) {
	server, client, _, apiKey, _ := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "hello", "temperature": 0.2,
	}, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production",
		"input": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "hello", "image_url": "forbidden"}}}},
	}, map[string]string{"Authorization": "Bearer " + apiKey})
	if status != http.StatusBadRequest {
		t.Fatalf("nested unsupported field returned %d: %s", status, payload)
	}
}

func TestResponsesHTTPEnforcesRateAndScopes(t *testing.T) {
	server, client, deployment, apiKey, _ := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{
		RequestsPerMinute: 1, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "first",
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-rate-1"})
	if status != http.StatusOK {
		t.Fatalf("first rate-limited Response %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "second",
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-rate-2"})
	if status != http.StatusTooManyRequests || !strings.Contains(string(payload), `"code":"rate_limit_exceeded"`) {
		t.Fatalf("rate limit returned %d: %s", status, payload)
	}

	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/serving-credentials", map[string]any{
		"tenantId": "tenant-serving", "name": "create-only", "deploymentIds": []domain.DeploymentID{deployment.ID},
		"scopes": []string{domain.ScopeResponsesCreate},
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create scoped credential %d: %s", status, payload)
	}
	createOnly := decodeBody[app.CreatedAPICredential](t, payload)
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/resp_missing", nil, map[string]string{
		"Authorization": "Bearer " + createOnly.APIKey,
	})
	if status != http.StatusForbidden || !strings.Contains(string(payload), `"code":"insufficient_scope"`) {
		t.Fatalf("missing read scope returned %d: %s", status, payload)
	}
}

func TestResponsesResumeAfterServiceRestart(t *testing.T) {
	execution := newFakeServingExecution(false)
	server, client, _, apiKey, store := setupServingHTTP(t, execution, domain.DeploymentPolicy{
		RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30_000,
	})
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{
		"model": "research-public@production", "input": "resume me", "background": true,
	}, map[string]string{"Authorization": "Bearer " + apiKey, "Idempotency-Key": "response-restart-1"})
	if status != http.StatusOK {
		t.Fatalf("create resumable Response %d: %s", status, payload)
	}
	created := decodeBody[app.ResponseObject](t, payload)
	waitFor(t, func() bool {
		execution.mu.Lock()
		defer execution.mu.Unlock()
		return execution.watches == 1
	})
	execution.mu.Lock()
	execution.autoComplete = true
	execution.mu.Unlock()

	restarted := app.NewService(store, app.Options{Execution: execution})
	if err := restarted.ResumeActiveResponses(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		response, err := store.GetResponse(context.Background(), created.ID)
		if err != nil || response.Status != domain.ResponseCompleted {
			return false
		}
		execution.mu.Lock()
		defer execution.mu.Unlock()
		return execution.watches >= 2
	})
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
