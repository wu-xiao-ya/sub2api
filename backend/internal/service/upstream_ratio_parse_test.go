package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const upstreamRatioSamplePayload = `{
  "success": true,
  "data": [
    {
      "model_name": "deepseek-v4.1-flash",
      "quota_type": 0,
      "model_ratio": 0.5,
      "completion_ratio": 2,
      "enable_groups": ["国模自营（1号主力池）"],
      "icon": "data:image/png;base64,AAAA",
      "description": "deepseek flash"
    },
    {
      "model_name": "glm-5.3",
      "quota_type": "0",
      "model_ratio": "1.5",
      "completion_ratio": "4",
      "cache_ratio": 0.1
    },
    {
      "model_name": "per-call-model",
      "quota_type": 1,
      "model_price": 0.02
    },
    {
      "model_name": "broken-model"
    },
    {
      "quota_type": 0,
      "model_ratio": 1
    }
  ],
  "group_ratio": {
    "国模自营（1号主力池）": 0.03,
    "default": 1
  }
}`

func TestParseUpstreamRatioPricing(t *testing.T) {
	rows, groupRatio, err := ParseUpstreamRatioPricing([]byte(upstreamRatioSamplePayload), "国模自营（1号主力池）")
	require.NoError(t, err)
	require.InDelta(t, 0.03, groupRatio, 1e-9)
	require.Len(t, rows, 3) // broken-model (no ratio) and the nameless entry are skipped

	byName := map[string]UpstreamModelRatio{}
	for _, row := range rows {
		byName[row.ModelName] = row
	}

	flash := byName["deepseek-v4.1-flash"]
	require.Equal(t, 0, flash.QuotaType)
	require.InDelta(t, 0.5, flash.ModelRatio, 1e-9)
	require.InDelta(t, 2, flash.EffectiveCompletionRatio(), 1e-9)
	require.InDelta(t, 0.03, flash.GroupRatio, 1e-9)
	require.NotNil(t, flash.Raw)
	require.NotContains(t, flash.Raw, "icon")
	require.NotContains(t, flash.Raw, "description")
	require.Contains(t, flash.Raw, "enable_groups")

	glm := byName["glm-5.3"]
	require.Equal(t, 0, glm.QuotaType)
	require.InDelta(t, 1.5, glm.ModelRatio, 1e-9)
	require.InDelta(t, 4, glm.EffectiveCompletionRatio(), 1e-9)
	require.NotNil(t, glm.CacheRatio)
	require.InDelta(t, 0.1, *glm.CacheRatio, 1e-9)

	perCall := byName["per-call-model"]
	require.Equal(t, 1, perCall.QuotaType)
	require.InDelta(t, 0.02, perCall.ModelPrice, 1e-9)
}

func TestParseUpstreamRatioPricingGroupFallbacks(t *testing.T) {
	rows, groupRatio, err := ParseUpstreamRatioPricing([]byte(upstreamRatioSamplePayload), "")
	require.NoError(t, err)
	require.InDelta(t, 1, groupRatio, 1e-9) // empty group falls back to "default"
	require.Len(t, rows, 3)

	_, groupRatio, err = ParseUpstreamRatioPricing([]byte(upstreamRatioSamplePayload), "不存在的分组")
	require.NoError(t, err)
	require.InDelta(t, 1, groupRatio, 1e-9) // unknown group falls back to "default"
}

func TestParseUpstreamRatioPricingRejectsNonPricingPayloads(t *testing.T) {
	for name, body := range map[string]string{
		"html":       "<html><body>404 page not found</body></html>",
		"empty_data": `{"success": true, "data": []}`,
		"no_data":    `{"success": true}`,
		"no_entries": `{"success": true, "data": [{"quota_type": 0, "model_ratio": 1}]}`,
	} {
		_, _, err := ParseUpstreamRatioPricing([]byte(body), "default")
		require.Error(t, err, name)
	}
}

func TestUpstreamModelRatioCostUSD(t *testing.T) {
	const quotaPerUnit = 500000.0

	// 按量：group_ratio × model_ratio × (prompt + completion × completion_ratio) / quota_per_unit
	ratio := UpstreamModelRatio{QuotaType: 0, ModelRatio: 0.5, CompletionRatio: 2, GroupRatio: 0.03}
	cost := ratio.CostUSD(UsageTokens{InputTokens: 1_000_000, OutputTokens: 500_000}, 1, quotaPerUnit)
	// (1M + 0.5M×2) × 0.5 × 0.03 / 0.5M = 0.06
	require.InDelta(t, 0.06, cost, 1e-9)

	// 缓存读按 cache_ratio 计入，缓存写按全价
	cached := UpstreamModelRatio{QuotaType: 0, ModelRatio: 1, GroupRatio: 1, CacheRatio: ptrUpstreamRatio(0.1)}
	cost = cached.CostUSD(UsageTokens{InputTokens: 100_000, CacheCreationTokens: 50_000, CacheReadTokens: 400_000}, 1, quotaPerUnit)
	// (100k + 50k + 400k×0.1) × 1 / 0.5M = 0.38
	require.InDelta(t, 0.38, cost, 1e-9)

	// 无 cache_ratio 时缓存读按全价（保守，不少算）
	uncached := UpstreamModelRatio{QuotaType: 0, ModelRatio: 1, GroupRatio: 1}
	cost = uncached.CostUSD(UsageTokens{CacheReadTokens: 500_000}, 1, quotaPerUnit)
	require.InDelta(t, 1.0, cost, 1e-9)

	// 缺 completion_ratio 视为 1
	noCompletion := UpstreamModelRatio{QuotaType: 0, ModelRatio: 1, GroupRatio: 1}
	cost = noCompletion.CostUSD(UsageTokens{OutputTokens: 250_000}, 1, quotaPerUnit)
	require.InDelta(t, 0.5, cost, 1e-9)

	// 按次：model_price × group_ratio × 次数
	perCall := UpstreamModelRatio{QuotaType: 1, ModelPrice: 0.02, GroupRatio: 0.5}
	require.InDelta(t, 0.01, perCall.CostUSD(UsageTokens{}, 1, quotaPerUnit), 1e-9)
	require.InDelta(t, 0.03, perCall.CostUSD(UsageTokens{}, 3, quotaPerUnit), 1e-9)

	// 非法配置不产生成本
	require.Equal(t, 0.0, UpstreamModelRatio{QuotaType: 0, ModelRatio: 0, GroupRatio: 1}.CostUSD(UsageTokens{InputTokens: 1000}, 1, quotaPerUnit))
	require.Equal(t, 0.0, UpstreamModelRatio{QuotaType: 1, ModelPrice: 0.02, GroupRatio: 1}.CostUSD(UsageTokens{}, 1, 0))
}

func TestNewAPIPricingURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://new.x5m5x.com":      "https://new.x5m5x.com/api/pricing",
		"https://new.x5m5x.com/":     "https://new.x5m5x.com/api/pricing",
		"https://www.f-api.site/v1":  "https://www.f-api.site/api/pricing",
		"https://www.f-api.site/v1/": "https://www.f-api.site/api/pricing",
		"https://tm-token.com/api":   "https://tm-token.com/api/api/pricing", // 非法后缀原样拼接，交给探测标记 unsupported
	} {
		got, err := newAPIPricingURL(raw)
		require.NoError(t, err)
		require.Equal(t, want, got, raw)
	}
	_, err := newAPIPricingURL("   ")
	require.Error(t, err)
}

func ptrUpstreamRatio(value float64) *float64 {
	return &value
}
