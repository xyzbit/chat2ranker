package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

func TestOperatorReleaseHistoryPagesOneAlias(t *testing.T) {
	server, client, deployment, _, store := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	ctx := context.Background()
	alias, err := store.GetDeploymentAlias(ctx, deployment.ID, "production")
	if err != nil {
		t.Fatal(err)
	}
	head := alias.CurrentReleaseRevisionID
	for i := 0; i < 31; i++ {
		revision, err := store.AppendReleaseRevision(ctx, domain.ReleaseRevision{ID: domain.ReleaseRevisionID(fmt.Sprintf("history-%d", i)), DeploymentAliasID: alias.ID, AgentVersionID: alias.CurrentRelease.AgentVersionID, PolicyResult: domain.ReleasePolicyManualOverride, Actor: "operator", Reason: "history fixture", CreatedAt: time.Now().UTC()}, head)
		if err != nil {
			t.Fatal(err)
		}
		head = revision.ID
	}
	base := server.URL + "/api/deployments/" + string(deployment.ID) + "/aliases/production/releases"
	status, payload := requestJSON(t, client, http.MethodGet, base, nil, nil)
	page := decodeBody[domain.ReleaseHistoryPage](t, payload)
	if status != 200 || len(page.Items) != 30 || page.Items[0].ID != head || page.NextCursor != 3 || page.Items[0].PreviousAgentVersionID != alias.CurrentRelease.AgentVersionID {
		t.Fatalf("history: %d %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodGet, base+"?before=3", nil, nil)
	page = decodeBody[domain.ReleaseHistoryPage](t, payload)
	if status != 200 || len(page.Items) != 2 || page.NextCursor != 0 || page.Items[1].Revision != 1 {
		t.Fatalf("older history: %d %s", status, payload)
	}
	for _, value := range []string{"0", "-1", "nan"} {
		status, _ = requestJSON(t, client, http.MethodGet, base+"?before="+value, nil, nil)
		if status != 400 {
			t.Fatalf("accepted invalid cursor %q: %d", value, status)
		}
	}
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/deployments/"+string(deployment.ID)+"/aliases/staging/releases", nil, nil)
	if status != 200 || len(decodeBody[domain.ReleaseHistoryPage](t, payload).Items) != 0 {
		t.Fatalf("cross-alias history: %s", payload)
	}
	status, _ = requestJSON(t, client, http.MethodGet, server.URL+"/api/deployments/missing/aliases/production/releases", nil, nil)
	if status != 404 {
		t.Fatalf("unknown deployment accepted: %d", status)
	}
}

func TestOperatorHistoryAndRevocation(t *testing.T) {
	server, client, deployment, key, _ := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	headers := map[string]string{"Authorization": "Bearer " + key}
	var ids []domain.ResponseID
	for range 3 {
		status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{"model": "research-public@production", "input": "operator history fixture"}, headers)
		if status != 200 {
			t.Fatalf("create: %d %s", status, payload)
		}
		ids = append(ids, decodeBody[app.ResponseObject](t, payload).ID)
	}
	base := server.URL + "/api/responses?tenantId=tenant-serving&limit=2&deploymentId=" + string(deployment.ID)
	status, payload := requestJSON(t, client, http.MethodGet, base, nil, nil)
	if status != 200 {
		t.Fatalf("list: %d %s", status, payload)
	}
	page := decodeBody[domain.ResponsePage](t, payload)
	if len(page.Items) != 2 || page.Items[0].ID != ids[2] || page.NextCursor != ids[1] {
		t.Fatalf("page: %#v", page)
	}
	if strings.Contains(string(payload), "operator history fixture") || strings.Contains(string(payload), key) {
		t.Fatal("list leaked input or credential")
	}
	status, payload = requestJSON(t, client, http.MethodGet, base+"&before="+string(page.NextCursor), nil, nil)
	second := decodeBody[domain.ResponsePage](t, payload)
	if status != 200 || len(second.Items) != 1 || second.Items[0].ID != ids[0] || second.NextCursor != "" {
		t.Fatalf("second page: %s", payload)
	}
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/responses/"+string(ids[0])+"?tenantId=tenant-serving", nil, nil)
	detail := decodeBody[app.OperatorResponseDetail](t, payload)
	if status != 200 || detail.Input != "operator history fixture" || len(detail.Events) == 0 {
		t.Fatalf("detail: %s", payload)
	}
	for _, path := range []string{"/api/responses?limit=-1", "/api/responses?limit=oops", "/api/responses?status=unknown"} {
		status, _ := requestJSON(t, client, http.MethodGet, server.URL+path, nil, nil)
		if status != 400 {
			t.Fatalf("invalid filter accepted: %s %d", path, status)
		}
	}
	for _, path := range []string{"/api/responses/" + string(ids[0]) + "?tenantId=other", "/api/responses?tenantId=other&before=" + string(ids[0])} {
		status, _ := requestJSON(t, client, http.MethodGet, server.URL+path, nil, nil)
		if status != 404 {
			t.Fatalf("cross tenant: %s %d", path, status)
		}
	}
	status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/serving-credentials?tenantId=tenant-serving", nil, nil)
	credentials := decodeBody[[]domain.APICredential](t, payload)
	if status != 200 || len(credentials) != 1 || strings.Contains(string(payload), key) || strings.Contains(string(payload), "secretHash") {
		t.Fatalf("credentials: %s", payload)
	}
	revoke := server.URL + "/api/serving-credentials/" + string(credentials[0].ID) + "/revoke?tenantId=tenant-serving"
	for range 2 {
		status, payload = requestJSON(t, client, http.MethodPost, revoke, map[string]any{}, nil)
		if status != 200 {
			t.Fatalf("revoke: %d %s", status, payload)
		}
	}
	status, _ = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{"model": "research-public@production", "input": "denied"}, headers)
	if status != 401 {
		t.Fatalf("revoked credential accepted: %d", status)
	}
}

