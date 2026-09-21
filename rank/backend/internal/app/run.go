package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"math"
	"sync"
	"time"

	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

const (
	defaultTrialCount = 5
	maxTrialCount     = 20
)

type RunOptions struct {
	TrialCount int `json:"trialCount"`
}

type ComparisonOptions struct {
	AgentVersionIDs []string `json:"agentVersionIds"`
	TrialCount      int      `json:"trialCount"`
}

type ComparisonResult struct {
	Group domain.RunGroup `json:"runGroup"`
	Runs  []domain.Run    `json:"runs"`
}

func normalizeTrialCount(value int) (int, error) {
	if value == 0 {
		return defaultTrialCount, nil
	}
	if value < 1 || value > maxTrialCount {
		return 0, problem(400, "invalid_trial_count", "每个用例的运行次数必须在 1 到 20 之间")
	}
	return value, nil
}

func (s *Service) StartRun(ctx context.Context, experimentID, idempotencyKey string, requested ...RunOptions) (domain.Run, error) {
	if idempotencyKey == "" {
		idempotencyKey = newID("request")
	}
	options := RunOptions{}
	if len(requested) > 0 {
		options = requested[0]
	}
	trialCount, err := normalizeTrialCount(options.TrialCount)
	if err != nil {
		return domain.Run{}, err
	}
	if existing, err := s.repo.GetRunByIdempotencyKey(ctx, experimentID, idempotencyKey); err == nil {
		if existing.TrialCount != trialCount {
			return domain.Run{}, problem(409, "idempotency_conflict", "相同幂等键已用于不同的运行次数")
		}
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Run{}, err
	}
	selected, err := s.repo.GetExperiment(ctx, experimentID)
	if err != nil {
		return domain.Run{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	if selected.DatasetVersionID == "" || selected.AgentVersionID == "" {
		return domain.Run{}, problem(409, "experiment_not_ready", "请先选择测试集和 Agent")
	}
	prepared, err := s.prepareEvaluation(ctx, selected.DatasetVersionID, []string{selected.AgentVersionID})
	if err != nil {
		return domain.Run{}, err
	}
	var created domain.Run
	replayed := false
	err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		existing, err := repo.GetRunByIdempotencyKey(ctx, experimentID, idempotencyKey)
		if err == nil {
			if existing.TrialCount != trialCount {
				return problem(409, "idempotency_conflict", "相同幂等键已用于不同的运行次数")
			}
			created, replayed = existing, true
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		experiment, err := repo.GetExperiment(ctx, experimentID)
		if err != nil {
			return mapNotFound(err, "experiment_not_found", "实验不存在")
		}
		if experiment.DatasetVersionID == "" || experiment.AgentVersionID == "" {
			return problem(409, "experiment_not_ready", "请先选择测试集和 Agent")
		}
		runs, err := repo.ListRunsByExperiment(ctx, experimentID)
		if err != nil {
			return err
		}
		for _, run := range runs {
			if domain.IsActiveRun(run.Status) {
				return problem(409, "run_already_active", "当前实验已有运行中的任务")
			}
		}
		dataset, err := repo.GetDatasetVersion(ctx, experiment.DatasetVersionID)
		if err != nil {
			return err
		}
		agent, err := repo.GetAgentVersion(ctx, experiment.AgentVersionID)
		if err != nil {
			return err
		}
		if dataset.ID != selected.DatasetVersionID || agent.ID != selected.AgentVersionID {
			return problem(409, "experiment_changed", "实验配置已变化，请重新开始")
		}
		var items []domain.RunItem
		var trials []domain.RunTrial
		var event domain.RunEvent
		created, items, trials, event = s.buildRun(experimentID, idempotencyKey, "", dataset, agent, trialCount, s.now().UTC(), prepared[agent.ID])
		return s.createRunWithDispatch(ctx, repo, created, items, trials, event)
	})
	if err != nil {
		return domain.Run{}, err
	}
	if !replayed && s.workers {
		s.dispatch(created.ID)
	}
	return s.repo.GetRun(ctx, created.ID)
}

func (s *Service) StartComparison(ctx context.Context, experimentID, idempotencyKey string, options ComparisonOptions) (ComparisonResult, error) {
	if idempotencyKey == "" {
		idempotencyKey = newID("request")
	}
	trialCount, err := normalizeTrialCount(options.TrialCount)
	if err != nil {
		return ComparisonResult{}, err
	}
	agentIDs := uniqueStrings(options.AgentVersionIDs)
	if len(agentIDs) < 2 || len(agentIDs) > 4 {
		return ComparisonResult{}, problem(400, "invalid_comparison_agents", "对比运行需要选择 2 到 4 个 Agent 版本")
	}
	if existing, err := s.repo.GetRunGroupByIdempotencyKey(ctx, experimentID, idempotencyKey); err == nil {
		if existing.TrialCount != trialCount || !sameStrings(existing.AgentVersionIDs, agentIDs) {
			return ComparisonResult{}, problem(409, "idempotency_conflict", "相同幂等键已用于不同的对比运行")
		}
		result := ComparisonResult{Group: existing, Runs: []domain.Run{}}
		for _, id := range existing.RunIDs {
			run, err := s.repo.GetRun(ctx, id)
			if err != nil {
				return ComparisonResult{}, err
			}
			result.Runs = append(result.Runs, run)
		}
		return result, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return ComparisonResult{}, err
	}
	selected, err := s.repo.GetExperiment(ctx, experimentID)
	if err != nil {
		return ComparisonResult{}, mapNotFound(err, "experiment_not_found", "实验不存在")
	}
	if selected.DatasetVersionID == "" {
		return ComparisonResult{}, problem(409, "experiment_not_ready", "请先选择测试集")
	}
	prepared, err := s.prepareEvaluation(ctx, selected.DatasetVersionID, agentIDs)
	if err != nil {
		return ComparisonResult{}, err
	}
	var group domain.RunGroup
	var runs []domain.Run
	replayed := false
	err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		existing, err := repo.GetRunGroupByIdempotencyKey(ctx, experimentID, idempotencyKey)
		if err == nil {
			if existing.TrialCount != trialCount || !sameStrings(existing.AgentVersionIDs, agentIDs) {
				return problem(409, "idempotency_conflict", "相同幂等键已用于不同的对比运行")
			}
			group, replayed = existing, true
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		experiment, err := repo.GetExperiment(ctx, experimentID)
		if err != nil {
			return mapNotFound(err, "experiment_not_found", "实验不存在")
		}
		if experiment.DatasetVersionID == "" {
			return problem(409, "experiment_not_ready", "请先选择测试集")
		}
		existingRuns, err := repo.ListRunsByExperiment(ctx, experimentID)
		if err != nil {
			return err
		}
		for _, run := range existingRuns {
			if domain.IsActiveRun(run.Status) {
				return problem(409, "run_already_active", "当前实验已有运行中的任务")
			}
		}
		dataset, err := repo.GetDatasetVersion(ctx, experiment.DatasetVersionID)
		if err != nil {
			return err
		}
		agents := make([]domain.AgentVersion, 0, len(agentIDs))
		for _, id := range agentIDs {
			agent, err := repo.GetAgentVersion(ctx, id)
			if err != nil {
				return mapNotFound(err, "agent_version_not_found", "Agent 版本不存在")
			}
			if dataset.ID != selected.DatasetVersionID {
				return problem(409, "experiment_changed", "实验配置已变化，请重新开始")
			}
			agents = append(agents, agent)
		}
		now := s.now().UTC()
		group = domain.RunGroup{ID: newID("group"), ExperimentID: experimentID, IdempotencyKey: idempotencyKey, DatasetVersionID: dataset.ID, AgentVersionIDs: agentIDs, TrialCount: trialCount, Status: domain.RunQueued, CreatedAt: now}
		if err := repo.CreateRunGroup(ctx, group); err != nil {
			return err
		}
		for index, agent := range agents {
			run, items, trials, event := s.buildRun(experimentID, fmt.Sprintf("%s/%d", idempotencyKey, index+1), group.ID, dataset, agent, trialCount, now, prepared[agent.ID])
			if err := s.createRunWithDispatch(ctx, repo, run, items, trials, event); err != nil {
				return err
			}
			group.RunIDs = append(group.RunIDs, run.ID)
			runs = append(runs, run)
		}
		return nil
	})
	if err != nil {
		return ComparisonResult{}, err
	}
	if replayed {
		runs = make([]domain.Run, 0, len(group.RunIDs))
		for _, id := range group.RunIDs {
			run, err := s.repo.GetRun(ctx, id)
			if err != nil {
				return ComparisonResult{}, err
			}
			runs = append(runs, run)
		}
	} else if s.workers {
		for _, run := range runs {
			s.dispatch(run.ID)
		}
	}
	group, err = s.repo.GetRunGroup(ctx, group.ID)
	return ComparisonResult{Group: group, Runs: runs}, err
}

