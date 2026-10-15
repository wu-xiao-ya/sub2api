package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAggregateMigrationCreatesKindAndMembers(t *testing.T) {
	sqlBytes, err := FS.ReadFile("254_api_key_aggregate.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS key_kind")
	require.Contains(t, sql, "api_key_aggregate_members")
	require.Contains(t, sql, "aggregate_key_id")
	require.Contains(t, sql, "member_key_id")
	require.Contains(t, sql, "sort_order")
	require.Contains(t, sql, "'group', 'aggregate'")
	require.GreaterOrEqual(t, strings.Count(sql, "CREATE INDEX IF NOT EXISTS"), 3)
}
