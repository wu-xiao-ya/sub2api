package service

// Official CNY prices per million tokens, converted at the site's frozen
// CNY/USD rate. Source: https://www.kimi.com/resources/kimi-k3-1-pro-pricing
const kimiPricingCNYPerUSD = 6.7841

func kimiK31Pricing() map[string]*LiteLLMModelPricing {
	const unit = 1e-6 / kimiPricingCNYPerUSD
	return map[string]*LiteLLMModelPricing{
		"kimi-k3.1-pro": {
			InputCostPerToken: 13 * unit, OutputCostPerToken: 68 * unit,
			CacheReadInputTokenCost:     1.30 * unit,
			CacheCreationInputTokenCost: 13 * unit, CacheCreationInputTokenCostAbove1hr: 26 * unit,
			LiteLLMProvider: "moonshot", Mode: "chat", SupportsPromptCaching: true,
		},
		"kimi-k3.1-pro-highspeed": {
			InputCostPerToken: 26 * unit, OutputCostPerToken: 136 * unit,
			CacheReadInputTokenCost:     1.30 * unit,
			CacheCreationInputTokenCost: 26 * unit, CacheCreationInputTokenCostAbove1hr: 52 * unit,
			LiteLLMProvider: "moonshot", Mode: "chat", SupportsPromptCaching: true,
		},
	}
}
