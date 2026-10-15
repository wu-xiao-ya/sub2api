package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDomesticAggregatePlatformsMigration(t *testing.T) {
	content, err := FS.ReadFile("243_publish_domestic_aggregate_platforms.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, platform := range []string{"qwen", "minimax", "mimo", "hunyuan"} {
		require.Contains(t, sql, "'"+platform+"'")
	}
	require.Contains(t, sql, "WHEN 'deepseek' THEN 0.6000")
	require.Contains(t, sql, "WHEN 'kimi' THEN 0.4500")
	require.Contains(t, sql, "WHEN 'glm' THEN 0.3000")
	require.Contains(t, sql, "'Qwen', 'Domestic aggregate Qwen models', 'qwen', 0.5000")
	require.Contains(t, sql, "qwen3.7-plus")
	require.Contains(t, sql, "minimax-m2.7-highspeed")
	require.Contains(t, sql, "mimo-v2.5-pro")
	require.Contains(t, sql, "hunyuan-hy3")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "user_platform_quotas_platform_check")
	require.Contains(t, sql, "existing.max_tokens IS NOT DISTINCT FROM interval.max_tokens")
	require.NotContains(t, strings.ToLower(sql), "insert into accounts")
	require.NotContains(t, strings.ToLower(sql), "sk-")
}
