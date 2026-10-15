package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAstraLongContextSurcharge(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-astra", "gpt-6-astra-max", "gpt-6-astra-preview", "gpt-6-astra-2026-09-01", "openai/gpt-6-astra"} {
		for _, tier := range []struct {
			name  string
			scale float64
		}{{"", 1}, {"priority", 2}, {"flex", 0.5}} {
			t.Run(model+"/"+tier.name, func(t *testing.T) {
				svc := NewBillingService(&config.Config{}, nil)
				// Threshold is exclusive and counts input plus cache read and cache write.
				below := UsageTokens{InputTokens: 261000, OutputTokens: 100, CacheReadTokens: 10000, CacheCreationTokens: 1000}
				belowCost, err := svc.CalculateCostWithServiceTier(model, below, 1, tier.name)
				require.NoError(t, err)
				require.False(t, belowCost.LongContextBillingApplied)
				require.InDelta(t, float64(below.InputTokens)*10e-6*tier.scale, belowCost.InputCost, 1e-9)
				require.InDelta(t, 100*50e-6*tier.scale, belowCost.OutputCost, 1e-12)
				require.InDelta(t, 10000*1e-6*tier.scale, belowCost.CacheReadCost, 1e-12)
				require.InDelta(t, 1000*12.5e-6*tier.scale, belowCost.CacheCreationCost, 1e-12)

				for _, input := range []int{261001, 272000, 900000} {
					tokens := UsageTokens{InputTokens: input, OutputTokens: 100, CacheReadTokens: 10000, CacheCreationTokens: 1000}
					cost, err := svc.CalculateCostWithServiceTier(model, tokens, 1, tier.name)
					require.NoError(t, err)
					require.True(t, cost.LongContextBillingApplied)
					require.InDelta(t, float64(input)*10e-6*2*tier.scale, cost.InputCost, 1e-9)
					require.InDelta(t, 100*50e-6*1.5*tier.scale, cost.OutputCost, 1e-12)
					require.InDelta(t, 10000*1e-6*2*tier.scale, cost.CacheReadCost, 1e-12)
					require.InDelta(t, 1000*12.5e-6*2*tier.scale, cost.CacheCreationCost, 1e-12)
				}
			})
		}
	}
}

func TestAstraFillsMissingLongContextWithoutMutatingSource(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	stale := &ModelPricing{InputPricePerToken: 10e-6, OutputPricePerToken: 50e-6,
		InputPricePerTokenPriority: 20e-6, OutputPricePerTokenPriority: 100e-6}
	for _, alias := range []string{"gpt-6-astra", "gpt-astra-max", "gpt-6-astra-preview"} {
		got := svc.applyModelSpecificPricingPolicy(alias, stale)
		require.Equal(t, openAIGPT54LongContextInputThreshold, got.LongContextInputThreshold)
		require.Equal(t, openAIGPT54LongContextInputMultiplier, got.LongContextInputMultiplier)
		require.Equal(t, openAIGPT54LongContextOutputMultiplier, got.LongContextOutputMultiplier)
		require.Equal(t, 100e-6, got.OutputPricePerTokenPriority)
		require.InDelta(t, 12.5e-6, got.CacheCreationPricePerToken, 1e-15)
		require.Zero(t, stale.LongContextInputThreshold)
	}
}

func TestAstraStaleFileAndUnifiedBilling(t *testing.T) {
	entry := map[string]any{
		"input_cost_per_token": 10e-6, "output_cost_per_token": 50e-6,
		"cache_read_input_token_cost": 1e-6,
	}
	body, err := json.Marshal(map[string]any{"gpt-6-astra": entry, "gpt-5.6-sol": entry})
	require.NoError(t, err)
	ps := &PricingService{}
	parsed, err := ps.parsePricingData(body)
	require.NoError(t, err)
	require.Zero(t, parsed["gpt-6-astra"].LongContextInputTokenThreshold)
	require.Equal(t, 0, parsed["gpt-5.6-sol"].LongContextInputTokenThreshold)
	bs := NewBillingService(&config.Config{}, nil)
	base := &ModelPricing{InputPricePerToken: 10e-6, OutputPricePerToken: 50e-6}
	resolved := &ResolvedPricing{Mode: BillingModeToken, BasePricing: base}
	input := CostInput{Model: "gpt-6-astra", Tokens: UsageTokens{InputTokens: 300000, OutputTokens: 100},
		RateMultiplier: 0.2, Resolver: NewModelPricingResolver(nil, bs), Resolved: resolved}
	cost, err := bs.CalculateCostUnified(input)
	require.NoError(t, err)
	require.True(t, cost.LongContextBillingApplied)
	require.InDelta(t, (6.0+0.0075)*0.2, cost.ActualCost, 1e-12)
	require.Zero(t, base.LongContextInputThreshold)
}

func TestAccountStatsFallbackDeepSeekTimePricing(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash-0731", "deepseek-v4-pro-0813"} {
		for _, hour := range []int{8, 9, 11, 12, 13, 14, 17, 18, 23} {
			svc := NewBillingService(&config.Config{}, nil)
			svc.now = func() time.Time { return time.Date(2026, 9, 12, hour, 0, 0, 0, chinaStandardTime) }
			tokens := UsageTokens{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 200, CacheCreationTokens: 20}
			base := domesticNumericPricing(model)
			scale := 1.0
			if (hour >= 9 && hour < 12) || (hour >= 14 && hour < 18) {
				scale = 2
			}
			want := (1000*base.InputPricePerToken + 100*base.OutputPricePerToken + 200*base.CacheReadPricePerToken + 20*base.CacheCreationPricePerToken) * scale
			got := tryModelFilePricing(svc, model, tokens)
			require.NotNil(t, got)
			require.InDelta(t, want, *got, 1e-12, "%s at %d", model, hour)
			billed, err := svc.CalculateCost(model, tokens, 1)
			require.NoError(t, err)
			require.InDelta(t, billed.TotalCost, *got, 1e-12)
		}
	}
}
