package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

type dshAdapter struct {
	commandPath string
	historyPath string
	budgetPath  string
	cli         Adapter
}

// NewDSH returns the first-party DeepSeek Harness adapter. Per-Agent model and
// system-prompt values are applied as an invocation-local DSH patch layer.
func NewDSH(repositoryRoot string) Adapter {
	commandPath := strings.TrimSpace(os.Getenv("RANK_DSH_BIN"))
	if commandPath == "" {
		commandPath = filepath.Join(repositoryRoot, "apps/cli/lib/bin.js")
	}
	return &dshAdapter{
		commandPath: commandPath,
		historyPath: filepath.Join(repositoryRoot, "apps/execution-runtime/history-runner.mjs"),
		budgetPath:  filepath.Join(repositoryRoot, "apps/execution-runtime/model-budget.mjs"),
		cli: NewCommand(CommandConfig{
			ID: "dsh", Label: "DeepSeek Harness",
			Argv:                []string{"node", "--expose-internals", commandPath, "--profile", "{preset}", "{prompt}"},
			RequiredFile:        commandPath,
			RequiredEnvironment: "DEEPSEEK_API_KEY",
		}),
	}
}

func (*dshAdapter) ID() string { return "dsh" }

func (adapter *dshAdapter) Probe(ctx context.Context) contract.Availability {
	availability := adapter.cli.Probe(ctx)
	_, err := os.Stat(adapter.historyPath)
	availability.StructuredHistory = err == nil
	_, err = os.Stat(adapter.budgetPath)
	availability.ModelRequestBudget = err == nil
	availability.ModelQuota = err == nil
	return availability
}

