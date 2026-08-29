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
	results, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "show-results-1", Type: app.ControlShowResults, Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var resultView struct {
		RunCount int   `json:"runCount"`
		Runs     []any `json:"runs"`
	}
	if err := json.Unmarshal(results.Result, &resultView); err != nil {
		t.Fatal(err)
	}
	if resultView.RunCount != 0 || len(resultView.Runs) != 0 {
		t.Fatalf("unexpected empty experiment results: %#v", resultView)
	}
	view, err = service.GetExperiment(ctx, experiment.ID)
	if err != nil || view.ControlEvents[1].Type != "a2ui/show_experiment_results" {
		t.Fatalf("experiment result card event missing: %#v, %v", view.ControlEvents, err)
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

func TestControlSessionCreatesAndSelectsVersionedAssets(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "Chat assets")
	if err != nil {
		t.Fatal(err)
	}
	datasetCommand, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "create-dataset-1", Type: app.ControlCreateDataset,
		Payload: json.RawMessage(`{"name":"对话测试集","source":"conversation","cases":[{"id":"chat-1","title":"对话用例","input":"完成任务","expected":{"demoOutcome":"pass"}}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var datasetResult struct {
		DatasetVersionID string `json:"datasetVersionId"`
	}
	if err := json.Unmarshal(datasetCommand.Result, &datasetResult); err != nil {
		t.Fatal(err)
	}
	addedCommand, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "add-case-1", Type: app.ControlAddDatasetCases,
		Payload: json.RawMessage(`{"baseDatasetVersionId":"` + datasetResult.DatasetVersionID + `","cases":[{"id":"chat-2","title":"新增用例","input":"完成第二个任务","expected":{"demoOutcome":"pass"}}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var addedResult struct {
		DatasetVersionID string `json:"datasetVersionId"`
	}
	if err := json.Unmarshal(addedCommand.Result, &addedResult); err != nil {
		t.Fatal(err)
	}
	agentCommand, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "create-agent-1", Type: app.ControlCreateAgent,
		Payload: json.RawMessage(`{"name":"对话 Agent","runnerType":"mock","tools":["web_search"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var agentResult struct {
		AgentVersionID string `json:"agentVersionId"`
	}
	if err := json.Unmarshal(agentCommand.Result, &agentResult); err != nil {
		t.Fatal(err)
	}
	agentVersionCommand, err := service.ApplyControlCommand(ctx, app.ControlCommandInput{
		ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID,
		IdempotencyKey: "create-agent-version-1", Type: app.ControlCreateAgentVersion,
		Payload: json.RawMessage(`{"baseAgentVersionId":"` + agentResult.AgentVersionID + `","tools":["web_search","browser"],"skills":["web-research"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var agentVersionResult struct {
		AgentVersionID string `json:"agentVersionId"`
	}
	if err := json.Unmarshal(agentVersionCommand.Result, &agentVersionResult); err != nil {
		t.Fatal(err)
	}
	view, err := service.GetExperiment(ctx, experiment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.DatasetVersionID != addedResult.DatasetVersionID || view.AgentVersionID != agentVersionResult.AgentVersionID || view.Dataset.Version != 2 || view.Dataset.CaseCount != 2 || view.Agent.Version != 2 || len(view.Agent.Tools) != 2 || len(view.Agent.Skills) != 1 {
		t.Fatalf("control-created assets were not selected: %#v", view)
	}
	base, err := store.GetDatasetVersion(ctx, datasetResult.DatasetVersionID)
	if err != nil || base.CaseCount != 1 {
		t.Fatalf("base dataset was mutated: %#v, %v", base, err)
	}
	baseAgent, err := store.GetAgentVersion(ctx, agentResult.AgentVersionID)
	if err != nil || len(baseAgent.Tools) != 1 || len(baseAgent.Skills) != 0 {
		t.Fatalf("base Agent was mutated: %#v, %v", baseAgent, err)
	}
	if len(view.ControlEvents) != 4 || view.ControlEvents[0].Type != "control/create_dataset" || view.ControlEvents[1].Type != "control/add_dataset_cases" || view.ControlEvents[2].Type != "control/create_agent" || view.ControlEvents[3].Type != "control/create_agent_version" {
		t.Fatalf("unexpected control asset events: %#v", view.ControlEvents)
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

func TestTitleOnlyUpdateDoesNotAppendConfigurationMessage(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment, err := service.CreateExperiment(ctx, "未命名实验")
	if err != nil {
		t.Fatal(err)
	}
	title := "提示词优化对比"
	updated, err := service.UpdateExperiment(ctx, experiment.ID, app.ExperimentPatch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || len(updated.Messages) != len(experiment.Messages) {
		t.Fatalf("title update polluted the conversation: %#v", updated)
	}
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
	if first.DatasetSnapshot.ID != "dataset-web-research-v3" || first.AgentSnapshot.ID != "agent-research-demo-v1" || first.EvaluatorSnapshot.ID == "" || first.TrialCount != 5 || first.Total != 60 {
		t.Fatalf("run did not freeze selected versions: %#v", first)
	}
	items, err := store.ListRunItems(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 12 {
		t.Fatalf("expected 12 RunItems, got %d", len(items))
	}
	trials, err := store.ListRunTrials(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trials) != 60 {
		t.Fatalf("expected 60 independent trials, got %d", len(trials))
	}
}

func TestStartComparisonCreatesIndependentIdempotentRuns(t *testing.T) {
	service, store, path := testService(t, false)
	experiment := configuredExperiment(t, service)
	second, err := service.CreateAgent(context.Background(), app.CreateAgentInput{Name: "Research Variant", Handle: "@demo/research-v2", RunnerType: "mock", Model: "deterministic-demo"})
	if err != nil {
		t.Fatal(err)
	}
	options := app.ComparisonOptions{AgentVersionIDs: []string{"agent-research-demo-v1", second.ID}, TrialCount: 1}
	first, err := service.StartComparison(context.Background(), experiment.ID, "compare-1", options)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.StartComparison(context.Background(), experiment.ID, "compare-1", options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Group.ID != replayed.Group.ID || len(first.Runs) != 2 || len(replayed.Runs) != 2 {
		t.Fatalf("comparison was not replayed: %#v %#v", first.Group, replayed.Group)
	}
	for index, run := range first.Runs {
		if run.GroupID != first.Group.ID || run.DatasetSnapshot.ID != experiment.DatasetVersionID || run.AgentSnapshot.ID != options.AgentVersionIDs[index] || run.Total != 12 {
			t.Fatalf("child run did not freeze comparison input: %#v", run)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	group, err := reopened.GetRunGroup(context.Background(), first.Group.ID)
	if err != nil || len(group.RunIDs) != 2 || group.Status != domain.RunQueued {
		t.Fatalf("persisted comparison is incomplete: %#v, %v", group, err)
	}
}

func TestAgentPresetDefaultsOnlyForDSH(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()

	dsh, err := service.CreateAgent(context.Background(), app.CreateAgentInput{Name: "DSH default", RunnerType: "dsh"})
	if err != nil {
		t.Fatal(err)
	}
	if dsh.Preset != "headless" {
		t.Fatalf("DSH preset = %q, want headless", dsh.Preset)
	}
	demo, err := service.CreateAgent(context.Background(), app.CreateAgentInput{Name: "Demo default", RunnerType: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	if demo.Preset != "" {
		t.Fatalf("mock preset = %q, want empty", demo.Preset)
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
	if run.Status != domain.RunComplete || run.Passed != 50 || run.Total != 60 || run.PassRate != 83 || run.ReliableCases != 10 || run.CaseCount != 12 || run.PassHat3 != 83.3 || len(run.Results) != 12 || run.Cost <= 0 {
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
