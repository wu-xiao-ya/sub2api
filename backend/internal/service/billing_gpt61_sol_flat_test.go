package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGPT61SolRemoteSchemasCannotEnableLongContext(t *testing.T) {
	svc := &PricingService{}
	data, err := svc.parsePricingData([]byte(`{
		"gpt-6.1-sol": {
			"input_cost_per_token": 0.000002,
			"output_cost_per_token": 0.00001,
			"input_cost_per_token_above_272k_tokens": 0.000004,
			"output_cost_per_token_above_272k_tokens": 0.000015,
			"cache_read_input_token_cost": 0.0000001,
			"cache_creation_input_token_cost": 0.0000025,
			"input_cost_per_token_priority": 0.000004,
			"output_cost_per_token_priority": 0.00002,
			"supports_service_tier": true
		},
		"openai/gpt-6.1-sol-high": {
			"input_cost_per_token": 0.000002,
			"output_cost_per_token": 0.00001,
			"long_context_input_token_threshold": 272000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5
		},
		"gpt-6-sol": {
			"input_cost_per_token": 0.000002,
			"output_cost_per_token": 0.00001,
			"input_cost_per_token_above_272k_tokens": 0.000004,
			"output_cost_per_token_above_272k_tokens": 0.000015
		}
	}`))
	require.NoError(t, err)
	for _, name := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol-high"} {
		require.Zero(t, data[name].LongContextInputTokenThreshold)
		require.Zero(t, data[name].LongContextInputCostMultiplier)
		require.Zero(t, data[name].LongContextOutputCostMultiplier)
	}
	require.InDelta(t, 2e-6, data["gpt-6.1-sol"].InputCostPerToken, 1e-15)
	require.InDelta(t, 1e-5, data["gpt-6.1-sol"].OutputCostPerToken, 1e-15)
	require.InDelta(t, 4e-6, data["gpt-6.1-sol"].InputCostPerTokenPriority, 1e-15)
	require.InDelta(t, 2e-5, data["gpt-6.1-sol"].OutputCostPerTokenPriority, 1e-15)
	require.InDelta(t, 1e-7, data["gpt-6.1-sol"].CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 2.5e-6, data["gpt-6.1-sol"].CacheCreationInputTokenCost, 1e-15)
	require.Equal(t, 272000, data["gpt-6-sol"].LongContextInputTokenThreshold)
	require.Equal(t, 2.0, data["gpt-6-sol"].LongContextInputCostMultiplier)
	require.Equal(t, 1.5, data["gpt-6-sol"].LongContextOutputCostMultiplier)
}

func gpt61SolFlatTestCard() *ModelPricing {
	return &ModelPricing{
		InputPricePerToken:                 2e-6,
		InputPricePerTokenPriority:         4e-6,
		OutputPricePerToken:                1e-5,
		OutputPricePerTokenPriority:        2e-5,
		CacheReadPricePerToken:             1e-7,
		CacheReadPricePerTokenPriority:     2e-7,
		CacheCreationPricePerToken:         2.5e-6,
		CacheCreationPricePerTokenPriority: 5e-6,
		CacheCreationPriceExplicit:         true,
		LongContextInputThreshold:          272000,
		LongContextInputMultiplier:         2,
		LongContextOutputMultiplier:        1.5,
	}
}

func TestGPT61SolFlatPolicyPreservesCardsAndOtherModels(t *testing.T) {
	svc := &BillingService{}
	card := gpt61SolFlatTestCard()
	before := *card
	for _, model := range []string{
		"gpt-6.1-sol", "openai/gpt-6.1-sol", "models/gpt-6.1-sol",
		"GPT-6.1-SOL", "gpt-6.1-sol-high", "gpt-6.1-sol-max", "gpt-6.1-sol-preview",
	} {
		result := svc.applyModelSpecificPricingPolicy(model, card)
		expected := before
		expected.LongContextInputThreshold = 0
		expected.LongContextInputMultiplier = 0
		expected.LongContextOutputMultiplier = 0
		require.Equal(t, expected, *result, model)
		require.Equal(t, before, *card, "shared card must remain unchanged")
	}
	for _, model := range []string{"gpt-6-sol", "gpt-6-astra", "gpt-5.6-sol", "gpt-6.1-solar"} {
		result := svc.applyModelSpecificPricingPolicy(model, card)
		require.Equal(t, 272000, result.LongContextInputThreshold, model)
		require.Equal(t, 2.0, result.LongContextInputMultiplier, model)
		require.Equal(t, 1.5, result.LongContextOutputMultiplier, model)
	}
}

