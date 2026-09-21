package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/xyzbit/agentkiln/rank/backend/internal/app"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func TestDeploymentReleaseRegistryPinsAliasHistoryAndExactVersions(t *testing.T) {
	service, store, _ := testService(t, false)
	defer store.Close()
	ctx := context.Background()
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{
		TenantID: "tenant-one", AgentFamilyID: "agent-research-demo",
		Name: "customer-support", PolicyDigest: "sha256:policy-v1", Policy: testDeploymentPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertUUIDv7ID(t, string(deployment.ID), "deploy_")
	if len(deployment.Aliases) != 1 {
		t.Fatalf("deployment aliases = %d, want 1", len(deployment.Aliases))
	}
	if _, err := service.ResolveDeploymentReference(ctx, "tenant-one", "customer-support"); err == nil {
		t.Fatal("unpublished deployment resolved")
	}
	initial, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID: "agent-research-demo-v1", PolicyResult: domain.ReleasePolicyManualOverride,
		Actor: "operator", Reason: "initial reviewed release",
	})
	if err != nil {
		t.Fatal(err)
	}
	latestV1, err := service.ResolveDeploymentReference(ctx, "tenant-one", "customer-support")
	if err != nil {
		t.Fatal(err)
	}
	if latestV1.Kind != domain.ResolutionAlias || latestV1.AgentVersion.Version != 1 || latestV1.ReleaseRevision == nil || latestV1.ReleaseRevision.Revision != 1 {
		t.Fatalf("unexpected initial latest resolution: %#v", latestV1)
	}

	v2, err := service.CreateAgentVersion(ctx, "agent-research-demo", app.CreateAgentInput{
		RunnerType: "mock", Model: "deterministic-demo", Description: "v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	latestV2, err := service.ResolveDeploymentReference(ctx, "tenant-one", "customer-support")
	if err != nil {
		t.Fatal(err)
	}
	if latestV2.AgentVersion.ID != latestV1.AgentVersion.ID || latestV2.ReleaseRevision.ID != initial.ID {
		t.Fatalf("creating an Agent version changed the deployed version: %#v", latestV2)
	}
	historical, err := service.ListDeploymentsByAgentVersion(ctx, "tenant-one", latestV1.AgentVersion.ID)
	if err != nil || len(historical) != 1 || historical[0].ID != deployment.ID {
		t.Fatalf("historical version lost its deployment: %v, %v", historical, err)
	}
	foreign, err := service.ListDeploymentsByAgentVersion(ctx, "tenant-two", latestV1.AgentVersion.ID)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("version lookup crossed tenants: %v, %v", foreign, err)
	}
	exactV1, err := service.ResolveDeploymentReference(ctx, "tenant-one", "customer-support@v1")
	if err != nil {
		t.Fatal(err)
	}
	if exactV1.Kind != domain.ResolutionExact || exactV1.AgentVersion.Version != 1 || exactV1.ReleaseRevision != nil {
		t.Fatalf("exact resolution should not invent a release revision: %#v", exactV1)
	}

	productionV2, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID: v2.ID, PolicyResult: domain.ReleasePolicyManualOverride,
		Actor: "operator@example.com", Reason: "initial production release",
		ExpectedReleaseRevisionID: initial.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID: exactV1.AgentVersion.ID, PolicyResult: domain.ReleasePolicyManualOverride,
		Actor: "operator@example.com", Reason: "stale rollback",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale release did not conflict: %v", err)
	}
	rollback, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID:            exactV1.AgentVersion.ID,
		ExpectedReleaseRevisionID: productionV2.ID,
		PolicyResult:              domain.ReleasePolicyManualOverride,
		Actor:                     "operator@example.com", Reason: "rollback after regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rollback.Revision != 3 || rollback.PreviousReleaseRevisionID != productionV2.ID {
		t.Fatalf("rollback replaced history instead of appending: %#v", rollback)
	}
	production, err := service.ResolveDeploymentReference(ctx, "tenant-one", "customer-support")
	if err != nil {
		t.Fatal(err)
	}
	if production.AgentVersion.Version != 1 || production.ReleaseRevision.ID != rollback.ID {
		t.Fatalf("production did not resolve through rollback revision: %#v", production)
	}
	var revisions int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM release_revisions AS revision
		JOIN deployment_aliases AS alias ON alias.id = revision.deployment_alias_id
		JOIN deployments AS deployment ON deployment.id = alias.deployment_id
		WHERE deployment.public_id = $1
	`, deployment.ID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 3 {
		t.Fatalf("release revision count = %d, want 3; stale CAS may have persisted", revisions)
	}
}

func TestPolicyPassedReleaseRequiresCompletedMatchingEvaluation(t *testing.T) {
	service, store, _ := testService(t, true)
	defer store.Close()
	ctx := context.Background()
	deployment, err := service.CreateDeployment(ctx, app.CreateDeploymentInput{
		TenantID: "tenant-eval", AgentFamilyID: "agent-research-demo",
		Name: "research", PolicyDigest: "sha256:policy-v1", Policy: testDeploymentPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID: "agent-research-demo-v1", PolicyResult: domain.ReleasePolicyPassed,
		Actor: "release-controller",
	}); err == nil {
		t.Fatal("policy-passed release accepted no evaluation evidence")
	}
	experiment := configuredExperiment(t, service)
	created, err := service.StartRun(ctx, experiment.ID, "release-evidence")
	if err != nil {
		t.Fatal(err)
	}
	run := waitComplete(t, service, created.ID)
	if run.Status != domain.RunComplete || !run.EvaluationComplete {
		t.Fatalf("evaluation evidence is incomplete: %#v", run)
	}
	if _, err := service.SaveReleasePolicy(ctx, deployment.ID, domain.ReleasePolicy{DatasetVersionID: run.DatasetSnapshot.ID, MinPassRate: 80, MinValidTrialRate: 100, MaxEvidenceAgeHours: 168}, 0); err != nil {
		t.Fatal(err)
	}
	release, err := service.PromoteDeploymentAlias(ctx, app.PromoteDeploymentInput{
		DeploymentID: deployment.ID, Alias: app.DeploymentAliasProduction,
		AgentVersionID: "agent-research-demo-v1", EvidenceRunID: run.ID,
		PolicyResult: domain.ReleasePolicyPassed, Actor: "release-controller", ExpectedPolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if release.EvidenceRunID != run.ID || release.PolicyResult != domain.ReleasePolicyPassed {
		t.Fatalf("release lost evaluation evidence: %#v", release)
	}
}

func testDeploymentPolicy() domain.DeploymentPolicy {
	return domain.DeploymentPolicy{RequestsPerMinute: 60, MaxConcurrentResponses: 4, ExecutionTimeoutMs: 60_000}
}

func assertUUIDv7ID(t *testing.T, value, prefix string) {
	t.Helper()
	if !strings.HasPrefix(value, prefix) {
		t.Fatalf("id %q does not have prefix %q", value, prefix)
	}
	parsed, err := uuid.Parse(strings.TrimPrefix(value, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Version() != 7 {
		t.Fatalf("id %q uses UUID version %d, want 7", value, parsed.Version())
	}
}
