package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type performancePlanNode struct {
	Type     string                `json:"Node Type"`
	Relation string                `json:"Relation Name"`
	Index    string                `json:"Index Name"`
	Rows     float64               `json:"Actual Rows"`
	Loops    float64               `json:"Actual Loops"`
	Plans    []performancePlanNode `json:"Plans"`
}

func explainPerformancePlan(t *testing.T, db *sql.DB, name, query string, timeout time.Duration, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	var raw []byte
	require.NoError(t, db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw))
	var plans []struct {
		Plan          performancePlanNode `json:"Plan"`
		ExecutionTime float64             `json:"Execution Time"`
	}
	require.NoError(t, json.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)
	var inspect func(performancePlanNode)
	inspect = func(n performancePlanNode) {
		if n.Relation != "" {
			t.Logf("%s: %s relation=%s index=%s rows=%.0f loops=%.0f", name, n.Type, n.Relation, n.Index, n.Rows, n.Loops)
			if n.Relation == "usage_logs" || n.Relation == "channel_performance_facts" || n.Relation == "channel_performance_buckets" {
				require.NotEqual(t, "Seq Scan", n.Type, "selective production query must not scan the full synthetic history")
			}
		}
		for _, child := range n.Plans {
			inspect(child)
		}
	}
	inspect(plans[0].Plan)
	t.Logf("%s execution_ms=%.3f (synthetic CI fixture, not a production latency guarantee)", name, plans[0].ExecutionTime)
	if dir := os.Getenv("PERFORMANCE_PLAN_REPORT_DIR"); dir != "" {
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), raw, 0644))
	}
}

func TestPerformanceDatabaseSelectiveQueryPlans(t *testing.T) {
	db := performanceTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Hour)
	start := now.Add(-30 * 24 * time.Hour)
	// Mirrors the existing usage timestamp index from migration 001; no new
	// production billing index or trigger is installed by performance monitoring.
	_, err := db.Exec("CREATE INDEX idx_usage_logs_created_at ON usage_logs(created_at)")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_logs(api_key_id,request_id,group_id,model,created_at,output_tokens,stream)
		SELECT 1,'load-'||n,1+n%100,'model-load',$1::timestamptz+(n%720)*INTERVAL '1 hour',90,true
		FROM generate_series(1,100000) AS n`, start)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO channel_performance_facts(api_key_id,request_id,group_id,model,outcome,started_at,completed_at,stream,latency_breakdown)
		SELECT api_key_id,request_id,group_id,model,'success',created_at,created_at+INTERVAL '10 seconds',true,
		'{"version":2,"first_output_ms":1000,"first_character_ms":1000,"total_duration_ms":10000}'::jsonb FROM usage_logs`)
	require.NoError(t, err)
	for _, table := range []string{"usage_logs", "channel_performance_facts"} {
		_, err = db.Exec("ANALYZE " + pq.QuoteIdentifier(table))
		require.NoError(t, err)
	}
	explainPerformancePlan(t, db, "recompute-hour-100k", performanceSourceSQL, 20*time.Second, now.Add(-time.Hour), now, 60)
	_, err = db.Exec(`INSERT INTO channel_performance_buckets(bucket_seconds,bucket_start,group_id,model,service_tier,reasoning_effort,stream,success_count)
		SELECT 3600,$1::timestamptz+h*INTERVAL '1 hour',g,'model-load','default','unknown',1,10
		FROM generate_series(0,719) AS h CROSS JOIN generate_series(1,100) AS g`, start)
	require.NoError(t, err)
	_, err = db.Exec("ANALYZE channel_performance_buckets")
	require.NoError(t, err)
	explainPerformancePlan(t, db, "visible-groups-72k-buckets", performanceQuerySQL, 8*time.Second,
		3600, now.Add(-7*24*time.Hour), now, pq.Array([]int64{1, 2}), pq.Array([]string{"model-load"}), "", "", nil, 21600)
}
