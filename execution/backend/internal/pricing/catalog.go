package pricing

import (
	"strings"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

const (
	updatedAt = "2026-08-27"
	cnyPerUSD = 7.0
)

var providers = []contract.ModelProvider{
	{
		ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com", Protocol: contract.ProtocolOpenAIChat,
		SourceURL: "https://api-docs.deepseek.com/quick_start/pricing",
		Models: []contract.CatalogModel{
			model("deepseek-v4-flash", "DeepSeek V4 Flash", .44, 1.32, .014, 0, "峰值时段价格；非峰值时段半价"),
			model("deepseek-v4-pro", "DeepSeek V4 Pro", 1.32, 3.96, .044, 0, "峰值时段价格；非峰值时段半价"),
			model("deepseek-v4-flash-vision-exp", "DeepSeek V4 Flash Vision Exp", .44, 1.32, .014, 0, "实验视觉模型；峰值时段价格；非峰值时段半价"),
		},
	},
	{
		ID: "minimax", Name: "MiniMax", BaseURL: "https://api.minimaxi.com/v1", Protocol: contract.ProtocolOpenAIChat,
		SourceURL: "https://platform.minimaxi.com/docs/guides/pricing-paygo",
		Models: []contract.CatalogModel{
			cnyModel("MiniMax-M3", "MiniMax M3", 2.1, 8.4, .42, 0, "≤512K 输入的当前标准价格；超长输入按官方阶梯价；官网未单列缓存写入价格"),
			cnyModel("MiniMax-M2.7", "MiniMax M2.7", 2.1, 8.4, .42, 2.625, ""),
			cnyModel("MiniMax-M2.7-highspeed", "MiniMax M2.7 Highspeed", 4.2, 16.8, .42, 2.625, ""),
			cnyModel("MiniMax-M2.5", "MiniMax M2.5", 2.1, 8.4, .21, 2.625, ""),
			cnyModel("MiniMax-M2.5-highspeed", "MiniMax M2.5 Highspeed", 4.2, 16.8, .21, 2.625, ""),
		},
	},
	{
		ID: "glm", Name: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Protocol: contract.ProtocolOpenAIChat,
		SourceURL: "https://bigmodel.cn/pricing",
		Models: []contract.CatalogModel{
			cnyModel("glm-5.2", "GLM-5.2", 8, 28, 2, 0, ""),
			cnyModel("glm-5.1", "GLM-5.1", 6, 24, 1.3, 0, "≤32K 输入的标准价格；更长输入按官方阶梯价"),
			cnyModel("glm-5-turbo", "GLM-5 Turbo", 5, 22, 1.2, 0, "≤32K 输入的标准价格；更长输入按官方阶梯价"),
			cnyModel("glm-5", "GLM-5", 4, 18, 1, 0, "≤32K 输入的标准价格；更长输入按官方阶梯价"),
			cnyModel("glm-4.7", "GLM-4.7", 2, 8, .4, 0, "≤32K 输入且输出不超过 200 Tokens 的标准价格；其他情况按官方阶梯价"),
			cnyModel("glm-4.7-flashx", "GLM-4.7 FlashX", .5, 3, .1, 0, ""),
			cnyModel("glm-4.7-flash", "GLM-4.7 Flash", 0, 0, 0, 0, "官方免费模型"),
		},
	},
	{
		ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Protocol: contract.ProtocolOpenAIResponses,
		SourceURL: "https://developers.openai.com/api/docs/pricing",
		Models: []contract.CatalogModel{
			model("gpt-5.6-sol", "GPT-5.6 Sol", 4, 20, .4, 5, "Standard 短上下文价格；长上下文单独计价"),
			model("gpt-5.6-terra", "GPT-5.6 Terra", 2, 12, .2, 2.5, "Standard 短上下文价格；长上下文单独计价"),
			model("gpt-5.6-luna", "GPT-5.6 Luna", .2, 1.2, .02, .25, "Standard 短上下文价格；长上下文单独计价"),
		},
	},
	{
		ID: "anthropic", Name: "Claude", BaseURL: "https://api.anthropic.com", Protocol: contract.ProtocolAnthropic,
		SourceURL: "https://platform.claude.com/docs/en/about-claude/pricing",
		Models: []contract.CatalogModel{
			model("claude-sonnet-5", "Claude Sonnet 5", 2, 10, .2, 2.5, "缓存写入按 5 分钟 TTL；1 小时 TTL 价格更高"),
			model("claude-opus-5", "Claude Opus 5", 5, 25, .5, 6.25, "缓存写入按 5 分钟 TTL；1 小时 TTL 价格更高"),
			model("claude-sonnet-4-6", "Claude Sonnet 4.6", 3, 15, .3, 3.75, "缓存写入按 5 分钟 TTL；1 小时 TTL 价格更高"),
			model("claude-haiku-4-5-20251001", "Claude Haiku 4.5", 1, 5, .1, 1.25, "缓存写入按 5 分钟 TTL；1 小时 TTL 价格更高"),
		},
	},
	{
		ID: "kimi", Name: "Kimi", BaseURL: "https://api.moonshot.cn/v1", Protocol: contract.ProtocolOpenAIChat,
		SourceURL: "https://platform.kimi.com/docs/pricing/chat",
		Models: []contract.CatalogModel{
			cnyModelSource("kimi-k3", "Kimi K3", 20, 100, 2, 0, "", "https://platform.kimi.com/docs/pricing/chat-k3"),
			cnyModelSource("kimi-k2.7-code", "Kimi K2.7 Code", 6.5, 27, 1.3, 0, "", "https://platform.kimi.com/docs/pricing/chat-k27-code"),
			cnyModelSource("kimi-k2.7-code-highspeed", "Kimi K2.7 Code Highspeed", 13, 54, 2.6, 0, "", "https://platform.kimi.com/docs/pricing/chat-k27-code"),
			cnyModelSource("kimi-k2.6", "Kimi K2.6", 6.5, 27, 1.1, 0, "", "https://platform.kimi.com/docs/pricing/chat-k26"),
		},
	},
}

func model(id, name string, input, output, cacheRead, cacheWrite float64, note string) contract.CatalogModel {
	return contract.CatalogModel{ID: id, Name: name, Price: contract.ModelPrice{Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite}, PriceNote: note, UpdatedAt: updatedAt}
}

func modelSource(id, name string, input, output, cacheRead, cacheWrite float64, note, sourceURL string) contract.CatalogModel {
	item := model(id, name, input, output, cacheRead, cacheWrite, note)
	item.SourceURL = sourceURL
	return item
}

func cnyModel(id, name string, input, output, cacheRead, cacheWrite float64, note string) contract.CatalogModel {
	return model(id, name, input/cnyPerUSD, output/cnyPerUSD, cacheRead/cnyPerUSD, cacheWrite/cnyPerUSD, cnyNote(note))
}

func cnyModelSource(id, name string, input, output, cacheRead, cacheWrite float64, note, sourceURL string) contract.CatalogModel {
	item := cnyModel(id, name, input, output, cacheRead, cacheWrite, note)
	item.SourceURL = sourceURL
	return item
}

func cnyNote(note string) string {
	const conversion = "官方人民币价格按 ¥7.00 = $1 折算，仅用于估算对比"
	if note == "" {
		return conversion
	}
	return conversion + "；" + note
}

func Providers() []contract.ModelProvider {
	result := make([]contract.ModelProvider, len(providers))
	copy(result, providers)
	return result
}

func ProviderForBaseURL(baseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, provider := range providers {
		if normalized == strings.TrimRight(provider.BaseURL, "/") {
			return provider.ID
		}
	}
	return "custom"
}

func Price(providerID, modelID string) (contract.ModelPrice, bool) {
	for _, provider := range providers {
		if provider.ID != providerID {
			continue
		}
		for _, item := range provider.Models {
			if item.ID == modelID {
				return item.Price, true
			}
		}
	}
	return contract.ModelPrice{}, false
}

func Apply(result *contract.Result, spec contract.Spec, connection *contract.ModelConnection) {
	if result.CostKnown && result.CostSource == contract.CostSourceProvider {
		return
	}
	result.Cost, result.CostKnown, result.CostSource, result.Pricing = 0, false, "", nil
	modelID := first(result.ResolvedModel, spec.Model)
	provider := result.ResolvedProvider
	if connection != nil {
		modelID = first(modelID, connection.DefaultModel)
		provider = first(connection.Provider, provider)
	}
	result.ResolvedModel, result.ResolvedProvider = modelID, provider
	price, source, known := contract.ModelPrice{}, "", false
	if connection != nil {
		price, known = connection.Prices[modelID]
		if known {
			source = contract.CostSourceConnection
		}
	}
	if !known {
		price, known = Price(provider, modelID)
		if known {
			source = contract.CostSourceCatalog
		}
	}
	if !known || totalTokens(result.Usage) == 0 || !supportsUsage(price, result.Usage) {
		return
	}
	result.Cost = (float64(result.Usage.InputTokens)*price.Input + float64(result.Usage.OutputTokens)*price.Output + float64(result.Usage.CacheReadTokens)*price.CacheRead + float64(result.Usage.CacheWriteTokens)*price.CacheWrite) / 1_000_000
	result.CostKnown, result.CostSource = true, source
	result.Pricing = &contract.PricingSnapshot{Provider: provider, Model: modelID, Source: source, Price: price}
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func totalTokens(usage contract.Usage) int64 {
	return usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
}

func supportsUsage(price contract.ModelPrice, usage contract.Usage) bool {
	if price == (contract.ModelPrice{}) { // A catalog or connection may explicitly define a free model.
		return true
	}
	return (usage.InputTokens == 0 || price.Input > 0) &&
		(usage.OutputTokens == 0 || price.Output > 0) &&
		(usage.CacheReadTokens == 0 || price.CacheRead > 0) &&
		(usage.CacheWriteTokens == 0 || price.CacheWrite > 0)
}
