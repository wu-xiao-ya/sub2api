package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishGLM53MigrationCoversCatalogAndAvailableChannels(t *testing.T) {
	sqlBytes, err := FS.ReadFile("238_publish_glm_5_3.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "UPDATE groups")
	require.Contains(t, sql, "models_list_config")
	require.Contains(t, sql, "platform = 'glm'")
	require.Contains(t, sql, "INSERT INTO channel_model_pricing")
	require.Contains(t, sql, "FROM channel_groups")
	require.GreaterOrEqual(t, strings.Count(sql, "glm-5.3"), 4)
	require.Contains(t, sql, "0.000000800000")
	require.Contains(t, sql, "0.000002560000")
	require.Contains(t, sql, "0.000000160000")
}

func TestCorrectGLM53PricingMigrationOnlyMatchesCopiedGLM52Price(t *testing.T) {
	sqlBytes, err := FS.ReadFile("239_correct_glm_5_3_channel_pricing.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "UPDATE channel_model_pricing")
	require.Contains(t, sql, `models @> '["glm-5.3"]'::jsonb`)
	require.Contains(t, sql, "input_price = 0.000001400000")
	require.Contains(t, sql, "output_price = 0.000004400000")
	require.Contains(t, sql, "cache_read_price = 0.000000260000")
	require.Contains(t, sql, "SET input_price = 0.000000800000")
	require.Contains(t, sql, "output_price = 0.000002560000")
	require.Contains(t, sql, "cache_read_price = 0.000000160000")
}
