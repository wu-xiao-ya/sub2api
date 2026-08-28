package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishGLM53FlashMigrationCoversCatalogAndAvailableChannels(t *testing.T) {
	sqlBytes, err := FS.ReadFile("242_publish_glm_5_3_flash.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "UPDATE groups")
	require.Contains(t, sql, "models_list_config")
	require.Contains(t, sql, "platform = 'glm'")
	require.Contains(t, sql, "INSERT INTO channel_model_pricing")
	require.Contains(t, sql, "FROM channel_groups")
	require.GreaterOrEqual(t, strings.Count(sql, "glm-5.3-flash"), 4)
	require.Contains(t, sql, "0.000000800000")
	require.Contains(t, sql, "0.000002800000")
	require.Contains(t, sql, "0.000000230000")
	require.Contains(t, sql, "NOT EXISTS")
}