func (s *Service) buildRun(experimentID, key, groupID string, dataset domain.DatasetVersion, agent domain.AgentVersion, trialCount int, now time.Time, judge domain.JudgeConfig) (domain.Run, []domain.RunItem, []domain.RunTrial, domain.RunEvent) {
	scheduled := len(dataset.Cases) * trialCount
	run := domain.Run{ExecutionFormat: 1, ID: newID("run"), ExperimentID: experimentID, GroupID: groupID, IdempotencyKey: key, Status: domain.RunQueued, DatasetSnapshot: dataset, AgentSnapshot: agent, EvaluatorSnapshot: s.freezeEvaluator(dataset, agent), TrialCount: trialCount, Concurrency: 5, CreatedAt: now, Total: scheduled, ScheduledTrials: scheduled, CaseCount: len(dataset.Cases), CostKnown: true, Results: []domain.CaseResult{}, Events: []domain.RunEvent{}}
	run.EvaluatorSnapshot.Judge = judge
	if runner, ok := s.runners[agent.RunnerType].(ExecutionRunner); ok {
		run.ExecutionSettings = domain.RunExecutionSettings{TenantID: runner.config.TenantID, TimeoutMs: runner.config.Timeout.Milliseconds()}
	}
	items := make([]domain.RunItem, len(dataset.Cases))
	trials := make([]domain.RunTrial, 0, scheduled)
	ordinal := 0
	for caseIndex, caseItem := range dataset.Cases {
		itemID := newID("item")
		items[caseIndex] = domain.RunItem{ID: itemID, RunID: run.ID, CaseID: caseItem.ID, Title: caseItem.Title, Ordinal: caseIndex, Status: domain.ItemQueued, CreatedAt: now}
		for trialIndex := 1; trialIndex <= trialCount; trialIndex++ {
			trials = append(trials, domain.RunTrial{ID: newID("trial"), RunID: run.ID, ItemID: itemID, CaseID: caseItem.ID, TrialIndex: trialIndex, Ordinal: ordinal, Status: domain.TrialQueued, CreatedAt: now})
			ordinal++
		}
	}
	event := domain.RunEvent{RunID: run.ID, Type: "run.created", At: now, DatasetVersionID: dataset.ID, AgentVersionID: agent.ID, Reason: fmt.Sprintf("每个用例独立运行 %d 次", trialCount)}
	return run, items, trials, event
}

