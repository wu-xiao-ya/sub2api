package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekStandardPeakPricingMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("252_deepseek_standard_peak_pricing.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "deepseek-v4.1-flash")
	require.Contains(t, sql, "deepseek-flash")
	require.Contains(t, sql, "deepseek-v4-flash-vision-exp")
	require.Contains(t, sql, "0.000001000000::numeric")
	require.Contains(t, sql, "0.000004000000::numeric")
	require.Contains(t, sql, "0.000000020000::numeric")
	require.Contains(t, sql, "0.000004500000::numeric")
	require.Contains(t, sql, "0.000013500000::numeric")
	require.Contains(t, sql, "0.000000150000::numeric")
	require.Contains(t, sql, "jsonb_array_length(pricing.models) = 1")
	require.Contains(t, sql, "LOWER(pricing.models->>0) = policy.model")
	require.Contains(t, sql, "platform = 'deepseek'")
	require.Contains(t, sql, "model_mapping")
	require.Contains(t, sql, "NOT EXISTS")
	require.NotContains(t, sql, "0.000003000000")
	require.GreaterOrEqual(t, strings.Count(sql, "deepseek-v4.1-flash"), 3)
}
