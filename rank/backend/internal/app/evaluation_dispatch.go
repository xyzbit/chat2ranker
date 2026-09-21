package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type evaluationDispatcher struct {
	repo    domain.Repository
	client  ServingExecutionClient
	retry   time.Duration
	observe time.Duration
}

// invoke retries observation and delivery with the stored request. A terminal
// remote outcome is cached; restarting orchestration never repeats a model call.
func (d evaluationDispatcher) invoke(ctx context.Context, proposed contract.SubmitRequest, emit func(contract.Event) error) (contract.Execution, error) {
	id := domain.InvocationID(proposed.IdempotencyKey)
	for {
		if err := ctx.Err(); err != nil {
			return contract.Execution{}, err
		}
		value, err := d.repo.GetEvaluationInvocation(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			payload, encodeErr := json.Marshal(proposed)
			if encodeErr != nil {
				return contract.Execution{}, encodeErr
			}
			err = d.repo.WithinTx(ctx, func(ctx context.Context, repo domain.Repository) error {
				return repo.CreateEvaluationInvocation(ctx, domain.EvaluationInvocation{ID: id, RunID: proposed.Spec.Metadata["runId"], TrialID: proposed.Spec.Metadata["trialId"], Request: payload})
			})
			if errors.Is(err, domain.ErrConflict) {
				run, loadErr := d.repo.GetRun(ctx, proposed.Spec.Metadata["runId"])
				if loadErr == nil && (run.CancelRequested || !domain.IsActiveRun(run.Status)) {
					return contract.Execution{}, context.Canceled
				}
			}
			if err == nil {
				continue
			}
		}
		if err == nil {
			if len(value.Result) > 0 {
				return invocationOutcome(value.Result)
			}
			var request contract.SubmitRequest
			if err := json.Unmarshal(value.Request, &request); err != nil {
				return contract.Execution{}, fmt.Errorf("decode stored invocation: %w", err)
			}
			execution, observeErr := d.observeInvocation(ctx, value, request, emit)
			if observeErr == nil && contract.IsTerminal(execution.Status) {
				payload, encodeErr := json.Marshal(execution)
				if encodeErr != nil {
					return execution, encodeErr
				}
				if err := d.repo.CompleteInvocation(ctx, id, payload); err == nil {
					return invocationOutcome(payload)
				}
			}
		}
		if err := wait(ctx, d.retry); err != nil {
			return contract.Execution{}, err
		}
	}
}

func invocationOutcome(payload json.RawMessage) (contract.Execution, error) {
	var execution contract.Execution
	if err := json.Unmarshal(payload, &execution); err != nil {
		return execution, err
	}
	if execution.Status != contract.StatusCompleted || execution.Result == nil {
		return execution, fmt.Errorf("execution %s ended as %s: %s", execution.ID, execution.Status, execution.Error)
	}
	return execution, nil
}

func (d evaluationDispatcher) observeInvocation(ctx context.Context, value domain.EvaluationInvocation, request contract.SubmitRequest, emit func(contract.Event) error) (contract.Execution, error) {
	if !value.SubmissionStarted {
		if err := d.repo.BeginInvocationSubmission(ctx, value.ID); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				run, loadErr := d.repo.GetRun(ctx, value.RunID)
				if loadErr == nil && run.CancelRequested {
					return contract.Execution{Status: contract.StatusCancelled, Error: "cancelled before submission"}, nil
				}
			}
			return contract.Execution{}, err
		}
	}
	execution, err := d.client.Submit(ctx, request)
	if err != nil {
		return execution, err
	}
	if err := d.repo.SaveInvocationExecution(ctx, value.ID, execution.ID); err != nil {
		return execution, err
	}
	run, err := d.repo.GetRun(ctx, value.RunID)
	if err != nil {
		return execution, err
	}
	if run.CancelRequested {
		execution, err = d.client.Cancel(ctx, execution.ID)
		if err != nil {
			return execution, err
		}
	}
	observer, stop := context.WithTimeout(ctx, d.observe)
	defer stop()
	_, err = d.client.Watch(observer, execution.ID, value.Sequence, func(event contract.Event) error {
		if emit != nil {
			return emit(event)
		}
		return nil
	})
	if err != nil {
		return execution, err
	}
	return d.client.Get(ctx, execution.ID)
}

// createRunWithDispatch commits candidate requests with their Trial admission.
// Judges are admitted only after the candidate result and checks are available.
func (s *Service) createRunWithDispatch(ctx context.Context, repo domain.Repository, run domain.Run, items []domain.RunItem, trials []domain.RunTrial, event domain.RunEvent) error {
	if err := repo.CreateRun(ctx, run, items, trials, event); err != nil {
		return err
	}
	runner, ok := s.runners[run.AgentSnapshot.RunnerType].(ExecutionRunner)
	if !ok {
		return nil
	}
	cases := map[string]domain.Case{}
	for _, item := range run.DatasetSnapshot.Cases {
		cases[item.ID] = item
	}
	for _, trial := range trials {
		request := runner.candidateRequest(ExecutionSpec{Settings: run.ExecutionSettings, RunID: run.ID, TrialID: trial.ID, TrialIndex: trial.TrialIndex, Case: cases[trial.CaseID], Agent: run.AgentSnapshot})
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if err := repo.CreateEvaluationInvocation(ctx, domain.EvaluationInvocation{ID: domain.InvocationID(request.IdempotencyKey), RunID: run.ID, TrialID: trial.ID, Request: payload}); err != nil {
			return err
		}
	}
	return nil
}

// prepareEvaluation performs remote availability and model-binding reads before
// taking the Rank business transaction lock.
func (s *Service) prepareEvaluation(ctx context.Context, datasetID string, agentIDs []string) (map[string]domain.JudgeConfig, error) {
	dataset, err := s.repo.GetDatasetVersion(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	configs := map[string]domain.JudgeConfig{}
	for _, id := range agentIDs {
		agent, err := s.repo.GetAgentVersion(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := validateFrozenAgentModel(agent); err != nil {
			return nil, err
		}
		if availability := s.probe(ctx, agent); !availability.Available {
			return nil, problem(409, "runner_unavailable", availability.Reason)
		}
		evaluator := s.freezeEvaluator(dataset, agent)
		config := evaluator.Judge
		if runner, ok := s.runners[agent.RunnerType].(ExecutionRunner); ok {
			config, err = runner.PrepareJudge(ctx, evaluator)
			if err != nil {
				return nil, problem(409, "judge_unavailable", err.Error())
			}
		}
		configs[id] = config
	}
	return configs, nil
}
