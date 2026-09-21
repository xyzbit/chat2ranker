package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
	"sort"
)

type ReleaseDraftInput struct {
	DeploymentID                domain.DeploymentID `json:"deploymentId"`
	Alias                       string              `json:"alias"`
	AgentVersionID              string              `json:"agentVersionId"`
	DevelopmentDatasetVersionID *string             `json:"developmentDatasetVersionId"`
	RegressionDatasetVersionID  *string             `json:"regressionDatasetVersionId"`
}

func (s *Service) PrepareReleaseDraft(ctx context.Context, input ReleaseDraftInput) (domain.ReleaseDraft, error) {
	var draft domain.ReleaseDraft
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		var err error
		draft, err = s.releaseDraft(ctx, repo, input)
		return err
	})
	return draft, err
}

func (s *Service) releaseDraft(ctx context.Context, repo domain.Repository, input ReleaseDraftInput) (domain.ReleaseDraft, error) {
	preflight, err := evaluateRelease(ctx, repo, PromoteDeploymentInput{DeploymentID: input.DeploymentID, Alias: input.Alias, AgentVersionID: input.AgentVersionID}, s.now().UTC())
	if err != nil {
		return domain.ReleaseDraft{}, err
	}
	agent, err := repo.GetAgentVersion(ctx, input.AgentVersionID)
	if err != nil {
		return domain.ReleaseDraft{}, err
	}
	defaults, err := repo.GetAgentEvaluationDefaults(ctx, agent.FamilyID)
	if err != nil {
		return domain.ReleaseDraft{}, err
	}
	draft := domain.ReleaseDraft{DeploymentID: input.DeploymentID, Alias: input.Alias, AgentVersionID: agent.ID, CurrentAgentVersionID: preflight.CurrentAgentVersionID, ExpectedReleaseRevisionID: preflight.ExpectedReleaseRevisionID, Policy: preflight.Evaluation.Policy}
	if draft.Policy.Revision == 0 {
		draft.Policy.MinPassRate, draft.Policy.MinValidTrialRate, draft.Policy.MaxEvidenceAgeHours = 100, 100, 24
	}
	for _, field := range []struct {
		requested *string
		family    string
		target    *string
	}{
		{input.DevelopmentDatasetVersionID, defaults.DevelopmentDatasetID, &draft.DevelopmentDatasetVersionID},
		{input.RegressionDatasetVersionID, defaults.RegressionDatasetID, &draft.RegressionDatasetVersionID},
	} {
		if field.requested != nil {
			*field.target = *field.requested
		} else {
			*field.target, err = latestDatasetVersion(ctx, repo, field.family)
			if err != nil {
				return draft, err
			}
		}
		if *field.target != "" {
			dataset, err := repo.GetDatasetVersion(ctx, *field.target)
			if err != nil {
				return draft, err
			}
			if len(dataset.Cases) == 0 {
				return draft, problem(400, "empty_dataset", "测试集至少需要一个用例")
			}
		}
	}
	draft.Policy.DatasetVersionID = draft.RegressionDatasetVersionID
	candidates, err := developmentEvidence(ctx, repo, agent.ID, draft.DevelopmentDatasetVersionID)
	if err != nil {
		return draft, err
	}
	if len(candidates) > 0 {
		draft.CandidateEvidence = &candidates[0]
	}
	if draft.CurrentAgentVersionID != "" {
		current, err := developmentEvidence(ctx, repo, draft.CurrentAgentVersionID, draft.DevelopmentDatasetVersionID)
		if err != nil {
			return draft, err
		}
		for i := range candidates {
			for j := range current {
				if candidates[i].DatasetVersionID == current[j].DatasetVersionID && candidates[i].EvaluatorDigest == current[j].EvaluatorDigest && candidates[i].TrialCount == current[j].TrialCount {
					draft.CandidateEvidence, draft.CurrentEvidence, draft.Comparable = &candidates[i], &current[j], true
					return draft, nil
				}
			}
		}
	}
	return draft, nil
}

