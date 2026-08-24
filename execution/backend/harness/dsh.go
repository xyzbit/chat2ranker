package harness

import (
	"context"
	"encoding/json"
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
	commandPath := filepath.Join(repositoryRoot, "apps/cli/lib/bin.js")
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
	patches := dshInvocationPatches(invocation.Spec)
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
	configured := NewCommand(CommandConfig{ID: "dsh", Label: "DeepSeek Harness", Argv: argv, RequiredFile: adapter.commandPath, RequiredEnvironment: "DEEPSEEK_API_KEY"})
	withoutDuplicatedSystemPrompt := invocation
	withoutDuplicatedSystemPrompt.Spec.SystemPrompt = ""
	result, err := configured.Run(ctx, withoutDuplicatedSystemPrompt)
	if err != nil {
		return result, err
	}
	usage, cost, costKnown, accountingErr := readDSHAccounting(invocation.HarnessHome, os.Getenv("DEEPSEEK_BASE_URL"))
	if accountingErr != nil {
		if invocation.Emit != nil {
			_ = invocation.Emit(ProgressEvent{Type: "harness.usage.unavailable", Message: accountingErr.Error()})
		}
		return result, nil
	}
	result.Usage = usage
	result.Cost = cost
	result.CostKnown = costKnown
	return result, nil
}

func dshInvocationPatches(spec contract.Spec) []map[string]any {
	patches := []map[string]any{}
	model := strings.TrimSpace(spec.Model)
	if model != "" && !strings.HasPrefix(model, "由") {
		patches = append(patches, map[string]any{
			"id": "agent-default-model",
			"config": map[string]any{
				"provider": "deepseek-official",
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
