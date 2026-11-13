package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestFable52Catalog(t *testing.T) {
	s := &PricingService{}
	data, _, err := s.buildPricingData([]byte(`{"test-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"litellm_provider":"openai","mode":"chat"}}`))
	require.NoError(t, err)
	s.pricingData = data
	require.Equal(t, claude52FallbackPricing, s.GetModelPricing("claude-fable-5-2"))
	data, _, err = s.buildPricingData([]byte(`{"claude-fable-5-2":{"input_cost_per_token":0.000009,"output_cost_per_token":0.000045,"litellm_provider":"anthropic","mode":"chat"}}`))
	require.NoError(t, err)
	require.Equal(t, 9e-6, data["claude-fable-5-2"].InputCostPerToken)
}

func TestFable52Pricing(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	for _, model := range []string{"claude-fable-5-2", "claude-fable-5.2", "anthropic/claude-fable-5-2", "claude-fable-5-2-20261109"} {
		t.Run(model, func(t *testing.T) {
			p := billing.getFallbackPricing(model)
			require.NotNil(t, p)
			require.Equal(t, 8e-6, p.InputPricePerToken)
			require.Equal(t, 40e-6, p.OutputPricePerToken)
			require.Equal(t, 10e-6, p.CacheCreation5mPrice)
			require.Equal(t, 16e-6, p.CacheCreation1hPrice)
			require.Equal(t, 0.2e-6, p.CacheReadPricePerToken)
			require.Equal(t, claude52FallbackPricing, pricing.GetModelPricing(model))
		})
	}
	require.False(t, claude.IsFable52("claude-fable-5-20"))
	require.Equal(t, 10e-6, billing.getFallbackPricing("claude-fable-5-1").InputPricePerToken)
}
