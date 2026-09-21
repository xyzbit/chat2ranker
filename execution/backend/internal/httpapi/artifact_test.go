package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/httpapi"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestArtifactHTTPPreservesBinaryBytesAndAuthorizesFailedEvidence(t *testing.T) {
	root := t.TempDir()
	repo, err := sqlite.Open(t.Context(), filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	value := contract.Execution{ID: "evidence", IdempotencyKey: "evidence", Status: contract.StatusQueued, CreatedAt: now, QueueExpiresAt: now.Add(time.Minute)}
	if err := repo.Create(t.Context(), value, contract.Event{ExecutionID: value.ID, Type: "execution.queued", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunning(t.Context(), value.ID, 1, "local-process", "owner", "test", now, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", At: now}); err != nil {
		t.Fatal(err)
	}
	binary := []byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0xff}
	if err := os.WriteFile(filepath.Join(root, "binary"), binary, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unpublished"), binary, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordEvidence(t.Context(), value.ID, 1, contract.Result{Artifacts: []contract.ArtifactRef{{ExecutionID: value.ID, Path: "binary"}}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(t.Context(), value.ID, "later upload failed", now, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.failed", At: now}); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(app.NewService(repo, nil, app.Options{ArtifactRoot: root}), httpapi.Options{ServiceToken: "private"})
	for _, path := range []string{"binary", "unpublished"} {
		req := httptest.NewRequest("GET", "/v1/executions/evidence/artifacts?path="+path, nil)
		req.Header.Set("Authorization", "Bearer private")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if path == "unpublished" {
			if response.Code != 404 {
				t.Fatal("unreferenced object was readable")
			}
			continue
		}
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		var content contract.ArtifactContent
		if err := json.Unmarshal(response.Body.Bytes(), &content); err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(content.Content)
		if err != nil || content.Encoding != "base64" || content.Truncated || !bytes.Equal(decoded, binary) {
			t.Fatal("HTTP JSON changed artifact bytes")
		}
	}
}