func uniqueStrings(values []string) []string {
	seen, result := map[string]bool{}, make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value], result = true, append(result, value)
		}
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Service) GetRun(ctx context.Context, id string) (domain.Run, error) {
	run, err := s.repo.GetRun(ctx, id)
	if err != nil {
		return domain.Run{}, mapNotFound(err, "run_not_found", "运行不存在")
	}
	return run, nil
}

func (s *Service) RunEvents(ctx context.Context, id string, afterSequence int64) ([]domain.RunEvent, error) {
	if _, err := s.GetRun(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListRunEvents(ctx, id, afterSequence)
}

func (s *Service) WaitRunEvents(ctx context.Context, id string, afterSequence int64, heartbeat time.Duration) ([]domain.RunEvent, bool, error) {
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(heartbeat)
	defer timer.Stop()
	for {
		events, err := s.RunEvents(ctx, id, afterSequence)
		if err != nil || len(events) > 0 {
			return events, false, err
		}
		run, err := s.GetRun(ctx, id)
		if err != nil {
			return nil, false, err
		}
		if !domain.IsActiveRun(run.Status) {
			return nil, true, nil
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, false, nil
		case <-ticker.C:
		}
	}
}

// CancelRun durably stops scheduling first; remote invocations are reconciled
// before the Run becomes cancelled.
func (s *Service) CancelRun(ctx context.Context, id string) (domain.Run, error) {
	if _, err := s.GetRun(ctx, id); err != nil {
		return domain.Run{}, err
	}
	if err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		run, err := repo.GetRun(ctx, id)
		if err != nil {
			return err
		}
		if !domain.IsActiveRun(run.Status) {
			return nil
		}
		if err := repo.RequestRunCancellation(ctx, id); err != nil {
			return err
		}
		return repo.AppendRunEventOnce(ctx, "cancel-requested", domain.RunEvent{RunID: id, Type: "run.cancel_requested", Status: run.Status, At: s.now().UTC()})
	}); err != nil {
		return domain.Run{}, err
	}
	s.mu.Lock()
	if cancel := s.cancels[id]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	s.dispatch(id)
	return s.GetRun(ctx, id)
}

