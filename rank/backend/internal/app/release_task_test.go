package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/testdb"
)

func reviewedTask(t *testing.T, s *app.Service, deployment domain.Deployment, regression bool) app.StartReleaseTaskInput {
	t.Helper()
	draft, err := s.PrepareReleaseDraft(context.Background(), app.ReleaseDraftInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: "agent-research-demo-v1"})
	if err != nil {
		t.Fatal(err)
	}
	input := app.StartReleaseTaskInput{ReleaseDraftInput: app.ReleaseDraftInput{DeploymentID: deployment.ID, Alias: draft.Alias, AgentVersionID: draft.AgentVersionID, DevelopmentDatasetVersionID: &draft.DevelopmentDatasetVersionID, RegressionDatasetVersionID: &draft.RegressionDatasetVersionID}, ExpectedReleaseRevisionID: draft.ExpectedReleaseRevisionID, ExpectedPolicyRevision: draft.Policy.Revision, RunRegression: &regression, TrialCount: 1, Actor: "test"}
	if draft.CandidateEvidence != nil {
		input.ExpectedCandidateRunID = draft.CandidateEvidence.RunID
	}
	if draft.CurrentEvidence != nil {
		input.ExpectedCurrentRunID = draft.CurrentEvidence.RunID
	}
	return input
}

