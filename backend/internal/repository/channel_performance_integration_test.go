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
	for i := 0; i < 2; i++ {
		for _, name := range []string{"248_channel_performance.sql", "249_websocket_performance_identity.sql", "250_performance_runtime_coverage.sql", "251_performance_usage_reconciliation.sql"} {
			migration, err := os.ReadFile("../../migrations/" + name)
			require.NoError(t, err)
			_, err = db.Exec(string(migration))
			require.NoError(t, err, "migration must be repeatable")
		}
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

func TestPerformanceDatabaseTPSExcludesImagesNonStreamAndMissingStages(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	for _, tc := range []struct {
		id       string
		stream   bool
		images   int
		endpoint string
		stages   bool
	}{
		{"text", true, 0, "/v1/responses", true},
		{"image-count", true, 1, "/v1/responses", true},
		{"image-endpoint", true, 0, "/v1/images/generations", true},
		{"non-stream", false, 0, "/v1/responses", true},
		{"missing-stages", true, 0, "/v1/responses", false},
	} {
		_, err := db.Exec(`INSERT INTO usage_logs(api_key_id,request_id,group_id,model,created_at,output_tokens,stream,image_count,inbound_endpoint)
			VALUES(1,$1,1,'model',$2,90,$3,$4,$5)`, tc.id, at, tc.stream, tc.images, tc.endpoint)
		require.NoError(t, err)
		fact := service.ChannelPerformanceFact{APIKeyID: 1, RequestID: tc.id, GroupID: 1, Model: "model",
			Outcome: service.PerformanceSuccess, Stream: &tc.stream, StartedAt: at, CompletedAt: at.Add(10 * time.Second)}
		if tc.stages {
			start, done := 1000, 10000
			fact.Latency = &service.UsageLatencyBreakdown{Version: 2, FirstOutputMs: &start, TotalDurationMs: &done}
		}
		require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{fact}))
	}
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: at, End: at.Add(time.Hour), Model: "model", Bucket: time.Hour}, []int64{1})
	require.NoError(t, err)
	var total service.ChannelPerformanceCounts
	for _, row := range rows {
		total.Add(row.ChannelPerformanceCounts)
	}
	require.EqualValues(t, 5, total.Success)
	require.EqualValues(t, 1, total.OutputCount)
	require.EqualValues(t, 90, total.OutputTokens)
	require.EqualValues(t, 9000, total.OutputMs)
	require.InDelta(t, 10, *total.Metric().OutputTPS, 0.001)
	require.Nil(t, total.Metric().FirstCharacterMs, "missing visible-text milestones must not be substituted")
}

func TestPerformanceDatabaseEarlierWitnessInvalidatesBothHours(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	earlier := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	later := earlier.Add(time.Hour)
	fact := service.ChannelPerformanceFact{APIKeyID: 1, RequestID: "local:moved", GroupID: 1, Model: "model",
		Outcome: service.PerformanceSuccess, StartedAt: later, CompletedAt: later.Add(time.Second)}
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{fact}))
	require.NoError(t, r.Recompute(ctx, later, later.Add(time.Hour)))
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM channel_performance_dirty_hours WHERE hour_start=$1", later).Scan(&count))
	require.Zero(t, count)
	// A delayed witness can move a previously aggregated request backwards.
	fact.StartedAt = earlier
	fact.Outcome = service.PerformanceFailure
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{fact}))
	require.NoError(t, db.QueryRow("SELECT count(*) FROM channel_performance_dirty_hours WHERE hour_start IN ($1,$2)", earlier, later).Scan(&count))
	require.Equal(t, 2, count, "both old and new buckets must be invalidated")
	require.NoError(t, r.Recompute(ctx, earlier, earlier.Add(time.Hour)))
	require.NoError(t, r.Recompute(ctx, later, later.Add(time.Hour)))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: earlier, End: later.Add(time.Hour), Model: "model", Bucket: time.Hour}, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].At.Equal(earlier))
	require.EqualValues(t, 1, rows[0].Success, "a late failure cannot replace success")
	require.Zero(t, rows[0].Failure)
}

func TestPerformanceDatabaseRollingSweepRecoversLowerUsageID(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-4 * time.Hour)
	// Model a transaction which reserved ID 1 but committed after ID 2.
	_, err := db.Exec("INSERT INTO usage_logs(id,api_key_id,request_id,group_id,model,created_at) VALUES(2,1,'later-id',1,'model',$1)", at)
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now))
	var cursor int64
	require.NoError(t, db.QueryRow("SELECT usage_cursor FROM channel_performance_watermark WHERE id=1").Scan(&cursor))
	require.EqualValues(t, 2, cursor)
	_, err = db.Exec("INSERT INTO usage_logs(id,api_key_id,request_id,group_id,model,created_at) VALUES(1,1,'late-commit',1,'model',$1)", at)
	require.NoError(t, err)
	// Completed initial backfill must start a new durable sweep.
	_, err = db.Exec("UPDATE channel_performance_watermark SET backfill_cursor=$1 WHERE id=1", now.Add(-31*24*time.Hour))
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now))
	var sweep time.Time
	require.NoError(t, db.QueryRow("SELECT backfill_cursor FROM channel_performance_watermark WHERE id=1").Scan(&sweep))
	require.True(t, sweep.Equal(now.Truncate(time.Hour)))
	// Fast-forward that bounded sweep to the affected historical hour.
	_, err = db.Exec("UPDATE channel_performance_watermark SET backfill_cursor=$1 WHERE id=1", at.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: at, End: at.Add(time.Hour), Model: "model", Bucket: time.Hour}, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, rows[0].Unknown, "late lower IDs must not be lost permanently")
}

