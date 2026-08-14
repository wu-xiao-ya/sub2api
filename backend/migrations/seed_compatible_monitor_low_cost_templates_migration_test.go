package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSeedCompatibleMonitorLowCostTemplatesMigration(t *testing.T) {
	content, err := FS.ReadFile("224_seed_compatible_monitor_low_cost_templates.sql")
	require.NoError(t, err)

	sql := string(content)
	require.False(t, strings.HasPrefix(sql, "\xef\xbb\xbf"))
	require.Contains(t, sql, "Compatibility marker")
	require.Contains(t, sql, "DO $$ BEGIN END $$;")
	require.NotContains(t, sql, "INSERT INTO channel_monitor_request_templates")
}

func TestFixCompatibleMonitorLowCostBudgetMigration(t *testing.T) {
	content, err := FS.ReadFile("225_fix_compatible_monitor_low_cost_budget.sql")
	require.NoError(t, err)

	sql := string(content)
	require.False(t, strings.HasPrefix(sql, "\xef\xbb\xbf"))
	require.Contains(t, sql, "UPDATE channel_monitor_request_templates")
	require.Contains(t, sql, "UPDATE channel_monitors")
	require.Contains(t, sql, "'deepseek', 'kimi', 'glm'")
	require.Contains(t, sql, "api_mode = 'chat_completions'")
	require.Contains(t, sql, `"max_tokens": 16`)
	require.Contains(t, sql, `"max_tokens": 1`)
}

func TestDisableKimiThinkingForLowCostMonitorMigration(t *testing.T) {
	content, err := FS.ReadFile("226_disable_kimi_thinking_for_low_cost_monitor.sql")
	require.NoError(t, err)

	sql := string(content)
	require.False(t, strings.HasPrefix(sql, "\xef\xbb\xbf"))
	require.Contains(t, sql, "channel_monitor_request_templates")
	require.Contains(t, sql, "channel_monitors")
	require.Contains(t, sql, "provider = 'kimi'")
	require.Contains(t, sql, `"thinking": {"type": "disabled"}`)
	require.Contains(t, sql, `"max_tokens": 16`)
}
