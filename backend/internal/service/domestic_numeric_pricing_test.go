package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDomesticNumericPricing(t *testing.T) {
	tests := []struct {
		model       string
		inputMTok   float64
		outputMTok  float64
		cacheMTok   float64
		threshold   int
		longCtxRate float64
	}{
		{"deepseek-v4-flash-0731", 3, 9, 0.1, 0, 0},
		{"provider/deepseek-v4-pro-0813", 9, 27, 0.3, 0, 0},
		{"glm-5.1", 8, 28, 2, 0, 0},
		{"glm-5.3-flash", 0.8, 2.8, 0.23, 0, 0},
		{"kimi-k3", 20, 100, 2, 0, 0},
		{"kimi-k2.7-code", 6.5, 27, 1.3, 0, 0},
		{"kimi-k2.7code", 6.5, 27, 1.3, 0, 0},
		{"kimi-k2.5", 4, 21, 0.7, 0, 0},
		{"mimo-v2.5-pro", 3, 6, 0.025, 0, 0},
		{"hy3", 1, 4, 0.25, 0, 0},
		{"qwen3.7-plus", 2, 8, 0.4, 256000, 3},
		{"qwen3.8-max", 12, 36, 1.2, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing := domesticNumericPricing(tt.model)
			require.NotNil(t, pricing)
			require.InDelta(t, tt.inputMTok*1e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.outputMTok*1e-6, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheMTok*1e-6, pricing.CacheReadPricePerToken, 1e-12)
			require.Equal(t, tt.threshold, pricing.LongContextInputThreshold)
			require.InDelta(t, tt.longCtxRate, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, tt.longCtxRate, pricing.LongContextOutputMultiplier, 1e-12)
		})
	}

	require.Nil(t, domesticNumericPricing("gpt-5.6-sol"))
}

func TestApplyDomesticTimePricingAtDeepSeekPeakWindows(t *testing.T) {
	base := tokenPricing(3, 9, 0.1)
	base.CacheCreationPricePerToken = 3e-6

	tests := []struct {
		name       string
		hour       int
		minute     int
		multiplier float64
	}{
		{"before morning peak", 8, 59, 0.5},
		{"morning peak starts", 9, 0, 1},
		{"morning peak ends", 12, 0, 0.5},
		{"afternoon peak starts", 14, 0, 1},
		{"afternoon peak ends", 18, 0, 0.5},
		{"night off peak", 23, 30, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 28, tt.hour, tt.minute, 0, 0, chinaStandardTime)
			got := applyDomesticTimePricingAt("deepseek-v4-flash", base, now)
			require.InDelta(t, base.InputPricePerToken*tt.multiplier, got.InputPricePerToken, 1e-15)
			require.InDelta(t, base.OutputPricePerToken*tt.multiplier, got.OutputPricePerToken, 1e-15)
			require.InDelta(t, base.CacheReadPricePerToken*tt.multiplier, got.CacheReadPricePerToken, 1e-15)
			require.InDelta(t, base.CacheCreationPricePerToken*tt.multiplier, got.CacheCreationPricePerToken, 1e-15)
		})
	}
}

func TestApplyDomesticTimePricingAtDoesNotChangeOtherModels(t *testing.T) {
	base := tokenPricing(8, 28, 2)
	now := time.Date(2026, time.August, 28, 20, 0, 0, 0, chinaStandardTime)

	require.Same(t, base, applyDomesticTimePricingAt("glm-5.3", base, now))
}

func TestDeepSeekTimePricingAppliesToExplicitChannelPricing(t *testing.T) {
	peak := time.Date(2026, time.August, 28, 10, 0, 0, 0, chinaStandardTime)
	offPeak := time.Date(2026, time.August, 28, 13, 0, 0, 0, chinaStandardTime)
	inputPrice := 3e-6
	outputPrice := 9e-6
	cacheReadPrice := 0.1e-6
	channelPricing := &ChannelModelPricing{
		InputPrice:     &inputPrice,
		OutputPrice:    &outputPrice,
		CacheReadPrice: &cacheReadPrice,
	}
	tokens := UsageTokens{
		InputTokens:     1_000_000,
		OutputTokens:    1_000_000,
		CacheReadTokens: 1_000_000,
	}

	svc := NewBillingService(nil, nil)
	svc.now = func() time.Time { return peak }
	peakCost, err := svc.calculateCostInternalWithPolicy(
		"deepseek-v4-flash",
		tokens,
		1,
		"",
		channelPricing,
		true,
	)
	require.NoError(t, err)
	require.InDelta(t, 12.1, peakCost.ActualCost, 1e-12)

	svc.now = func() time.Time { return offPeak }
	offPeakCost, err := svc.calculateCostInternalWithPolicy(
		"deepseek-v4-flash",
		tokens,
		1,
		"",
		channelPricing,
		true,
	)
	require.NoError(t, err)
	require.InDelta(t, 6.05, offPeakCost.ActualCost, 1e-12)
}

func TestApplyDomesticNumericPricingPreservesNonPriceMetadata(t *testing.T) {
	base := &ModelPricing{
		InputPricePerToken:      99,
		OutputPricePerToken:     99,
		CacheReadPricePerToken:  99,
		ImageInputPricePerToken: 7,
	}

	got := applyDomesticNumericPricing("glm-5.3", base)
	require.NotSame(t, base, got)
	require.InDelta(t, 8e-6, got.InputPricePerToken, 1e-12)
	require.InDelta(t, 28e-6, got.OutputPricePerToken, 1e-12)
	require.InDelta(t, 2e-6, got.CacheReadPricePerToken, 1e-12)
	require.Equal(t, float64(7), got.ImageInputPricePerToken)
	require.Equal(t, float64(99), base.InputPricePerToken)
}
