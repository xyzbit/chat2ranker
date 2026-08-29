package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

type nativeCLIAdapter struct{ id, label, binary string }

func NewCodex() Adapter { return &nativeCLIAdapter{id: "codex", label: "Codex", binary: "codex"} }
func NewClaudeCode() Adapter {
	return &nativeCLIAdapter{id: "claude-code", label: "Claude Code", binary: "claude"}
}
func NewHermes() Adapter { return &nativeCLIAdapter{id: "hermes", label: "Hermes", binary: "hermes"} }

func (a *nativeCLIAdapter) ID() string { return a.id }

func (a *nativeCLIAdapter) Probe(ctx context.Context) contract.Availability {
	availability := contract.Availability{Label: a.label}
	path, err := exec.LookPath(a.binary)
	if err != nil {
		availability.Reason = a.label + " CLI 未安装"
		return availability
	}
	availability.Installed = true
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(check, path, "--version").CombinedOutput(); err != nil {
		availability.Reason = a.label + " CLI 无法启动：" + compact(string(output), 180)
		return availability
	}
	availability.Available, availability.Configured = true, true
	return availability
}

func (a *nativeCLIAdapter) Run(ctx context.Context, invocation Invocation) (contract.Result, error) {
	if invocation.ModelConnection != nil {
		if a.id == "claude-code" {
			return contract.Result{}, errors.New("Claude Code 使用自身登录，不接受 OpenAI 兼容模型连接")
		}
		if a.id == "codex" && invocation.ModelConnection.Protocol != contract.ProtocolOpenAIResponses {
			return contract.Result{}, errors.New("Codex 自定义模型连接仅支持 OpenAI Responses 协议")
		}
		if a.id == "hermes" && invocation.ModelConnection.Protocol != contract.ProtocolOpenAIChat {
			return contract.Result{}, errors.New("Hermes 自定义模型连接仅支持 OpenAI Chat Completions 协议")
		}
	}
	prompt := effectivePrompt(invocation.Spec)
	configured := invocation
	userHome := os.Getenv("EXECUTION_USER_HOME")
	var argv []string
	switch a.id {
	case "codex":
		argv = codexArgv(invocation.ModelConnection != nil)
		if model := strings.TrimSpace(invocation.Spec.Model); model != "" {
			argv = append(argv, "--model", model)
		}
		if connection := invocation.ModelConnection; connection != nil {
			config := fmt.Sprintf("model_provider = \"rank\"\n[model_providers.rank]\nname = \"Rank connection\"\nbase_url = %q\nenv_key = \"RANK_MODEL_API_KEY\"\nwire_api = \"responses\"\n", connection.BaseURL)
			if err := os.WriteFile(filepath.Join(invocation.HarnessHome, "config.toml"), []byte(config), 0o600); err != nil {
				return contract.Result{}, err
			}
			configured.Environment = mergeEnvironment(configured.Environment, map[string]string{"CODEX_HOME": invocation.HarnessHome, "RANK_MODEL_API_KEY": invocation.Credential})
		} else if userHome != "" {
			configured.Environment = mergeEnvironment(configured.Environment, map[string]string{"CODEX_HOME": filepath.Join(userHome, ".codex")})
		}
		argv = append(argv, prompt)
	case "claude-code":
		if userHome != "" {
			configured.Environment = mergeEnvironment(configured.Environment, map[string]string{"HOME": userHome, "CLAUDE_CONFIG_DIR": filepath.Join(userHome, ".claude")})
		}
		argv = claudeArgv()
		if model := strings.TrimSpace(invocation.Spec.Model); model != "" {
			argv = append(argv, "--model", model)
		}
		argv = append(argv, prompt)
	case "hermes":
		usagePath := filepath.Join(invocation.ArtifactDir, "hermes-usage.json")
		argv = []string{"hermes", "-z", prompt, "--usage-file", usagePath}
		if model := strings.TrimSpace(invocation.Spec.Model); model != "" {
			argv = append(argv, "--model", model)
		}
		if connection := invocation.ModelConnection; connection != nil {
			configured.Environment = mergeEnvironment(configured.Environment, map[string]string{"OPENAI_BASE_URL": connection.BaseURL, "OPENAI_API_KEY": invocation.Credential})
		}
	}
	base, err := NewCommand(CommandConfig{ID: a.id, Label: a.label, Argv: argv}).Run(ctx, configured)
	if err != nil {
		return base, err
	}
	switch a.id {
	case "codex":
		base, err = parseCodexResult(base)
	case "claude-code":
		base, err = parseClaudeResult(base)
	case "hermes":
		base, err = parseHermesResult(base, filepath.Join(invocation.ArtifactDir, "hermes-usage.json"))
	}
	if err != nil {
		return base, err
	}
	base.ResolvedModel = firstText(base.ResolvedModel, invocation.Spec.Model)
	if connection := invocation.ModelConnection; connection != nil {
		base.ResolvedProvider = connection.Provider
		base.ResolvedModel = firstText(base.ResolvedModel, connection.DefaultModel)
	}
	return base, nil
}

