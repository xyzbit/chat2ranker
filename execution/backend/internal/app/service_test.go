package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/harness"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/app"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/executor"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/sqlite"
)

func TestExecutionServicePersistsAndRunsAnIsolatedInvocation(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backendRoot := filepath.Clean(filepath.Join(workingDirectory, "../.."))
	temporaryRoot := t.TempDir()
	workerBinary := filepath.Join(temporaryRoot, "execution-worker")
	build := exec.Command("go", "build", "-o", workerBinary, "./cmd/execution-worker")
	build.Dir = backendRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build execution-worker: %v\n%s", err, output)
	}
	store, err := sqlite.Open(context.Background(), filepath.Join(temporaryRoot, "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	artifactRoot := filepath.Join(temporaryRoot, "artifacts")
	harnesses, err := harness.DefaultRegistry(backendRoot)
	if err != nil {
		t.Fatal(err)
	}
	local := executor.NewLocal(executor.LocalConfig{WorkerBinary: workerBinary, ArtifactRoot: artifactRoot, SandboxRoot: filepath.Join(temporaryRoot, "sandboxes"), Harnesses: harnesses})
	service := app.NewService(store, local, app.Options{Workers: true, WorkerVersion: "test-build", ArtifactRoot: artifactRoot})
	created, err := service.Submit(context.Background(), contract.SubmitRequest{IdempotencyKey: "case-1-agent", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "complete the task", Expected: map[string]any{"summary": "structured result"}}})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitTerminal(t, service, created.ID)
	if completed.Status != contract.StatusCompleted || completed.Attempt != 1 || completed.WorkerVersion != "test-build" || completed.Result == nil || len(completed.Result.Artifacts) < 2 {
		t.Fatalf("unexpected execution: %#v", completed)
	}
	replayed, err := service.Submit(context.Background(), contract.SubmitRequest{IdempotencyKey: "case-1-agent", Spec: created.Spec})
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("idempotent submission returned %#v, %v", replayed, err)
	}
	artifact := completed.Result.Artifacts[0]
	content, err := service.ReadArtifact(context.Background(), completed.ID, artifact.Path)
	if err != nil || content.Content == "" {
		t.Fatalf("artifact was not readable: %#v, %v", content, err)
	}
	events, err := service.Events(context.Background(), completed.ID, 0)
	if err != nil || len(events) < 5 || events[0].Type != "execution.queued" || events[1].Type != "execution.running" || events[len(events)-1].Type != "execution.completed" {
		t.Fatalf("unexpected durable event stream: %#v, %v", events, err)
	}
	if !containsEvent(events, "harness.started") || !containsEvent(events, "harness.output") {
		t.Fatalf("harness progress was not retained: %#v", events)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event sequence is not monotonic: %#v", events)
		}
	}
}

func containsEvent(events []contract.Event, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func waitTerminal(t *testing.T, service *app.Service, id string) contract.Execution {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		value, err := service.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if contract.IsTerminal(value.Status) {
			return value
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("execution did not finish")
	return contract.Execution{}
}
