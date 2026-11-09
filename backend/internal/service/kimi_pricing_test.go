package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestKimiK31BillingFallback(t *testing.T) {
	s := NewBillingService(&config.Config{}, nil)
	for name, expected := range kimiK31Pricing() {
		for _, model := range []string{name, "moonshot/" + name} {
			p := s.getFallbackPricing(model)
			require.NotNil(t, p, model)
			require.Equal(t, expected.InputCostPerToken, p.InputPricePerToken)
			require.Equal(t, expected.OutputCostPerToken, p.OutputPricePerToken)
			require.Equal(t, expected.CacheReadInputTokenCost, p.CacheReadPricePerToken)
			require.Equal(t, expected.CacheCreationInputTokenCost, p.CacheCreation5mPrice)
			require.Equal(t, expected.CacheCreationInputTokenCostAbove1hr, p.CacheCreation1hPrice)
			require.True(t, p.SupportsCacheBreakdown)
		}
	}
	for _, model := range []string{"kimi-k3.1", "kimi-k3.1-pro-preview", "kimi-k3.1-pro-highspeed-unknown"} {
		require.Nil(t, s.getFallbackPricing(model), model)
	}
}

func TestKimiK31OfficialCNYPrices(t *testing.T) {
	s := &PricingService{cfg: &config.Config{}}
	data, _, err := s.buildPricingData([]byte(`{"existing":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`))
	require.NoError(t, err)
	for _, tc := range []struct {
		name                   string
		input, output, write1h float64
	}{
		{"kimi-k3.1-pro", 13, 68, 26},
		{"kimi-k3.1-pro-highspeed", 26, 136, 52},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := data[tc.name]
			require.NotNil(t, p)
			const scale = kimiPricingCNYPerUSD * 1e6
			require.InDelta(t, tc.input, p.InputCostPerToken*scale, 1e-9)
			require.InDelta(t, tc.output, p.OutputCostPerToken*scale, 1e-9)
			require.InDelta(t, 1.3, p.CacheReadInputTokenCost*scale, 1e-9)
			require.InDelta(t, tc.input, p.CacheCreationInputTokenCost*scale, 1e-9)
			require.InDelta(t, tc.write1h, p.CacheCreationInputTokenCostAbove1hr*scale, 1e-9)
		})
	}
	data, _, err = s.buildPricingData([]byte(`{"kimi-k3.1-pro":{"input_cost_per_token":0.000009,"output_cost_per_token":0.00001}}`))
	require.NoError(t, err)
	require.Equal(t, 0.000009, data["kimi-k3.1-pro"].InputCostPerToken)
}