func TestPerformanceDatabaseWebSocketNativeUsageJoin(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Hour)
	at := now.Add(-time.Hour)
	_, err := db.Exec(`INSERT INTO usage_logs(api_key_id,request_id,group_id,model,created_at,output_tokens,stream)
		VALUES(9,'resp_native',5,'mapped-model',$1,90,true),(10,'resp_native',6,'other-model',$1,900,true)`, now)
	require.NoError(t, err)
	require.NoError(t, r.Recompute(ctx, now, now.Add(time.Hour)))
	start, done := 1000, 10000
	stream := true
	f := service.ChannelPerformanceFact{APIKeyID: 9, RequestID: "ws:connection:1", UsageRequestID: "resp_native", GroupID: 5, Model: "public-model",
		Outcome: service.PerformanceSuccess, Stream: &stream, StartedAt: at, CompletedAt: at.Add(10 * time.Second),
		Latency: &service.UsageLatencyBreakdown{Version: 2, FirstOutputMs: &start, FirstCharacterMs: &start, TotalDurationMs: &done}}
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{f}))
	var dirty int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM channel_performance_dirty_hours WHERE hour_start IN ($1,$2)", at, now).Scan(&dirty))
	require.Equal(t, 2, dirty, "a late witness must remove the formerly unknown usage bucket")
	failed := f
	failed.RequestID = "ws:connection:2"
	failed.UsageRequestID = ""
	failed.Outcome = service.PerformanceFailure
	failed.Latency = nil
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{failed}))
	require.NoError(t, r.Recompute(ctx, at, now.Add(time.Hour)))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: at, End: now.Add(time.Hour), Bucket: time.Hour}, []int64{5})
	require.NoError(t, err)
	require.Len(t, rows, 1, "native usage must not remain as a second unknown request")
	require.Equal(t, "public-model", rows[0].Model)
	require.EqualValues(t, 1, rows[0].Success)
	require.EqualValues(t, 1, rows[0].Failure)
	require.Zero(t, rows[0].Unknown)
	require.EqualValues(t, 90, rows[0].OutputTokens, "other API keys with the same response ID remain isolated")
	require.InDelta(t, 10, *rows[0].Metric().OutputTPS, 0.001)
	var requestID string
	require.NoError(t, db.QueryRow("SELECT request_id FROM usage_logs WHERE api_key_id=9").Scan(&requestID))
	require.Equal(t, "resp_native", requestID, "telemetry must not rewrite billing identities")
}

func TestPerformanceDatabaseRuntimeCrashCoverage(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	started := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, r.HeartbeatRuntime(ctx, "crashed", started))
	require.NoError(t, r.HeartbeatRuntime(ctx, "healthy", started.Add(time.Minute)))
	require.NoError(t, r.HeartbeatRuntime(ctx, "clean", started.Add(-time.Minute)))
	require.NoError(t, r.CloseRuntime(ctx, "clean"))
	coverage, err := r.Coverage(ctx)
	require.NoError(t, err)
	require.Nil(t, coverage.IncompleteSince, "live and cleanly drained runtimes must not imply loss")
	_, err = db.Exec("UPDATE channel_performance_runtimes SET last_seen_at=now()-INTERVAL '2 minutes' WHERE instance_id='crashed'")
	require.NoError(t, err)
	coverage, err = r.Coverage(ctx)
	require.NoError(t, err)
	require.NotNil(t, coverage.IncompleteSince, "queries detect a crash even before the next heartbeat sweep")
	require.True(t, started.Equal(*coverage.IncompleteSince))
	require.NoError(t, r.HeartbeatRuntime(ctx, "candidate", time.Now().UTC()))
	var closed bool
	require.NoError(t, db.QueryRow("SELECT closed_at IS NOT NULL FROM channel_performance_runtimes WHERE instance_id='crashed'").Scan(&closed))
	require.True(t, closed)
	// Recovery of a paused instance cannot erase the already persisted gap.
	require.NoError(t, r.HeartbeatRuntime(ctx, "crashed", started))
	coverage, err = r.Coverage(ctx)
	require.NoError(t, err)
	require.NotNil(t, coverage.IncompleteSince)
	require.True(t, started.Equal(*coverage.IncompleteSince))
	require.NoError(t, db.QueryRow("SELECT closed_at IS NOT NULL FROM channel_performance_runtimes WHERE instance_id='healthy'").Scan(&closed))
	require.False(t, closed, "rolling deployment must not retire another healthy instance")
}

