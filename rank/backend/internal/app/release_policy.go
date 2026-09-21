package app

import (
	"context"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Service) GetReleasePolicy(ctx context.Context, id domain.DeploymentID) (domain.ReleasePolicy, error) {
	if _, err := s.repo.GetDeployment(ctx, id); err != nil {
		return domain.ReleasePolicy{}, err
	}
	return s.repo.GetReleasePolicy(ctx, id)
}

// SaveReleasePolicy changes future decisions without altering frozen release evidence.
func (s *Service) SaveReleasePolicy(ctx context.Context, id domain.DeploymentID, policy domain.ReleasePolicy, expected int64) (domain.ReleasePolicy, error) {
	var saved domain.ReleasePolicy
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		var err error
		saved, err = s.saveReleasePolicy(ctx, repo, id, policy, expected)
		return err
	})
	return saved, err
}

func (s *Service) saveReleasePolicy(ctx context.Context, repo domain.Repository, id domain.DeploymentID, policy domain.ReleasePolicy, expected int64) (domain.ReleasePolicy, error) {
	policy.DatasetVersionID = strings.TrimSpace(policy.DatasetVersionID)
	if policy.MinPassRate < 0 || policy.MinPassRate > 100 || policy.MinValidTrialRate < 1 || policy.MinValidTrialRate > 100 || policy.MaxEvidenceAgeHours < 1 || expected < 0 {
		return domain.ReleasePolicy{}, problem(400, "invalid_release_policy", "发布门槛需要 0-100% 通过率、1-100% 有效覆盖率和正数有效期")
	}
	policy.Revision, policy.UpdatedAt = expected+1, s.now().UTC()
	if _, err := repo.GetDeployment(ctx, id); err != nil {
		return domain.ReleasePolicy{}, err
	}
	if policy.DatasetVersionID != "" {
		dataset, err := repo.GetDatasetVersion(ctx, policy.DatasetVersionID)
		if err != nil {
			return domain.ReleasePolicy{}, err
		}
		if len(dataset.Cases) == 0 {
			return domain.ReleasePolicy{}, problem(400, "empty_release_dataset", "发布评测集必须包含用例")
		}
	}
	return policy, repo.SaveReleasePolicy(ctx, id, policy, expected)
}

// PreflightRelease reads one consistent policy, alias, and evidence snapshot without publishing.
func (s *Service) PreflightRelease(ctx context.Context, input PromoteDeploymentInput) (domain.ReleasePreflight, error) {
	var result domain.ReleasePreflight
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		var err error
		result, err = evaluateRelease(ctx, repo, input, s.now().UTC())
		return err
	})
	return result, err
}

func evaluateRelease(ctx context.Context, repo domain.Repository, input PromoteDeploymentInput, now time.Time) (domain.ReleasePreflight, error) {
	return evaluateReleaseWithPolicy(ctx, repo, input, now, nil)
}

func evaluateReleaseWithPolicy(ctx context.Context, repo domain.Repository, input PromoteDeploymentInput, now time.Time, frozen *domain.ReleasePolicy) (domain.ReleasePreflight, error) {
	result := domain.ReleasePreflight{DeploymentID: input.DeploymentID, Alias: input.Alias, AgentVersionID: input.AgentVersionID}
	if input.Alias == DeploymentAliasLatest {
		return result, problem(400, "latest_alias_managed", "latest 为历史只读别名，请通过部署发布当前版本")
	}
	deployment, err := repo.GetDeployment(ctx, input.DeploymentID)
	if err != nil {
		return result, err
	}
	alias, err := repo.GetDeploymentAlias(ctx, input.DeploymentID, input.Alias)
	if err != nil {
		return result, err
	}
	version, err := repo.GetAgentVersion(ctx, input.AgentVersionID)
	if err != nil {
		return result, err
	}
	if version.FamilyID != deployment.AgentFamilyID {
		return result, problem(400, "release_agent_family_mismatch", "目标 Agent 版本不属于 Deployment 的 Agent 资产")
	}
	result.ExpectedReleaseRevisionID = alias.CurrentReleaseRevisionID
	result.DeploymentAliasID = alias.ID
	if alias.CurrentRelease != nil {
		result.CurrentAgentVersionID = alias.CurrentRelease.AgentVersionID
	} else if input.Alias == DeploymentAliasProduction {
		if current := publishedDeploymentAlias(deployment); current != nil {
			result.CurrentAgentVersionID = current.CurrentRelease.AgentVersionID
		}
	}
	policy, err := repo.GetReleasePolicy(ctx, input.DeploymentID)
	if err != nil {
		return result, err
	}
	if frozen != nil {
		policy = *frozen
	}
	evaluation := domain.ReleaseEvaluation{Policy: policy, EvidenceRunID: input.EvidenceRunID, Passed: true, CheckedAt: now, Checks: []domain.ReleaseCheck{}}
	check := func(code string, passed bool, actual, required float64) {
		evaluation.Checks = append(evaluation.Checks, domain.ReleaseCheck{Code: code, Passed: passed, Actual: actual, Required: required})
		evaluation.Passed = evaluation.Passed && passed
	}
	check("policy_configured", frozen != nil || policy.Revision > 0, 0, 0)
	check("evidence_selected", input.EvidenceRunID != "", 0, 0)
	if input.EvidenceRunID != "" {
		run, err := repo.GetRun(ctx, input.EvidenceRunID)
		if err != nil {
			return result, err
		}
		evaluation.DatasetVersionID, evaluation.EvaluatorVersionID, evaluation.EvidenceCompletedAt = run.DatasetSnapshot.ID, run.EvaluatorSnapshot.ID, run.CompletedAt
		check("agent_version", run.AgentSnapshot.ID == version.ID, 0, 0)
		check("run_complete", run.Status == domain.RunComplete && run.CompletedAt != nil && run.ScheduledTrials > 0 && run.CompletedTrials == run.ScheduledTrials, 0, 0)
		check("valid_trials", run.ValidTrials > 0, float64(run.ValidTrials), 1)
		if frozen != nil || policy.Revision > 0 {
			check("dataset_version", run.DatasetSnapshot.ID == policy.DatasetVersionID, 0, 0)
			coverage, passRate := 0.0, 0.0
			if run.ScheduledTrials > 0 {
				coverage = float64(run.ValidTrials) * 100 / float64(run.ScheduledTrials)
			}
			if run.ValidTrials > 0 {
				passRate = float64(run.Passed) * 100 / float64(run.ValidTrials)
			}
			check("valid_trial_rate", coverage >= float64(policy.MinValidTrialRate), coverage, float64(policy.MinValidTrialRate))
			check("pass_rate", passRate >= float64(policy.MinPassRate), passRate, float64(policy.MinPassRate))
			age := 0.0
			if run.CompletedAt != nil {
				age = now.Sub(*run.CompletedAt).Hours()
			}
			check("evidence_age", run.CompletedAt != nil && age >= 0 && age <= float64(policy.MaxEvidenceAgeHours), age, float64(policy.MaxEvidenceAgeHours))
		}
	}
	result.Evaluation = evaluation
	return result, nil
}

func (s *Service) ListReleaseDecisions(ctx context.Context, id domain.DeploymentID, alias string, before int64) (domain.ReleaseDecisionPage, error) {
	if before < 0 {
		return domain.ReleaseDecisionPage{}, problem(400, "invalid_cursor", "游标必须为非负数")
	}
	if _, err := s.repo.GetDeploymentAlias(ctx, id, alias); err != nil {
		return domain.ReleaseDecisionPage{}, err
	}
	return s.repo.ListReleaseDecisions(ctx, id, alias, before, 30)
}
