package postgres

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestQueueExpiryNeverStartsOverdueWorkOrInterruptsRunningWork(t *testing.T) {
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
			now := time.Now().UTC().Truncate(time.Millisecond)
			for _, id := range []string{"expired", "missing", "running", "cancelled", "future"} {
				value := executionValue(id)
				value.QueueExpiresAt = now.Add(time.Hour)
				if id == "expired" {
					value.QueueExpiresAt = now.Add(-time.Second)
				}
				if id == "missing" {
					value.QueueExpiresAt = time.Time{}
				}
				if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
					t.Fatal(err)
				}
			}
			start := func(id string, at time.Time) error {
				return store.MarkRunning(t.Context(), id, 1, "local-process", "worker", "test", at, contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: at})
			}
			// A stale timestamp sampled before waiting for the database cannot admit
			// work after wall-clock expiry, even before the reaper runs.
			for _, id := range []string{"expired", "missing"} {
				if err := start(id, now.Add(-time.Minute)); !errors.Is(err, domain.ErrQueueExpired) {
					t.Fatalf("late start %s: %v", id, err)
				}
			}
			if err := start("running", now); err != nil {
				t.Fatal(err)
			}
			if err := store.RequestCancel(t.Context(), "cancelled", now); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errorsCh := make(chan error, 4)
			for range 4 {
				wg.Add(1)
				go func() { defer wg.Done(); errorsCh <- store.ExpireQueued(t.Context(), now) }()
			}
			wg.Wait()
			close(errorsCh)
			for err := range errorsCh {
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"expired", "missing"} {
				value, err := store.Get(t.Context(), id)
				prefix := "execution_queue_expired:"
				if id == "missing" {
					prefix = "execution_limits_missing:"
				}
				if err != nil || value.Status != contract.StatusFailed || value.Attempt != 0 || value.StartedAt != nil || value.RuntimeCleanupPending || !strings.HasPrefix(value.Error, prefix) {
					t.Fatalf("expired %s: %+v %v", id, value, err)
				}
				events, err := store.ListEvents(t.Context(), id, 0)
				if err != nil || len(events) != 2 || events[1].Message != value.Error {
					t.Fatalf("expiry events: %+v %v", events, err)
				}
			}
			future, _ := store.Get(t.Context(), "future")
			if future.Status != contract.StatusQueued {
				t.Fatal("unexpired work changed")
			}
			if err := store.ExpireQueued(t.Context(), now.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			for id, status := range map[string]string{"running": contract.StatusRunning, "cancelled": contract.StatusCancelled} {
				value, err := store.Get(t.Context(), id)
				if err != nil || value.Status != status {
					t.Fatalf("unrelated outcome changed: %+v %v", value, err)
				}
			}
			if pg, ok := store.(*Store); ok {
				var state string
				if err := pg.pool.QueryRow(t.Context(), `SELECT j.state FROM river_job j JOIN executions e ON e.job_id=j.id WHERE e.id='expired'`).Scan(&state); err != nil || state != "cancelled" {
					t.Fatalf("expired delivery: %s %v", state, err)
				}
			}
		})
	}
}

func TestQueueExpiryRollsBackOutcomeWhenRiverCancellationFails(t *testing.T) {
	store := testStore(t, 2)
	value := executionValue("rollback-expiry")
	value.QueueExpiresAt = time.Now().Add(-time.Second)
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(t.Context(), `CREATE FUNCTION reject_queue_cancel() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture cancellation unavailable'; END $$;
CREATE TRIGGER reject_queue_cancel BEFORE UPDATE ON river_job FOR EACH ROW EXECUTE FUNCTION reject_queue_cancel()`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireQueued(t.Context(), time.Now()); err == nil {
		t.Fatal("unavailable delivery write was swallowed")
	}
	current, _ := store.Get(t.Context(), value.ID)
	events, _ := store.ListEvents(t.Context(), value.ID, 0)
	if current.Status != contract.StatusQueued || len(events) != 1 {
		t.Fatal("expiry survived a failed transaction")
	}
	if _, err := store.pool.Exec(t.Context(), `DROP TRIGGER reject_queue_cancel ON river_job`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExpireQueued(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestQueueDeadlineIsCheckedAfterWaitingForAdmissionLock(t *testing.T) {
	store := testStore(t, 2)
	value := executionValue("locked-admission")
	value.QueueExpiresAt = time.Now().Add(500 * time.Millisecond)
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, store.Schema()+"/runtime-admission"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	done := make(chan error, 1)
	go func() {
		done <- store.MarkRunning(t.Context(), value.ID, 1, "local-process", "worker", "test", at, contract.Event{ExecutionID: value.ID, Attempt: 1, Type: "execution.running", Status: contract.StatusRunning, At: at})
	}()
	waitUntil := time.Now().Add(3 * time.Second)
	for {
		var blocked bool
		if err := store.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, tx.Conn().PgConn().PID()).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatal("start did not wait for admission lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if remaining := time.Until(value.QueueExpiresAt.Add(5 * time.Millisecond)); remaining > 0 {
		time.Sleep(remaining)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, domain.ErrQueueExpired) {
		t.Fatalf("stale start after lock wait: %v", err)
	}
	current, err := store.Get(t.Context(), value.ID)
	if err != nil || current.Status != contract.StatusQueued || current.Attempt != 0 || current.RuntimeCleanupPending {
		t.Fatalf("claim reserved resources: %+v %v", current, err)
	}
}
