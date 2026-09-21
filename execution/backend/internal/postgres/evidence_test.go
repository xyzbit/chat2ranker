package postgres

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestEvidenceSurvivesFailureWithoutAcceptingLateAttemptWrites(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var repo domain.Repository
			if backend == "postgres" {
				repo = testStore(t, 10)
			} else {
				store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "evidence.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				repo = store
			}
			value := executionValue("evidence")
			if err := repo.Create(t.Context(), value, queuedEvent(value)); err != nil {
				t.Fatal(err)
			}
			result := contract.Result{Output: "observed", Usage: contract.Usage{InputTokens: 12, OutputTokens: 3}, Artifacts: []contract.ArtifactRef{{ExecutionID: value.ID, Kind: "stdout", Path: value.ID + "/sha256/digest/stdout.txt", SHA256: "digest", Size: 8}}}
			if err := repo.RecordEvidence(t.Context(), value.ID, 0, result); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("queued evidence accepted: %v", err)
			}
			now := time.Now().UTC()
			if err := repo.MarkRunning(t.Context(), value.ID, 1, "local-process", "owner", "test", now, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", At: now}); err != nil {
				t.Fatal(err)
			}
			if err := repo.RecordEvidence(t.Context(), value.ID, 2, result); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("foreign attempt accepted: %v", err)
			}
			if err := repo.RecordEvidence(t.Context(), value.ID, 1, result); err != nil {
				t.Fatal(err)
			}
			if err := repo.RequestCancel(t.Context(), value.ID, now); err != nil {
				t.Fatal(err)
			}
			if err := repo.RecordEvidence(t.Context(), value.ID, 1, result); err != nil {
				t.Fatalf("cancelling attempt lost observed evidence: %v", err)
			}
			if err := repo.Fail(t.Context(), value.ID, "artifact storage failed", now, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.failed", At: now}); err != nil {
				t.Fatal(err)
			}
			late := result
			late.Output = "late overwrite"
			if err := repo.RecordEvidence(t.Context(), value.ID, 1, late); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("terminal evidence overwritten: %v", err)
			}
			stored, err := repo.Get(t.Context(), value.ID)
			if err != nil || stored.Status != contract.StatusFailed || stored.Result == nil || stored.Result.Output != "observed" || stored.Result.Usage.OutputTokens != 3 || len(stored.Result.Artifacts) != 1 || !stored.RuntimeCleanupPending {
				t.Fatalf("failure lost evidence or cleanup obligation: %+v %v", stored, err)
			}
		})
	}
}
