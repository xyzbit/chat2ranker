package app_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type completionBetweenReads struct {
	domain.Repository
	complete bool
}

func (r *completionBetweenReads) ListEvents(ctx context.Context, id string, after int64) ([]contract.Event, error) {
	events, err := r.Repository.ListEvents(ctx, id, after)
	if err == nil && len(events) == 0 && !r.complete {
		r.complete = true
		if err := r.Repository.ConfirmRuntimeStopped(ctx, id, 1); err != nil {
			return nil, err
		}
		err = r.Repository.Complete(ctx, id, contract.Result{Output: "done"}, time.Now().UTC(), contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.completed", Status: contract.StatusCompleted, At: time.Now().UTC()})
	}
	return events, err
}

func TestEventStreamDrainsCompletionCommittedBetweenReads(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := "terminal-race"
	now := time.Now().UTC()
	if err := store.Create(ctx, contract.Execution{ID: id, IdempotencyKey: id, Status: contract.StatusQueued, QueueExpiresAt: time.Now().Add(time.Minute), CreatedAt: now}, contract.Event{ExecutionID: id, Type: "execution.queued", At: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, id, 1, "local", "handle", "test", now, contract.Event{ExecutionID: id, Attempt: 1, Type: "execution.running", At: now}); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(&completionBetweenReads{Repository: store}, nil, app.Options{})
	events, terminal, err := service.WaitEvents(ctx, id, 2, time.Second)
	if err != nil || terminal || len(events) != 1 || events[0].Type != "execution.completed" || events[0].Sequence != 3 {
		t.Fatalf("completion was skipped: %v terminal=%v err=%v", events, terminal, err)
	}
	events, terminal, err = service.WaitEvents(ctx, id, 3, time.Second)
	if err != nil || !terminal || len(events) != 0 {
		t.Fatalf("terminal stream not drained: %v %v %v", events, terminal, err)
	}
}