func (s *Service) ResumeActiveRuns(ctx context.Context) error {
	runs, err := s.repo.ListActiveRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if err := validateFrozenAgentModel(run.AgentSnapshot); err != nil {
			return err
		}
		if _, remote := s.runners[run.AgentSnapshot.RunnerType].(ExecutionRunner); remote && run.ExecutionFormat != 1 {
			return fmt.Errorf("active Run %s predates durable invocation requests; resolve it before upgrading", run.ID)
		}
		err = s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
			if err := repo.ResetActiveRunTrials(ctx, run.ID); err != nil {
				return err
			}
			now := s.now().UTC()
			if err := repo.UpdateRunStatus(ctx, run.ID, domain.RunQueued, now, ""); err != nil {
				return err
			}
			return repo.AppendRunEvent(ctx, domain.RunEvent{RunID: run.ID, Type: "run.recovered", Status: domain.RunQueued, At: now})
		})
		if err != nil {
			return err
		}
		if s.workers {
			s.dispatch(run.ID)
		}
	}
	return nil
}

func (s *Service) dispatch(runID string) {
	if s.externalDispatch {
		return
	}
	s.mu.Lock()
	if _, exists := s.cancels[runID]; exists || s.runStopping {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.runContext)
	s.cancels[runID] = cancel
	s.runWG.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.runWG.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.cancels, runID); s.mu.Unlock() }()
		for s.runContext.Err() == nil {
			_ = s.executeRun(ctx, runID)
			current, err := s.repo.GetRun(s.runContext, runID)
			if err == nil && !domain.IsActiveRun(current.Status) {
				return
			}
			if err == nil && current.CancelRequested {
				if s.reconcileCancelledRun(s.runContext, current) == nil {
					return
				}
			}
			if err := wait(s.runContext, s.dispatchRetryInterval); err != nil {
				return
			}
			if err := s.repo.ResetActiveRunTrials(s.runContext, runID); err != nil {
				continue
			}
		}
	}()
}

// StopRunWorkers waits for local evaluation observers without cancelling the
// accepted remote invocations that the next Rank owner will recover.
func (s *Service) StopRunWorkers() {
	s.mu.Lock()
	s.runStopping = true
	s.stopRuns()
	s.mu.Unlock()
	s.runWG.Wait()
}

func (s *Service) reconcileCancelledRun(ctx context.Context, run domain.Run) error {
	values, err := s.repo.ListEvaluationInvocations(ctx, run.ID)
	if err != nil {
		return err
	}
	if len(values) > 0 {
		runner, ok := s.runners[run.AgentSnapshot.RunnerType].(ExecutionRunner)
		if !ok {
			return fmt.Errorf("Run %s has no invocation owner", run.ID)
		}
		for _, value := range values {
			if len(value.Result) > 0 {
				continue
			}
			var request contract.SubmitRequest
			if err := json.Unmarshal(value.Request, &request); err != nil {
				return err
			}
			result, err := runner.invoke(ctx, request, nil)
			if !contract.IsTerminal(result.Status) {
				return err
			}
		}
	}
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		return repo.CancelRun(ctx, run.ID, s.now().UTC(), domain.RunEvent{RunID: run.ID, Type: "run.status", Status: domain.RunCancelled, At: s.now().UTC()})
	})
}

func (s *Service) transition(ctx context.Context, runID, status string) error {
	now := s.now().UTC()
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		if err := repo.UpdateRunStatus(ctx, runID, status, now, ""); err != nil {
			return err
		}
		return repo.AppendRunEvent(ctx, domain.RunEvent{RunID: runID, Type: "run.status", Status: status, At: now})
	})
}

