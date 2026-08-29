package harness

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

type mockAdapter struct{}

// NewMock returns the deterministic keyless acceptance adapter.
func NewMock() Adapter { return mockAdapter{} }

func (mockAdapter) ID() string { return "mock" }

func (mockAdapter) Probe(context.Context) contract.Availability {
	return contract.Availability{Available: true, Installed: true, Configured: true, Label: "隔离 Demo Harness"}
}

func (mockAdapter) Run(ctx context.Context, invocation Invocation) (contract.Result, error) {
	if invocation.Emit != nil {
		if err := invocation.Emit(ProgressEvent{Type: "harness.started", Message: "deterministic demo harness started", At: time.Now().UTC()}); err != nil {
			return contract.Result{}, err
		}
	}
	select {
	case <-ctx.Done():
		return contract.Result{}, ctx.Err()
	case <-time.After(35 * time.Millisecond):
	}
	if invocation.Spec.Kind == contract.KindJudge {
		passed := stringValue(invocation.Spec.Expected["demoOutcome"]) != "fail"
		reason := "满足测试集断言"
		if !passed {
			reason = stringValue(invocation.Spec.Expected["failureReason"])
			if reason == "" {
				reason = "未满足测试集断言"
			}
		}
		score := 0.0
		if passed {
			score = 1
		}
		result := contract.Result{Passed: passed, Score: score, Reason: reason, Cost: 0.006, CostKnown: true, CostSource: contract.CostSourceProvider, Usage: contract.Usage{InputTokens: 40, OutputTokens: 8}}
		if invocation.Emit != nil {
			if err := invocation.Emit(ProgressEvent{Type: "harness.output", Message: reason, At: time.Now().UTC()}); err != nil {
				return contract.Result{}, err
			}
		}
		return result, nil
	}
	summary := stringValue(invocation.Spec.Expected["summary"])
	if summary == "" {
		summary = "任务成功完成"
	}
	output := fmt.Sprintf("已完成任务：%s。结果包含：%s。", invocation.Spec.Prompt, summary)
	if stringValue(invocation.Spec.Expected["demoOutcome"]) == "fail" {
		output = fmt.Sprintf("已完成任务：%s，但部分证据无法验证。", invocation.Spec.Prompt)
	}
	hash := fnv.New32a()
	caseID := invocation.Metadata["caseId"]
	if caseID == "" {
		caseID = invocation.ExecutionID
	}
	_, _ = hash.Write([]byte(caseID + invocation.Spec.Model))
	cost := float64(18+hash.Sum32()%19) / 1000
	if invocation.Emit != nil {
		if err := invocation.Emit(ProgressEvent{Type: "harness.output", Message: output, At: time.Now().UTC()}); err != nil {
			return contract.Result{}, err
		}
	}
	return contract.Result{Output: output, Cost: cost, CostKnown: true, CostSource: contract.CostSourceProvider, Usage: contract.Usage{InputTokens: 70, OutputTokens: 30}}, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
