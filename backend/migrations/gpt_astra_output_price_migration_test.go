package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCorrectGPTAstraOutputPriceMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("247_correct_gpt_astra_output_price.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "gpt-6-astra")
	require.Contains(t, sql, "0.000060000000")
	require.Contains(t, sql, "0.000050000000")
	require.Contains(t, sql, "cache_write_price")
	require.Contains(t, sql, "cache_read_price")
	require.Equal(t, 1, strings.Count(sql, "UPDATE channel_model_pricing"))
}
