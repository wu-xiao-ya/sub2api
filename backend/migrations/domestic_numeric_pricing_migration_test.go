package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDomesticNumericPricingMigrationIsScopedToExactModelRows(t *testing.T) {
	sqlBytes, err := FS.ReadFile("241_domestic_numeric_parity_pricing.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "jsonb_array_length(pricing.models) = 1")
	require.Contains(t, sql, "LOWER(pricing.models->>0) = policy.model")
	require.Contains(t, sql, "('glm-5.1'")
	require.Contains(t, sql, "0.000008000000::numeric")
	require.Contains(t, sql, "('glm-5.3-flash'")
	require.Contains(t, sql, "0.000000800000::numeric")
	require.Contains(t, sql, "('deepseek-v4-pro-0813'")
	require.Contains(t, sql, "0.000027000000::numeric")
	require.Contains(t, sql, "('kimi-k3'")
	require.Contains(t, sql, "0.000100000000::numeric")
	require.Contains(t, sql, "('kimi-k2.5'")
	require.Contains(t, sql, "0.000021000000::numeric")
	require.Contains(t, sql, "('kimi-k2.7code'")
	require.Contains(t, sql, "LOWER(pricing.models->>0) = 'qwen3.7-plus'")
	require.Contains(t, sql, "0.000001200000::numeric")
	require.GreaterOrEqual(t, strings.Count(sql, "256000"), 3)
}
