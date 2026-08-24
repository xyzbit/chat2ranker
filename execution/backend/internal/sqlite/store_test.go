package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestAppendEventAllocatesOrderedSequences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/execution.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	execution := contract.Execution{ID: "exec-events", IdempotencyKey: "events", Status: contract.StatusQueued, Executor: "test", WorkerVersion: "test", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test"}, CreatedAt: now}
	if err := store.Create(ctx, execution, contract.Event{ExecutionID: execution.ID, Type: "execution.queued", Status: contract.StatusQueued, At: now}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 16)
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errors <- store.AppendEvent(ctx, contract.Event{ExecutionID: execution.ID, Type: fmt.Sprintf("progress.%02d", index), Status: contract.StatusRunning, At: now})
		}(index)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ListEvents(ctx, execution.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 17 {
		t.Fatalf("got %d events, want 17", len(events))
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event %d has sequence %d", index, event.Sequence)
		}
	}
}
