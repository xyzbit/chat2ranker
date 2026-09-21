package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestRuntimeCleanupSurvivesUnknownOutcomeAndRejectsStaleConfirmation(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			var store domain.Repository
			if database == "postgres" {
				store = testStore(t, 20)
			} else {
				local, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = local.Close() })
				store = local
			}
			value := executionValue("unknown-runtime")
			ctx := t.Context()
			if err := store.Create(ctx, value, queuedEvent(value)); err != nil {
				t.Fatal(err)
			}
			if err := store.ConfirmRuntimeStopped(ctx, value.ID, 0); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("unstarted cleanup accepted: %v", err)
			}
			now := time.Now().UTC()
			running := contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: now}
			if err := store.MarkRunning(ctx, value.ID, 1, "local-process", "lost-worker", "test", now, running); err != nil {
				t.Fatal(err)
			}
			checkPending := func(want bool) {
				t.Helper()
				current, err := store.Get(ctx, value.ID)
				if err != nil || current.RuntimeCleanupPending != want {
					t.Fatalf("cleanup pending=%v expected %v: %v", current.RuntimeCleanupPending, want, err)
				}
			}
			checkPending(true)
			terminal := running
			terminal.Type = "execution.completed"
			terminal.Status = contract.StatusCompleted
			if err := store.Complete(ctx, value.ID, contract.Result{}, now, terminal); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("unconfirmed completion accepted: %v", err)
			}
			if err := store.RequestCancel(ctx, value.ID, now); err != nil {
				t.Fatal(err)
			}
			terminal.Type = "execution.cancelled"
			terminal.Status = contract.StatusCancelled
			if err := store.Cancel(ctx, value.ID, now, terminal); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("unconfirmed cancellation accepted: %v", err)
			}
			terminal.Type = "execution.failed"
			terminal.Status = contract.StatusFailed
			if err := store.Fail(ctx, value.ID, "execution_outcome_unknown", now, terminal); err != nil {
				t.Fatal(err)
			}
			checkPending(true)
			if err := store.ConfirmRuntimeStopped(ctx, value.ID, 2); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("wrong attempt released cleanup: %v", err)
			}
			checkPending(true)
			for range 2 {
				if err := store.ConfirmRuntimeStopped(ctx, value.ID, 1); err != nil {
					t.Fatal(err)
				}
			}
			checkPending(false)
			current, err := store.Get(ctx, value.ID)
			if err != nil || current.Status != contract.StatusFailed || current.Error != "execution_outcome_unknown" {
				t.Fatalf("cleanup changed business outcome: %#v %v", current, err)
			}
		})
	}
}

func TestUnknownRuntimeRetainsGlobalModelAndTenantCapacity(t *testing.T) {
	for _, quota := range []string{"model", "tenant"} {
		t.Run(quota, func(t *testing.T) {
			store := testStore(t, 10)
			if quota == "model" {
				store.config.MaxModelConcurrent = 1
			} else {
				store.config.MaxTenantConcurrent = 1
			}
			first, second := executionValue("lost"), executionValue("waiting")
			for _, value := range []*contract.Execution{&first, &second} {
				value.Spec.ModelConnectionID = "shared"
				value.Spec.Model = "model"
				value.Spec.Metadata = map[string]string{"tenantId": "same"}
				if err := store.Create(t.Context(), *value, queuedEvent(*value)); err != nil {
					t.Fatal(err)
				}
			}
			start := func(value contract.Execution) error {
				now := time.Now().UTC()
				return store.MarkRunning(t.Context(), value.ID, 1, "local-process", "worker", "test", now, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: now})
			}
			if err := start(first); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			if err := store.Fail(t.Context(), first.ID, "execution_outcome_unknown", now, contract.Event{ExecutionID: first.ID, Attempt: 1, Type: "execution.failed", Status: contract.StatusFailed, At: now}); err != nil {
				t.Fatal(err)
			}
			if err := start(second); !errors.Is(err, domain.ErrQuotaExceeded) {
				t.Fatalf("unknown runtime released %s capacity: %v", quota, err)
			}
			if err := store.ConfirmRuntimeStopped(t.Context(), first.ID, 1); err != nil {
				t.Fatal(err)
			}
			if err := start(second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrationRetainsUnconfirmedAttemptsAndIsRepeatable(t *testing.T) {
	store := testStore(t, 10)
	value := executionValue("old-unknown")
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	// Reproduce the older serialized record, before cleanup was independently persisted.
	if _, err := store.pool.Exec(t.Context(), `UPDATE executions SET body=(body || '{"status":"failed","attempt":1,"error":"execution_outcome_unknown"}'::jsonb)-'runtimeCleanupPending' WHERE id=$1`, value.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(t.Context(), `UPDATE execution_schema SET version=1`); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckSchema(t.Context()); err == nil {
		t.Fatal("old schema accepted without migration")
	}
	for range 2 {
		if err := store.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || !current.RuntimeCleanupPending {
		t.Fatalf("migration forgot unknown runtime: %#v %v", current, err)
	}
	if err := store.ConfirmRuntimeStopped(t.Context(), value.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(t.Context(), value.ID)
	if err != nil || current.RuntimeCleanupPending {
		t.Fatalf("repeat migration resurrected cleanup: %#v %v", current, err)
	}
}
