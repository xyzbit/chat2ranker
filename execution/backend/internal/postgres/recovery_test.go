package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func TestConcurrentWorkersReserveGlobalCapacityUntilCancellationConfirmed(t *testing.T) {
	store := testStore(t, 20)
	store.config.MaxModelConcurrent = 1
	store.config.MaxTenantConcurrent = 2
	var pending []contract.Execution
	for i := range 8 {
		value := executionValue(fmt.Sprintf("quota-%d", i))
		value.Spec.ModelConnectionID, value.Spec.Model = "shared-provider", "test"
		value.Spec.Metadata = map[string]string{"tenantId": "same-tenant"}
		if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
			t.Fatal(err)
		}
		pending = append(pending, value)
	}
	var wg sync.WaitGroup
	winners := make(chan string, len(pending))
	for _, value := range pending {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: time.Now().UTC()}
			err := store.MarkRunning(t.Context(), value.ID, 1, "local-process", value.ID, "release", event.At, event)
			if err == nil {
				winners <- value.ID
			} else if !errors.Is(err, domain.ErrQuotaExceeded) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(winners)
	if len(winners) != 1 {
		t.Fatalf("global capacity admitted %d workers", len(winners))
	}
	owner := <-winners
	if err := store.RequestCancel(t.Context(), owner, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var waiting contract.Execution
	for _, value := range pending {
		if value.ID != owner {
			waiting = value
			break
		}
	}
	start := contract.Event{ExecutionID: waiting.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: time.Now().UTC()}
	if err := store.MarkRunning(t.Context(), waiting.ID, 1, "local-process", "next", "release", start.At, start); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("cancelling released capacity: %v", err)
	}
	finished := contract.Event{ExecutionID: owner, Attempt: 1, Type: "execution.cancelled", Status: contract.StatusCancelled, At: time.Now().UTC()}
	if err := store.ConfirmRuntimeStopped(t.Context(), owner, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(t.Context(), owner, finished.At, finished); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(t.Context(), waiting.ID, 1, "local-process", "next", "release", start.At, start); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileDoesNotReleaseUnknownRuntimeBeforeItsFrozenLifetime(t *testing.T) {
	store := testStore(t, 2)
	value := executionValue("orphaned-cancel")
	value.RuntimeTimeoutMs, value.CleanupGraceMs = 1000, 500
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	event := contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: started}
	if err := store.MarkRunning(t.Context(), value.ID, 1, "local-process", "lost", "release", started, event); err != nil {
		t.Fatal(err)
	}
	if err := store.RequestCancel(t.Context(), value.ID, started); err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(t.Context(), started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || current.Status != contract.StatusCancelling {
		t.Fatalf("early reconciliation: %#v %v", current, err)
	}
	if _, err := store.pool.Exec(t.Context(), `DELETE FROM river_job WHERE id=(SELECT job_id FROM executions WHERE id=$1)`, value.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(t.Context(), started.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(t.Context(), value.ID)
	if err != nil || current.Status != contract.StatusFailed || current.Attempt != 1 {
		t.Fatalf("late reconciliation: %#v %v", current, err)
	}
}
