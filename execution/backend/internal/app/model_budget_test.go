package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
)

type budgetExecutor struct{ runtimeOutcome }

func (*budgetExecutor) Probe(context.Context, string) contract.Availability {
	return contract.Availability{Available: true, Installed: true, ModelRequestBudget: true}
}

func TestModelBudgetIsFrozenBeforeDeliveryAndUnsupportedWorkersCannotClaim(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, &budgetExecutor{}, app.Options{MaxModelRequests: 4})
	input := contract.SubmitRequest{IdempotencyKey: "budget", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "dsh", Prompt: "hello", MaxModelRequests: 10}}
	first, err := service.Submit(t.Context(), input)
	if err != nil || first.MaxModelRequests != 4 {
		t.Fatalf("budget not capped: %+v %v", first, err)
	}
	restarted := app.NewService(store, &budgetExecutor{}, app.Options{MaxModelRequests: 1})
	replay, err := restarted.Submit(t.Context(), input)
	if err != nil || replay.MaxModelRequests != 4 {
		t.Fatalf("budget changed: %+v %v", replay, err)
	}
	unsupported := app.NewService(store, &runtimeOutcome{}, app.Options{})
	capacity, err := unsupported.Capacity(t.Context())
	if err != nil || capacity.Evaluation.Ready != 0 || capacity.Evaluation.CapabilityBlocked != 1 {
		t.Fatalf("incompatible budget inflated capacity: %+v %v", capacity, err)
	}
	if err := unsupported.Execute(t.Context(), first.ID); !errors.Is(err, domain.ErrExecutorUnavailable) {
		t.Fatalf("unsupported worker claimed: %v", err)
	}
	stored, _ := store.Get(t.Context(), first.ID)
	if stored.Attempt != 0 || stored.Status != contract.StatusQueued {
		t.Fatal("budget bypass started invocation")
	}
	input.IdempotencyKey = "smaller"
	input.Spec.MaxModelRequests = 2
	limited, err := service.Submit(t.Context(), input)
	if err != nil || limited.MaxModelRequests != 2 {
		t.Fatalf("request limit not respected: %+v %v", limited, err)
	}
	input.IdempotencyKey = "unsupported"
	_, err = unsupported.Submit(t.Context(), input)
	var problem *app.Error
	if !errors.As(err, &problem) || problem.Code != "unsupported_model_budget" {
		t.Fatalf("unsupported cap accepted: %v", err)
	}
	input.IdempotencyKey = "invalid"
	input.Spec.MaxModelRequests = -1
	if _, err = service.Submit(t.Context(), input); err == nil {
		t.Fatal("negative cap accepted")
	}
	input.Spec.MaxModelRequests = contract.MaxModelRequestBudget + 1
	if _, err = service.Submit(t.Context(), input); err == nil {
		t.Fatal("unrepresentable cap accepted")
	}
	input.IdempotencyKey = "legacy"
	input.Spec.MaxModelRequests = 0
	legacy, err := unsupported.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), legacy.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err = store.Get(t.Context(), legacy.ID)
	if err != nil || legacy.Status != contract.StatusFailed || legacy.Attempt != 0 {
		t.Fatalf("missing budget started: %+v %v", legacy, err)
	}
}
