//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImageUpstreamCostSQLMatchesServiceAndDoesNotChangeTokenCosts(t *testing.T) {
	ctx := context.Background()
	client := testEntTx(t).Client()
	repo := NewSettingRepository(client)
	svc := service.NewSettingService(repo, nil)
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{
		service.SettingKeyImageCostIgnoreUpstreamRateSnapshot: "true",
		service.SettingKeyImageUpstreamCostPerImage:           "0.9",
		service.SettingKeyImageUpstreamCostByAccount:          `{"42":0.8}`,
		service.SettingKeyImageUpstreamCostByModel:            `{" MiXeD ":{"1K":0.1,"2K":0.2,"4K":0.4}}`,
		service.SettingKeyImageUpstreamCostByAccountModel:     `{"42":{"mixed":{"2K":0,"4K":0.3}}}`,
	}))
	for _, size := range []string{"1K", "1024x768", "2K", "2048x1152", "4K", "4000 x 3000", "auto", "invalid"} {
		query := `WITH usage_logs AS (SELECT 42::bigint AS account_id, 'MIXED'::text AS requested_model,
			'MIXED'::text AS model, $1::text AS image_size, 'image'::text AS billing_mode,
			3::int AS image_count, 100::numeric AS account_stats_cost, 150::numeric AS total_cost,
			0.5::numeric AS account_rate_multiplier)
			SELECT ` + inlineImageAwareAccountCostExpr("usage_logs") + ` FROM usage_logs`
		want := 3 * svc.ResolveImageUpstreamCost(ctx, 42, "MIXED", size)
		require.InDelta(t, want, querySingleFloat(t, ctx, client, query, size), 1e-10, size)
	}
	for _, mode := range []string{"token", "request"} {
		query := `WITH usage_logs AS (SELECT 42::bigint AS account_id, 'MIXED'::text AS requested_model,
			'MIXED'::text AS model, '2K'::text AS image_size, $1::text AS billing_mode,
			3::int AS image_count, 100::numeric AS account_stats_cost, 150::numeric AS total_cost,
			0.5::numeric AS account_rate_multiplier)
			SELECT ` + inlineImageAwareAccountCostExpr("usage_logs") + ` FROM usage_logs`
		require.InDelta(t, 50, querySingleFloat(t, ctx, client, query, mode), 1e-10)
	}
	// Invalid persisted JSON is isolated to its override level, not a report failure.
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{
		service.SettingKeyImageUpstreamCostByModel:        "invalid JSON",
		service.SettingKeyImageUpstreamCostByAccountModel: "invalid JSON",
	}))
	query := `WITH usage_logs AS (SELECT 42::bigint AS account_id, 'MIXED'::text AS requested_model,
		'MIXED'::text AS model, '2K'::text AS image_size, 'image'::text AS billing_mode,
		3::int AS image_count, 100::numeric AS account_stats_cost, 150::numeric AS total_cost,
		0.5::numeric AS account_rate_multiplier)
		SELECT ` + inlineImageAwareAccountCostExpr("usage_logs") + ` FROM usage_logs`
	require.InDelta(t, 2.4, querySingleFloat(t, ctx, client, query), 1e-10)
}
