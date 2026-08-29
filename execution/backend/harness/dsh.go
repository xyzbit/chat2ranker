package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

type dshAdapter struct {
	commandPath string
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
		cli: NewCommand(CommandConfig{
			ID: "dsh", Label: "DeepSeek Harness",
			Argv:                []string{"node", commandPath, "--profile", "{preset}", "{prompt}"},
			RequiredFile:        commandPath,
			RequiredEnvironment: "DEEPSEEK_API_KEY",
		}),
	}
}

func (*dshAdapter) ID() string { return "dsh" }

func (adapter *dshAdapter) Probe(ctx context.Context) contract.Availability {
	return adapter.cli.Probe(ctx)
}

func (adapter *dshAdapter) Run(ctx context.Context, invocation Invocation) (contract.Result, error) {
	provider := "deepseek-official"
	requiredEnvironment := "DEEPSEEK_API_KEY"
	if connection := invocation.ModelConnection; connection != nil {
		provider, requiredEnvironment = "rank-connection", ""
		api := dshAPI(connection.Protocol)
		model := strings.TrimSpace(invocation.Spec.Model)
		if model == "" {
			model = connection.DefaultModel
		}
		settings := fmt.Sprintf("llm-pi-ai:\n  providers:\n    rank-connection:\n      displayName: Rank connection\n      apiKeyEnv: RANK_MODEL_API_KEY\n      api: %s\n      baseURL: %q\n      models:\n        - id: %q\n          name: %q\n", api, connection.BaseURL, model, model)
		if err := os.WriteFile(filepath.Join(invocation.HarnessHome, "settings.yaml"), []byte(settings), 0o600); err != nil {
			return contract.Result{}, err
		}
		invocation.Environment = mergeEnvironment(invocation.Environment, map[string]string{"RANK_MODEL_API_KEY": invocation.Credential})
	}
	patches := dshInvocationPatchesFor(invocation.Spec, provider)
	argv := []string{"node", adapter.commandPath, "--profile", "{preset}"}
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
	argv = append(argv, "{prompt}")
	configured := NewCommand(CommandConfig{ID: "dsh", Label: "DeepSeek Harness", Argv: argv, RequiredFile: adapter.commandPath, RequiredEnvironment: requiredEnvironment})
	withoutDuplicatedSystemPrompt := invocation
	withoutDuplicatedSystemPrompt.Spec.SystemPrompt = ""
	result, err := configured.Run(ctx, withoutDuplicatedSystemPrompt)
	if err != nil {
		return result, err
	}
	usage, resolvedProvider, resolvedModel, accountingErr := readDSHAccounting(invocation.HarnessHome)
	if accountingErr != nil {
		if invocation.Emit != nil {
			_ = invocation.Emit(ProgressEvent{Type: "harness.usage.unavailable", Message: accountingErr.Error()})
		}
		return result, nil
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
	return result, nil
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