func TestGPT61SolFlatUnifiedChannelBillingAcrossContextAndTier(t *testing.T) {
	svc := &BillingService{}
	resolver := NewModelPricingResolver(nil, svc)
	enabled := true
	for _, tier := range []string{"", "priority"} {
		for _, tokens := range []UsageTokens{
			{InputTokens: 272000, OutputTokens: 10},
			{InputTokens: 272001, OutputTokens: 10},
			{InputTokens: 878417, OutputTokens: 10},
			{InputTokens: 3, CacheReadTokens: 200000, CacheCreationTokens: 100000, OutputTokens: 10},
		} {
			for _, channelPriced := range []bool{false, true} {
				card := gpt61SolFlatTestCard()
				resolved := &ResolvedPricing{Mode: BillingModeToken, BasePricing: card}
				if channelPriced {
					in, out, read, write := 2e-6, 1e-5, 1e-7, 2.5e-6
					resolver.applyTokenOverrides(&ChannelModelPricing{
						InputPrice: &in, OutputPrice: &out, CacheReadPrice: &read, CacheWritePrice: &write,
					}, resolved)
				}
				cost, err := svc.CalculateCostUnified(CostInput{
					Ctx: context.Background(), Model: "gpt-6.1-sol",
					Tokens: tokens, RateMultiplier: 0.27, ServiceTier: tier,
					Resolver: resolver, Resolved: resolved, LongContextBillingEnabled: &enabled,
				})
				require.NoError(t, err)
				require.False(t, cost.LongContextBillingApplied)
				factor := 1.0
				if tier == "priority" && !channelPriced {
					factor = 2
				}
				require.InDelta(t, float64(tokens.InputTokens)*2e-6*factor, cost.InputCost, 1e-10)
				require.InDelta(t, float64(tokens.OutputTokens)*1e-5*factor, cost.OutputCost, 1e-10)
				require.InDelta(t, float64(tokens.CacheReadTokens)*1e-7*factor, cost.CacheReadCost, 1e-10)
				require.InDelta(t, float64(tokens.CacheCreationTokens)*2.5e-6*factor, cost.CacheCreationCost, 1e-10)
				require.InDelta(t, cost.TotalCost*0.27, cost.ActualCost, 1e-10)
			}
		}
	}
}

func TestGPT61SolFlatLegacyBillingAndSharedPriceCache(t *testing.T) {
	raw := &LiteLLMModelPricing{
		InputCostPerToken: 2e-6, OutputCostPerToken: 1e-5,
		LongContextInputTokenThreshold: 272000,
		LongContextInputCostMultiplier: 2, LongContextOutputCostMultiplier: 1.5,
	}
	svc := NewBillingService(&config.Config{}, &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{"gpt-6.1-sol": raw},
	})
	cost, err := svc.CalculateCost("gpt-6.1-sol", UsageTokens{InputTokens: 300000, OutputTokens: 100}, 0.2)
	require.NoError(t, err)
	require.False(t, cost.LongContextBillingApplied)
	require.InDelta(t, 0.6, cost.InputCost, 1e-10)
	require.InDelta(t, 0.001, cost.OutputCost, 1e-10)
	require.InDelta(t, 0.1202, cost.ActualCost, 1e-10)
	require.Equal(t, 272000, raw.LongContextInputTokenThreshold)
	require.Equal(t, 2.0, raw.LongContextInputCostMultiplier)
}

func TestGPT61SolFlatDisplayAfterRemotePriceRefresh(t *testing.T) {
	ps := &PricingService{}
	data, err := ps.parsePricingData([]byte(`{
		"gpt-6.1-sol": {
			"input_cost_per_token": 0.000002,
			"output_cost_per_token": 0.00001,
			"long_context_input_token_threshold": 272000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5
		}
	}`))
	require.NoError(t, err)
	ps.pricingData = data
	svc := &ChannelService{pricingService: ps}
	input := 2e-6
	models := []SupportedModel{{Name: "gpt-6.1-sol", Pricing: &ChannelModelPricing{InputPrice: &input}}}
	svc.fillGlobalPricingFallback(models)
	svc.fillAstraLongContextDisplay(models)
	require.Nil(t, models[0].Pricing.LongContextInputTokenThreshold)
	require.Nil(t, models[0].Pricing.LongContextInputCostMultiplier)
	require.Nil(t, models[0].Pricing.LongContextOutputCostMultiplier)
	require.InDelta(t, input, *models[0].Pricing.InputPrice, 1e-15)
}
