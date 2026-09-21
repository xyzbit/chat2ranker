package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

func TestQueueDeadlineIsFrozenAcrossReplayAndOwnerConfigurationChanges(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	service := app.NewService(store, &runtimeOutcome{}, app.Options{QueueTimeout: time.Minute, Clock: func() time.Time { return now }})
	request := contract.SubmitRequest{IdempotencyKey: "queue", Spec: contract.Spec{ProtocolVersion: 2, Kind: "agent", Harness: "mock", Prompt: "hello", QueueTimeoutMs: 1000}}
	created, err := service.Submit(t.Context(), request)
	if err != nil || !created.QueueExpiresAt.Equal(now.Add(time.Second)) {
		t.Fatalf("resolved deadline: %+v %v", created, err)
	}
	request.IdempotencyKey = "capped"
	request.Spec.QueueTimeoutMs = 120000
	capped, err := service.Submit(t.Context(), request)
	if err != nil || !capped.QueueExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("service cap: %+v %v", capped, err)
	}
	request.IdempotencyKey = "default"
	request.Spec.QueueTimeoutMs = 0
	defaulted, err := service.Submit(t.Context(), request)
	if err != nil || !defaulted.QueueExpiresAt.Equal(capped.QueueExpiresAt) {
		t.Fatalf("default limit: %+v %v", defaulted, err)
	}
	now = now.Add(2 * time.Second)
	restarted := app.NewService(store, &runtimeOutcome{}, app.Options{QueueTimeout: time.Hour, Clock: func() time.Time { return now }})
	request.IdempotencyKey, request.Spec.QueueTimeoutMs = "queue", 1000
	replay, err := restarted.Submit(t.Context(), request)
	if err != nil || replay.ID != created.ID || !replay.QueueExpiresAt.Equal(created.QueueExpiresAt) {
		t.Fatalf("replay extended admission: %+v %v", replay, err)
	}
	capacity, err := restarted.Capacity(t.Context())
	if err != nil || capacity.Evaluation.Expired != 1 {
		t.Fatalf("expired demand: %+v %v", capacity, err)
	}
	// Execute reaches expiry before the test runtime; its normal Run would succeed.
	if err := restarted.Execute(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	done, err := restarted.Get(t.Context(), created.ID)
	if err != nil || done.Status != contract.StatusFailed || done.Attempt != 0 || done.Result != nil {
		t.Fatalf("expired invocation ran: %+v %v", done, err)
	}
	request.Spec.QueueTimeoutMs = -1
	if _, err := restarted.Submit(t.Context(), request); err == nil {
		t.Fatal("negative queue timeout accepted")
	}
	request.Spec.QueueTimeoutMs = 1<<63 - 1
	if _, err := restarted.Submit(t.Context(), request); err == nil {
		t.Fatal("overflowing queue timeout accepted")
	}
}
