package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type ExecutionSpec struct {
	RunID      string
	TrialID    string
	TrialIndex int
	Attempt    int
	Case       domain.Case
	Agent      domain.AgentVersion
	Emit       func(RunnerEvent) error
}

type CandidateResult struct {
	Output        string
	Cost          float64
	CostKnown     bool
	CostEstimated bool
	DurationMs    int64
	ExecutionID   string
	Usage         domain.Usage
	Artifacts     []domain.ArtifactRef
}

type JudgeSpec struct {
	RunID      string
	TrialID    string
	TrialIndex int
	Attempt    int
	Case       domain.Case
	Agent      domain.AgentVersion
	Evaluator  domain.EvaluatorVersion
	Criterion  domain.RubricCriterion
	Candidate  CandidateResult
	Emit       func(RunnerEvent) error
}

type JudgeResult struct {
	Passed        bool
	Score         float64
	Reason        string
	Cost          float64
	CostKnown     bool
	CostEstimated bool
	DurationMs    int64
	ExecutionID   string
	Usage         domain.Usage
	Artifacts     []domain.ArtifactRef
}

type RunnerEvent struct {
	Type   string
	Status string
	Reason string
}

type AgentRunner interface {
	Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability
	RunCandidate(context.Context, ExecutionSpec) (CandidateResult, error)
	RunJudge(context.Context, JudgeSpec) (JudgeResult, error)
}

type RunnerRegistry map[string]AgentRunner

func DefaultRunners() RunnerRegistry {
	return RunnerRegistry{
		"mock":        DemoRunner{},
		"dsh":         UnavailableRunner{Label: "DeepSeek Harness", Reason: "Execution Service 未配置"},
		"pi":          UnavailableRunner{Label: "Pi", Reason: "Execution Service 未配置"},
		"claude-code": UnavailableRunner{Label: "Claude Code", Reason: "Execution Service 未配置"},
		"codex":       UnavailableRunner{Label: "Codex", Reason: "Execution Service 未配置"},
		"hermes":      UnavailableRunner{Label: "Hermes", Reason: "Execution Service 未配置"},
	}
}

type UnavailableRunner struct{ Label, Reason string }

func (runner UnavailableRunner) Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability {
	return domain.RuntimeAvailability{Available: false, Label: runner.Label, Reason: runner.Reason}
}

func (runner UnavailableRunner) RunCandidate(context.Context, ExecutionSpec) (CandidateResult, error) {
	return CandidateResult{}, fmt.Errorf("%s", runner.Reason)
}

func (runner UnavailableRunner) RunJudge(context.Context, JudgeSpec) (JudgeResult, error) {
	return JudgeResult{}, fmt.Errorf("%s", runner.Reason)
}

type DemoRunner struct{}

func (DemoRunner) Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability {
	return domain.RuntimeAvailability{Available: true, Label: "内置演示 Runner"}
}

func (DemoRunner) RunCandidate(ctx context.Context, spec ExecutionSpec) (CandidateResult, error) {
	started := time.Now()
	select {
	case <-ctx.Done():
		return CandidateResult{}, ctx.Err()
	case <-time.After(90 * time.Millisecond):
	}

	summary := stringValue(spec.Case.Expected["summary"])
	if summary == "" {
		summary = "任务成功完成"
	}
	output := fmt.Sprintf("已完成「%s」，输出满足 %s。", spec.Case.Title, summary)
	if stringValue(spec.Case.Expected["demoOutcome"]) == "fail" {
		reason := stringValue(spec.Case.Expected["failureReason"])
		output = fmt.Sprintf("已完成「%s」，但%s。", spec.Case.Title, reason)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(spec.Case.ID + spec.Agent.ID))
	cost := float64(18+hash.Sum32()%19) / 1000
	return CandidateResult{Output: output, Cost: cost, CostKnown: true, DurationMs: time.Since(started).Milliseconds()}, nil
}

func (DemoRunner) RunJudge(ctx context.Context, spec JudgeSpec) (JudgeResult, error) {
	started := time.Now()
	select {
	case <-ctx.Done():
		return JudgeResult{}, ctx.Err()
	case <-time.After(25 * time.Millisecond):
	}
	passed := stringValue(spec.Case.Expected["demoOutcome"]) != "fail"
	reason := "满足测试集断言"
	if !passed {
		reason = stringValue(spec.Case.Expected["failureReason"])
		if reason == "" {
			reason = "未满足测试集断言"
		}
	}
	score := 0.0
	if passed {
		score = 1
	}
	return JudgeResult{Passed: passed, Score: score, Reason: reason, Cost: 0.006, CostKnown: true, DurationMs: time.Since(started).Milliseconds()}, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
