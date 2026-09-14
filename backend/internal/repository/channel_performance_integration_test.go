package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func performanceTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PERFORMANCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PERFORMANCE_TEST_DATABASE_URL required; use isolated CI PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := fmt.Sprintf("performance_test_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE"); _ = db.Close() })
	_, err = db.Exec("SET search_path TO " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE usage_logs (
        id BIGSERIAL PRIMARY KEY, api_key_id BIGINT NOT NULL, request_id TEXT NOT NULL,
        group_id BIGINT, requested_model TEXT, model TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
        service_tier TEXT, reasoning_effort TEXT, stream BOOLEAN NOT NULL DEFAULT false,
        usage_source TEXT, output_tokens BIGINT NOT NULL DEFAULT 0, image_count INTEGER NOT NULL DEFAULT 0,
        inbound_endpoint TEXT, latency_breakdown JSONB,
        UNIQUE(api_key_id,request_id)
    )`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/248_channel_performance.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.Exec(string(migration))
		require.NoError(t, err, "migration must be repeatable")
	}
	return db
}

func TestPerformanceDatabaseAggregation(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Hour)
	at := now.Add(-3 * time.Hour)
	number := func(n int) *int { return &n }
	stream := true
	mkFact := func(id string, group int64, outcome service.ChannelPerformanceOutcome, when time.Time) service.ChannelPerformanceFact {
		return service.ChannelPerformanceFact{APIKeyID: 1, RequestID: id, GroupID: group, Model: "gpt-test", Outcome: outcome, Stream: &stream,
			StartedAt: when, CompletedAt: when.Add(10 * time.Second), Latency: &service.UsageLatencyBreakdown{
				Version: 2, FirstCharacterMs: number(1000), FirstOutputMs: number(1000), TotalDurationMs: number(10000),
			}}
	}
	usage := func(key int64, id string, group int64, tokens int, source string, when time.Time) {
		_, err := db.Exec(`INSERT INTO usage_logs(api_key_id,request_id,group_id,model,requested_model,created_at,output_tokens,stream,usage_source)
            VALUES($1,$2,$3,'mapped-internal','gpt-test',$4,$5,true,NULLIF($6,''))`, key, id, group, when, tokens, source)
		require.NoError(t, err)
	}
	usage(1, "local:free-success", 1, 90, "", at)
	usage(1, "local:stream-failed", 1, 90, "", at)
	usage(1, "local:legacy", 1, 900, "", at)
	usage(1, "local:probe", 1, 900, "channel_monitor", at)
	usage(2, "local:free-success", 2, 900, "", at)
	facts := []service.ChannelPerformanceFact{
		mkFact("local:free-success", 1, service.PerformanceSuccess, at),
		mkFact("local:stream-failed", 1, service.PerformanceFailure, at),
		mkFact("local:user-balance", 1, service.PerformanceExcluded, at),
		mkFact("local:upstream-balance", 1, service.PerformanceFailure, at),
	}
	require.NoError(t, r.RecordFacts(ctx, facts))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	f := service.ChannelPerformanceFilter{Range: "24h", Start: at, End: at.Add(time.Hour), Model: "gpt-test", Bucket: time.Hour}
	rows, _, err := r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].Success)
	require.EqualValues(t, 2, rows[0].Failure)
	require.EqualValues(t, 1, rows[0].Unknown)
	require.EqualValues(t, 90, rows[0].OutputTokens)
	require.EqualValues(t, 9000, rows[0].OutputMs)
	require.InDelta(t, 10, *rows[0].Metric().OutputTPS, 0.001)
	require.Equal(t, "partial", rows[0].Metric().CoverageStatus)
	f.ServiceTier = "standard"
	rows, _, err = r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.Empty(t, rows)
	f.ServiceTier = "unknown"
	rows, _, err = r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	f.ServiceTier = ""
	// Recompute replaces buckets; it must not increment them a second time.
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	rows, _, err = r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows[0].Success)
	// Same request ID under another API key remains an independent fact.
	second := mkFact("local:free-success", 2, service.PerformanceSuccess, at)
	second.APIKeyID = 2
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{second}))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	rows, _, err = r.Query(ctx, f, []int64{2})
	require.NoError(t, err)
	require.EqualValues(t, 900, rows[0].OutputTokens)
	// A later successful witness replaces failure even across hour boundaries.
	retry := mkFact("local:stream-failed", 1, service.PerformanceSuccess, at.Add(time.Hour))
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{retry}))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	rows, _, err = r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.EqualValues(t, 2, rows[0].Success)
	require.EqualValues(t, 1, rows[0].Failure)
	// An out-of-order failure cannot roll a successful logical request back.
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{facts[1]}))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	rows, _, err = r.Query(ctx, f, []int64{1})
	require.NoError(t, err)
	require.EqualValues(t, 2, rows[0].Success)
}

func TestPerformanceDatabaseLateUsageAndNoBillingTriggers(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-time.Hour)
	start, done := 100, 1100
	stream := true
	f := service.ChannelPerformanceFact{APIKeyID: 1, RequestID: "local:late", GroupID: 1, Model: "model", Outcome: service.PerformanceSuccess, Stream: &stream,
		StartedAt: at, CompletedAt: at.Add(time.Second), Latency: &service.UsageLatencyBreakdown{Version: 2, FirstOutputMs: &start, TotalDurationMs: &done}}
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{f}))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	_, err := db.Exec(`INSERT INTO usage_logs(api_key_id,request_id,group_id,model,created_at,output_tokens,stream)
        VALUES(1,'local:late',1,'model',$1,100,true)`, now)
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: at, End: now, Model: "model"}, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 100, rows[0].OutputTokens)
	var triggers int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM pg_trigger WHERE tgrelid='usage_logs'::regclass AND NOT tgisinternal").Scan(&triggers))
	require.Zero(t, triggers, "statistics must not add billing-write triggers")
}