func developmentEvidence(ctx context.Context, repo domain.Repository, version, dataset string) ([]domain.EvaluationEvidence, error) {
	items := []domain.EvaluationEvidence{}
	if dataset == "" {
		return items, nil
	}
	summaries, err := repo.ListAgentEvaluationSummaries(ctx, version)
	if err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		if summary.Status != domain.RunComplete {
			continue
		}
		run, err := repo.GetRun(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		if run.DatasetSnapshot.ID != dataset || run.CompletedAt == nil || run.ValidTrials == 0 || run.CompletedTrials != run.ScheduledTrials {
			continue
		}
		payload, err := json.Marshal(run.EvaluatorSnapshot)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(payload)
		items = append(items, domain.EvaluationEvidence{RunID: run.ID, DatasetVersionID: dataset, EvaluatorVersionID: run.EvaluatorSnapshot.ID, EvaluatorDigest: hex.EncodeToString(digest[:]), AgentVersionID: version, TrialCount: run.TrialCount, Passed: run.Passed, ValidTrials: run.ValidTrials, ScheduledTrials: run.ScheduledTrials, Cost: run.Cost, CostKnown: run.CostKnown, DurationMs: int64(run.DurationMs), CompletedAt: run.CompletedAt})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CompletedAt.After(*items[j].CompletedAt) })
	return items, nil
}

// StartReleaseComparison creates its workspace and child Runs atomically.
func (s *Service) StartReleaseComparison(ctx context.Context, input ReleaseDraftInput) (domain.ExperimentView, error) {
	initial, err := s.releaseDraft(ctx, s.repo, input)
	if err != nil {
		return domain.ExperimentView{}, err
	}
	if initial.DevelopmentDatasetVersionID == "" {
		return domain.ExperimentView{}, problem(400, "development_dataset_required", "请选择开发评测集")
	}
	ids := []string{initial.AgentVersionID}
	if initial.CurrentAgentVersionID != "" && initial.CurrentAgentVersionID != initial.AgentVersionID {
		ids = append(ids, initial.CurrentAgentVersionID)
	}
	prepared, err := s.prepareEvaluation(ctx, initial.DevelopmentDatasetVersionID, ids)
	if err != nil {
		return domain.ExperimentView{}, err
	}
	var experiment domain.Experiment
	var runIDs []string
	err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		draft, err := s.releaseDraft(ctx, repo, input)
		if err != nil {
			return err
		}
		if draft.DevelopmentDatasetVersionID == "" {
			return problem(400, "development_dataset_required", "请选择开发评测集")
		}
		dataset, err := repo.GetDatasetVersion(ctx, draft.DevelopmentDatasetVersionID)
		if err != nil {
			return err
		}
		ids := []string{draft.AgentVersionID}
		if draft.CurrentAgentVersionID != "" && draft.CurrentAgentVersionID != draft.AgentVersionID {
			ids = append(ids, draft.CurrentAgentVersionID)
		}
		agents := make([]domain.AgentVersion, 0, len(ids))
		for _, id := range ids {
			agent, err := repo.GetAgentVersion(ctx, id)
			if err != nil {
				return err
			}
			if _, ok := prepared[agent.ID]; !ok || dataset.ID != initial.DevelopmentDatasetVersionID {
				return problem(409, "release_review_changed", "发布配置已变化，请刷新确认")
			}
			agents = append(agents, agent)
		}
		now := s.now().UTC()
		id := newID("exp")
		experiment = domain.Experiment{ID: id, Title: "发布前版本对比", DatasetVersionID: dataset.ID, AgentVersionID: draft.AgentVersionID, ControlSessionID: "control-" + id, CreatedAt: now, UpdatedAt: now}
		if err := repo.CreateExperiment(ctx, experiment, domain.Message{ID: newID("msg"), ExperimentID: id, Role: "assistant", Content: "版本对比已开始。", CreatedAt: now}); err != nil {
			return err
		}
		groupID := ""
		if len(agents) > 1 {
			groupID = newID("group")
			if err := repo.CreateRunGroup(ctx, domain.RunGroup{ID: groupID, ExperimentID: id, IdempotencyKey: "release-comparison", DatasetVersionID: dataset.ID, AgentVersionIDs: ids, TrialCount: 1, Status: domain.RunQueued, CreatedAt: now}); err != nil {
				return err
			}
		}
		for _, agent := range agents {
			run, items, trials, event := s.buildRun(id, agent.ID, groupID, dataset, agent, 1, now, prepared[agent.ID])
			if err := s.createRunWithDispatch(ctx, repo, run, items, trials, event); err != nil {
				return err
			}
			runIDs = append(runIDs, run.ID)
		}
		return nil
	})
	if err != nil {
		return domain.ExperimentView{}, err
	}
	if s.workers {
		for _, id := range runIDs {
			s.dispatch(id)
		}
	}
	return s.GetExperiment(ctx, experiment.ID)
}