func TestPerformanceDatabasePendingUsageSurvivesOutOfOrderCommit(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	at := now.Truncate(time.Hour).Add(-4 * time.Hour)
	start, done := 100, 1100
	stream := true
	fact := service.ChannelPerformanceFact{APIKeyID: 1, RequestID: "ws:late:1", UsageRequestID: "resp_late", GroupID: 1, Model: "model",
		Outcome: service.PerformanceSuccess, Stream: &stream, StartedAt: at, CompletedAt: at.Add(time.Second),
		Latency: &service.UsageLatencyBreakdown{Version: 2, FirstOutputMs: &start, FirstCharacterMs: &start, TotalDurationMs: &done}}
	require.NoError(t, r.RecordFacts(ctx, []service.ChannelPerformanceFact{fact}))
	require.NoError(t, r.Recompute(ctx, at, at.Add(time.Hour)))
	var schema string
	require.NoError(t, db.QueryRow("SELECT current_schema()").Scan(&schema))
	writer, err := sql.Open("postgres", os.Getenv("PERFORMANCE_TEST_DATABASE_URL"))
	require.NoError(t, err)
	writer.SetMaxOpenConns(1)
	defer writer.Close()
	_, err = writer.Exec("SET search_path TO " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	tx, err := writer.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO usage_logs(id,api_key_id,request_id,group_id,model,created_at,output_tokens,stream)
        VALUES(1,1,'resp_late',1,'model',$1,100,true)`, at)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_logs(id,api_key_id,request_id,group_id,model,created_at)
        VALUES(2,2,'already-committed',2,'other',$1)`, now)
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now.Add(time.Second)))
	var cursor int64
	require.NoError(t, db.QueryRow("SELECT usage_cursor FROM channel_performance_watermark WHERE id=1").Scan(&cursor))
	require.EqualValues(t, 2, cursor)
	require.NoError(t, tx.Commit())
	// A historical backlog must not delay this already-observed logical request.
	_, err = db.Exec("INSERT INTO channel_performance_dirty_hours(hour_start) VALUES($1) ON CONFLICT DO NOTHING", at.Add(-time.Hour))
	require.NoError(t, err)
	require.NoError(t, r.ProcessPending(ctx, now.Add(35*time.Second)))
	rows, _, err := r.Query(ctx, service.ChannelPerformanceFilter{Range: "24h", Start: at, End: at.Add(time.Hour), Model: "model", Bucket: time.Hour}, []int64{1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1, rows[0].Success)
	require.Zero(t, rows[0].Unknown)
	require.EqualValues(t, 100, rows[0].OutputTokens)
	require.InDelta(t, 100, *rows[0].Metric().OutputTPS, 0.001)
	var resolved bool
	require.NoError(t, db.QueryRow("SELECT usage_resolved FROM channel_performance_facts WHERE request_id=$1", fact.RequestID).Scan(&resolved))
	require.True(t, resolved)
}

func TestPerformanceDatabasePendingUsageBoundedAndNoEmptyInvalidation(t *testing.T) {
	db := performanceTestDatabase(t)
	r := NewChannelPerformanceRepository(db)
	now := time.Now().UTC().Add(time.Second)
	_, err := db.Exec(`INSERT INTO channel_performance_facts(api_key_id,request_id,group_id,model,outcome,started_at,completed_at)
        SELECT 1,'pending:'||i,1,'model','success',now()-INTERVAL '1 minute',now() FROM generate_series(1,2001) i`)
	require.NoError(t, err)
	_, err = db.Exec("DELETE FROM channel_performance_dirty_hours")
	require.NoError(t, err)
	now = time.Now().UTC().Add(time.Second)
	require.NoError(t, r.reconcilePendingUsage(context.Background(), now))
	var checked, dirty int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM channel_performance_facts WHERE usage_check_attempts=1").Scan(&checked))
	require.Equal(t, 2000, checked)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM channel_performance_dirty_hours").Scan(&dirty))
	require.Zero(t, dirty, "missing usage does not change any performance metric")
	var delay float64
	require.NoError(t, db.QueryRow("SELECT max(extract(epoch FROM usage_next_check_at-$1)) FROM channel_performance_facts WHERE usage_check_attempts=1", now).Scan(&delay))
	require.InDelta(t, 30, delay, 0.001)
	_, err = db.Exec("UPDATE channel_performance_facts SET usage_check_attempts=100,usage_next_check_at=$1", now)
	require.NoError(t, err)
	require.NoError(t, r.reconcilePendingUsage(context.Background(), now))
	require.NoError(t, db.QueryRow("SELECT max(extract(epoch FROM usage_next_check_at-$1)) FROM channel_performance_facts", now).Scan(&delay))
	require.InDelta(t, 300, delay, 0.001)
}
