package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"github.com/xyzbit/agentkiln/rank/backend/internal/testdb"
)

// Varied durable Run summaries exercise policy arithmetic and incomplete evidence.
type releaseEvidenceRepository struct {
	domain.Repository
	run domain.Run
}

func (r releaseEvidenceRepository) GetRun(context.Context, string) (domain.Run, error) {
	return r.run, nil
}
func (r releaseEvidenceRepository) WithinTx(ctx context.Context, fn func(context.Context, domain.Repository) error) error {
	return r.Repository.WithinTx(ctx, func(ctx context.Context, tx domain.Repository) error {
		return fn(ctx, releaseEvidenceRepository{Repository: tx, run: r.run})
	})
}

func TestReleasePolicyChecksExactEvidence(t *testing.T) {
	service, store, _ := testService(t, true)
	defer store.Close()
	ctx := context.Background()
	experiment := configuredExperiment(t, service)
	created, err := service.StartRun(ctx, experiment.ID, "policy-evidence")
	if err != nil {
		t.Fatal(err)
	}
	run := waitComplete(t, service, created.ID)
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{TenantID: "policy-tests", Name: "gated", AgentFamilyID: run.AgentSnapshot.FamilyID, Policy: testDeploymentPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	policy := domain.ReleasePolicy{DatasetVersionID: run.DatasetSnapshot.ID, MinPassRate: 67, MinValidTrialRate: 80, MaxEvidenceAgeHours: 24}
	if _, err := service.SaveReleasePolicy(ctx, deployment.ID, policy, 0); err != nil {
		t.Fatal(err)
	}
	now := *run.CompletedAt
	run.ScheduledTrials, run.CompletedTrials, run.ValidTrials, run.Passed = 3, 3, 3, 3
	for _, tc := range []struct {
		name, failed string
		change       func(*domain.Run)
	}{
		{"passing", "", func(*domain.Run) {}},
		{"rounded 67 is below 67", "pass_rate", func(r *domain.Run) { r.Passed = 2; r.PassRate = 67 }},
		{"insufficient coverage", "valid_trial_rate", func(r *domain.Run) { r.ValidTrials = 2; r.Passed = 2 }},
		{"permitted invalid trials", "", func(r *domain.Run) {
			r.ScheduledTrials = 5
			r.CompletedTrials = 5
			r.ValidTrials = 4
			r.Passed = 4
			r.EvaluationComplete = false
		}},
		{"zero valid trials", "valid_trials", func(r *domain.Run) { r.ValidTrials = 0; r.Passed = 0 }},
		{"unfinished", "run_complete", func(r *domain.Run) { r.Status = domain.RunRunning }},
		{"uncompleted trial", "run_complete", func(r *domain.Run) { r.CompletedTrials-- }},
		{"no completion time", "run_complete", func(r *domain.Run) { r.CompletedAt = nil }},
		{"different agent", "agent_version", func(r *domain.Run) { r.AgentSnapshot.ID = "another-version" }},
		{"different dataset", "dataset_version", func(r *domain.Run) { r.DatasetSnapshot.ID = "another-dataset" }},
		{"expired", "evidence_age", func(r *domain.Run) { at := now.Add(-24*time.Hour - time.Second); r.CompletedAt = &at }},
		{"expiry boundary", "", func(r *domain.Run) { at := now.Add(-24 * time.Hour); r.CompletedAt = &at }},
		{"future timestamp", "evidence_age", func(r *domain.Run) { at := now.Add(time.Second); r.CompletedAt = &at }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := run
			tc.change(&evidence)
			checker := app.NewService(releaseEvidenceRepository{Repository: store, run: evidence}, app.Options{Clock: func() time.Time { return now }})
			preflight, err := checker.PreflightRelease(ctx, app.PromoteDeploymentInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: run.AgentSnapshot.ID, EvidenceRunID: run.ID})
			if err != nil {
				t.Fatal(err)
			}
			if preflight.Evaluation.Passed != (tc.failed == "") {
				t.Fatalf("unexpected evaluation: %+v", preflight.Evaluation)
			}
			if tc.failed != "" {
				for _, check := range preflight.Evaluation.Checks {
					if check.Code == tc.failed && !check.Passed {
						return
					}
				}
				t.Fatalf("missing rejected check %s", tc.failed)
			}
		})
	}
}

