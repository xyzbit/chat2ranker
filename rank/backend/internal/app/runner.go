package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type ExecutionSpec struct {
	RunID string
	Case  domain.Case
	Agent domain.AgentVersion
}

type AgentRunner interface {
	Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability
	RunCase(context.Context, ExecutionSpec) (domain.CaseResult, error)
}

type RunnerRegistry map[string]AgentRunner

func DefaultRunners() RunnerRegistry {
	return RunnerRegistry{
		"mock":        DemoRunner{},
		"dsh":         UnavailableRunner{Label: "DeepSeek Harness", Reason: "rank-worker 未配置"},
		"pi":          UnavailableRunner{Label: "Pi", Reason: "rank-worker 未配置"},
		"claude-code": UnavailableRunner{Label: "Claude Code", Reason: "rank-worker 未配置"},
		"codex":       UnavailableRunner{Label: "Codex", Reason: "rank-worker 未配置"},
		"hermes":      UnavailableRunner{Label: "Hermes", Reason: "rank-worker 未配置"},
	}
}

type UnavailableRunner struct{ Label, Reason string }

func (runner UnavailableRunner) Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability {
	return domain.RuntimeAvailability{Available: false, Label: runner.Label, Reason: runner.Reason}
}

func (runner UnavailableRunner) RunCase(context.Context, ExecutionSpec) (domain.CaseResult, error) {
	return domain.CaseResult{}, fmt.Errorf("%s", runner.Reason)
}

type DemoRunner struct{}

func (DemoRunner) Probe(context.Context, domain.AgentVersion) domain.RuntimeAvailability {
	return domain.RuntimeAvailability{Available: true, Label: "内置演示 Runner"}
}

func (DemoRunner) RunCase(ctx context.Context, spec ExecutionSpec) (domain.CaseResult, error) {
	started := time.Now()
	select {
	case <-ctx.Done():
		return domain.CaseResult{}, ctx.Err()
	case <-time.After(90 * time.Millisecond):
	}

	passed := stringValue(spec.Case.Expected["demoOutcome"]) != "fail"
	summary := stringValue(spec.Case.Expected["summary"])
	if summary == "" {
		summary = "任务成功完成"
	}
	reason := "满足断言"
	output := fmt.Sprintf("已完成「%s」，输出满足 %s。", spec.Case.Title, summary)
	if !passed {
		reason = stringValue(spec.Case.Expected["failureReason"])
		if reason == "" {
			reason = "未满足断言"
		}
		output = fmt.Sprintf("已完成「%s」，但%s。", spec.Case.Title, reason)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(spec.Case.ID + spec.Agent.ID))
	cost := float64(18+hash.Sum32()%19) / 1000
	score := 0.0
	if passed {
		score = 1
	}
	return domain.CaseResult{
		CaseID: spec.Case.ID, Title: spec.Case.Title, Passed: passed,
		Cost: cost, CostKnown: true, Score: score, Output: output, Reason: reason,
		DurationMs: time.Since(started).Milliseconds(),
	}, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