func (s *Service) executeRun(ctx context.Context, runID string) error {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	runner := s.runners[run.AgentSnapshot.RunnerType]
	if runner == nil {
		return s.failRun(context.Background(), runID, fmt.Errorf("runner %q is not registered", run.AgentSnapshot.RunnerType))
	}
	if run.CancelRequested {
		return nil
	}
	if err := s.transition(ctx, runID, domain.RunPreparing); err != nil {
		return err
	}
	if err := wait(ctx, s.workerLatency); err != nil {
		return err
	}
	if err := s.transition(ctx, runID, domain.RunRunning); err != nil {
		return err
	}
	trials, err := s.repo.ListRunTrials(ctx, runID)
	if err != nil {
		return err
	}
	caseByID := map[string]domain.Case{}
	for _, caseItem := range run.DatasetSnapshot.Cases {
		caseByID[caseItem.ID] = caseItem
	}
	queue := make(chan domain.RunTrial, len(trials))
	for _, trial := range trials {
		if trial.Status != domain.TrialComplete {
			queue <- trial
		}
	}
	close(queue)
	concurrency := run.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(trials) {
		concurrency = len(trials)
	}
	var workers sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for index := 0; index < concurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for trial := range queue {
				if ctx.Err() != nil {
					return
				}
				if processErr := s.processTrial(ctx, run, runner, trial, caseByID[trial.CaseID]); processErr != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = processErr
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if firstErr != nil {
		return firstErr
	}
	if err := s.transition(ctx, runID, domain.RunScoring); err != nil {
		return err
	}
	return s.aggregateRun(ctx, run)
}

func (s *Service) processTrial(ctx context.Context, run domain.Run, runner AgentRunner, trial domain.RunTrial, caseItem domain.Case) error {
	started := s.now().UTC()
	claimed := false
	if err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		value, err := repo.ClaimRunTrial(ctx, run.ID, trial.ID, started)
		if err != nil {
			return err
		}
		claimed = value
		if !claimed {
			return nil
		}
		if err := repo.AppendRunEventOnce(ctx, trial.ID+":trial.started", trialEvent(run.ID, trial, "trial.started", started)); err != nil {
			return err
		}
		return repo.AppendRunEventOnce(ctx, trial.ID+":case.started", trialEvent(run.ID, trial, "case.started", started))
	}); err != nil || !claimed {
		return err
	}
	emit := func(event RunnerEvent) error {
		return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
			value := trialEvent(run.ID, trial, event.Type, s.now().UTC())
			value.Status, value.Reason = event.Status, event.Reason
			if event.InvocationID != "" {
				invocation, err := repo.GetEvaluationInvocation(ctx, event.InvocationID)
				if err != nil {
					return err
				}
				if event.Sequence <= invocation.Sequence {
					return nil
				}
				if err := repo.AdvanceInvocationSequence(ctx, invocation.ID, invocation.Sequence, event.Sequence); err != nil {
					return err
				}
			}
			return repo.AppendRunEvent(ctx, value)
		})
	}
	attempts := 1
	candidate, candidateErr := runner.RunCandidate(ctx, ExecutionSpec{Settings: run.ExecutionSettings, RunID: run.ID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, Case: caseItem, Agent: run.AgentSnapshot, Emit: emit})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if candidateErr != nil {
		result := baseTrialResult(run, trial, candidate, started, attempts)
		result.Valid, result.FailureClass, result.Reason = false, domain.FailureInfra, candidateErr.Error()
		return s.completeTrial(ctx, trial, result, "trial.invalid")
	}
	criteria, deterministicPassed := deterministicResults(caseItem, candidate, run.EvaluatorSnapshot)
	if !deterministicPassed {
		reason := firstFailureReason(criteria, "未通过确定性检查")
		result := baseTrialResult(run, trial, candidate, started, attempts)
		result.Valid, result.Passed, result.Score, result.FailureClass, result.Reason, result.Criteria = true, false, 0, domain.FailureQuality, reason, criteria
		return s.completeTrial(ctx, trial, result, "trial.completed")
	}
	judgeIDs := []string{}
	judgeArtifacts := []domain.ArtifactRef{}
	evaluationCost, durationMs := 0.0, candidate.DurationMs
	usage := candidate.Usage
	costKnown := candidate.CostKnown
	costEstimated := candidate.CostEstimated
	for _, rubric := range run.EvaluatorSnapshot.Rubric {
		verdict, judgeErr := runner.RunJudge(ctx, JudgeSpec{Settings: run.ExecutionSettings, RunID: run.ID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, Case: caseItem, Agent: run.AgentSnapshot, Evaluator: run.EvaluatorSnapshot, Criterion: rubric, Candidate: candidate, Emit: emit})
		attempts++
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if judgeErr != nil {
			result := baseTrialResult(run, trial, candidate, started, attempts)
			result.Valid, result.FailureClass, result.Reason, result.Criteria = false, domain.FailureGrading, judgeErr.Error(), criteria
			result.EvaluationCost, result.Cost = evaluationCost, candidate.Cost+evaluationCost
			result.CostKnown, result.CostEstimated, result.DurationMs, result.Usage = costKnown && verdict.CostKnown, costEstimated || verdict.CostEstimated, durationMs+verdict.DurationMs, usage
			result.EvaluationCost += verdict.Cost
			result.Cost += verdict.Cost
			result.Usage.InputTokens += verdict.Usage.InputTokens
			result.Usage.OutputTokens += verdict.Usage.OutputTokens
			result.Usage.CacheReadTokens += verdict.Usage.CacheReadTokens
			result.Usage.CacheWriteTokens += verdict.Usage.CacheWriteTokens
			result.Usage.ReasoningTokens += verdict.Usage.ReasoningTokens
			if verdict.ExecutionID != "" {
				judgeIDs = append(judgeIDs, verdict.ExecutionID)
			}
			judgeArtifacts = append(judgeArtifacts, verdict.Artifacts...)
			result.JudgeExecutionIDs = judgeIDs
			result.Artifacts = append(result.Artifacts, judgeArtifacts...)
			return s.completeTrial(ctx, trial, result, "trial.invalid")
		}
		score := verdict.Score
		passed := score >= rubric.Threshold
		criteria = append(criteria, domain.CriterionResult{CriterionID: rubric.ID, Kind: "rubric", Name: rubric.Name, Status: verdictStatus(passed), Passed: &passed, Score: &score, Reason: verdict.Reason, Critical: rubric.Critical, Weight: rubric.Weight, ExecutionID: verdict.ExecutionID})
		judgeIDs = append(judgeIDs, verdict.ExecutionID)
		judgeArtifacts = append(judgeArtifacts, verdict.Artifacts...)
		evaluationCost += verdict.Cost
		durationMs += verdict.DurationMs
		usage.InputTokens += verdict.Usage.InputTokens
		usage.OutputTokens += verdict.Usage.OutputTokens
		usage.CacheReadTokens += verdict.Usage.CacheReadTokens
		usage.CacheWriteTokens += verdict.Usage.CacheWriteTokens
		usage.ReasoningTokens += verdict.Usage.ReasoningTokens
		costKnown = costKnown && verdict.CostKnown
		costEstimated = costEstimated || verdict.CostEstimated
		verdictEvent := trialEvent(run.ID, trial, "judge.verdict", s.now().UTC())
		verdictEvent.Passed, verdictEvent.Score, verdictEvent.Reason = &passed, &score, verdict.Reason
		if err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
			return repo.AppendRunEventOnce(ctx, trial.ID+":verdict:"+rubric.ID, verdictEvent)
		}); err != nil {
			return err
		}
	}
	score, rubricPassed := weightedRubric(criteria, run.EvaluatorSnapshot.PassPolicy.RubricThreshold)
	result := baseTrialResult(run, trial, candidate, started, attempts)
	result.Valid, result.Passed, result.Score, result.Criteria = true, rubricPassed, score, criteria
	result.EvaluationCost, result.Cost = evaluationCost, candidate.Cost+evaluationCost
	result.CostKnown, result.CostEstimated, result.DurationMs, result.Usage = costKnown, costEstimated, durationMs, usage
	result.JudgeExecutionIDs = judgeIDs
	result.Artifacts = append(result.Artifacts, judgeArtifacts...)
	if rubricPassed {
		result.Reason = "确定性检查与 Rubric 均通过"
	} else {
		result.FailureClass = domain.FailureQuality
		result.Reason = firstFailureReason(criteria, "Rubric 未达到通过标准")
	}
	return s.completeTrial(ctx, trial, result, "trial.completed")
}

