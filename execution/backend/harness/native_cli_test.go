package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestParseCodexJSONLEvents(t *testing.T) {
	result, err := parseCodexResult(contract.Result{Output: "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"done\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}"})
	if err != nil || result.Output != "done" || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 {
		t.Fatalf("unexpected Codex result: %#v %v", result, err)
	}
}

func TestParseClaudeStreamJSON(t *testing.T) {
	result, err := parseClaudeResult(contract.Result{Output: "{\"type\":\"system\"}\n{\"type\":\"result\",\"result\":\"answer\",\"total_cost_usd\":0.012,\"usage\":{\"input_tokens\":8,\"output_tokens\":2}}"})
	if err != nil || result.Output != "answer" || result.CostKnown || result.Cost != 0 || result.Usage.InputTokens != 8 {
		t.Fatalf("unexpected Claude result: %#v %v", result, err)
	}
}

func TestParseHermesUsageReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"input_tokens":9,"output_tokens":4,"estimated_cost_usd":0.02}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := parseHermesResult(contract.Result{Output: "answer"}, path)
	if err != nil || result.CostKnown || result.Cost != 0 || result.Usage.OutputTokens != 4 {
		t.Fatalf("unexpected Hermes result: %#v %v", result, err)
	}
}

func TestParseHermesAcceptsOnlyProviderActualCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"input_tokens":9,"output_tokens":4,"cost_status":"actual","actual_cost_usd":0.01}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := parseHermesResult(contract.Result{Output: "answer"}, path)
	if err != nil || !result.CostKnown || result.Cost != .01 || result.CostSource != contract.CostSourceProvider {
		t.Fatalf("unexpected Hermes actual cost: %#v %v", result, err)
	}
}

func TestNativeCLIConfigurationIsolationFlags(t *testing.T) {
	if !contains(codexArgv(false), "--ignore-user-config") || contains(codexArgv(true), "--ignore-user-config") || !contains(claudeArgv(), "--safe-mode") {
		t.Fatal("native CLI runs must isolate user configuration")
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
