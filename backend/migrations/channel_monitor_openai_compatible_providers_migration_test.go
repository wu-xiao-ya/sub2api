package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorOpenAICompatibleProvidersMigration(t *testing.T) {
	content, err := FS.ReadFile("223_channel_monitor_openai_compatible_providers.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "channel_monitor_request_templates_provider_check")
	require.Contains(t, sql, "'deepseek'")
	require.Contains(t, sql, "'kimi'")
	require.Contains(t, sql, "'glm'")
	require.Contains(t, sql, "'antigravity'")
	require.Contains(t, sql, "position('deepseek' IN monitor_constraint_def) = 0")
	require.Contains(t, sql, "position('kimi' IN template_constraint_def) = 0")
	require.Contains(t, sql, "position('glm' IN template_constraint_def) = 0")
}
