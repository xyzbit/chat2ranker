package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func TestCapacityProjectsRiverEligibilityAndRetainedQuota(t *testing.T) {
	store := testStore(t, 20)
	store.config.MaxModelConcurrent = 2
	store.config.MaxTenantConcurrent = 3
	now := time.Now().UTC()
	for _, id := range []string{"active", "unknown", "ready", "retry", "snooze", "abandoned", "missing"} {
		value := executionValue(id)
		value.Spec.ModelConnectionID = "connection"
		value.Spec.Model = "model"
		value.Spec.Metadata = map[string]string{"responseId": id, "tenantId": "tenant"}
		value.Spec.Messages = []contract.Message{{Role: "user", Content: "private message"}}
		if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
			t.Fatal(err)
		}
	}
	start := func(id string) error {
		return store.MarkRunning(t.Context(), id, 1, "local-process", "worker", "test", now, contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: now})
	}
	for _, id := range []string{"active", "unknown"} {
		if err := start(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Fail(t.Context(), "unknown", "unconfirmed stop", now, contract.Event{ExecutionID: "unknown", Attempt: 1, Type: "execution.failed", Status: contract.StatusFailed, At: now}); err != nil {
		t.Fatal(err)
	}
	for id, state := range map[string]string{"retry": "retryable", "snooze": "scheduled", "abandoned": "discarded"} {
		_, err := store.pool.Exec(t.Context(), `UPDATE river_job SET state=$1::text::river_job_state, scheduled_at=now()+interval '1 hour', finalized_at=CASE WHEN $1='discarded' THEN now() ELSE NULL END WHERE id=(SELECT job_id FROM executions WHERE id=$2)`, state, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(t.Context(), `UPDATE executions SET job_id=NULL WHERE id='missing'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.CapacityState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MaxModelConcurrent != 2 || snapshot.MaxTenantConcurrent != 3 || len(snapshot.Entries) != 7 {
		t.Fatalf("snapshot %+v", snapshot)
	}
	for _, entry := range snapshot.Entries {
		wantReady := entry.ID != "retry" && entry.ID != "abandoned" && entry.ID != "missing"
		if entry.Queue != "online" || entry.Connection != "connection" || entry.Model != "model" || entry.Tenant != "tenant" || !entry.StructuredHistory || entry.DeliveryReady != wantReady {
			t.Fatalf("entry %+v", entry)
		}
		if entry.ID == "unknown" && (!entry.CleanupPending || entry.Status != contract.StatusFailed) {
			t.Fatalf("lost cleanup reservation: %+v", entry)
		}
	}
	if err := start("ready"); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("unknown stop did not reserve quota: %v", err)
	}
	if err := store.ConfirmRuntimeStopped(t.Context(), "unknown", 1); err != nil {
		t.Fatal(err)
	}
	if err := start("ready"); err != nil {
		t.Fatalf("confirmed stop did not release quota: %v", err)
	}
	snapshot, err = store.CapacityState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 6 {
		t.Fatalf("terminal clean execution retained: %+v", snapshot)
	}
}
