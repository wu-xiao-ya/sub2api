-- Independent passive performance data; never changes billing or V2 monitor keys.
CREATE TABLE IF NOT EXISTS channel_performance_facts (
    api_key_id BIGINT NOT NULL,
    request_id TEXT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    service_tier TEXT NOT NULL DEFAULT 'unknown',
    reasoning_effort TEXT NOT NULL DEFAULT 'unknown',
    stream BOOLEAN,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'failure', 'excluded', 'unknown')),
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    latency_breakdown JSONB,
    PRIMARY KEY (api_key_id, request_id)
);
CREATE INDEX IF NOT EXISTS channel_performance_facts_started_idx
    ON channel_performance_facts (started_at, group_id);

CREATE TABLE IF NOT EXISTS channel_performance_buckets (
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (60, 3600)),
    bucket_start TIMESTAMPTZ NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    service_tier TEXT NOT NULL,
    reasoning_effort TEXT NOT NULL,
    stream SMALLINT NOT NULL CHECK (stream IN (-1, 0, 1)),
    success_count BIGINT NOT NULL DEFAULT 0,
    failure_count BIGINT NOT NULL DEFAULT 0,
    unknown_count BIGINT NOT NULL DEFAULT 0,
    character_count BIGINT NOT NULL DEFAULT 0,
    character_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    duration_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    output_count BIGINT NOT NULL DEFAULT 0,
    output_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_seconds, bucket_start, group_id, model, service_tier, reasoning_effort, stream)
);
CREATE INDEX IF NOT EXISTS channel_performance_buckets_scope_idx
    ON channel_performance_buckets (group_id, bucket_seconds, bucket_start);

CREATE TABLE IF NOT EXISTS channel_performance_dirty_hours (
    hour_start TIMESTAMPTZ PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 1
);

-- Journal both locations atomically when an earlier terminal witness moves a
-- logical request. This trigger belongs only to telemetry, never usage_logs.
CREATE OR REPLACE FUNCTION channel_performance_mark_fact_hours() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO channel_performance_dirty_hours(hour_start)
    VALUES (date_trunc('hour', NEW.started_at))
    ON CONFLICT(hour_start) DO UPDATE
        SET revision=channel_performance_dirty_hours.revision+1;
    IF TG_OP = 'UPDATE' AND date_trunc('hour', OLD.started_at) <> date_trunc('hour', NEW.started_at) THEN
        INSERT INTO channel_performance_dirty_hours(hour_start)
        VALUES (date_trunc('hour', OLD.started_at))
        ON CONFLICT(hour_start) DO UPDATE
            SET revision=channel_performance_dirty_hours.revision+1;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS channel_performance_fact_hours ON channel_performance_facts;
CREATE TRIGGER channel_performance_fact_hours
    AFTER INSERT OR UPDATE ON channel_performance_facts
    FOR EACH ROW EXECUTE FUNCTION channel_performance_mark_fact_hours();
CREATE TABLE IF NOT EXISTS channel_performance_watermark (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    coverage_start TIMESTAMPTZ,
    coverage_end TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
	 incomplete_since TIMESTAMPTZ,
    backfill_cursor TIMESTAMPTZ NOT NULL DEFAULT date_trunc('hour', now()),
    usage_cursor BIGINT NOT NULL DEFAULT 0
);
INSERT INTO channel_performance_watermark (id) VALUES (1) ON CONFLICT DO NOTHING;

-- No triggers on billing tables. The background worker journals late usage by ID.
