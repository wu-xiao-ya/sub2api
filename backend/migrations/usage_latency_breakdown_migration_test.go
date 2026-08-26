package migrations

import (
	"strings"
	"testing"
)

func TestUsageLatencyBreakdownMigrationAddsBothJSONBColumns(t *testing.T) {
	sqlBytes, err := FS.ReadFile("240_usage_latency_breakdown.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sqlText := strings.ToLower(string(sqlBytes))
	for _, clause := range []string{
		"alter table usage_logs",
		"add column if not exists latency_breakdown jsonb",
		"alter table channel_monitor_histories",
	} {
		if !strings.Contains(sqlText, clause) {
			t.Fatalf("migration missing %q", clause)
		}
	}
}
