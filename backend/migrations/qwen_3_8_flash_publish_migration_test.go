package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishQwen38FlashMigrationCoversCatalogAndAvailableChannels(t *testing.T) {
	sqlBytes, err := FS.ReadFile("253_publish_qwen_3_8_flash.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "UPDATE groups")
	require.Contains(t, sql, "models_list_config")
	require.Contains(t, sql, "platform = 'qwen'")
	require.Contains(t, sql, "INSERT INTO channel_model_pricing")
	require.Contains(t, sql, "FROM channel_groups")
	require.GreaterOrEqual(t, strings.Count(sql, "qwen3.8-flash"), 4)
	require.Contains(t, sql, "0.000000800000")
	require.Contains(t, sql, "0.000002700000")
	require.Contains(t, sql, "0.000001250000")
	require.Contains(t, sql, "0.000000100000")
	require.Contains(t, sql, "NOT EXISTS")
	require.Contains(t, sql, "extra_models")
}
