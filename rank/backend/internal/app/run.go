package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

func (s *Service) StartRun(ctx context.Context, experimentID, idempotencyKey string) (domain.Run, error) {
	if idempotencyKey == "" {
		idempotencyKey = newID("request")
	}
	var created domain.Run
	replayed := false
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		existing, err := repo.GetRunByIdempotencyKey(ctx, experimentID, idempotencyKey)
		if err == nil {
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
		availability := s.probe(ctx, agent)
		if !availability.Available {
			return problem(409, "runner_unavailable", availability.Reason)
		}
		now := s.now().UTC()
		created = domain.Run{ID: newID("run"), ExperimentID: experimentID, IdempotencyKey: idempotencyKey, Status: domain.RunQueued, DatasetSnapshot: dataset, AgentSnapshot: agent, Concurrency: 3, CreatedAt: now, Total: len(dataset.Cases), CostKnown: true, Results: []domain.CaseResult{}, Events: []domain.RunEvent{}}
		items := make([]domain.RunItem, len(dataset.Cases))
		for index, item := range dataset.Cases {
			items[index] = domain.RunItem{ID: newID("item"), RunID: created.ID, CaseID: item.ID, Title: item.Title, Ordinal: index, Status: domain.ItemQueued, CreatedAt: now}
		}
		event := domain.RunEvent{RunID: created.ID, Type: "run.created", At: now, DatasetVersionID: dataset.ID, AgentVersionID: agent.ID}
		return repo.CreateRun(ctx, created, items, event)
	})
	if err != nil {
		return domain.Run{}, err
	}
	if !replayed && s.workers {
		s.dispatch(created.ID)
	}
	return s.repo.GetRun(ctx, created.ID)
}

func (s *Service) GetRun(ctx context.Context, id string) (domain.Run, error) {
	run, err := s.repo.GetRun(ctx, id)
	if err != nil {
		return domain.Run{}, mapNotFound(err, "run_not_found", "运行不存在")
	}
	return run, nil
}

func (s *Service) CancelRun(ctx context.Context, id string) (domain.Run, error) {
	now := s.now().UTC()
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		return repo.CancelRun(ctx, id, now, domain.RunEvent{RunID: id, Type: "run.status", Status: domain.RunCancelled, At: now})
	})
	if err != nil && !errors.Is(err, domain.ErrConflict) {
		return domain.Run{}, mapNotFound(err, "run_not_found", "运行不存在")
	}
	s.mu.Lock()
	if cancel := s.cancels[id]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	return s.GetRun(ctx, id)
}