func TestOperatorReplaysOnlyFailedEnabledWebhook(t *testing.T) {
	server, client, deployment, key, store := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	_, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{"model": "research-public@production", "input": "fixture"}, map[string]string{"Authorization": "Bearer " + key})
	response := decodeBody[app.ResponseObject](t, payload)
	now := time.Now().UTC()
	ctx := context.Background()
	endpoint := domain.WebhookEndpoint{ID: "endpoint-operator", TenantID: "tenant-serving", Name: "operator", URL: "https://events.example.com/hook", SecretRef: "fixture", Enabled: true, DeploymentIDs: []domain.DeploymentID{deployment.ID}, Events: []string{"response.completed"}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWebhookEndpoint(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
	delivery := domain.WebhookDelivery{ID: "delivery-operator", EndpointID: endpoint.ID, ResponseID: response.ID, EventType: "response.completed", Payload: []byte(`{"id":"stable-event"}`), Status: domain.WebhookDeliveryFailed, Attempts: 10, NextAttemptAt: now, CreatedAt: now}
	if err := store.CreateWebhookDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	path := server.URL + "/api/webhook-endpoints/" + string(endpoint.ID) + "/deliveries/" + string(delivery.ID) + "/retry"
	status, payload := requestJSON(t, client, http.MethodPost, path, map[string]any{}, nil)
	if status != 202 {
		t.Fatalf("retry: %d %s", status, payload)
	}
	rows, err := store.ListWebhookDeliveries(ctx, endpoint.ID)
	if err != nil || len(rows) != 1 || rows[0].ID != delivery.ID || rows[0].Status != domain.WebhookDeliveryPending || rows[0].Attempts != 0 || string(rows[0].Payload) != string(delivery.Payload) {
		t.Fatalf("replay lost event: %#v %v", rows, err)
	}
	status, _ = requestJSON(t, client, http.MethodPost, path, map[string]any{}, nil)
	if status != 409 {
		t.Fatalf("pending replay accepted: %d", status)
	}
	if err := store.SetWebhookEndpointEnabled(ctx, endpoint.ID, false, now); err != nil {
		t.Fatal(err)
	}
	status, _ = requestJSON(t, client, http.MethodPost, path, map[string]any{}, nil)
	if status != 409 {
		t.Fatalf("disabled replay accepted: %d", status)
	}
}
