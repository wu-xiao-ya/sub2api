package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// claude-sonnet-5-5 计费适配（与 Sonnet 5 同档 $2/$10，5m/1h 缓存写分档计费），
// 兼覆盖 opus-5-5 兜底卡补配 5m 缓存写价的回归（此前兜底路径 5m 缓存写按 $0 计费）。
//
// billing_service_test.go 的公共助手困在误嵌套代码块里（历史问题），这里直接构造服务。
func claudeSonnet55TestBillingService() *BillingService {
	return NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}})
}

func TestGetModelPricing_ClaudeSonnet55MatchesSonnet5Card(t *testing.T) {
	svc := claudeSonnet55TestBillingService()

	for _, model := range []string{"claude-sonnet-5-5", "claude-sonnet-5-5-20260929", "claude-sonnet-5.5", "claude-sonnet-5"} {
		pricing, err := svc.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12, model)
		require.InDelta(t, 10e-6, pricing.OutputPricePerToken, 1e-12, model)
		require.InDelta(t, 2.5e-6, pricing.CacheCreationPricePerToken, 1e-12, model)
		require.InDelta(t, 0.2e-6, pricing.CacheReadPricePerToken, 1e-12, model)
	}
}

func TestCalculateCost_ClaudeSonnet55CacheCreationTiers(t *testing.T) {
	svc := claudeSonnet55TestBillingService()

	// 5m/1h 缓存写入分档计费：1M token 5m($2.50) + 1M token 1h($4) = $6.50
	tiered, err := svc.CalculateCost("claude-sonnet-5-5", UsageTokens{CacheCreation5mTokens: 1_000_000, CacheCreation1hTokens: 1_000_000}, 1)
	require.NoError(t, err)
	require.InDelta(t, 6.5, tiered.CacheCreationCost, 1e-9)
	require.InDelta(t, 6.5, tiered.TotalCost, 1e-9)

	// 未带 ephemeral 明细时全部按 5m 价计费
	noTier, err := svc.CalculateCost("claude-sonnet-5-5", UsageTokens{CacheCreationTokens: 1_000_000}, 1)
	require.NoError(t, err)
	require.InDelta(t, 2.5, noTier.CacheCreationCost, 1e-9)
}

func TestCalculateCost_ClaudeOpus55FiveMinuteCacheWriteNotFree(t *testing.T) {
	svc := claudeSonnet55TestBillingService()

	// opus-5-5 兜底卡此前漏配 5m 缓存写单价，纯 5m 缓存写按 $0 计费。
	got, err := svc.CalculateCost("claude-opus-5-5", UsageTokens{CacheCreationTokens: 1_000_000}, 1)
	require.NoError(t, err)
	require.InDelta(t, 5.0, got.CacheCreationCost, 1e-9)
}

func TestGetModelPricing_ClaudeSonnet45StaysOnOwnCard(t *testing.T) {
	svc := claudeSonnet55TestBillingService()

	// Sonnet 4.5 保持在自身档位，不得混入 5.5 价格
	sonnet45, err := svc.GetModelPricing("claude-sonnet-4-5-20250929")
	require.NoError(t, err)
	require.InDelta(t, 3e-6, sonnet45.InputPricePerToken, 1e-12)
	require.InDelta(t, 15e-6, sonnet45.OutputPricePerToken, 1e-12)
}