func codexArgv(customConnection bool) []string {
	argv := []string{"codex", "exec", "--json", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only"}
	if !customConnection {
		argv = append(argv, "--ignore-user-config")
	}
	return argv
}

func claudeArgv() []string {
	return []string{"claude", "--print", "--output-format", "stream-json", "--verbose", "--permission-mode", "plan", "--no-session-persistence", "--safe-mode"}
}

func parseCodexResult(result contract.Result) (contract.Result, error) {
	var output string
	scanner := bufio.NewScanner(strings.NewReader(result.Output))
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event["type"] == "item.completed" {
			item, _ := event["item"].(map[string]any)
			if item["type"] == "agent_message" {
				output, _ = item["text"].(string)
			}
		}
		if event["type"] == "turn.completed" {
			if usage, ok := event["usage"].(map[string]any); ok {
				result.Usage = usageFromMap(usage)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if strings.TrimSpace(output) == "" {
		return result, errors.New("Codex 未返回 agent_message")
	}
	result.Output = output
	return result, nil
}

func parseClaudeResult(result contract.Result) (contract.Result, error) {
	scanner := bufio.NewScanner(strings.NewReader(result.Output))
	var found bool
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event["type"] != "result" {
			continue
		}
		found = true
		if output, ok := event["result"].(string); ok {
			result.Output = output
		}
		if usage, ok := event["usage"].(map[string]any); ok {
			result.Usage = usageFromMap(usage)
		}
		if models, ok := event["modelUsage"].(map[string]any); ok && len(models) == 1 {
			for model := range models {
				result.ResolvedModel = model
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if !found {
		return result, errors.New("Claude Code 未返回 result 事件")
	}
	return result, nil
}

func parseHermesResult(result contract.Result, path string) (contract.Result, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return result, err
	}
	var usage map[string]any
	if err := json.Unmarshal(payload, &usage); err != nil {
		return result, fmt.Errorf("parse Hermes usage: %w", err)
	}
	result.Usage = usageFromMap(usage)
	if status, _ := usage["cost_status"].(string); status == "actual" {
		if cost, ok := firstNumber(usage, "actual_cost_usd"); ok {
			result.Cost, result.CostKnown, result.CostSource = cost, true, contract.CostSourceProvider
		}
	}
	result.ResolvedModel, _ = usage["model"].(string)
	result.ResolvedProvider, _ = usage["provider"].(string)
	return result, nil
}

func usageFromMap(value map[string]any) contract.Usage {
	integer := func(keys ...string) int64 { number, _ := firstNumber(value, keys...); return int64(number) }
	return contract.Usage{InputTokens: integer("input_tokens", "inputTokens"), OutputTokens: integer("output_tokens", "outputTokens"), CacheReadTokens: integer("cache_read_input_tokens", "cacheReadTokens"), CacheWriteTokens: integer("cache_creation_input_tokens", "cacheWriteTokens"), ReasoningTokens: integer("reasoning_tokens", "reasoningTokens")}
}

func firstNumber(value map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if result, ok := number(value[key]); ok {
			return result, true
		}
	}
	return 0, false
}
func number(value any) (float64, bool) { result, ok := value.(float64); return result, ok }
func firstText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
func compact(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value == "" {
		return "未知错误"
	}
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}
func mergeEnvironment(base, extra map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}
