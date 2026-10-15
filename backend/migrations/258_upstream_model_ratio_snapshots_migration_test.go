package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamModelRatioSnapshotsMigration(t *testing.T) {
	content, err := FS.ReadFile("258_upstream_model_ratio_snapshots.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS upstream_model_ratio_snapshots")
	require.Contains(t, sql, "account_id BIGINT NOT NULL")
	require.Contains(t, sql, "uq_upstream_model_ratio_snapshots_identity")
	require.Contains(t, sql, "ON upstream_model_ratio_snapshots (account_id, model_name, observed_at)")
	require.Contains(t, sql, "idx_upstream_model_ratio_snapshots_lookup")
}