func taskDeployment(t *testing.T, s *app.Service) domain.Deployment {
	t.Helper()
	deployment, err := s.CreateDeployment(context.Background(), app.CreateDeploymentInput{TenantID: "local", Name: "pipeline", AgentFamilyID: "agent-research-demo", Policy: testDeploymentPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	return deployment
}

func TestAgentDefaultsResolveFamiliesWithoutReplacingWorkspaceSelections(t *testing.T) {
	s, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	dataset, err := s.CreateDataset(ctx, app.CreateDatasetInput{Name: "Defaults", Cases: []domain.Case{{ID: "a", Input: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := s.SaveAgentEvaluationDefaults(ctx, "agent-research-demo", domain.AgentEvaluationDefaults{DevelopmentDatasetID: dataset.FamilyID, RegressionDatasetID: dataset.FamilyID}, 0)
	if err != nil || defaults.Revision != 1 {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
	if _, err = s.SaveAgentEvaluationDefaults(ctx, "agent-research-demo", defaults, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	invalid := defaults
	invalid.RegressionDatasetID = dataset.ID
	if _, err = s.SaveAgentEvaluationDefaults(ctx, "agent-research-demo", invalid, 1); err == nil {
		t.Fatal("accepted version ID as family")
	}
	experiment, err := s.CreateExperiment(ctx, "defaults")
	if err != nil {
		t.Fatal(err)
	}
	version := "agent-research-demo-v1"
	experiment, err = s.UpdateExperiment(ctx, experiment.ID, app.ExperimentPatch{AgentVersionID: &version})
	if err != nil || experiment.DatasetVersionID != dataset.ID {
		t.Fatalf("default not selected: %+v %v", experiment, err)
	}
	explicit := "dataset-web-research-v3"
	experiment, err = s.UpdateExperiment(ctx, experiment.ID, app.ExperimentPatch{DatasetVersionID: &explicit, AgentVersionID: &version})
	if err != nil || experiment.DatasetVersionID != explicit {
		t.Fatalf("explicit selection lost: %v", err)
	}
	deployment := taskDeployment(t, s)
	draft, err := s.PrepareReleaseDraft(ctx, app.ReleaseDraftInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: version})
	if err != nil || draft.RegressionDatasetVersionID != dataset.ID || draft.DevelopmentDatasetVersionID != dataset.ID {
		t.Fatalf("draft: %+v %v", draft, err)
	}
	empty := ""
	replaced, err := s.PrepareReleaseDraft(ctx, app.ReleaseDraftInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: version, RegressionDatasetVersionID: &explicit, DevelopmentDatasetVersionID: &empty})
	if err != nil || replaced.RegressionDatasetVersionID != explicit || replaced.DevelopmentDatasetVersionID != "" {
		t.Fatalf("replacement: %+v %v", replaced, err)
	}
	if _, err = s.SaveReleasePolicy(ctx, deployment.ID, domain.ReleasePolicy{MinPassRate: 100, MinValidTrialRate: 100, MaxEvidenceAgeHours: 24}, 0); err != nil {
		t.Fatal("rules unnecessarily require dataset", err)
	}
}

func TestReleaseTasksRecoverSkipIdempotencyAndRejectConcurrentPublication(t *testing.T) {
	s, store, db := testService(t, false)
	ctx := context.Background()
	deployment := taskDeployment(t, s)
	missing := reviewedTask(t, s, deployment, true)
	missing.RunRegression = nil
	if _, err := s.StartReleaseTask(ctx, missing, "missing"); err == nil {
		t.Fatal("default regression accepted missing dataset")
	}
	input := reviewedTask(t, s, deployment, false)
	first, err := s.StartReleaseTask(ctx, input, "once")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartReleaseTask(ctx, input, "once")
	if err != nil || second.ID != first.ID {
		t.Fatalf("duplicate: %+v %v", second, err)
	}
	changed := input
	changed.Reason = "different"
	if _, err = s.StartReleaseTask(ctx, changed, "once"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed idempotent request: %v", err)
	}
	competing, err := s.StartReleaseTask(ctx, input, "competing")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := testdb.Open(t, db)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered := app.NewService(reopened, app.Options{})
	if err = recovered.ReconcileReleaseTasks(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := recovered.GetReleaseTask(ctx, first.ID)
	if err != nil || done.Status != "published" {
		t.Fatalf("not resumed: %+v %v", done, err)
	}
	conflicted, _ := recovered.GetReleaseTask(ctx, competing.ID)
	if conflicted.Status != "conflict" {
		t.Fatalf("concurrent target changed: %+v", conflicted)
	}
	replay, err := recovered.StartReleaseTask(ctx, input, "once")
	if err != nil || replay.ID != done.ID {
		t.Fatalf("replay after publication: %v", err)
	}
	history, err := recovered.ListReleaseDecisions(ctx, deployment.ID, "production", 0)
	if err != nil || len(history.Items) != 1 || history.Items[0].Result != "regression_skipped" {
		t.Fatalf("skip audit: %+v %v", history, err)
	}
	if _, err = recovered.CancelReleaseTask(ctx, done.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cancelled published: %v", err)
	}
}

func TestReleaseRegressionFailureOverrideCancellationAndPolicyConflict(t *testing.T) {
	for _, mode := range []string{"pass", "fail", "cancel", "policy"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _ := testService(t, mode == "pass" || mode == "fail")
			defer store.Close()
			ctx := context.Background()
			dataset, err := s.CreateDataset(ctx, app.CreateDatasetInput{Name: "Regression", Cases: []domain.Case{{ID: "one", Input: "hello", Expected: map[string]any{"demoOutcome": map[bool]string{true: "fail", false: "pass"}[mode == "fail"]}}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SaveAgentEvaluationDefaults(ctx, "agent-research-demo", domain.AgentEvaluationDefaults{RegressionDatasetID: dataset.FamilyID}, 0); err != nil {
				t.Fatal(err)
			}
			deployment := taskDeployment(t, s)
			input := reviewedTask(t, s, deployment, true)
			task, err := s.StartReleaseTask(ctx, input, "regression")
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != "regression" || task.RunID == "" {
				t.Fatalf("no run: %+v", task)
			}
			if mode == "cancel" {
				cancelled, err := s.CancelReleaseTask(ctx, task.ID)
				if err != nil || cancelled.Status != "cancelled" {
					t.Fatalf("cancel: %v", err)
				}
				waitComplete(t, s, task.RunID)
				run, _ := s.GetRun(ctx, task.RunID)
				if run.Status != domain.RunCancelled {
					t.Fatalf("run not cancelled: %s", run.Status)
				}
			} else if mode == "policy" {
				if _, err = s.CancelRun(ctx, task.RunID); err != nil {
					t.Fatal(err)
				}
				waitComplete(t, s, task.RunID)
				if _, err = s.SaveReleasePolicy(ctx, deployment.ID, domain.ReleasePolicy{MinPassRate: 0, MinValidTrialRate: 100, MaxEvidenceAgeHours: 24}, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				waitComplete(t, s, task.RunID)
			}
			if err = s.ReconcileReleaseTasks(ctx); err != nil {
				t.Fatal(err)
			}
			task, err = s.GetReleaseTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"pass": "published", "fail": "blocked", "cancel": "cancelled", "policy": "conflict"}[mode]
			if task.Status != want {
				t.Fatalf("got %s want %s: %+v", task.Status, want, task.Evaluation)
			}
			if mode == "fail" {
				if _, err = s.OverrideReleaseTask(ctx, task.ID, " "); err == nil {
					t.Fatal("override without reason")
				}
				task, err = s.OverrideReleaseTask(ctx, task.ID, "accepted known failure")
				if err != nil || task.Status != "published" || task.Evaluation.Passed {
					t.Fatalf("override: %+v %v", task, err)
				}
			}
		})
	}
}

func TestConversationCreatesDeploymentDefaultsAndDurableReleaseReview(t *testing.T) {
	s, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	experiment := configuredExperiment(t, s)
	command := func(kind, key string, payload any) domain.ControlCommand {
		t.Helper()
		data, _ := json.Marshal(payload)
		result, err := s.ApplyControlCommand(ctx, app.ControlCommandInput{ExperimentID: experiment.ID, ControlSessionID: experiment.ControlSessionID, IdempotencyKey: key, Type: kind, Payload: data})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	create := command(app.ControlCreateDeployment, "create", map[string]any{"name": "chat-pipeline"})
	replay := command(app.ControlCreateDeployment, "create", map[string]any{"name": "chat-pipeline"})
	if create.ID != replay.ID {
		t.Fatal("duplicate deployment command")
	}
	var result struct {
		Deployment domain.Deployment `json:"deployment"`
	}
	if err := json.Unmarshal(create.Result, &result); err != nil {
		t.Fatal(err)
	}
	dataset, err := store.GetDatasetVersion(ctx, experiment.DatasetVersionID)
	if err != nil {
		t.Fatal(err)
	}
	command(app.ControlSetEvaluationDefaults, "defaults", map[string]any{"developmentDatasetId": dataset.FamilyID})
	prepare := command(app.ControlPrepareRelease, "review", map[string]any{"deploymentId": result.Deployment.ID, "alias": "production"})
	input := reviewedTask(t, s, result.Deployment, false)
	input.ExperimentID, input.ReviewCommandID = experiment.ID, prepare.ID
	task, err := s.StartReleaseTask(ctx, input, "chat-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if task.ReviewCommandID != prepare.ID {
		t.Fatal("lost conversation position")
	}
	input.ReviewCommandID = create.ID
	if _, err = s.StartReleaseTask(ctx, input, "wrong-review"); err == nil {
		t.Fatal("accepted unrelated conversation event")
	}
	command(app.ControlReleaseStatus, "status", map[string]any{})
	tasks, err := s.ListReleaseTasks(ctx, "", experiment.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("chat tasks: %+v %v", tasks, err)
	}
}
