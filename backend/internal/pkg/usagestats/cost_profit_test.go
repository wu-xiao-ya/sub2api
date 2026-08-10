package usagestats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyCostProfitMetricsUsesActualCostAsRevenue(t *testing.T) {
	groups := []GroupStat{
		{
			GroupID:     1,
			Requests:    10,
			TotalTokens: 1000,
			Cost:        55.25,
			ActualCost:  11.05,
			AccountCost: 6.63,
		},
		{
			GroupID:     2,
			Requests:    5,
			TotalTokens: 500,
			Cost:        10,
			ActualCost:  4,
			AccountCost: 2.5,
		},
	}

	summary := ApplyCostProfitMetrics(groups)

	require.InDelta(t, 6.63, groups[0].UpstreamCost, 1e-9)
	require.InDelta(t, 6.63/55.25, groups[0].UpstreamMultiplier, 1e-9)
	require.InDelta(t, 4.42, groups[0].Profit, 1e-9)
	require.InDelta(t, 4.42/11.05, groups[0].ProfitMargin, 1e-9)
	require.Equal(t, int64(15), summary.Requests)
	require.Equal(t, int64(1500), summary.TotalTokens)
	require.InDelta(t, 65.25, summary.StandardCost, 1e-9)
	require.InDelta(t, 15.05, summary.ActualCost, 1e-9)
	require.InDelta(t, 9.13, summary.UpstreamCost, 1e-9)
	require.InDelta(t, 9.13/65.25, summary.UpstreamMultiplier, 1e-9)
	require.InDelta(t, 5.92, summary.Profit, 1e-9)
	require.InDelta(t, 5.92/15.05, summary.ProfitMargin, 1e-9)
}

func TestApplyCostProfitMetricsHandlesZeroRevenue(t *testing.T) {
	groups := []GroupStat{{AccountCost: 1}}

	summary := ApplyCostProfitMetrics(groups)

	require.Zero(t, groups[0].ProfitMargin)
	require.Zero(t, summary.ProfitMargin)
	require.InDelta(t, -1, summary.Profit, 1e-9)
}
