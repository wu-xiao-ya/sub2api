package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// gpt-6.1-sol launched without a pricing entry anywhere: pricing resolution
// fell through to the DefaultTestModel card, so the 272k whole-request
// long-context surcharge never applied even though the channel already bills
// the model at gpt-6-sol base rates. These tests pin the static card and the
// surcharge end to end.
//
// billing_service_test.go's helpers are trapped inside a mis-nested function
// block (pre-existing), so these tests build their services directly.

func gpt61SolPricingService() *PricingService {
	return &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
}

func TestGetModelPricing_GPT61SolUsesSolCardWithLongContext(t *testing.T) {
	svc := NewBillingService(&config.Config{}, gpt61SolPricingService())

	for _, model := range []string{"gpt-6.1-sol", "gpt-6.1-sol-high"} {
		pricing, err := svc.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12, model)
		require.InDelta(t, 10e-6, pricing.OutputPricePerToken, 1e-12, model)
		require.InDelta(t, 0.2e-6, pricing.CacheReadPricePerToken, 1e-12, model)
		require.InDelta(t, 2.5e-6, pricing.CacheCreationPricePerToken, 1e-12, model)
		require.Equal(t, openAIGPT54LongContextInputThreshold, pricing.LongContextInputThreshold, model)
		require.InDelta(t, openAIGPT54LongContextInputMultiplier, pricing.LongContextInputMultiplier, 1e-12, model)
		require.InDelta(t, openAIGPT54LongContextOutputMultiplier, pricing.LongContextOutputMultiplier, 1e-12, model)
	}
}

// No pricing service at all: the hardcoded fallbackPrices entry must carry the
// same card.
func TestGetModelPricing_GPT61SolFallbackTable(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	pricing, err := svc.GetModelPricing("gpt-6.1-sol")
	require.NoError(t, err)
	require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 10e-6, pricing.OutputPricePerToken, 1e-12)
	require.Equal(t, openAIGPT54LongContextInputThreshold, pricing.LongContextInputThreshold)
}

func TestCalculateCost_GPT61SolAppliesLongContextSurcharge(t *testing.T) {
	svc := NewBillingService(&config.Config{}, gpt61SolPricingService())

	short, err := svc.CalculateCost("gpt-6.1-sol", UsageTokens{InputTokens: 10000, OutputTokens: 1000}, 1)
	require.NoError(t, err)
	require.False(t, short.LongContextBillingApplied)
	require.InDelta(t, 10000*2e-6, short.InputCost, 1e-9)
	require.InDelta(t, 1000*10e-6, short.OutputCost, 1e-9)

	long, err := svc.CalculateCost("gpt-6.1-sol", UsageTokens{InputTokens: 272001, OutputTokens: 1000}, 1)
	require.NoError(t, err)
	require.True(t, long.LongContextBillingApplied)
	require.InDelta(t, 272001*2e-6*2, long.InputCost, 1e-9)
	require.InDelta(t, 1000*10e-6*1.5, long.OutputCost, 1e-9)
}

// Production bills gpt-6.1-sol through channel-level base prices; the
// long-context rule must survive that merge (channel prices stay authoritative
// for base tokens only).
func TestGetModelPricingWithChannel_GPT61SolKeepsGlobalLongContext(t *testing.T) {
	svc := NewBillingService(&config.Config{}, gpt61SolPricingService())
	input := 2e-6
	output := 10e-6
	channel := &ChannelModelPricing{InputPrice: &input, OutputPrice: &output}

	pricing, err := svc.GetModelPricingWithChannel("gpt-6.1-sol", channel)
	require.NoError(t, err)
	require.InDelta(t, input, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, output, pricing.OutputPricePerToken, 1e-12)
	require.Equal(t, openAIGPT54LongContextInputThreshold, pricing.LongContextInputThreshold)
	require.InDelta(t, openAIGPT54LongContextInputMultiplier, pricing.LongContextInputMultiplier, 1e-12)
}
