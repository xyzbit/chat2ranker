package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type StartReleaseTaskInput struct {
	ReleaseDraftInput
	ExpectedReleaseRevisionID domain.ReleaseRevisionID `json:"expectedReleaseRevisionId"`
	ExpectedPolicyRevision    int64                    `json:"expectedPolicyRevision"`
	ExpectedCandidateRunID    string                   `json:"expectedCandidateRunId"`
	ExpectedCurrentRunID      string                   `json:"expectedCurrentRunId"`
	RunRegression             *bool                    `json:"runRegression"`
	TrialCount                int                      `json:"trialCount"`
	ExperimentID              string                   `json:"experimentId"`
	ReviewCommandID           string                   `json:"reviewCommandId"`
	Actor                     string                   `json:"actor"`
	Reason                    string                   `json:"reason"`
}

func (s *Service) StartReleaseTask(ctx context.Context, input StartReleaseTaskInput, key string) (domain.ReleaseTask, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(input.Actor) == "" {
		return domain.ReleaseTask{}, problem(400, "release_request_required", "发布需要请求标识和操作者")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return domain.ReleaseTask{}, err
	}
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	if existing, err := s.repo.GetReleaseTaskByKey(ctx, input.DeploymentID, key); err == nil {
		if existing.RequestHash != hash {
			return domain.ReleaseTask{}, domain.ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.ReleaseTask{}, err
	}
	prepared := map[string]domain.JudgeConfig{}
	initial, err := s.releaseDraft(ctx, s.repo, input.ReleaseDraftInput)
	if err != nil {
		return domain.ReleaseTask{}, err
	}
	if input.RunRegression == nil || *input.RunRegression {
		if initial.RegressionDatasetVersionID == "" {
			return domain.ReleaseTask{}, problem(400, "regression_dataset_required", "请选择发布回归集，或关闭本次回归")
		}
		prepared, err = s.prepareEvaluation(ctx, initial.RegressionDatasetVersionID, []string{initial.AgentVersionID})
		if err != nil {
			return domain.ReleaseTask{}, err
		}
	}
	var task domain.ReleaseTask
	var dispatch string
	err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		existing, err := repo.GetReleaseTaskByKey(ctx, input.DeploymentID, key)
		if err == nil {
			if existing.RequestHash != hash {
				return domain.ErrConflict
			}
			task = existing
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		draft, err := s.releaseDraft(ctx, repo, input.ReleaseDraftInput)
		if err != nil {
			return err
		}
		evidenceID := func(e *domain.EvaluationEvidence) string {
			if e == nil {
				return ""
			}
			return e.RunID
		}
		if draft.ExpectedReleaseRevisionID != input.ExpectedReleaseRevisionID || draft.Policy.Revision != input.ExpectedPolicyRevision || evidenceID(draft.CandidateEvidence) != input.ExpectedCandidateRunID || evidenceID(draft.CurrentEvidence) != input.ExpectedCurrentRunID {
			return problem(409, "release_review_changed", "发布版本、规则或对比结果已变化，请刷新确认")
		}
		trials, err := normalizeTrialCount(input.TrialCount)
		if err != nil {
			return err
		}
		regression := input.RunRegression == nil || *input.RunRegression
		if regression && draft.RegressionDatasetVersionID == "" {
			return problem(400, "regression_dataset_required", "请选择发布回归集，或关闭本次回归")
		}
		if input.ExperimentID != "" {
			if _, err := repo.GetExperiment(ctx, input.ExperimentID); err != nil {
				return err
			}
		}
		if input.ReviewCommandID != "" {
			events, err := repo.ListControlEvents(ctx, input.ExperimentID)
			if err != nil {
				return err
			}
			matched := false
			for _, event := range events {
				if event.CommandID != input.ReviewCommandID || event.Type != "a2ui/prepare_release" {
					continue
				}
				var review struct {
					DeploymentID   domain.DeploymentID `json:"deploymentId"`
					Alias          string              `json:"alias"`
					AgentVersionID string              `json:"candidateAgentVersionId"`
				}
				if err := json.Unmarshal(event.Payload, &review); err != nil {
					return err
				}
				matched = review.DeploymentID == input.DeploymentID && review.Alias == input.Alias && review.AgentVersionID == input.AgentVersionID
			}
			if !matched {
				return problem(409, "release_review_mismatch", "发布确认与当前对话不匹配")
			}
		}
		now := s.now().UTC()
		task = domain.ReleaseTask{ID: domain.ReleaseTaskID(newServingID("release-task")), ReleaseDraft: draft, IdempotencyKey: key, RequestHash: hash, ExperimentID: input.ExperimentID, RunRegression: regression, TrialCount: trials, Status: "ready", Actor: strings.TrimSpace(input.Actor), Reason: strings.TrimSpace(input.Reason), CreatedAt: now, UpdatedAt: now}
		task.ReviewCommandID = input.ReviewCommandID
		if regression {
			dataset, err := repo.GetDatasetVersion(ctx, draft.RegressionDatasetVersionID)
			if err != nil {
				return err
			}
			agent, err := repo.GetAgentVersion(ctx, draft.AgentVersionID)
			if err != nil {
				return err
			}
			if _, ok := prepared[agent.ID]; !ok || dataset.ID != initial.RegressionDatasetVersionID {
				return problem(409, "release_review_changed", "发布配置已变化，请刷新确认")
			}
			experimentID := newID("exp")
			experiment := domain.Experiment{ID: experimentID, Title: "发布回归", DatasetVersionID: dataset.ID, AgentVersionID: agent.ID, ControlSessionID: "control-" + experimentID, CreatedAt: now, UpdatedAt: now}
			if err := repo.CreateExperiment(ctx, experiment, domain.Message{ID: newID("msg"), ExperimentID: experiment.ID, Role: "assistant", Content: "发布回归已创建。", CreatedAt: now}); err != nil {
				return err
			}
			run, items, trials, event := s.buildRun(experiment.ID, string(task.ID), "", dataset, agent, trials, now, prepared[agent.ID])
			if err := s.createRunWithDispatch(ctx, repo, run, items, trials, event); err != nil {
				return err
			}
			task.RunID, task.Status, dispatch = run.ID, "regression", run.ID
		}
		return repo.CreateReleaseTask(ctx, task)
	})
	if err == nil && dispatch != "" && s.workers {
		s.dispatch(dispatch)
	}
	return task, err
}

