package app

import (
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestRuntimeModelOmitsDisplayOnlyDefaults(t *testing.T) {
	t.Parallel()
	if value := runtimeModel("由运行时决定"); value != "" {
		t.Fatalf("display-only model leaked into execution spec: %q", value)
	}
	if value := runtimeModel("  deepseek-chat  "); value != "deepseek-chat" {
		t.Fatalf("explicit model was not normalized: %q", value)
	}
}

func TestExecutionEventEmitterLeavesCompletionToBusinessControlFlow(t *testing.T) {
	t.Parallel()
	events := []RunnerEvent{}
	emit := executionEventEmitter(func(event RunnerEvent) error {
		events = append(events, event)
		return nil
	}, "judge")
	if err := emit(contract.Event{Type: "execution.running", Status: contract.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := emit(contract.Event{Type: "execution.completed", Status: contract.StatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := emitExecutionCompleted(func(event RunnerEvent) error {
		events = append(events, event)
		return nil
	}, "judge"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "judge.running" || events[1].Type != "judge.completed" {
		t.Fatalf("unexpected mapped events: %#v", events)
	}
}