func baseTrialResult(run domain.Run, trial domain.RunTrial, candidate CandidateResult, started time.Time, attempts int) domain.TrialResult {
	return domain.TrialResult{ID: trial.ID, RunID: run.ID, CaseID: trial.CaseID, TrialIndex: trial.TrialIndex, Status: domain.TrialComplete, Output: candidate.Output, CandidateCost: candidate.Cost, Cost: candidate.Cost, CostKnown: candidate.CostKnown, CostEstimated: candidate.CostEstimated, DurationMs: candidate.DurationMs, Attempts: attempts, CandidateExecutionID: candidate.ExecutionID, JudgeExecutionIDs: []string{}, Usage: candidate.Usage, Artifacts: append([]domain.ArtifactRef(nil), candidate.Artifacts...), Criteria: []domain.CriterionResult{}, CreatedAt: trial.CreatedAt, StartedAt: &started}
}

func trialEvent(runID string, trial domain.RunTrial, eventType string, at time.Time) domain.RunEvent {
	return domain.RunEvent{RunID: runID, Type: eventType, CaseID: trial.CaseID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, At: at}
}

func firstFailureReason(criteria []domain.CriterionResult, fallback string) string {
	for _, criterion := range criteria {
		if criterion.Passed != nil && !*criterion.Passed && criterion.Reason != "" {
			return criterion.Name + "：" + criterion.Reason
		}
	}
	return fallback
}

