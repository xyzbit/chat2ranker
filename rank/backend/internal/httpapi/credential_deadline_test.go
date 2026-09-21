package httpapi_test

import (
	"bytes"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func TestCredentialDatabaseDeadlineIsNotAnInvalidAPIKey(t *testing.T) {
	if os.Getenv("RANK_TEST_DATABASE_URL") == "" {
		t.Skip("RANK_TEST_DATABASE_URL is required")
	}
	server, client, _, key, store := setupServingHTTP(t, newFakeServingExecution(true), domain.DeploymentPolicy{RequestsPerMinute: 100, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 30000})
	client.Timeout = 8 * time.Second
	lock, err := store.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.ExecContext(t.Context(), "LOCK TABLE api_credentials IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	start := time.Now()
	status, payload := requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/missing", nil, headers)
	if status != 503 || !bytes.Contains(payload, []byte(`"code":"dependency_timeout"`)) || time.Since(start) > 7*time.Second {
		t.Fatalf("dependency failure misreported: %d %s", status, payload)
	}
	lock.Rollback()
	status, _ = requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/missing", nil, headers)
	if status != 404 {
		t.Fatalf("valid credential did not recover: %d", status)
	}
	headers["Authorization"] = "Bearer invalid"
	status, _ = requestJSON(t, client, http.MethodGet, server.URL+"/v1/responses/missing", nil, headers)
	if status != 401 {
		t.Fatalf("invalid credential accepted: %d", status)
	}
}
