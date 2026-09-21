package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/artifacts"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type interruptedStore struct {
	artifacts.Store
	calls int
}

func (s *interruptedStore) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	s.calls++
	if s.calls == 2 {
		return errors.New("upload interrupted")
	}
	return s.Store.Put(ctx, key, reader, size)
}

func TestFailedArtifactUploadRetainsOnlyConfirmedEvidence(t *testing.T) {
	root := t.TempDir()
	repo, err := sqlite.Open(t.Context(), filepath.Join(root, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	value := contract.Execution{ID: "upload", IdempotencyKey: "upload", Status: contract.StatusQueued, CreatedAt: time.Now().UTC(), QueueExpiresAt: time.Now().Add(time.Minute)}
	if err := repo.Create(t.Context(), value, contract.Event{ExecutionID: value.ID, Type: "execution.queued", At: value.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunning(t.Context(), value.ID, 1, "local-process", "owner", "test", value.CreatedAt, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", At: value.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	value.Attempt = 1
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, value.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	result := contract.Result{Output: "known reply", Usage: contract.Usage{InputTokens: 12}}
	for _, name := range []string{"first", "interrupted", "not-started"} {
		key := value.ID + "/" + name
		if err := os.WriteFile(filepath.Join(source, key), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		result.Artifacts = append(result.Artifacts, contract.ArtifactRef{ExecutionID: value.ID, Path: key, Kind: "test"})
	}
	storage := &interruptedStore{Store: artifacts.Local{Root: t.TempDir()}}
	local := NewLocal(LocalConfig{Repository: repo, ArtifactRoot: source, ArtifactStore: storage})
	if err := local.publishArtifacts(t.Context(), value, &result); err == nil {
		t.Fatal("interrupted upload reported success")
	}
	stored, err := repo.Get(t.Context(), value.ID)
	if err != nil || stored.Result == nil || stored.Result.Output != "known reply" || stored.Result.Usage.InputTokens != 12 || len(stored.Result.Artifacts) != 1 || storage.calls != 2 || stored.Status != contract.StatusRunning {
		t.Fatalf("invalid upload evidence: %+v %v", stored, err)
	}
	ref := stored.Result.Artifacts[0]
	file, err := storage.Open(t.Context(), ref.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil || string(content) != "first" || len(ref.SHA256) != 64 {
		t.Fatal("confirmed artifact cannot be verified")
	}
}
