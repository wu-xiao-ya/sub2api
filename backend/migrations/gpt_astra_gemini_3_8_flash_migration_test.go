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
	require.Contains(t, sql, "0.000010000000")
	require.Contains(t, sql, "0.000050000000")
	require.Contains(t, sql, "0.000012500000")
	require.Contains(t, sql, "0.000001000000")
	require.Contains(t, sql, "cmp.input_price * 2")
	require.Contains(t, sql, "cmp.output_price * 2 AS output_price")
	require.Contains(t, sql, "cmp.cache_write_price * 2 AS cache_write_price")
	require.Contains(t, sql, "cmp.cache_read_price * 2 AS cache_read_price")
	require.Contains(t, sql, "channel_monitor_v2_config")
	require.Contains(t, sql, "model_mapping")
	require.GreaterOrEqual(t, strings.Count(sql, "NOT EXISTS"), 2)
}
