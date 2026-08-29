package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	executionclient "github.com/xyzbit/chat2ranker/execution/backend/client"
	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

type ExecutionRunnerConfig struct {
	Client       *executionclient.Client
	JudgeHarness string
	JudgeModel   string
	Timeout      time.Duration
}

type ExecutionRunner struct {
	harness string
	config  ExecutionRunnerConfig
}

func (runner ExecutionRunner) PreflightJudge(ctx context.Context) error {
	if runner.harness == "mock" {
		return nil
	}
	binding, err := runner.config.Client.GetSystemModelBinding(ctx, contract.SystemModelJudge)
	if err != nil {
		return fmt.Errorf("Judge 模型未配置：%w", err)
	}
	if binding.Connection.Status != "verified" {
		return fmt.Errorf("Judge 模型连接不可用")
	}
	return nil
}

func ExecutionRunners(config ExecutionRunnerConfig) RunnerRegistry {
	if config.JudgeHarness == "" {
		config.JudgeHarness = "dsh"
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Minute
	}
	result := RunnerRegistry{}
	for _, harness := range []string{"mock", "dsh", "pi", "claude-code", "codex", "hermes"} {
		result[harness] = ExecutionRunner{harness: harness, config: config}
	}
	return result
}

func (runner ExecutionRunner) Probe(ctx context.Context, agent domain.AgentVersion) domain.RuntimeAvailability {
	if runner.config.Client == nil {
		return domain.RuntimeAvailability{Reason: "Execution Service 未配置"}
	}
	availability, err := runner.config.Client.Probe(ctx, runner.harness)
	if err != nil {
		return domain.RuntimeAvailability{Label: harnessLabel(runner.harness), Reason: "Execution Service 不可用：" + err.Error()}
	}
	if agent.ModelConnectionID != "" && availability.Installed {
		availability.Available, availability.Configured, availability.Reason = true, true, ""
	}
	return domain.RuntimeAvailability{Available: availability.Available, Installed: availability.Installed, Configured: availability.Configured, Label: availability.Label, Reason: availability.Reason}
}

func (runner ExecutionRunner) RunCandidate(ctx context.Context, spec ExecutionSpec) (CandidateResult, error) {
	metadata := map[string]string{"runId": spec.RunID, "trialId": spec.TrialID, "trialIndex": fmt.Sprint(spec.TrialIndex), "caseId": spec.Case.ID, "caseTitle": spec.Case.Title, "agentVersionId": spec.Agent.ID}
	candidate, err := runner.config.Client.RunWithEvents(ctx, contract.SubmitRequest{
		IdempotencyKey: fmt.Sprintf("%s:%s:candidate:%d", spec.RunID, spec.TrialID, spec.Attempt),
		Spec:           contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: runner.harness, ModelConnectionID: spec.Agent.ModelConnectionID, Model: runtimeModel(spec.Agent.Model), Preset: spec.Agent.Preset, SystemPrompt: spec.Agent.SystemPrompt, Tools: spec.Agent.Tools, Skills: spec.Agent.Skills, Prompt: spec.Case.Input, Expected: spec.Case.Expected, TimeoutMs: runner.config.Timeout.Milliseconds(), Metadata: metadata},
	}, executionEventEmitter(spec.Emit, "candidate"))
	if err != nil {
		return CandidateResult{}, err
	}
	if err := emitExecutionCompleted(spec.Emit, "candidate"); err != nil {
		return CandidateResult{}, err
	}
	return CandidateResult{Output: candidate.Result.Output, Cost: candidate.Result.Cost, CostKnown: candidate.Result.CostKnown, CostEstimated: candidate.Result.CostKnown && candidate.Result.CostSource != contract.CostSourceProvider, DurationMs: candidate.Result.DurationMs, ExecutionID: candidate.ID, Usage: domain.Usage{
		InputTokens: candidate.Result.Usage.InputTokens, OutputTokens: candidate.Result.Usage.OutputTokens,
		CacheReadTokens: candidate.Result.Usage.CacheReadTokens, CacheWriteTokens: candidate.Result.Usage.CacheWriteTokens, ReasoningTokens: candidate.Result.Usage.ReasoningTokens,
	}, Artifacts: mapArtifacts(candidate.Result.Artifacts)}, nil
}

