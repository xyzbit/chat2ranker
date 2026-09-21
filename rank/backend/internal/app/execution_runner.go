package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	executionclient "github.com/xyzbit/agentkiln/execution/backend/client"
	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

type ExecutionRunnerConfig struct {
	Client       *executionclient.Client
	JudgeHarness string
	JudgeModel   string
	Timeout      time.Duration
	TenantID     string
}

type ExecutionRunner struct {
	invoke  func(context.Context, contract.SubmitRequest, func(contract.Event) error) (contract.Execution, error)
	harness string
	config  ExecutionRunnerConfig
}

// PrepareJudge resolves the system model before Run admission. Recovery uses
// this frozen configuration instead of consulting a mutable system binding.
func (runner ExecutionRunner) PrepareJudge(ctx context.Context, evaluator domain.EvaluatorVersion) (domain.JudgeConfig, error) {
	config := evaluator.Judge
	if runner.harness == "mock" || len(evaluator.Rubric) == 0 {
		return config, nil
	}
	binding, err := runner.config.Client.GetSystemModelBinding(ctx, contract.SystemModelJudge)
	if err != nil {
		return config, fmt.Errorf("Judge 模型未配置：%w", err)
	}
	if binding.Connection.Status != "verified" {
		return config, fmt.Errorf("Judge 模型连接不可用")
	}
	if err := contract.ValidateModelSelection(contract.Spec{ModelConnectionID: binding.ConnectionID, Model: binding.Model}); err != nil {
		return config, fmt.Errorf("Judge 模型未固定：%w", err)
	}
	config.Model, config.ModelConnectionID = binding.Model, binding.ConnectionID
	return config, nil
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
	return domain.RuntimeAvailability{StructuredHistory: availability.StructuredHistory, Available: availability.Available, Installed: availability.Installed, Configured: availability.Configured, Label: availability.Label, Reason: availability.Reason}
}

func (runner ExecutionRunner) RunCandidate(ctx context.Context, spec ExecutionSpec) (CandidateResult, error) {
	request := runner.candidateRequest(spec)
	candidate, err := runner.invoke(ctx, request, executionEventEmitter(spec.Emit, "candidate", domain.InvocationID(request.IdempotencyKey)))
	if candidate.Result == nil {
		return CandidateResult{ExecutionID: candidate.ID}, err
	}
	return CandidateResult{Output: candidate.Result.Output, Cost: candidate.Result.Cost, CostKnown: candidate.Result.CostKnown, CostEstimated: candidate.Result.CostKnown && candidate.Result.CostSource != contract.CostSourceProvider, DurationMs: candidate.Result.DurationMs, ExecutionID: candidate.ID, Usage: domain.Usage{
		InputTokens: candidate.Result.Usage.InputTokens, OutputTokens: candidate.Result.Usage.OutputTokens,
		CacheReadTokens: candidate.Result.Usage.CacheReadTokens, CacheWriteTokens: candidate.Result.Usage.CacheWriteTokens, ReasoningTokens: candidate.Result.Usage.ReasoningTokens,
	}, Artifacts: mapArtifacts(candidate.Result.Artifacts)}, err
}

func (runner ExecutionRunner) candidateRequest(spec ExecutionSpec) contract.SubmitRequest {
	metadata := map[string]string{"tenantId": spec.Settings.TenantID, "runId": spec.RunID, "trialId": spec.TrialID, "trialIndex": fmt.Sprint(spec.TrialIndex), "caseId": spec.Case.ID, "caseTitle": spec.Case.Title, "agentVersionId": spec.Agent.ID}
	return contract.SubmitRequest{
		IdempotencyKey: fmt.Sprintf("%s:%s:candidate", spec.RunID, spec.TrialID),
		Spec:           contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindAgent, Harness: runner.harness, ModelConnectionID: spec.Agent.ModelConnectionID, Model: runtimeModel(spec.Agent.Model), Preset: spec.Agent.Preset, SystemPrompt: spec.Agent.SystemPrompt, Tools: spec.Agent.Tools, Skills: spec.Agent.Skills, Prompt: spec.Case.Input, Expected: spec.Case.Expected, TimeoutMs: spec.Settings.TimeoutMs, Metadata: metadata},
	}
}

