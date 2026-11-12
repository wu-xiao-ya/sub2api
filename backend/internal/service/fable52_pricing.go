package service

import "strings"

// Fable 5.2 standard USD/token rates, released November 9, 2026.
// https://www.anthropic.com/claude-fable-and-mythos-5-2
// https://platform.claude.com/docs/en/about-claude/pricing
func fable52Pricing() *LiteLLMModelPricing {
	return &LiteLLMModelPricing{
		InputCostPerToken:                   8e-6,
		OutputCostPerToken:                  40e-6,
		CacheCreationInputTokenCost:         10e-6,
		CacheCreationInputTokenCostAbove1hr: 16e-6,
		CacheReadInputTokenCost:             0.2e-6,
		LiteLLMProvider:                     "anthropic",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
}

func isClaudeFable52Model(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, marker := range []string{"fable-5-2", "fable-5.2", "fable5.2", "fable52"} {
		if at := strings.Index(model, marker); at >= 0 {
			after := at + len(marker)
			if after == len(model) || model[after] < '0' || model[after] > '9' {
				return true
			}
		}
	}
	return false
}