func (adapter *dshAdapter) Run(ctx context.Context, invocation Invocation) (contract.Result, error) {
	provider := "deepseek-official"
	requiredEnvironment := "DEEPSEEK_API_KEY"
	if connection := invocation.ModelConnection; connection != nil {
		provider, requiredEnvironment = "rank-connection", ""
		api := dshAPI(connection.Protocol)
		model := strings.TrimSpace(invocation.Spec.Model)
		settings := fmt.Sprintf("llm-pi-ai:\n  providers:\n    rank-connection:\n      displayName: Rank connection\n      apiKeyEnv: RANK_MODEL_API_KEY\n      api: %s\n      baseURL: %q\n      models:\n        - id: %q\n          name: %q\n", api, connection.BaseURL, model, model)
		if invocation.ModelQuota != nil {
			settings += fmt.Sprintf("          maxTokens: %d\n", invocation.ModelQuota.MaxOutputTokens)
		}
		if err := os.WriteFile(filepath.Join(invocation.HarnessHome, "settings.yaml"), []byte(settings), 0o600); err != nil {
			return contract.Result{}, err
		}
		invocation.Environment = mergeEnvironment(invocation.Environment, map[string]string{"RANK_MODEL_API_KEY": invocation.Credential})
	}
	patchSpec := invocation.Spec
	if len(patchSpec.Messages) > 0 {
		hints := patchSpec
		hints.SystemPrompt = ""
		patchSpec.SystemPrompt = strings.TrimSpace(patchSpec.SystemPrompt + "\n\n" + effectivePrompt(hints))
	}
	patches := dshInvocationPatchesFor(patchSpec, provider)
	if invocation.ModelQuota != nil {
		encoded, err := json.Marshal(invocation.ModelQuota)
		if err != nil {
			return contract.Result{}, err
		}
		invocation.Environment = mergeEnvironment(invocation.Environment, map[string]string{"RANK_MODEL_QUOTA_TOKEN": string(encoded)})
	}
	if invocation.MaxModelRequests > 0 {
		patches = append(patches, map[string]any{"insert": []map[string]any{{"id": "rank-model-budget", "name": adapter.budgetPath, "config": map[string]any{"maxRequests": invocation.MaxModelRequests}}}})
	}
	if len(invocation.Spec.Messages) > 0 {
		inputPath := filepath.Join(invocation.HarnessHome, "history.json")
		if err := writeJSON(inputPath, map[string]any{"executionId": invocation.ExecutionID, "messages": invocation.Spec.Messages}); err != nil {
			return contract.Result{}, err
		}
		patches = append(patches,
			map[string]any{"id": "headless-runner", "disabled": true},
			map[string]any{"insert": []map[string]any{{"id": "rank-history-runner", "name": adapter.historyPath, "config": map[string]any{"inputPath": inputPath}}}},
		)
	}
	argv := []string{"node", "--expose-internals", adapter.commandPath, "--profile", "{preset}"}
	if len(patches) > 0 {
		payload, err := json.MarshalIndent(patches, "", "  ")
		if err != nil {
			return contract.Result{}, err
		}
		patchPath := filepath.Join(invocation.HarnessHome, "rank-agent.patch.yml")
		if err := os.WriteFile(patchPath, append(payload, '\n'), 0o600); err != nil {
			return contract.Result{}, err
		}
		argv = append(argv, "--patch", patchPath)
	}
	if len(invocation.Spec.Messages) > 0 {
		argv = append(argv, "rank-history")
	} else {
		argv = append(argv, "{prompt}")
	}
	configured := NewCommand(CommandConfig{ID: "dsh", Label: "DeepSeek Harness", Argv: argv, RequiredFile: adapter.commandPath, RequiredEnvironment: requiredEnvironment})
	withoutDuplicatedSystemPrompt := invocation
	withoutDuplicatedSystemPrompt.Spec.SystemPrompt = ""
	result, runErr := configured.Run(ctx, withoutDuplicatedSystemPrompt)
	usage, resolvedProvider, resolvedModel, accountingErr := readDSHAccounting(invocation.HarnessHome)
	if accountingErr != nil {
		if invocation.Emit != nil {
			_ = invocation.Emit(ProgressEvent{Type: "harness.usage.unavailable", Message: accountingErr.Error()})
		}
		return result, runErr
	}
	result.Usage = usage
	result.ResolvedProvider, result.ResolvedModel = resolvedProvider, resolvedModel
	if invocation.ModelConnection != nil {
		result.ResolvedProvider = invocation.ModelConnection.Provider
	} else if resolvedProvider == "deepseek-official" {
		baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL")), "/")
		if baseURL == "" || baseURL == "https://api.deepseek.com" {
			result.ResolvedProvider = "deepseek"
		} else {
			// A compatible gateway may expose DeepSeek model names without using
			// DeepSeek's prices. Keep it unpriced unless the user identifies the
			// provider through an explicit model connection.
			result.ResolvedProvider = "custom"
		}
	}
	return result, runErr
}

func dshAPI(protocol string) string {
	switch protocol {
	case contract.ProtocolOpenAIResponses:
		return "openai-responses"
	case contract.ProtocolAnthropic:
		return "anthropic-messages"
	default:
		return "openai-completions"
	}
}

func dshInvocationPatches(spec contract.Spec) []map[string]any {
	return dshInvocationPatchesFor(spec, "deepseek-official")
}

func dshInvocationPatchesFor(spec contract.Spec, provider string) []map[string]any {
	patches := []map[string]any{}
	model := strings.TrimSpace(spec.Model)
	if model != "" && !strings.HasPrefix(model, "由") {
		patches = append(patches, map[string]any{
			"id": "agent-default-model",
			"config": map[string]any{
				"provider": provider,
				"model":    model,
			},
		})
	}
	if systemPrompt := strings.TrimSpace(spec.SystemPrompt); systemPrompt != "" {
		patches = append(patches, map[string]any{"id": "system-prompt", "config": map[string]any{"persona": systemPrompt}})
	}
	// Rank executions need one accountable model lifecycle. DSH's optional
	// LLM-generated session title would create a second, unrelated provider call.
	patches = append(patches, map[string]any{"id": "session-title-llm", "disabled": true})
	return patches
}