func (s *Service) completeTrial(ctx context.Context, trial domain.RunTrial, result domain.TrialResult, eventType string) error {
	completed := s.now().UTC()
	result.CompletedAt = &completed
	passed, cost, score := result.Passed, result.Cost, result.Score
	events := []domain.RunEvent{{RunID: trial.RunID, Type: eventType, CaseID: trial.CaseID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, Status: result.FailureClass, Passed: &passed, Cost: &cost, Score: &score, Output: result.Output, Reason: result.Reason, At: completed}}
	for _, artifact := range result.Artifacts {
		events = append(events, domain.RunEvent{RunID: trial.RunID, Type: "artifact.available", CaseID: trial.CaseID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, Output: artifact.Path, Reason: artifact.Kind, At: completed})
	}
	resultKey := "trial:" + trial.ID
	if result.CandidateExecutionID != "" {
		resultKey = "execution:" + result.CandidateExecutionID
	}
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		_, err := repo.CompleteRunTrial(ctx, trial.ID, resultKey, result, completed, events)
		return err
	})
}

func (s *Service) aggregateRun(ctx context.Context, run domain.Run) error {
	items, err := s.repo.ListRunItems(ctx, run.ID)
	if err != nil {
		return err
	}
	trials, err := s.repo.ListRunTrials(ctx, run.ID)
	if err != nil {
		return err
	}
	trialsByCase := map[string][]domain.TrialResult{}
	for _, trial := range trials {
		if trial.Result != nil {
			trialsByCase[trial.CaseID] = append(trialsByCase[trial.CaseID], *trial.Result)
		}
	}
	caseByID := map[string]domain.Case{}
	for _, caseItem := range run.DatasetSnapshot.Cases {
		caseByID[caseItem.ID] = caseItem
	}
	completed := s.now().UTC()
	results := make([]domain.CaseResult, 0, len(items))
	if err := s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		for _, item := range items {
			result := aggregateCase(caseByID[item.CaseID], run.TrialCount, trialsByCase[item.CaseID])
			stored := result
			stored.Trials = nil
			if err := repo.SaveRunItemAggregate(ctx, item.ID, stored, completed); err != nil {
				return err
			}
			results = append(results, result)
		}
		return nil
	}); err != nil {
		return err
	}
	run.Status, run.CompletedAt = domain.RunComplete, &completed
	run.DurationMs = completed.Sub(run.CreatedAt).Milliseconds()
	run.Passed, run.ValidTrials, run.Total = 0, 0, 0
	run.Cost, run.CandidateCost, run.EvaluationCost = 0, 0, 0
	run.CostKnown, run.CostEstimated, run.EvaluationComplete = true, false, true
	run.Results = results
	passHatSum, passHatCases := 0.0, 0
	for _, result := range results {
		if result.Reliable {
			run.ReliableCases++
		}
		for _, trial := range result.Trials {
			run.Cost += trial.Cost
			run.CandidateCost += trial.CandidateCost
			run.EvaluationCost += trial.EvaluationCost
			if !trial.CostKnown {
				run.CostKnown = false
			}
			run.CostEstimated = run.CostEstimated || trial.CostEstimated
			if trial.FailureClass == domain.FailureInfra {
				run.InfraFailures++
				run.EvaluationComplete = false
			}
			if trial.FailureClass == domain.FailureGrading {
				run.GradingFailures++
				run.EvaluationComplete = false
			}
			if trial.Valid {
				run.ValidTrials++
				if trial.Passed {
					run.Passed++
				}
			}
		}
		if result.ValidTrials >= 3 {
			passHatSum += combinations(result.PassCount, 3) / combinations(result.ValidTrials, 3)
			passHatCases++
		}
	}
	run.Total = run.ValidTrials
	if run.Total > 0 {
		run.PassRate = int(math.Round(float64(run.Passed) * 100 / float64(run.Total)))
	}
	if passHatCases > 0 {
		run.PassHat3 = math.Round(passHatSum/float64(passHatCases)*1000) / 10
	}
	run.Cost, run.CandidateCost, run.EvaluationCost = roundCost(run.Cost), roundCost(run.CandidateCost), roundCost(run.EvaluationCost)
	content := fmt.Sprintf("运行完成：%d/%d 个有效 Trial 通过，通过率 %d%%；%d/%d 个用例稳定通过。", run.Passed, run.Total, run.PassRate, run.ReliableCases, run.CaseCount)
	if !run.EvaluationComplete {
		content += fmt.Sprintf(" 另有 %d 个执行异常、%d 个评分异常未计入质量分母。", run.InfraFailures, run.GradingFailures)
	}
	message := domain.Message{ID: newID("msg"), ExperimentID: run.ExperimentID, Role: "assistant", Content: content, RunID: run.ID, CreatedAt: completed}
	event := domain.RunEvent{RunID: run.ID, Type: "run.completed", At: completed, Reason: content}
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		return repo.FinishRun(ctx, run, message, event)
	})
}