func (s *Service) ResumeActiveRuns(ctx context.Context) error {
	runs, err := s.repo.ListActiveRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		err = s.repo.WithinTx(ctx, func(repo domain.Repository) error {
			if err := repo.ResetActiveRunItems(ctx, run.ID); err != nil {
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
	s.mu.Lock()
	if _, exists := s.cancels[runID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[runID] = cancel
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); delete(s.cancels, runID); s.mu.Unlock() }()
		_ = s.executeRun(ctx, runID)
	}()
}

func (s *Service) transition(ctx context.Context, runID, status string) error {
	now := s.now().UTC()
	return s.repo.WithinTx(ctx, func(repo domain.Repository) error {
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
		return fmt.Errorf("runner %q is not registered", run.AgentSnapshot.RunnerType)
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
	items, err := s.repo.ListRunItems(ctx, runID)
	if err != nil {
		return err
	}
	caseByID := map[string]domain.Case{}
	for _, item := range run.DatasetSnapshot.Cases {
		caseByID[item.ID] = item
	}
	concurrency := run.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}
	queue := make(chan domain.RunItem, len(items))
	for _, item := range items {
		if item.Status != domain.ItemComplete {
			queue <- item
		}
	}
	close(queue)
	var workers sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for index := 0; index < concurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				if ctx.Err() != nil {
					return
				}
				started := s.now().UTC()
				claimed, e := s.claimItem(ctx, runID, item, caseByID[item.CaseID], started)
				if e != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = e
					}
					errMu.Unlock()
					return
				}
				if !claimed {
					continue
				}
				result, e := runner.RunCase(ctx, ExecutionSpec{RunID: runID, Case: caseByID[item.CaseID], Agent: run.AgentSnapshot})
				if e != nil {
					if ctx.Err() != nil {
						return
					}
					errMu.Lock()
					if firstErr == nil {
						firstErr = e
					}
					errMu.Unlock()
					return
				}
				completed := s.now().UTC()
				e = s.repo.WithinTx(ctx, func(repo domain.Repository) error {
					passed, cost, score := result.Passed, result.Cost, result.Score
					events := []domain.RunEvent{{RunID: runID, Type: "agent.message", CaseID: item.CaseID, At: completed, Output: result.Output}, {RunID: runID, Type: "judge.completed", CaseID: item.CaseID, At: completed, Passed: &passed, Cost: &cost, Score: &score, Reason: result.Reason}, {RunID: runID, Type: "case.completed", CaseID: item.CaseID, At: completed, Passed: &passed, Cost: &cost, Score: &score, Output: result.Output, Reason: result.Reason}}
					for _, artifact := range result.Artifacts {
						events = append(events, domain.RunEvent{RunID: runID, Type: "artifact.available", CaseID: item.CaseID, At: completed, Output: artifact.Path, Reason: artifact.Kind})
					}
					resultKey := "result:" + item.ID
					if result.ExecutionID != "" {
						resultKey = "execution:" + result.ExecutionID
					}
					_, e := repo.CompleteRunItem(ctx, item.ID, resultKey, result, completed, events)
					return e
				})
				if e != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = e
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
		return s.failRun(context.Background(), runID, firstErr)
	}
	if err := s.transition(ctx, runID, domain.RunScoring); err != nil {
		return err
	}
	if err := wait(ctx, s.workerLatency); err != nil {
		return err
	}
	completedRun, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	completed := s.now().UTC()
	completedRun.Status = domain.RunComplete
	completedRun.CompletedAt = &completed
	completedRun.DurationMs = completed.Sub(completedRun.CreatedAt).Milliseconds()
	completedRun.Passed = 0
	completedRun.Cost = 0
	completedRun.CostKnown = true
	for _, result := range completedRun.Results {
		if result.Passed {
			completedRun.Passed++
		}
		completedRun.Cost += result.Cost
		if !result.CostKnown {
			completedRun.CostKnown = false
		}
	}
	completedRun.Cost = math.Round(completedRun.Cost*1000) / 1000
	if completedRun.Total > 0 {
		completedRun.PassRate = int(math.Round(float64(completedRun.Passed) * 100 / float64(completedRun.Total)))
	}
	message := domain.Message{ID: newID("msg"), ExperimentID: completedRun.ExperimentID, Role: "assistant", Content: fmt.Sprintf("运行完成：%d/%d 个用例通过，通过率 %d%%。", completedRun.Passed, completedRun.Total, completedRun.PassRate), RunID: runID, CreatedAt: completed}
	event := domain.RunEvent{RunID: runID, Type: "run.completed", At: completed}
	return s.repo.WithinTx(ctx, func(repo domain.Repository) error { return repo.FinishRun(ctx, completedRun, message, event) })
}

func (s *Service) claimItem(ctx context.Context, runID string, item domain.RunItem, caseItem domain.Case, started time.Time) (bool, error) {
	var claimed bool
	err := s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		value, e := repo.ClaimRunItem(ctx, runID, item.ID, started)
		if e != nil {
			return e
		}
		claimed = value
		if !claimed {
			return nil
		}
		if e = repo.AppendRunEvent(ctx, domain.RunEvent{RunID: runID, Type: "case.started", CaseID: item.CaseID, At: started}); e != nil {
			return e
		}
		return repo.AppendRunEvent(ctx, domain.RunEvent{RunID: runID, Type: "agent.message", CaseID: item.CaseID, At: started, Output: caseItem.Input})
	})
	return claimed, err
}

func (s *Service) failRun(ctx context.Context, runID string, cause error) error {
	now := s.now().UTC()
	return s.repo.WithinTx(ctx, func(repo domain.Repository) error {
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
