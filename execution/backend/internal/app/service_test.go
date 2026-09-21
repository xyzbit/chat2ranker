package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/harness"
	"github.com/xyzbit/agentkiln/execution/backend/internal/app"
	"github.com/xyzbit/agentkiln/execution/backend/internal/executor"
	"github.com/xyzbit/agentkiln/execution/backend/internal/sqlite"
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
	local := executor.NewLocal(executor.LocalConfig{Repository: store, WorkerBinary: workerBinary, ArtifactRoot: artifactRoot, SandboxRoot: filepath.Join(temporaryRoot, "sandboxes"), Harnesses: harnesses})
	service := app.NewService(store, local, app.Options{Workers: true, WorkerVersion: "test-build", ArtifactRoot: artifactRoot})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.StopLocal(ctx); err != nil {
			t.Error(err)
		}
	}()
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

func TestExecutionServiceRejectsInvalidStructuredOutputSchema(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, nil, app.Options{})
	_, err = service.Submit(context.Background(), contract.SubmitRequest{
		IdempotencyKey: "invalid-structured",
		Spec: contract.Spec{
			ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test",
			StructuredOutput: &contract.StructuredOutput{Name: "result", Schema: json.RawMessage(`{"$ref":"https://example.com/schema.json"}`)},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "must be a local fragment reference") {
		t.Fatalf("invalid structured schema returned %v", err)
	}
}

func TestExecutionServiceRejectsAttachmentDigestMismatch(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, nil, app.Options{})
	_, err = service.Submit(context.Background(), contract.SubmitRequest{
		IdempotencyKey: "invalid-attachment",
		Spec: contract.Spec{
			ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test",
			InputAttachments: []contract.InputAttachment{{ID: "sha256:wrong", Type: "input_file", Name: "brief.txt", MediaType: "text/plain", Data: []byte("brief")}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "content digest") {
		t.Fatalf("invalid attachment returned %v", err)
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

func TestExecutionRejectsUnresolvedOrAliasedModelBeforeAdmission(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := app.NewService(store, nil, app.Options{})
	for _, selection := range []struct{ connection, model string }{{"connection", ""}, {"connection", " model "}, {" connection ", "model"}} {
		_, err := service.Submit(t.Context(), contract.SubmitRequest{IdempotencyKey: "invalid-model", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Prompt: "test", ModelConnectionID: selection.connection, Model: selection.model}})
		var failure *app.Error
		if !errors.As(err, &failure) || failure.Code != "invalid_model_selection" {
			t.Fatalf("selection accepted: %v", err)
		}
	}
	active, err := store.ListActive(t.Context())
	if err != nil || len(active) != 0 {
		t.Fatalf("invalid admission persisted: %v %v", active, err)
	}
}

func TestExecutionHistoryCapabilityRejectsNewWorkButPreservesReplay(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workerBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	supported, _ := harness.NewRegistry(harness.NewMock())
	service := app.NewService(store, executor.NewLocal(executor.LocalConfig{Repository: store, WorkerBinary: workerBinary, Harnesses: supported}), app.Options{})
	request := contract.SubmitRequest{IdempotencyKey: "history", Spec: contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: "mock", Messages: []contract.Message{{Role: "user", Content: "hello"}}}}
	accepted, err := service.Submit(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	unsupported, _ := harness.NewRegistry(harness.NewCommand(harness.CommandConfig{ID: "mock", Argv: []string{"node", "prompt-only.mjs", "{prompt}"}}))
	service = app.NewService(store, executor.NewLocal(executor.LocalConfig{Repository: store, WorkerBinary: workerBinary, Harnesses: unsupported}), app.Options{})
	replay, err := service.Submit(t.Context(), request)
	if err != nil || replay.ID != accepted.ID {
		t.Fatalf("changed capability lost receipt: %+v %v", replay, err)
	}
	request.IdempotencyKey = "unsupported"
	_, err = service.Submit(t.Context(), request)
	var failure *app.Error
	if !errors.As(err, &failure) || failure.Code != "unsupported_history" {
		t.Fatalf("unsupported history accepted: %v", err)
	}
	active, err := store.ListActive(t.Context())
	if err != nil || len(active) != 1 {
		t.Fatalf("unsupported history admitted work: %+v %v", active, err)
	}
}
