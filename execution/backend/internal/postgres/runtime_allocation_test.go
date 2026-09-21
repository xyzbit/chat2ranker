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

func TestRuntimeIntentCannotBeRecreatedOrAttachedToAnotherAttempt(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var store domain.Repository
			if backend == "postgres" {
				store = testStore(t, 10)
			} else {
				local, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer local.Close()
				store = local
			}
			value := executionValue("allocation")
			now := time.Now().UTC()
			runtime := contract.RuntimeAllocation{Endpoint: "http://sandbox/v1", Scope: "test", Image: "fixed", ExpiresAt: now.Add(time.Minute)}
			if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
				t.Fatal(err)
			}
			if err := store.BeginRuntime(t.Context(), value.ID, 0, runtime); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("queued creation accepted: %v", err)
			}
			running := contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: now}
			if err := store.MarkRunning(t.Context(), value.ID, 1, "opensandbox", "worker", "test", now, running); err != nil {
				t.Fatal(err)
			}
			if err := store.BeginRuntime(t.Context(), value.ID, 1, runtime); err != nil {
				t.Fatal(err)
			}
			if err := store.BeginRuntime(t.Context(), value.ID, 1, runtime); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("duplicate create intent accepted: %v", err)
			}
			failed := running
			failed.Type = "execution.failed"
			failed.Status = contract.StatusFailed
			if err := store.Fail(t.Context(), value.ID, "unknown", now, failed); err != nil {
				t.Fatal(err)
			}
			if err := store.AttachRuntime(t.Context(), value.ID, 2, "remote"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("wrong attempt accepted: %v", err)
			}
			for range 2 {
				if err := store.AttachRuntime(t.Context(), value.ID, 1, "remote"); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.AttachRuntime(t.Context(), value.ID, 1, "other"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("runtime ownership replaced: %v", err)
			}
			pending, err := store.ListPendingCleanup(t.Context())
			if err != nil || len(pending) != 1 || pending[0].Runtime == nil || pending[0].Runtime.Handle != "remote" || pending[0].ExternalHandle != "worker" {
				t.Fatalf("pending allocation: %#v %v", pending, err)
			}
			if err := store.ConfirmRuntimeStopped(t.Context(), value.ID, 1); err != nil {
				t.Fatal(err)
			}
			pending, err = store.ListPendingCleanup(t.Context())
			if err != nil || len(pending) != 0 {
				t.Fatalf("confirmed runtime remained pending: %#v %v", pending, err)
			}
			if err := store.AttachRuntime(t.Context(), value.ID, 1, "remote"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("cleanup was resurrected: %v", err)
			}
		})
	}
}
