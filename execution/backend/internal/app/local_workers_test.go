package app_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type blockingRuntime struct {
	started chan string
	active  atomic.Int32
	peak    atomic.Int32
}

func (*blockingRuntime) Name() string { return "blocking-test-runtime" }
func (*blockingRuntime) Probe(context.Context, string) contract.Availability {
	return contract.Availability{Available: true}
}
func (runtime *blockingRuntime) Run(ctx context.Context, value contract.Execution, _ domain.EventSink) (contract.Result, error) {
	active := runtime.active.Add(1)
	defer runtime.active.Add(-1)
	for old := runtime.peak.Load(); active > old && !runtime.peak.CompareAndSwap(old, active); old = runtime.peak.Load() {
	}
	runtime.started <- value.ID
	<-ctx.Done()
	return contract.Result{}, ctx.Err()
}

func TestLocalPoolsBoundAdmissionAndDrainWithoutClaimingWaitingWork(t *testing.T) {
	store, err := sqlite.OpenWithQueueLimit(t.Context(), filepath.Join(t.TempDir(), "execution.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := &blockingRuntime{started: make(chan string, 4)}
	service := app.NewService(store, runtime, app.Options{Workers: true, OnlineSlots: 1, EvaluationSlots: 1, ExecutionTimeout: time.Second, PollInterval: time.Millisecond, CleanupGrace: time.Second, PersistenceTimeout: time.Second})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		_ = service.StopLocal(ctx)
	}()
	submit := func(key string, online bool) (contract.Execution, error) {
		metadata := map[string]string{}
		if online {
			metadata["responseId"] = key
		}
		return service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: key, Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: key, Metadata: metadata}})
	}
	awaitStart := func(id string) {
		t.Helper()
		select {
		case started := <-runtime.started:
			if started != id {
				t.Fatalf("started %s, expected %s", started, id)
			}
		case <-time.After(time.Second):
			t.Fatal("runtime did not start")
		}
	}
	online, err := submit("online", true)
	if err != nil {
		t.Fatal(err)
	}
	awaitStart(online.ID)
	evaluation, err := submit("evaluation", false)
	if err != nil {
		t.Fatal(err)
	}
	awaitStart(evaluation.ID)
	waiting, err := submit("waiting", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := submit("overloaded", true); err == nil {
		t.Fatal("queue exceeded admission limit")
	}
	duplicate, err := submit("waiting", true)
	if err != nil || duplicate.ID != waiting.ID {
		t.Fatalf("duplicate under load: %#v %v", duplicate, err)
	}
	if runtime.peak.Load() != 2 {
		t.Fatalf("active runtime peak=%d", runtime.peak.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := service.StopLocal(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.active.Load() != 0 {
		t.Fatal("shutdown returned while runtime was active")
	}
	for _, id := range []string{online.ID, evaluation.ID} {
		value, err := service.Get(t.Context(), id)
		if err != nil || value.Status != contract.StatusFailed {
			t.Fatalf("interrupted result: %#v %v", value, err)
		}
	}
	value, err := service.Get(t.Context(), waiting.ID)
	if err != nil || value.Status != contract.StatusQueued || value.Attempt != 0 {
		t.Fatalf("shutdown claimed waiting work: %#v %v", value, err)
	}
	if cancelled, err := service.Cancel(t.Context(), waiting.ID); err != nil || cancelled.Status != contract.StatusCancelled {
		t.Fatalf("queued cancellation: %#v %v", cancelled, err)
	}
}

func (*blockingRuntime) Cleanup(context.Context, contract.Execution) error {
	return domain.ErrRuntimeStopUnconfirmed
}
