package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

func TestCleanupMigrationAndReopenPreserveUnknownRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	value := contract.Execution{ID: "old", IdempotencyKey: "old", Status: contract.StatusQueued, CreatedAt: time.Now().UTC()}
	if err := store.Create(t.Context(), value, contract.Event{ExecutionID: value.ID, Type: "execution.queued", At: value.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM schema_migrations WHERE version=6; UPDATE executions SET status='failed',attempt=1,error='execution_outcome_unknown'; ALTER TABLE executions DROP COLUMN runtime_cleanup_pending;`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || !current.RuntimeCleanupPending {
		t.Fatalf("migration forgot cleanup: %#v %v", current, err)
	}
	if err := store.ConfirmRuntimeStopped(t.Context(), value.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	current, err = store.Get(t.Context(), value.ID)
	if err != nil || current.RuntimeCleanupPending || current.Status != contract.StatusFailed {
		t.Fatalf("reopen changed confirmed state: %#v %v", current, err)
	}
}
