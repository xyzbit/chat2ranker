package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/sqlite"
)

type evaluationRunner struct {
	mu             sync.Mutex
	candidateCalls int
	judgeCalls     int
	output         string
	judge          func(JudgeSpec) (JudgeResult, error)
}

func (runner *evaluationRunner) Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability {
	return domain.RuntimeAvailability{Available: true, Label: "evaluation test runner"}
}

func (runner *evaluationRunner) RunCandidate(_ context.Context, _ ExecutionSpec) (CandidateResult, error) {
	runner.mu.Lock()
	runner.candidateCalls++
	runner.mu.Unlock()
	return CandidateResult{Output: runner.output, Cost: .01, CostKnown: true, DurationMs: 1, ExecutionID: newID("candidate")}, nil
}

func (runner *evaluationRunner) RunJudge(_ context.Context, spec JudgeSpec) (JudgeResult, error) {
	runner.mu.Lock()
	runner.judgeCalls++
	runner.mu.Unlock()
	return runner.judge(spec)
}

func (runner *evaluationRunner) counts() (int, int) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.candidateCalls, runner.judgeCalls
}

func evaluationService(t *testing.T, runner *evaluationRunner, expected map[string]any) (*Service, *sqlite.Store, string) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "rank.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, Options{Workers: true, WorkerLatency: time.Millisecond, Runners: RunnerRegistry{"test": runner}})
	rubric := json.RawMessage(`{"criteria":[{"id":"quality","name":"质量","description":"满足任务要求","weight":1,"threshold":0.7,"critical":true}]}`)
	dataset, err := service.CreateDataset(context.Background(), CreateDatasetInput{Name: "Evaluation", Source: "test", Rubric: rubric, Cases: []domain.Case{{ID: "case-1", Title: "One", Input: "complete one", Expected: expected}}})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := service.CreateAgent(context.Background(), CreateAgentInput{Name: "Test", RunnerType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := service.CreateExperiment(context.Background(), "Evaluation")
	if err != nil {
		t.Fatal(err)
	}
	experiment, err = service.UpdateExperiment(context.Background(), experiment.ID, ExperimentPatch{DatasetVersionID: &dataset.ID})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err = service.UpdateExperiment(context.Background(), experiment.ID, ExperimentPatch{AgentVersionID: &agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, experiment.ID
}

func awaitEvaluationRun(t *testing.T, service *Service, runID string) domain.Run {
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
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("evaluation run did not finish")
	return domain.Run{}
}

func TestRequiredDeterministicFailureSkipsLLMJudge(t *testing.T) {
	runner := &evaluationRunner{output: "wrong", judge: func(JudgeSpec) (JudgeResult, error) {
		return JudgeResult{Passed: true, Score: 1, Reason: "should not run"}, nil
	}}
	service, store, experimentID := evaluationService(t, runner, map[string]any{"exactOutput": "expected"})
	defer store.Close()
	created, err := service.StartRun(context.Background(), experimentID, "deterministic-first", RunOptions{TrialCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	run := awaitEvaluationRun(t, service, created.ID)
	candidateCalls, judgeCalls := runner.counts()
	if candidateCalls != 1 || judgeCalls != 0 {
		t.Fatalf("deterministic gate called candidate=%d judge=%d", candidateCalls, judgeCalls)
	}
	trial := run.Results[0].Trials[0]
	if !trial.Valid || trial.Passed || trial.FailureClass != domain.FailureQuality || len(trial.Criteria) != 1 || trial.Criteria[0].Kind != "deterministic" || run.EvaluationCost != 0 {
		t.Fatalf("unexpected deterministic result: %#v", trial)
	}
}

func TestFiveTrialsAggregateReliabilityAndPassHat3(t *testing.T) {
	runner := &evaluationRunner{output: "candidate", judge: func(spec JudgeSpec) (JudgeResult, error) {
		score := 1.0
		if spec.TrialIndex == 5 {
			score = .2
		}
		return JudgeResult{Passed: false, Score: score, Reason: "rubric score", Cost: .002, CostKnown: true, DurationMs: 1, ExecutionID: newID("judge")}, nil
	}}
	service, store, experimentID := evaluationService(t, runner, map[string]any{"summary": "done"})
	defer store.Close()
	created, err := service.StartRun(context.Background(), experimentID, "five-trials", RunOptions{TrialCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	run := awaitEvaluationRun(t, service, created.ID)
	if run.Passed != 4 || run.Total != 5 || run.PassRate != 80 || run.ReliableCases != 0 || run.PassHat3 != 40 || !run.EvaluationComplete {
		t.Fatalf("unexpected aggregate: %#v", run)
	}
	result := run.Results[0]
	if result.PassCount != 4 || result.ValidTrials != 5 || result.Reliable || result.Passed {
		t.Fatalf("unexpected case reliability: %#v", result)
	}
	candidateCalls, judgeCalls := runner.counts()
	if candidateCalls != 5 || judgeCalls != 5 {
		t.Fatalf("expected five isolated candidates and judges, got %d/%d", candidateCalls, judgeCalls)
	}
}

func TestJudgeFailureIsExcludedFromQualityDenominator(t *testing.T) {
	runner := &evaluationRunner{output: "candidate", judge: func(JudgeSpec) (JudgeResult, error) {
		return JudgeResult{}, errors.New("judge unavailable")
	}}
	service, store, experimentID := evaluationService(t, runner, map[string]any{"summary": "done"})
	defer store.Close()
	created, err := service.StartRun(context.Background(), experimentID, "judge-failure", RunOptions{TrialCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	run := awaitEvaluationRun(t, service, created.ID)
	if run.Total != 0 || run.ValidTrials != 0 || run.GradingFailures != 1 || run.EvaluationComplete {
		t.Fatalf("grading failure polluted quality denominator: %#v", run)
	}
	_, judgeCalls := runner.counts()
	if judgeCalls != trialAttempts {
		t.Fatalf("judge retry budget was not applied, calls=%d", judgeCalls)
	}
}

func TestRoundCostPreservesSmallRealProviderCharges(t *testing.T) {
	if got := roundCost(0.0018525864); got != 0.001853 {
		t.Fatalf("small provider cost lost precision: %f", got)
	}
}