func TestReleaseDecisionsSurviveRejectionPolicyChangesAndRestart(t *testing.T) {
	service, store, dbPath := testService(t, true)
	ctx := context.Background()
	defer func() { store.Close() }()
	experiment := configuredExperiment(t, service)
	created, err := service.StartRun(ctx, experiment.ID, "audit-evidence")
	if err != nil {
		t.Fatal(err)
	}
	run := waitComplete(t, service, created.ID)
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{TenantID: "policy-tests", Name: "audited", AgentFamilyID: run.AgentSnapshot.FamilyID, Policy: testDeploymentPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	input := app.PromoteDeploymentInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: run.AgentSnapshot.ID, EvidenceRunID: run.ID, PolicyResult: domain.ReleasePolicyPassed, Actor: "test-operator"}
	_, err = service.PromoteDeploymentAlias(ctx, input)
	var rejected *app.Error
	if !errors.As(err, &rejected) || rejected.Code != "release_policy_rejected" {
		t.Fatalf("unconfigured policy accepted: %v", err)
	}
	policy := domain.ReleasePolicy{DatasetVersionID: run.DatasetSnapshot.ID, MinPassRate: 80, MinValidTrialRate: 100, MaxEvidenceAgeHours: 1}
	if _, err = service.SaveReleasePolicy(ctx, deployment.ID, policy, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveReleasePolicy(ctx, deployment.ID, policy, 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("policy CAS: %v", err)
	}
	if _, err = service.PromoteDeploymentAlias(ctx, input); !errors.As(err, &rejected) || rejected.Code != "release_policy_changed" {
		t.Fatalf("stale policy accepted: %v", err)
	}
	input.ExpectedPolicyRevision = 1
	preflight, err := service.PreflightRelease(ctx, input)
	if err != nil || !preflight.Evaluation.Passed {
		t.Fatalf("preflight: %+v, %v", preflight, err)
	}
	later := app.NewService(store, app.Options{Clock: func() time.Time { return run.CompletedAt.Add(2 * time.Hour) }})
	if _, err = later.PromoteDeploymentAlias(ctx, input); !errors.As(err, &rejected) || rejected.Code != "release_policy_rejected" {
		t.Fatalf("expired since preflight accepted: %v", err)
	}
	input.PolicyResult = domain.ReleasePolicyManualOverride
	if _, err = later.PromoteDeploymentAlias(ctx, input); err == nil {
		t.Fatal("override without reason accepted")
	}
	input.Reason = "emergency recovery"
	release, err := later.PromoteDeploymentAlias(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = later.PromoteDeploymentAlias(ctx, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale alias accepted: %v", err)
	}
	policy.MinPassRate = 100
	if _, err = service.SaveReleasePolicy(ctx, deployment.ID, policy, 1); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = testdb.Open(t, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	service = app.NewService(store, app.Options{})
	page, err := service.ListReleaseDecisions(ctx, deployment.ID, "production", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Items[0].Result != "manual_override" || page.Items[1].Result != "rejected" || page.Items[2].Evaluation.Policy.Revision != 0 {
		t.Fatalf("audit history: %+v", page)
	}
	if page.Items[0].Evaluation.Policy.MinPassRate != 80 || page.Items[0].Evaluation.Passed || page.Items[0].ReleaseRevisionID != release.ID {
		t.Fatal("override lost frozen failed checks")
	}
	resolved, err := service.ResolveDeploymentReference(ctx, "policy-tests", "audited@production")
	if err != nil || resolved.ReleaseRevision.ID != release.ID {
		t.Fatalf("restart lost alias: %v", err)
	}
	input.ExpectedReleaseRevisionID = release.ID
	input.ExpectedPolicyRevision = 2
	input.PolicyResult = domain.ReleasePolicyPassed
	input.EvidenceRunID = ""
	for range 31 {
		if _, err = service.PromoteDeploymentAlias(ctx, input); !errors.As(err, &rejected) || rejected.Code != "release_policy_rejected" {
			t.Fatalf("missing evidence: %v", err)
		}
	}
	first, err := service.ListReleaseDecisions(ctx, deployment.ID, "production", 0)
	if err != nil || len(first.Items) != 30 || first.NextCursor == 0 {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := service.ListReleaseDecisions(ctx, deployment.ID, "production", first.NextCursor)
	if err != nil || len(second.Items) != 4 || second.NextCursor != 0 {
		t.Fatalf("second page: %+v %v", second, err)
	}
	seen := map[domain.ReleaseDecisionID]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ID] {
			t.Fatal("duplicate decision across pages")
		}
		seen[item.ID] = true
	}
}

func TestReleasePolicyRejectsInvalidConfigurationWithoutSaving(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{TenantID: "policy-tests", Name: "validation", AgentFamilyID: "agent-research-demo", Policy: testDeploymentPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		change   func(*domain.ReleasePolicy)
		expected int64
	}{
		{"negative pass rate", func(p *domain.ReleasePolicy) { p.MinPassRate = -1 }, 0},
		{"excess pass rate", func(p *domain.ReleasePolicy) { p.MinPassRate = 101 }, 0},
		{"zero coverage", func(p *domain.ReleasePolicy) { p.MinValidTrialRate = 0 }, 0},
		{"excess coverage", func(p *domain.ReleasePolicy) { p.MinValidTrialRate = 101 }, 0},
		{"zero validity", func(p *domain.ReleasePolicy) { p.MaxEvidenceAgeHours = 0 }, 0},
		{"negative revision", func(*domain.ReleasePolicy) {}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := domain.ReleasePolicy{DatasetVersionID: "dataset-web-research-v3", MinPassRate: 80, MinValidTrialRate: 100, MaxEvidenceAgeHours: 24}
			tc.change(&policy)
			_, err := service.SaveReleasePolicy(ctx, deployment.ID, policy, tc.expected)
			var invalid *app.Error
			if !errors.As(err, &invalid) || invalid.Code != "invalid_release_policy" {
				t.Fatalf("configuration accepted: %v", err)
			}
			loaded, err := service.GetReleasePolicy(ctx, deployment.ID)
			if err != nil || loaded.Revision != 0 {
				t.Fatalf("invalid policy persisted: %+v, %v", loaded, err)
			}
		})
	}
}

func TestReleasePolicyMigrationPreservesExistingAlias(t *testing.T) {
	t.Setenv("RANK_TEST_DATABASE_URL", "") // This test reconstructs SQLite schema v17.
	service, store, dbPath := testService(t, false)
	defer func() { store.Close() }()
	ctx := context.Background()
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{TenantID: "migration-tests", Name: "existing", AgentFamilyID: "agent-research-demo", Policy: testDeploymentPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{DeploymentID: deployment.ID, Alias: "production", AgentVersionID: "agent-research-demo-v1", PolicyResult: domain.ReleasePolicyManualOverride, Actor: "local", Reason: "existing release"})
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the v17 schema while retaining its deployment and release rows.
	if _, err = store.DB().ExecContext(ctx, "DROP TABLE release_decisions; DROP TABLE release_policies; DELETE FROM schema_migrations WHERE version=18"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = testdb.Open(t, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	service = app.NewService(store, app.Options{})
	resolved, err := service.ResolveDeploymentReference(ctx, "migration-tests", "existing@production")
	if err != nil || resolved.ReleaseRevision.ID != release.ID {
		t.Fatalf("migration changed alias: %v", err)
	}
	policy, err := service.GetReleasePolicy(ctx, deployment.ID)
	if err != nil || policy.Revision != 0 {
		t.Fatalf("migration invented policy: %+v %v", policy, err)
	}
	decisions, err := service.ListReleaseDecisions(ctx, deployment.ID, "production", 0)
	if err != nil || len(decisions.Items) != 0 {
		t.Fatalf("migration invented historical decisions: %+v %v", decisions, err)
	}
	if _, err = service.SaveReleasePolicy(ctx, deployment.ID, domain.ReleasePolicy{DatasetVersionID: "dataset-web-research-v3", MinPassRate: 80, MinValidTrialRate: 100, MaxEvidenceAgeHours: 24}, 0); err != nil {
		t.Fatal(err)
	}
}