func (s *Service) GetReleaseTask(ctx context.Context, id domain.ReleaseTaskID) (domain.ReleaseTask, error) {
	return s.repo.GetReleaseTask(ctx, id)
}

func (s *Service) ListReleaseTasks(ctx context.Context, deployment domain.DeploymentID, experiment string) ([]domain.ReleaseTask, error) {
	if deployment == "" && experiment == "" {
		return nil, problem(400, "release_scope_required", "请指定部署或对话")
	}
	return s.repo.ListReleaseTasks(ctx, deployment, experiment)
}

// ReconcileReleaseTasks resumes durable tasks; publishing and task completion share one transaction.
func (s *Service) ReconcileReleaseTasks(ctx context.Context) error {
	tasks, err := s.repo.ListReleaseTasks(ctx, "", "")
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := s.advanceReleaseTask(ctx, task.ID, false, ""); err != nil && !errors.Is(err, domain.ErrConflict) {
			return err
		}
	}
	return nil
}

func (s *Service) RunReleaseDispatcher(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ReconcileReleaseTasks(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) advanceReleaseTask(ctx context.Context, id domain.ReleaseTaskID, override bool, reason string) error {
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		task, err := repo.GetReleaseTask(ctx, id)
		if err != nil {
			return err
		}
		previous := task.Status
		if override {
			if previous != "blocked" || strings.TrimSpace(reason) == "" {
				return problem(400, "override_reason_required", "仅回归未通过的发布可填写原因后继续")
			}
			task.Reason = strings.TrimSpace(reason)
		} else if previous != "ready" && previous != "regression" {
			return nil
		}
		if previous == "regression" {
			run, err := repo.GetRun(ctx, task.RunID)
			if err != nil {
				return err
			}
			if domain.IsActiveRun(run.Status) {
				return nil
			}
		}
		now := s.now().UTC()
		task.UpdatedAt = now
		alias, err := repo.GetDeploymentAlias(ctx, task.DeploymentID, task.Alias)
		if err != nil {
			return err
		}
		policy, err := repo.GetReleasePolicy(ctx, task.DeploymentID)
		if err != nil {
			return err
		}
		if alias.CurrentReleaseRevisionID != task.ExpectedReleaseRevisionID || policy.Revision != task.Policy.Revision {
			task.Status, task.Error = "conflict", "线上版本或发布规则已变化，请重新确认发布"
			return repo.SaveReleaseTask(ctx, task, previous)
		}
		mode := "regression_skipped"
		if task.RunRegression {
			preflight, err := evaluateReleaseWithPolicy(ctx, repo, PromoteDeploymentInput{DeploymentID: task.DeploymentID, Alias: task.Alias, AgentVersionID: task.AgentVersionID, EvidenceRunID: task.RunID}, now, &task.Policy)
			if err != nil {
				return err
			}
			task.Evaluation = &preflight.Evaluation
			mode = domain.ReleasePolicyPassed
			if !preflight.Evaluation.Passed && !override {
				task.Status, task.Error = "blocked", "回归测试未通过，尚未发布"
				if err := repo.AppendReleaseDecision(ctx, taskDecision(task, "rejected", now)); err != nil {
					return err
				}
				return repo.SaveReleaseTask(ctx, task, previous)
			}
		}
		if override {
			mode = domain.ReleasePolicyManualOverride
		}
		release, err := repo.AppendReleaseRevision(ctx, domain.ReleaseRevision{ID: domain.ReleaseRevisionID(newServingID("rel")), DeploymentAliasID: alias.ID, AgentVersionID: task.AgentVersionID, EvidenceRunID: task.RunID, PolicyResult: mode, Actor: task.Actor, Reason: task.Reason, CreatedAt: now}, task.ExpectedReleaseRevisionID)
		if err != nil {
			return err
		}
		task.ReleaseRevisionID, task.Status, task.Error = release.ID, "published", ""
		if err := repo.AppendReleaseDecision(ctx, taskDecision(task, mode, now)); err != nil {
			return err
		}
		return repo.SaveReleaseTask(ctx, task, previous)
	})
}

