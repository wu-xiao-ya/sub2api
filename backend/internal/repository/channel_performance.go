package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type channelPerformanceRepository struct{ db *sql.DB }

func NewChannelPerformanceRepository(db *sql.DB) *channelPerformanceRepository {
	return &channelPerformanceRepository{db: db}
}

var _ service.ChannelPerformanceRepository = (*channelPerformanceRepository)(nil)
var _ service.ChannelPerformanceFactRepository = (*channelPerformanceRepository)(nil)

const performanceQuerySQL = `
        SELECT group_id, model, to_timestamp(floor(extract(epoch FROM bucket_start)/$9)*$9) AS at,
            SUM(success_count), SUM(failure_count), SUM(unknown_count),
            SUM(character_count), SUM(character_sum), SUM(duration_count), SUM(duration_sum),
            SUM(output_tokens), SUM(output_ms), SUM(output_count)
        FROM channel_performance_buckets
        WHERE bucket_seconds = $1 AND bucket_start >= $2 AND bucket_start < $3
          AND group_id = ANY($4::bigint[])
          AND ($5 = '' OR model = $5) AND ($6 = '' OR service_tier = $6)
          AND ($7 = '' OR reasoning_effort = $7) AND ($8::smallint IS NULL OR stream = $8)
        GROUP BY group_id, model, at ORDER BY at, group_id, model`

