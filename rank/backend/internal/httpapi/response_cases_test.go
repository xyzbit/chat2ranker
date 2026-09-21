package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func TestResponseSearchAndReviewedDatasetCapture(t *testing.T) {
	server, client, _, key, store := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{"model": "research-public", "input": "private@example.com original input", "instructions": "one sentence"}, map[string]string{"Authorization": "Bearer " + key})
	if status != 200 {
		t.Fatalf("create: %d %s", status, payload)
	}
	response := decodeBody[app.ResponseObject](t, payload)
	detailURL := server.URL + "/api/responses/" + string(response.ID) + "?tenantId=tenant-serving"
	status, payload = requestJSON(t, client, http.MethodGet, detailURL, nil, nil)
	detail := decodeBody[app.OperatorResponseDetail](t, payload)
	if status != 200 || detail.Instructions != "one sentence" {
		t.Fatalf("detail: %s", payload)
	}
	for _, filter := range []string{"query=" + string(response.ID), "conversationId=" + string(detail.Response.ConversationID)} {
		status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/responses?tenantId=tenant-serving&"+filter, nil, nil)
		page := decodeBody[domain.ResponsePage](t, payload)
		if status != 200 || len(page.Items) != 1 || page.Items[0].ID != response.ID {
			t.Fatalf("search %s: %s", filter, payload)
		}
	}
	path := server.URL + "/api/responses/" + string(response.ID) + "/dataset-cases"
	input := map[string]any{"tenantId": "tenant-serving", "datasetName": "Support cases", "title": "Reviewed case", "input": "[email removed] input", "expected": "Expected useful answer", "reviewed": false}
	status, _ = requestJSON(t, client, http.MethodPost, path, input, nil)
	if status != 400 {
		t.Fatalf("unreviewed: %d", status)
	}
	input["reviewed"] = true
	input["tenantId"] = "other"
	status, _ = requestJSON(t, client, http.MethodPost, path, input, nil)
	if status != 404 {
		t.Fatalf("cross tenant: %d", status)
	}
	input["tenantId"] = "tenant-serving"
	status, payload = requestJSON(t, client, http.MethodPost, path, input, nil)
	first := decodeBody[domain.DatasetVersion](t, payload)
	if status != 201 || len(first.Cases) != 1 || first.Cases[0].Source.ResponseID != response.ID || strings.Contains(string(payload), "private@example.com") {
		t.Fatalf("capture: %d %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/v1/responses", map[string]any{"model": "research-public", "input": "another request"}, map[string]string{"Authorization": "Bearer " + key})
	second := decodeBody[app.ResponseObject](t, payload)
	delete(input, "datasetName")
	input["baseDatasetVersionId"] = first.ID
	secondPath := server.URL + "/api/responses/" + string(second.ID) + "/dataset-cases"
	status, payload = requestJSON(t, client, http.MethodPost, secondPath, input, nil)
	next := decodeBody[domain.DatasetVersion](t, payload)
	if status != 201 || next.Version != 2 || len(next.Cases) != 2 || next.Evaluator.ID != first.Evaluator.ID {
		t.Fatalf("append: %d %s", status, payload)
	}
	old, err := store.GetDatasetVersion(context.Background(), first.ID)
	if err != nil || len(old.Cases) != 1 {
		t.Fatalf("mutated original: %v", err)
	}
	status, _ = requestJSON(t, client, http.MethodPost, secondPath, input, nil)
	if status != 409 {
		t.Fatalf("stale: %d", status)
	}
	input["baseDatasetVersionId"] = next.ID
	status, payload = requestJSON(t, client, http.MethodPost, secondPath, input, nil)
	if status != 409 || !strings.Contains(string(payload), "response_already_captured") {
		t.Fatalf("duplicate: %d %s", status, payload)
	}
	input["expected"] = ""
	status, _ = requestJSON(t, client, http.MethodPost, secondPath, input, nil)
	if status != 400 {
		t.Fatalf("empty expectation: %d", status)
	}
}
