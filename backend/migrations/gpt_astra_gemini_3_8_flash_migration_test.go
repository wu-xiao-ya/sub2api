package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishGPTAstraAndGemini38FlashMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("246_publish_gpt_astra_gemini_3_8_flash.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "gpt-6-astra")
	require.Contains(t, sql, "gemini-3.8-flash")
	require.Contains(t, sql, "gemini-3.7-flash")
	require.Contains(t, sql, "gpt-5.6-sol")
	require.Contains(t, sql, "0.0400000000")
	require.Contains(t, sql, "channel_monitor_v2_config")
	require.Contains(t, sql, "model_mapping")
	require.GreaterOrEqual(t, strings.Count(sql, "NOT EXISTS"), 2)
}
