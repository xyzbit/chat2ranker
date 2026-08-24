package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/httpapi"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
)

func requestJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func decodeBody[T any](t *testing.T, payload []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	return value
}

func TestHTTPContractRunsAgainstSQLiteRankd(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, app.Options{Workers: true, WorkerLatency: 10 * time.Millisecond})
	if err := service.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service))
	defer server.Close()
	client := server.Client()
	status, payload := requestJSON(t, client, http.MethodGet, server.URL+"/api/health", nil, nil)
	if status != 200 {
		t.Fatalf("health %d: %s", status, payload)
	}
	health := decodeBody[map[string]any](t, payload)
	if health["runtime"] != "rankd" || health["storage"] != "sqlite" {
		t.Fatalf("unexpected health: %#v", health)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments", map[string]any{"title": "HTTP contract"}, nil)
	if status != 201 {
		t.Fatalf("create experiment %d: %s", status, payload)
	}
	experiment := decodeBody[domain.ExperimentView](t, payload)
	datasetID, agentID := "dataset-web-research-v3", "agent-research-demo-v1"
	status, payload = requestJSON(t, client, http.MethodPatch, server.URL+"/api/experiments/"+experiment.ID, map[string]any{"datasetVersionId": datasetID}, nil)
	if status != 200 {
		t.Fatalf("select dataset %d: %s", status, payload)
	}
	status, payload = requestJSON(t, client, http.MethodPatch, server.URL+"/api/experiments/"+experiment.ID, map[string]any{"agentVersionId": agentID}, nil)
	if status != 200 {
		t.Fatalf("select agent %d: %s", status, payload)
	}
	headers := map[string]string{"Idempotency-Key": "http-start-1"}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments/"+experiment.ID+"/runs", map[string]any{"idempotencyKey": "http-start-1"}, headers)
	if status != 202 {
		t.Fatalf("start run %d: %s", status, payload)
	}
	first := decodeBody[domain.Run](t, payload)
	if !bytes.Contains(payload, []byte(`"results":[]`)) || !bytes.Contains(payload, []byte(`"events":[`)) {
		t.Fatalf("active Run must expose array fields, got %s", payload)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments/"+experiment.ID+"/runs", map[string]any{"idempotencyKey": "http-start-1"}, headers)
	if status != 202 {
		t.Fatalf("replay run %d: %s", status, payload)
	}
	replayed := decodeBody[domain.Run](t, payload)
	if replayed.ID != first.ID {
		t.Fatalf("idempotent HTTP request created %s and %s", first.ID, replayed.ID)
	}
	deadline := time.Now().Add(5 * time.Second)
	var completed domain.Run
	for time.Now().Before(deadline) {
		status, payload = requestJSON(t, client, http.MethodGet, server.URL+"/api/runs/"+first.ID, nil, nil)
		if status != 200 {
			t.Fatalf("get run %d: %s", status, payload)
		}
		completed = decodeBody[domain.Run](t, payload)
		if completed.Status == domain.RunComplete {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completed.Status != domain.RunComplete || completed.Passed != 50 || completed.Total != 60 || completed.ReliableCases != 10 || completed.CaseCount != 12 {
		t.Fatalf("unexpected completed run: %#v", completed)
	}
}

func TestA2UICommandAuthenticationAndControlHostBoundary(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, app.Options{ActionSecret: "action-test-secret"})
	if err := service.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, httpapi.Options{ControlToken: "control-test-token"}))
	defer server.Close()
	client := server.Client()
	status, payload := requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments", map[string]any{"title": "A2UI"}, nil)
	if status != 201 {
		t.Fatalf("create experiment %d: %s", status, payload)
	}
	experiment := decodeBody[domain.ExperimentView](t, payload)
	action := experiment.A2UI.Actions[app.ControlSelectDataset]
	commandBody := map[string]any{
		"type": action.Command, "actionToken": action.Token,
		"payload": map[string]any{"datasetVersionId": "dataset-web-research-v3"},
	}
	headers := map[string]string{"Idempotency-Key": "a2ui-select-1", "X-Rank-Action-Token": action.Token}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments/"+experiment.ID+"/commands", commandBody, headers)
	if status != 200 {
		t.Fatalf("execute A2UI command %d: %s", status, payload)
	}
	var commandResponse struct {
		Experiment domain.ExperimentView `json:"experiment"`
		Command    domain.ControlCommand `json:"command"`
	}
	commandResponse = decodeBody[struct {
		Experiment domain.ExperimentView `json:"experiment"`
		Command    domain.ControlCommand `json:"command"`
	}](t, payload)
	if commandResponse.Experiment.DatasetVersionID != "dataset-web-research-v3" || len(commandResponse.Experiment.ControlEvents) != 1 {
		t.Fatalf("unexpected A2UI projection: %#v", commandResponse.Experiment)
	}
	status, payload = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments/"+experiment.ID+"/commands", commandBody, headers)
	if status != 200 {
		t.Fatalf("replay A2UI command %d: %s", status, payload)
	}
	replayed := decodeBody[struct {
		Command domain.ControlCommand `json:"command"`
	}](t, payload)
	if replayed.Command.ID != commandResponse.Command.ID {
		t.Fatalf("A2UI replay created %s and %s", commandResponse.Command.ID, replayed.Command.ID)
	}
	headers["X-Rank-Action-Token"] = "invalid"
	status, _ = requestJSON(t, client, http.MethodPost, server.URL+"/api/experiments/"+experiment.ID+"/commands", commandBody, headers)
	if status != 403 {
		t.Fatalf("invalid action token returned %d", status)
	}
	status, _ = requestJSON(t, client, http.MethodPost, server.URL+"/api/internal/control/commands", map[string]any{}, nil)
	if status != 401 {
		t.Fatalf("unauthenticated Control Host returned %d", status)
	}
}