func (runner ExecutionRunner) RunJudge(ctx context.Context, spec JudgeSpec) (JudgeResult, error) {
	judgeHarness := spec.Evaluator.Judge.Harness
	if judgeHarness == "" {
		judgeHarness = runner.config.JudgeHarness
	}
	if runner.harness == "mock" {
		judgeHarness = "mock"
	}
	model, connectionID := spec.Evaluator.Judge.Model, ""
	if judgeHarness != "mock" {
		binding, err := runner.config.Client.GetSystemModelBinding(ctx, contract.SystemModelJudge)
		if err != nil {
			return JudgeResult{}, fmt.Errorf("Judge 模型未配置：%w", err)
		}
		model, connectionID = binding.Model, binding.ConnectionID
	}
	expected := make(map[string]any, len(spec.Case.Expected)+1)
	for key, value := range spec.Case.Expected {
		expected[key] = value
	}
	expected["rubricCriterion"] = spec.Criterion
	metadata := map[string]string{"runId": spec.RunID, "trialId": spec.TrialID, "trialIndex": fmt.Sprint(spec.TrialIndex), "caseId": spec.Case.ID, "caseTitle": spec.Case.Title, "agentVersionId": spec.Agent.ID, "criterionId": spec.Criterion.ID}
	judge, err := runner.config.Client.RunWithEvents(ctx, contract.SubmitRequest{
		IdempotencyKey: fmt.Sprintf("%s:%s:judge:%s:%d", spec.RunID, spec.TrialID, spec.Criterion.ID, spec.Attempt),
		Spec:           contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindJudge, Harness: judgeHarness, ModelConnectionID: connectionID, Model: model, Prompt: spec.Case.Input, Expected: expected, CandidateOutput: spec.Candidate.Output, TimeoutMs: runner.config.Timeout.Milliseconds(), Metadata: metadata},
	}, executionEventEmitter(spec.Emit, "judge"))
	if err != nil {
		return JudgeResult{}, err
	}
	if err := emitExecutionCompleted(spec.Emit, "judge"); err != nil {
		return JudgeResult{}, err
	}
	if judge.Result.Score < 0 || judge.Result.Score > 1 || strings.TrimSpace(judge.Result.Reason) == "" {
		return JudgeResult{}, fmt.Errorf("judge %s returned an invalid verdict", spec.Criterion.ID)
	}
	return JudgeResult{Passed: judge.Result.Passed, Score: judge.Result.Score, Reason: judge.Result.Reason, Cost: judge.Result.Cost, CostKnown: judge.Result.CostKnown, CostEstimated: judge.Result.CostKnown && judge.Result.CostSource != contract.CostSourceProvider, DurationMs: judge.Result.DurationMs, ExecutionID: judge.ID, Usage: domain.Usage{
		InputTokens: judge.Result.Usage.InputTokens, OutputTokens: judge.Result.Usage.OutputTokens,
		CacheReadTokens: judge.Result.Usage.CacheReadTokens, CacheWriteTokens: judge.Result.Usage.CacheWriteTokens, ReasoningTokens: judge.Result.Usage.ReasoningTokens,
	}, Artifacts: mapArtifacts(judge.Result.Artifacts)}, nil
}

func executionEventEmitter(emit func(RunnerEvent) error, phase string) func(contract.Event) error {
	if emit == nil {
		return nil
	}
	return func(event contract.Event) error {
		if event.Type == "execution.completed" {
			return nil
		}
		return emit(RunnerEvent{Type: phase + "." + strings.TrimPrefix(event.Type, "execution."), Status: event.Status, Reason: event.Message})
	}
}

func emitExecutionCompleted(emit func(RunnerEvent) error, phase string) error {
	if emit == nil {
		return nil
	}
	return emit(RunnerEvent{Type: phase + ".completed", Status: contract.StatusCompleted})
}

func mapArtifacts(values []contract.ArtifactRef) []domain.ArtifactRef {
	result := make([]domain.ArtifactRef, len(values))
	for index, value := range values {
		result[index] = domain.ArtifactRef{Kind: value.Kind, ExecutionID: value.ExecutionID, Path: value.Path, Size: value.Size}
	}
	return result
}

func harnessLabel(harness string) string {
	if value := map[string]string{"mock": "隔离 Demo Harness", "dsh": "DeepSeek Harness", "pi": "Pi", "claude-code": "Claude Code", "codex": "Codex", "hermes": "Hermes"}[harness]; value != "" {
		return value
	}
	return fmt.Sprintf("Harness %s", harness)
}

func runtimeModel(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "由") {
		return ""
	}
	return value
}

type ExecutionArtifactReader struct{ Client *executionclient.Client }

func (reader ExecutionArtifactReader) ReadArtifact(ctx context.Context, executionID, path string) (ArtifactContent, error) {
	value, err := reader.Client.ReadArtifact(ctx, executionID, path)
	return ArtifactContent{Path: value.Path, Content: value.Content, Truncated: value.Truncated}, err
}