func aggregateCase(caseItem domain.Case, trialCount int, trials []domain.TrialResult) domain.CaseResult {
	result := domain.CaseResult{CaseID: caseItem.ID, Title: caseItem.Title, TrialCount: trialCount, CostKnown: true, Trials: trials}
	score, scoreCount := 0.0, 0
	for _, trial := range trials {
		result.Cost += trial.Cost
		result.CandidateCost += trial.CandidateCost
		result.EvaluationCost += trial.EvaluationCost
		result.DurationMs += trial.DurationMs
		result.Usage.InputTokens += trial.Usage.InputTokens
		result.Usage.OutputTokens += trial.Usage.OutputTokens
		result.Usage.CacheReadTokens += trial.Usage.CacheReadTokens
		result.Usage.CacheWriteTokens += trial.Usage.CacheWriteTokens
		result.Usage.ReasoningTokens += trial.Usage.ReasoningTokens
		result.Artifacts = append(result.Artifacts, trial.Artifacts...)
		if result.ExecutionID == "" {
			result.ExecutionID = trial.CandidateExecutionID
		}
		if result.JudgeExecutionID == "" && len(trial.JudgeExecutionIDs) > 0 {
			result.JudgeExecutionID = trial.JudgeExecutionIDs[0]
		}
		if trial.Output != "" {
			result.Output = trial.Output
		}
		if !trial.CostKnown {
			result.CostKnown = false
		}
		result.CostEstimated = result.CostEstimated || trial.CostEstimated
		if trial.Valid {
			result.ValidTrials++
			score += trial.Score
			scoreCount++
			if trial.Passed {
				result.PassCount++
			} else if result.Reason == "" {
				result.Reason = trial.Reason
			}
		} else if result.Reason == "" {
			result.Reason = trial.Reason
		}
	}
	result.Cost, result.CandidateCost, result.EvaluationCost = roundCost(result.Cost), roundCost(result.CandidateCost), roundCost(result.EvaluationCost)
	if result.ValidTrials > 0 {
		result.PassRate = int(math.Round(float64(result.PassCount) * 100 / float64(result.ValidTrials)))
	}
	if scoreCount > 0 {
		result.Score = score / float64(scoreCount)
	}
	result.Reliable = len(trials) == trialCount && result.ValidTrials == trialCount && result.PassCount == trialCount
	result.Passed = result.Reliable
	if result.Reliable {
		result.Reason = fmt.Sprintf("%d/%d 次均通过", result.PassCount, trialCount)
	} else if result.Reason == "" {
		result.Reason = fmt.Sprintf("%d/%d 次通过", result.PassCount, result.ValidTrials)
	}
	return result
}

func combinations(n, k int) float64 {
	if n < k || k < 0 {
		return 0
	}
	if k == 0 || n == k {
		return 1
	}
	value := 1.0
	for index := 1; index <= k; index++ {
		value *= float64(n-k+index) / float64(index)
	}
	return value
}

func (s *Service) failRun(ctx context.Context, runID string, cause error) error {
	now := s.now().UTC()
	return s.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
		if err := repo.UpdateRunStatus(ctx, runID, domain.RunFailed, now, cause.Error()); err != nil {
			return err
		}
		return repo.AppendRunEvent(ctx, domain.RunEvent{RunID: runID, Type: "run.status", Status: domain.RunFailed, Reason: cause.Error(), At: now})
	})
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
