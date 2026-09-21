package httpapi_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/access"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/httpapi"
	"github.com/xyzbit/agentkiln/rank/backend/internal/testdb"
)

func TestManagementAuthenticationAndControlServiceScope(t *testing.T) {
	token := strings.Repeat("a", 64)
	hash := sha256.Sum256([]byte(token))
	path := filepath.Join(t.TempDir(), "access.json")
	body, _ := json.Marshal(map[string]any{"version": 1, "operators": []access.Entry{{Subject: "alice", TokenSHA256: hex.EncodeToString(hash[:])}}})
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := access.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := testdb.Open(t, filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, app.Options{})
	defer service.StopServingResponses()
	if err := service.EnsureSeed(t.Context()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, httpapi.Options{AccessFile: file, ControlToken: "private-control-test"}))
	defer server.Close()
	client := server.Client()
	operator := map[string]string{"Authorization": "Bearer " + token}
	control := map[string]string{"X-Rank-Control-Token": "private-control-test"}
	for _, test := range []struct {
		method, path string
		body         any
		headers      map[string]string
		status       int
	}{
		{"GET", "/api/health", nil, nil, 200},
		{"GET", "/api/bootstrap", nil, nil, 401},
		{"POST", "/api/agents", map[string]any{}, nil, 401},
		{"GET", "/api/identity", nil, operator, 200},
		{"GET", "/api/identity", nil, map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+token))}, 200},
		{"GET", "/api/identity", nil, map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:"+token))}, 401},
		{"GET", "/api/bootstrap", nil, control, 200},
		{"GET", "/api/identity", nil, control, 403},
		{"GET", "/api/serving-credentials", nil, control, 403},
		{"POST", "/api/serving-credentials", map[string]any{}, control, 403},
		{"DELETE", "/api/model-connections/any", nil, control, 403},
		{"POST", "/api/model-connections", map[string]any{"apiKey": "secret-from-model"}, control, 403},
		{"GET", "/api/bootstrap", nil, map[string]string{"X-Rank-Control-Token": "wrong"}, 401},
		{"POST", "/v1/responses", map[string]any{}, operator, 401},
		{"POST", "/api/experiments", map[string]any{"title": "same origin"}, map[string]string{"Authorization": "Bearer " + token, "Origin": server.URL}, 201},
		{"POST", "/api/experiments", map[string]any{"title": "wrong scheme"}, map[string]string{"Authorization": "Bearer " + token, "Origin": strings.Replace(server.URL, "http://", "https://", 1)}, 403},
		{"POST", "/api/experiments", map[string]any{"title": "cross-site Basic"}, map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+token)), "Origin": server.URL, "Sec-Fetch-Site": "cross-site"}, 403},
		{"POST", "/api/experiments", map[string]any{"title": "forged origin"}, map[string]string{"Authorization": "Bearer " + token, "Origin": "https://other.test"}, 403},
	} {
		status, payload := requestJSON(t, client, test.method, server.URL+test.path, test.body, test.headers)
		if status != test.status {
			t.Fatalf("%s %s status %d want %d: %s", test.method, test.path, status, test.status, payload)
		}
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"operators":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	status, _ := requestJSON(t, client, "GET", server.URL+"/api/bootstrap", nil, operator)
	if status != 401 {
		t.Fatal("revoked operator can read management")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	status, _ = requestJSON(t, client, "GET", server.URL+"/api/bootstrap", nil, operator)
	if status != 503 {
		t.Fatal("missing credential file did not fail closed")
	}
}
