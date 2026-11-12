package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCarbonUsesArgonPricing(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-4-argon": {InputCostPerToken: 2e-6, OutputCostPerToken: 10e-6, CacheReadInputTokenCost: 0.1e-6},
	}}
	for _, model := range []string{"gemini-4-carbon", "gemini-4-carbon-high", "gemini-4-carbon-low", "gemini-4-carbon-medium", "gemini-4-carbon-tiered", "models/gemini-4-carbon", "publishers/google/models/gemini-4-carbon-high"} {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, billing.getFallbackPricing("gemini-4-argon"), billing.getFallbackPricing(model))
			p := pricing.GetModelPricing(model)
			require.NotNil(t, p)
			require.Equal(t, 2e-6, p.InputCostPerToken)
			require.Equal(t, 10e-6, p.OutputCostPerToken)
			require.Equal(t, 0.1e-6, p.CacheReadInputTokenCost)
		})
	}
	require.Nil(t, billing.getFallbackPricing("gemini-4-carbon-unknown"))
}