func taskDecision(task domain.ReleaseTask, result string, at time.Time) domain.ReleaseDecision {
	evaluation := domain.ReleaseEvaluation{Policy: task.Policy, CheckedAt: at, Checks: []domain.ReleaseCheck{}}
	if task.Evaluation != nil {
		evaluation = *task.Evaluation
	}
	return domain.ReleaseDecision{ID: domain.ReleaseDecisionID(newServingID("decision")), DeploymentID: task.DeploymentID, Alias: task.Alias, AgentVersionID: task.AgentVersionID, ReleaseRevisionID: task.ReleaseRevisionID, Actor: task.Actor, Reason: task.Reason, Result: result, Evaluation: evaluation}
}

func (s *Service) OverrideReleaseTask(ctx context.Context, id domain.ReleaseTaskID, reason string) (domain.ReleaseTask, error) {
	if err := s.advanceReleaseTask(ctx, id, true, reason); err != nil {
		return domain.ReleaseTask{}, err
	}
	return s.repo.GetReleaseTask(ctx, id)
}

func (s *Service) CancelReleaseTask(ctx context.Context, id domain.ReleaseTaskID) (domain.ReleaseTask, error) {
	var task domain.ReleaseTask
	err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		var err error
		task, err = repo.GetReleaseTask(ctx, id)
		if err != nil {
			return err
		}
		if task.Status == "cancelled" {
			return nil
		}
		if task.Status == "published" {
			return domain.ErrConflict
		}
		previous := task.Status
		task.Status, task.UpdatedAt = "cancelled", s.now().UTC()
		if task.RunID != "" {
			run, err := repo.GetRun(ctx, task.RunID)
			if err != nil {
				return err
			}
			if domain.IsActiveRun(run.Status) {
				if err := repo.RequestRunCancellation(ctx, task.RunID); err != nil {
					return err
				}
			}
		}
		return repo.SaveReleaseTask(ctx, task, previous)
	})
	if err != nil {
		return task, err
	}
	if task.RunID != "" {
		run, getErr := s.repo.GetRun(ctx, task.RunID)
		if getErr != nil {
			return task, getErr
		}
		if domain.IsActiveRun(run.Status) || run.Status == domain.RunCancelled {
			_, err = s.CancelRun(ctx, task.RunID)
		}
	}
	return task, err
}