func (r *channelPerformanceRepository) Query(ctx context.Context, f service.ChannelPerformanceFilter, groups []int64) ([]service.ChannelPerformanceRow, service.ChannelPerformanceCoverage, error) {
	result := []service.ChannelPerformanceRow{}
	if len(groups) == 0 {
		return result, service.ChannelPerformanceCoverage{}, nil
	}
	seconds := 60
	if f.Range == "7d" || f.Range == "30d" {
		seconds = 3600
	}
	bucket := int(f.Bucket.Seconds())
	if bucket < seconds {
		bucket = seconds
	}
	var stream any
	if f.Stream != nil {
		stream = 0
		if *f.Stream {
			stream = 1
		}
	}
	rows, err := r.db.QueryContext(ctx, performanceQuerySQL,
		seconds, f.Start, f.End, pq.Array(groups), f.Model, f.ServiceTier, f.ReasoningEffort, stream, bucket)
	if err != nil {
		return nil, service.ChannelPerformanceCoverage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var row service.ChannelPerformanceRow
		if err := rows.Scan(&row.GroupID, &row.Model, &row.At, &row.Success, &row.Failure, &row.Unknown,
			&row.CharacterCount, &row.CharacterSum, &row.DurationCount, &row.DurationSum, &row.OutputTokens, &row.OutputMs, &row.OutputCount); err != nil {
			return nil, service.ChannelPerformanceCoverage{}, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, service.ChannelPerformanceCoverage{}, err
	}
	// Close before the second query, including with a one-connection pool.
	if err := rows.Close(); err != nil {
		return nil, service.ChannelPerformanceCoverage{}, err
	}
	coverage, err := r.Coverage(ctx)
	return result, coverage, err
}

func (r *channelPerformanceRepository) Coverage(ctx context.Context) (service.ChannelPerformanceCoverage, error) {
	var c service.ChannelPerformanceCoverage
	err := r.db.QueryRowContext(ctx, `SELECT coverage_start, coverage_end, updated_at,
        LEAST(incomplete_since, (SELECT MIN(started_at) FROM channel_performance_runtimes
            WHERE closed_at IS NULL AND last_seen_at < now() - INTERVAL '90 seconds'))
		FROM channel_performance_watermark WHERE id = 1`).Scan(&c.Start, &c.End, &c.UpdatedAt, &c.IncompleteSince)
	if err == sql.ErrNoRows {
		err = nil
	}
	return c, err
}

// A stale runtime is conservatively incomplete from startup, not its last
// heartbeat: an in-flight logical request may have started much earlier.
const persistStalePerformanceRuntimes = `WITH stale AS (
    UPDATE channel_performance_runtimes SET closed_at=now()
    WHERE closed_at IS NULL AND last_seen_at < now() - INTERVAL '90 seconds'
    RETURNING started_at
) UPDATE channel_performance_watermark
  SET incomplete_since=LEAST(incomplete_since,(SELECT MIN(started_at) FROM stale))
  WHERE id=1 AND EXISTS(SELECT 1 FROM stale)`

func (r *channelPerformanceRepository) HeartbeatRuntime(ctx context.Context, id string, started time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, persistStalePerformanceRuntimes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_performance_runtimes(instance_id,started_at)
        VALUES($1,$2) ON CONFLICT(instance_id) DO UPDATE SET last_seen_at=now(),closed_at=NULL`, id, started)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *channelPerformanceRepository) CloseRuntime(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE channel_performance_runtimes SET closed_at=now() WHERE instance_id=$1", id)
	return err
}

func (r *channelPerformanceRepository) RecordFacts(ctx context.Context, facts []service.ChannelPerformanceFact) error {
	if len(facts) == 0 {
		return nil
	}
	if len(facts) > 128 {
		return fmt.Errorf("performance batch too large")
	}
	// Validate before opening a transaction. No partial batch can be committed.
	for i := range facts {
		if err := facts[i].Validate(); err != nil {
			return err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range facts {
		latency, err := json.Marshal(f.Latency.Map())
		if err != nil {
			return err
		}
		// Once a logical request succeeded, delayed failure witnesses cannot
		// overwrite it. An eventual retry success replaces an earlier failure.
		_, err = tx.ExecContext(ctx, `
            WITH saved AS (
                INSERT INTO channel_performance_facts AS existing
                    (api_key_id, request_id, group_id, model, service_tier, reasoning_effort,
                     stream, outcome, started_at, completed_at, latency_breakdown, usage_request_id)
                VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,NULLIF($12,''))
                ON CONFLICT (api_key_id, request_id) DO UPDATE SET
                    started_at = LEAST(existing.started_at, EXCLUDED.started_at),
                    completed_at = GREATEST(existing.completed_at, EXCLUDED.completed_at),
                    outcome = CASE WHEN existing.outcome = 'success' THEN existing.outcome ELSE EXCLUDED.outcome END,
                    group_id = CASE WHEN existing.outcome = 'success' THEN existing.group_id ELSE EXCLUDED.group_id END,
                    model = CASE WHEN existing.outcome = 'success' THEN existing.model ELSE EXCLUDED.model END,
                    service_tier = CASE WHEN existing.outcome = 'success' THEN existing.service_tier ELSE EXCLUDED.service_tier END,
                    reasoning_effort = CASE WHEN existing.outcome = 'success' THEN existing.reasoning_effort ELSE EXCLUDED.reasoning_effort END,
                    stream = CASE WHEN existing.outcome = 'success' THEN existing.stream ELSE EXCLUDED.stream END,
                    usage_request_id = CASE WHEN existing.outcome = 'success' THEN existing.usage_request_id ELSE EXCLUDED.usage_request_id END,
                    usage_resolved = CASE WHEN existing.outcome = 'success' THEN existing.usage_resolved ELSE false END,
                    usage_next_check_at = CASE WHEN existing.outcome = 'success' THEN existing.usage_next_check_at ELSE now() END,
                    usage_check_attempts = CASE WHEN existing.outcome = 'success' THEN existing.usage_check_attempts ELSE 0 END,
                    latency_breakdown = CASE WHEN existing.outcome = 'success' THEN existing.latency_breakdown ELSE EXCLUDED.latency_breakdown END
                RETURNING started_at
            )
            INSERT INTO channel_performance_dirty_hours(hour_start,priority)
            SELECT DISTINCT h,1 FROM (
                SELECT date_trunc('hour', started_at) AS h FROM saved
                UNION SELECT date_trunc('hour', $9::timestamptz)
                UNION SELECT date_trunc('hour', created_at) FROM usage_logs
                    WHERE api_key_id=$1 AND request_id=COALESCE(NULLIF($12,''),$2)
            ) hours
            ON CONFLICT (hour_start) DO UPDATE SET revision=channel_performance_dirty_hours.revision+1,priority=1`,
			f.APIKeyID, f.RequestID, f.GroupID, f.Model, f.ServiceTier, f.ReasoningEffort,
			f.Stream, string(f.Outcome), f.StartedAt.UTC(), f.CompletedAt.UTC(), string(latency), f.UsageRequestID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Historical usage alone cannot establish success (free success and paid stream
// failures both exist). Keep its unknown witness, without invented milestones.
const performanceSourceSQL = `
WITH source AS (
    SELECT f.started_at AS at, f.group_id, f.model,
        COALESCE(NULLIF(ul.service_tier,''), f.service_tier) AS service_tier,
        COALESCE(NULLIF(ul.reasoning_effort,''), f.reasoning_effort) AS reasoning_effort,
        CASE WHEN COALESCE(f.stream, ul.stream) THEN 1 WHEN COALESCE(f.stream, ul.stream) = false THEN 0 ELSE -1 END AS stream,
        f.outcome, COALESCE(NULLIF(f.latency_breakdown,'null'::jsonb), ul.latency_breakdown) AS latency,
        ul.output_tokens, COALESCE(ul.image_count,0) = 0 AND
          COALESCE(ul.inbound_endpoint,'') NOT LIKE '%/images/%' AS text_request
    FROM channel_performance_facts f
    LEFT JOIN LATERAL (
        SELECT * FROM usage_logs u WHERE u.api_key_id=f.api_key_id AND u.request_id=COALESCE(NULLIF(f.usage_request_id,''),f.request_id)
          AND u.usage_source IS DISTINCT FROM 'channel_monitor' ORDER BY u.id DESC LIMIT 1
    ) ul ON true
    WHERE f.started_at >= $1 AND f.started_at < $2
    UNION ALL
    SELECT ul.created_at, ul.group_id,
        COALESCE(NULLIF(TRIM(ul.requested_model),''), ul.model),
        COALESCE(NULLIF(ul.service_tier,''),'unknown'), COALESCE(NULLIF(ul.reasoning_effort,''),'unknown'),
        CASE WHEN ul.stream THEN 1 ELSE 0 END, 'unknown', NULL::jsonb, ul.output_tokens, false
    FROM usage_logs ul
    WHERE ul.created_at >= $1 AND ul.created_at < $2 AND ul.group_id IS NOT NULL
      AND ul.usage_source IS DISTINCT FROM 'channel_monitor'
      AND NOT EXISTS (SELECT 1 FROM channel_performance_facts f WHERE f.api_key_id=ul.api_key_id AND COALESCE(NULLIF(f.usage_request_id,''),f.request_id)=ul.request_id)
), measured AS (
    SELECT *,
      CASE WHEN outcome='success' AND latency->>'version'='2' THEN (latency->>'first_character_ms')::bigint END AS first_character,
      CASE WHEN outcome='success' AND latency->>'version'='2' THEN (latency->>'total_duration_ms')::bigint END AS duration,
      CASE WHEN outcome='success' AND latency->>'version'='2' AND stream=1 AND text_request
        THEN (latency->>'total_duration_ms')::bigint - (latency->>'first_output_ms')::bigint END AS output_duration
    FROM source WHERE outcome <> 'excluded'
)
INSERT INTO channel_performance_buckets
    (bucket_seconds,bucket_start,group_id,model,service_tier,reasoning_effort,stream,
     success_count,failure_count,unknown_count,character_count,character_sum,duration_count,duration_sum,output_tokens,output_ms,output_count)
SELECT $3::integer, to_timestamp(floor(extract(epoch FROM at)/$3)*$3), group_id, model, service_tier, reasoning_effort, stream,
    COUNT(*) FILTER (WHERE outcome='success'), COUNT(*) FILTER (WHERE outcome='failure'), COUNT(*) FILTER (WHERE outcome='unknown'),
    COUNT(*) FILTER (WHERE first_character>=0 AND first_character<=duration),
    COALESCE(SUM(first_character) FILTER (WHERE first_character>=0 AND first_character<=duration),0),
    COUNT(*) FILTER (WHERE duration>=0), COALESCE(SUM(duration) FILTER (WHERE duration>=0),0),
    COALESCE(SUM(output_tokens) FILTER (WHERE output_duration>0 AND output_duration<=duration AND output_tokens>0),0),
    COALESCE(SUM(output_duration) FILTER (WHERE output_duration>0 AND output_duration<=duration AND output_tokens>0),0),
    COUNT(*) FILTER (WHERE output_duration>0 AND output_duration<=duration AND output_tokens>0)
FROM measured GROUP BY 2,3,4,5,6,7`

func (r *channelPerformanceRepository) Recompute(ctx context.Context, start, end time.Time) error {
	start = start.UTC().Truncate(time.Hour)
	end = end.UTC()
	if !end.Equal(end.Truncate(time.Hour)) {
		end = end.Truncate(time.Hour).Add(time.Hour)
	}
	if !start.Before(end) {
		return nil
	}
	if end.Sub(start) > 2*time.Hour {
		return fmt.Errorf("performance recompute exceeds two hours")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL statement_timeout = '20s'"); err != nil {
		return err
	}
	// Only a single worker recomputes; the transaction snapshot and revision
	// journal preserve writes which arrive while an older bucket is rebuilding.
	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM channel_performance_dirty_hours WHERE hour_start=$1", start).Scan(&revision)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM channel_performance_buckets WHERE bucket_start >= $1 AND bucket_start < $2", start, end); err != nil {
		return err
	}
	for _, seconds := range []int{60, 3600} {
		if seconds == 60 && end.Before(time.Now().UTC().Add(-48*time.Hour)) {
			continue
		}
		if _, err = tx.ExecContext(ctx, performanceSourceSQL, start, end, seconds); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM channel_performance_dirty_hours WHERE hour_start=$1 AND revision=$2", start, revision); err != nil {
		return err
	}
	// Only extend contiguous coverage. Separate dirty historical hours cannot
	// pretend that all intervening history has already been backfilled.
	_, err = tx.ExecContext(ctx, `UPDATE channel_performance_watermark SET
        coverage_start = CASE WHEN coverage_start IS NULL THEN $1 WHEN $2 >= coverage_start THEN LEAST(coverage_start,$1) ELSE coverage_start END,
        coverage_end = CASE WHEN coverage_end IS NULL THEN LEAST($2,now()) WHEN $1 <= coverage_end THEN GREATEST(coverage_end,LEAST($2,now())) ELSE coverage_end END,
        updated_at=now() WHERE id=1`, start, end)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ProcessPending is bounded independently of the amount of retained history.
// Late usage is journaled with an ID cursor and a bounded rolling history sweep.
// Sequence IDs are not commit order: the sweep also catches lower IDs committed
// after the cursor passed them, without triggers on billing tables.
func (r *channelPerformanceRepository) ProcessPending(ctx context.Context, now time.Time) error {
	if err := r.reconcilePendingUsage(ctx, now); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
        WITH batch AS MATERIALIZED (
            SELECT id, api_key_id, request_id, created_at, usage_source FROM usage_logs
            WHERE id > (SELECT usage_cursor FROM channel_performance_watermark WHERE id=1)
            ORDER BY id LIMIT 2000
        ), dirty AS (
            INSERT INTO channel_performance_dirty_hours(hour_start)
            SELECT DISTINCT h FROM (
                SELECT date_trunc('hour',created_at) h FROM batch WHERE usage_source IS DISTINCT FROM 'channel_monitor'
                UNION SELECT date_trunc('hour',f.started_at) FROM batch b JOIN channel_performance_facts f
                    ON f.api_key_id=b.api_key_id AND COALESCE(NULLIF(f.usage_request_id,''),f.request_id)=b.request_id
            ) hours WHERE h >= $1
            ON CONFLICT(hour_start) DO UPDATE SET revision=channel_performance_dirty_hours.revision+1
        ) UPDATE channel_performance_watermark SET usage_cursor=COALESCE((SELECT MAX(id) FROM batch),usage_cursor) WHERE id=1`, now.Add(-30*24*time.Hour))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Always recompute the current hour first; it includes asynchronous usage.
	if err = r.Recompute(ctx, now.Truncate(time.Hour), now); err != nil {
		return err
	}
	var dirty time.Time
	err = r.db.QueryRowContext(ctx, "SELECT hour_start FROM channel_performance_dirty_hours WHERE hour_start >= $1 AND hour_start < $2 ORDER BY priority DESC,hour_start LIMIT 1", now.Add(-30*24*time.Hour), now.Truncate(time.Hour)).Scan(&dirty)
	if err == nil {
		if err = r.Recompute(ctx, dirty, dirty.Add(time.Hour)); err != nil {
			return err
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	var cursor time.Time
	if err = r.db.QueryRowContext(ctx, "SELECT backfill_cursor FROM channel_performance_watermark WHERE id=1").Scan(&cursor); err != nil {
		return err
	}
	if cursor.After(now.Add(-30 * 24 * time.Hour)) {
		start := cursor.Add(-time.Hour)
		if err = r.Recompute(ctx, start, cursor); err != nil {
			return err
		}
		if _, err = r.db.ExecContext(ctx, "UPDATE channel_performance_watermark SET backfill_cursor=$1 WHERE id=1", start); err != nil {
			return err
		}
	} else {
		// Never permanently stop reconciliation after the initial backfill.
		// One historical hour per tick keeps work bounded and survives restarts.
		if _, err = r.db.ExecContext(ctx, "UPDATE channel_performance_watermark SET backfill_cursor=$1 WHERE id=1", now.Truncate(time.Hour)); err != nil {
			return err
		}
	}
	for _, prune := range []struct {
		query  string
		cutoff time.Time
	}{
		{"DELETE FROM channel_performance_buckets WHERE ctid IN (SELECT ctid FROM channel_performance_buckets WHERE bucket_seconds=60 AND bucket_start < $1 ORDER BY bucket_start LIMIT 2000)", now.Add(-48 * time.Hour)},
		{"DELETE FROM channel_performance_buckets WHERE ctid IN (SELECT ctid FROM channel_performance_buckets WHERE bucket_seconds=3600 AND bucket_start < $1 ORDER BY bucket_start LIMIT 2000)", now.Add(-30 * 24 * time.Hour)},
		{"DELETE FROM channel_performance_facts WHERE ctid IN (SELECT ctid FROM channel_performance_facts WHERE started_at < $1 ORDER BY started_at LIMIT 2000)", now.Add(-30 * 24 * time.Hour)},
		{"DELETE FROM channel_performance_dirty_hours WHERE hour_start IN (SELECT hour_start FROM channel_performance_dirty_hours WHERE hour_start < $1 ORDER BY hour_start LIMIT 2000)", now.Add(-30 * 24 * time.Hour)},
		{"DELETE FROM channel_performance_runtimes WHERE instance_id IN (SELECT instance_id FROM channel_performance_runtimes WHERE closed_at < $1 ORDER BY closed_at LIMIT 2000)", now.Add(-30 * 24 * time.Hour)},
	} {
		if _, err = r.db.ExecContext(ctx, prune.query, prune.cutoff); err != nil {
			return err
		}
	}
	return nil
}

// A success witness may precede the usage transaction's commit, even when a
// later usage ID was already observed. Probe bounded indexed identities, not
// billing-table scans or triggers. Missing/free usage backs off to five minutes.
func (r *channelPerformanceRepository) reconcilePendingUsage(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `WITH pending AS MATERIALIZED (
        SELECT api_key_id,request_id,usage_request_id,started_at,usage_check_attempts
        FROM channel_performance_facts
        WHERE outcome='success' AND NOT usage_resolved
          AND usage_next_check_at <= $1 AND started_at >= $1 - INTERVAL '30 days'
        ORDER BY usage_next_check_at,started_at LIMIT 2000 FOR UPDATE SKIP LOCKED
    ), observed AS MATERIALIZED (
        SELECT p.*,u.created_at FROM pending p LEFT JOIN LATERAL (
            SELECT created_at FROM usage_logs WHERE api_key_id=p.api_key_id
              AND request_id=COALESCE(NULLIF(p.usage_request_id,''),p.request_id)
              AND usage_source IS DISTINCT FROM 'channel_monitor' LIMIT 1
        ) u ON true
    ), updated AS (
        UPDATE channel_performance_facts f
        SET usage_resolved=o.created_at IS NOT NULL,
            usage_next_check_at=$1 + LEAST(300,30*power(2,LEAST(o.usage_check_attempts,4))) * INTERVAL '1 second',
            usage_check_attempts=LEAST(o.usage_check_attempts+1,128)
        FROM observed o WHERE f.api_key_id=o.api_key_id AND f.request_id=o.request_id
        RETURNING f.request_id
    ) INSERT INTO channel_performance_dirty_hours(hour_start,priority)
      SELECT DISTINCT h,1 FROM (
        SELECT date_trunc('hour',started_at) h FROM observed WHERE created_at IS NOT NULL
        UNION SELECT date_trunc('hour',created_at) FROM observed WHERE created_at IS NOT NULL
      ) hours
      ON CONFLICT(hour_start) DO UPDATE SET revision=channel_performance_dirty_hours.revision+1,priority=1`, now.UTC())
	return err
}
