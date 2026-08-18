package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/app"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
)

func TestControlCommandsAreSessionBoundStructuredAndIdempotent(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Control session")
	if err != nil {
		t.Fatal(err)
	}
	if experiment.ControlSessionID == "" {
		t.Fatal("new experiment has no control session")
	}
	action := experiment.A2UI.Actions[app.ControlSelectDataset]
	if err := service.AuthorizeAction(experiment.ID, action.Command, action.Token); err != nil {
		t.Fatalf("issued A2UI action was not authorized: %v", err)
	}
	if err := service.AuthorizeAction(experiment.ID, action.Command, "wrong"); err == nil {
		t.Fatal("invalid A2UI token was accepted")
	}
	input := app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "tool-call-1", Type: app.ControlSelectDataset,
		Payload: json.RawMessage(`{"datasetVersionId":"dataset-web-research-v3"}`),
	}
	first, err := service.ApplyControlCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ApplyControlCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent command created %s and %s", first.ID, second.ID)
	}
	input.Payload = json.RawMessage(`{"datasetVersionId":"dataset-browser-actions-v1"}`)
	if _, err := service.ApplyControlCommand(ctx, input); err == nil {
		t.Fatal("same idempotency key accepted a different payload")
	}
	view, err := service.GetExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.DatasetVersionID != "dataset-web-research-v3" || len(view.ControlEvents) != 1 {
		t.Fatalf("unexpected control projection: %#v", view)
	}
	_, err = service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: "another-session",
		IdempotencyKey: "tool-call-2", Type: app.ControlPrepareRun, Payload: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("command from another session was accepted")
	}
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Code != "control_session_mismatch" {
		t.Fatalf("unexpected session mismatch error: %v", err)
	}
}

func TestControlTranscriptReconciliationIsIdempotent(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Transcript")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	messages := []domain.Message{
		{ID: "dsh-control-1", Role: "user", Content: "hello", CreatedAt: now},
		{ID: "dsh-control-2", Role: "assistant", Content: "world", CreatedAt: now.Add(time.Nanosecond)},
	}
	if _, err := service.AppendControlMessages(ctx, experiment.ID, experiment.ControlSessionID, messages); err != nil {
		t.Fatal(err)
	}
	view, err := service.AppendControlMessages(ctx, experiment.ID, experiment.ControlSessionID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Messages) != 3 {
		t.Fatalf("reconciliation duplicated transcript, got %d messages", len(view.Messages))
	}
}

func testService(t *testing.T, workers bool) (*app.Service, *sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rank.db")
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewService(store, app.Options{Workers: workers, WorkerLatency: 10 * time.Millisecond})
	if err := service.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service, store, path
}

func configuredExperiment(t *testing.T, service *app.Service) domain.ExperimentView {
	t.Helper()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Web research")
	if err != nil {
		t.Fatal(err)
	}
	datasetID := "dataset-web-research-v3"
	agentID := "agent-research-demo-v1"
	experiment, err = service.UpdateExperiment(ctx, experiment.ID, app.ExperimentPatch{DatasetVersionID: &datasetID})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err = service.UpdateExperiment(ctx, experiment.ID, app.ExperimentPatch{AgentVersionID: &agentID})
	if err != nil {
		t.Fatal(err)
	}
	return experiment
}

func waitComplete(t *testing.T, service *app.Service, runID string) domain.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := service.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if !domain.IsActiveRun(run.Status) {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return domain.Run{}
}

func TestStartRunIsTransactionalAndIdempotent(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	experiment := configuredExperiment(t, service)
	first, err := service.StartRun(context.Background(), experiment.ID, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartRun(context.Background(), experiment.ID, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate start created %s and %s", first.ID, second.ID)
	}
	if first.DatasetSnapshot.ID != "dataset-web-research-v3" || first.AgentSnapshot.ID != "agent-research-demo-v1" || first.Total != 12 {
		t.Fatalf("run did not freeze selected versions: %#v", first)
	}
	items, err := store.ListRunItems(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 12 {
		t.Fatalf("expected 12 RunItems, got %d", len(items))
	}
}

func TestDemoRunCompletesAndAggregatesResults(t *testing.T) {
	service, store, _ := testService(t, true)
	defer store.Close()
	experiment := configuredExperiment(t, service)
	created, err := service.StartRun(context.Background(), experiment.ID, "complete-demo")
	if err != nil {
		t.Fatal(err)
	}
	run := waitComplete(t, service, created.ID)
	if run.Status != domain.RunComplete || run.Passed != 10 || run.Total != 12 || run.PassRate != 83 || len(run.Results) != 12 || run.Cost <= 0 {
		t.Fatalf("unexpected completed run: status=%s passed=%d/%d rate=%d results=%d cost=%f", run.Status, run.Passed, run.Total, run.PassRate, len(run.Results), run.Cost)
	}
	view, err := service.GetExperiment(context.Background(), experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminalMessages := 0
	for _, message := range view.Messages {
		if message.RunID == run.ID {
			terminalMessages++
		}
	}
	if terminalMessages != 1 {
		t.Fatalf("expected one terminal message, got %d", terminalMessages)
	}
}

func TestQueuedRunRecoversAfterRepositoryRestart(t *testing.T) {
	service, store, path := testService(t, false)
	experiment := configuredExperiment(t, service)
	created, err := service.StartRun(context.Background(), experiment.ID, "recover-me")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed := app.NewService(reopened, app.Options{Workers: true, WorkerLatency: 10 * time.Millisecond})
	if err := resumed.ResumeActiveRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := waitComplete(t, resumed, created.ID)
	if run.Status != domain.RunComplete || len(run.Results) != 12 {
		t.Fatalf("recovered run incomplete: %#v", run)
	}
	view, err := resumed.GetExperiment(context.Background(), experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminalMessages := 0
	for _, message := range view.Messages {
		if message.RunID == run.ID {
			terminalMessages++
		}
	}
	if terminalMessages != 1 {
		t.Fatalf("recovery produced %d terminal messages", terminalMessages)
	}
}