func (runner ExecutionRunner) RunJudge(ctx context.Context, spec JudgeSpec) (JudgeResult, error) {
	judgeHarness := spec.Evaluator.Judge.Harness
	if judgeHarness == "" {
		judgeHarness = runner.config.JudgeHarness
	}
	if runner.harness == "mock" {
		judgeHarness = "mock"
	}
	model, connectionID := spec.Evaluator.Judge.Model, spec.Evaluator.Judge.ModelConnectionID
	expected := make(map[string]any, len(spec.Case.Expected)+1)
	for key, value := range spec.Case.Expected {
		expected[key] = value
	}
	expected["rubricCriterion"] = spec.Criterion
	metadata := map[string]string{"tenantId": spec.Settings.TenantID, "runId": spec.RunID, "trialId": spec.TrialID, "trialIndex": fmt.Sprint(spec.TrialIndex), "caseId": spec.Case.ID, "caseTitle": spec.Case.Title, "agentVersionId": spec.Agent.ID, "criterionId": spec.Criterion.ID}
	request := contract.SubmitRequest{
		IdempotencyKey: fmt.Sprintf("%s:%s:judge:%s", spec.RunID, spec.TrialID, spec.Criterion.ID),
		Spec:           contract.Spec{ProtocolVersion: contract.ProtocolVersion, Kind: contract.KindJudge, Harness: judgeHarness, ModelConnectionID: connectionID, Model: model, Prompt: spec.Case.Input, Expected: expected, CandidateOutput: spec.Candidate.Output, TimeoutMs: spec.Settings.TimeoutMs, Metadata: metadata},
	}
	judge, err := runner.invoke(ctx, request, executionEventEmitter(spec.Emit, "judge", domain.InvocationID(request.IdempotencyKey)))
	if judge.Result == nil {
		return JudgeResult{ExecutionID: judge.ID}, err
	}
	if err == nil && (judge.Result.Score < 0 || judge.Result.Score > 1 || strings.TrimSpace(judge.Result.Reason) == "") {
		return JudgeResult{}, fmt.Errorf("judge %s returned an invalid verdict", spec.Criterion.ID)
	}
	return JudgeResult{Passed: judge.Result.Passed, Score: judge.Result.Score, Reason: judge.Result.Reason, Cost: judge.Result.Cost, CostKnown: judge.Result.CostKnown, CostEstimated: judge.Result.CostKnown && judge.Result.CostSource != contract.CostSourceProvider, DurationMs: judge.Result.DurationMs, ExecutionID: judge.ID, Usage: domain.Usage{
		InputTokens: judge.Result.Usage.InputTokens, OutputTokens: judge.Result.Usage.OutputTokens,
		CacheReadTokens: judge.Result.Usage.CacheReadTokens, CacheWriteTokens: judge.Result.Usage.CacheWriteTokens, ReasoningTokens: judge.Result.Usage.ReasoningTokens,
	}, Artifacts: mapArtifacts(judge.Result.Artifacts)}, err
}

func executionEventEmitter(emit func(RunnerEvent) error, phase string, id domain.InvocationID) func(contract.Event) error {
	if emit == nil {
		return nil
	}
	return func(event contract.Event) error {
		return emit(RunnerEvent{InvocationID: id, Sequence: event.Sequence, Type: phase + "." + strings.TrimPrefix(event.Type, "execution."), Status: event.Status, Reason: event.Message})
	}
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
	return ArtifactContent{Path: value.Path, Content: value.Content, Encoding: value.Encoding, Truncated: value.Truncated}, err
}

func validateFrozenAgentModel(agent domain.AgentVersion) error {
	if err := contract.ValidateModelSelection(contract.Spec{ModelConnectionID: agent.ModelConnectionID, Model: runtimeModel(agent.Model)}); err != nil {
		return problem(409, "agent_model_unresolved", "Agent 版本未固定模型名称，请选择明确模型并保存新版本")
	}
	return nil
}
