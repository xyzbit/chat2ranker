package pricing

import (
	"math"
	"testing"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

func TestApplyCostPriority(t *testing.T) {
	usage := contract.Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 500_000}
	result := contract.Result{Usage: usage, ResolvedProvider: "deepseek", ResolvedModel: "deepseek-v4-flash"}
	Apply(&result, contract.Spec{}, nil)
	want := .44 + .1*1.32 + .5*.014
	if !result.CostKnown || result.CostSource != contract.CostSourceCatalog || math.Abs(result.Cost-want) > 1e-9 {
		t.Fatalf("unexpected catalog cost: %#v", result)
	}

	connection := &contract.ModelConnection{Provider: "deepseek", DefaultModel: "deepseek-v4-flash", Prices: map[string]contract.ModelPrice{"deepseek-v4-flash": {Input: 1, Output: 2, CacheRead: .5}}}
	result = contract.Result{Usage: usage}
	Apply(&result, contract.Spec{}, connection)
	if result.CostSource != contract.CostSourceConnection || result.Cost != 1.45 {
		t.Fatalf("unexpected override cost: %#v", result)
	}

	result = contract.Result{Cost: .03, CostKnown: true, CostSource: contract.CostSourceProvider, Usage: usage}
	Apply(&result, contract.Spec{}, connection)
	if result.Cost != .03 || result.CostSource != contract.CostSourceProvider {
		t.Fatalf("provider cost must win: %#v", result)
	}
}

func TestCatalogContainsOfficialConnectionDefaults(t *testing.T) {
	expected := map[string]struct {
		protocol string
		baseURL  string
		model    string
		input    float64
		output   float64
	}{
		"deepseek":  {contract.ProtocolOpenAIChat, "https://api.deepseek.com", "deepseek-v4-flash", .44, 1.32},
		"minimax":   {contract.ProtocolOpenAIChat, "https://api.minimaxi.com/v1", "MiniMax-M3", 2.1 / cnyPerUSD, 8.4 / cnyPerUSD},
		"glm":       {contract.ProtocolOpenAIChat, "https://open.bigmodel.cn/api/paas/v4", "glm-5.2", 8 / cnyPerUSD, 28 / cnyPerUSD},
		"openai":    {contract.ProtocolOpenAIResponses, "https://api.openai.com/v1", "gpt-5.6-sol", 4, 20},
		"anthropic": {contract.ProtocolAnthropic, "https://api.anthropic.com", "claude-sonnet-5", 2, 10},
		"kimi":      {contract.ProtocolOpenAIChat, "https://api.moonshot.cn/v1", "kimi-k3", 20 / cnyPerUSD, 100 / cnyPerUSD},
	}
	providers := Providers()
	if len(providers) != len(expected) {
		t.Fatalf("expected %d providers, got %#v", len(expected), providers)
	}
	for _, provider := range providers {
		want, ok := expected[provider.ID]
		if !ok || provider.BaseURL != want.baseURL || provider.SourceURL == "" || provider.Protocol != want.protocol {
			t.Fatalf("invalid provider metadata: %#v", provider)
		}
		price, found := Price(provider.ID, want.model)
		if !found || math.Abs(price.Input-want.input) > 1e-9 || math.Abs(price.Output-want.output) > 1e-9 {
			t.Fatalf("invalid %s price: %#v", provider.ID, price)
		}
	}
}

func TestApplyLeavesUnknownModelUnpriced(t *testing.T) {
	result := contract.Result{Usage: contract.Usage{InputTokens: 10}}
	Apply(&result, contract.Spec{Model: "internal-v3"}, &contract.ModelConnection{Provider: "custom", Prices: map[string]contract.ModelPrice{}})
	if result.CostKnown || result.Cost != 0 {
		t.Fatalf("unknown model must stay unpriced: %#v", result)
	}
}

func TestApplyLeavesIncompletePriceUnpriced(t *testing.T) {
	result := contract.Result{Usage: contract.Usage{InputTokens: 10, CacheWriteTokens: 5}}
	connection := &contract.ModelConnection{Provider: "custom", DefaultModel: "partial", Prices: map[string]contract.ModelPrice{"partial": {Input: 1, Output: 2}}}
	Apply(&result, contract.Spec{}, connection)
	if result.CostKnown || result.Cost != 0 {
		t.Fatalf("missing cache-write price must stay unpriced: %#v", result)
	}
}
