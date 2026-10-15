-- Track late usage independently of usage_logs sequence/commit order.
ALTER TABLE channel_performance_facts
    ADD COLUMN IF NOT EXISTS usage_resolved BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS usage_next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS usage_check_attempts INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS channel_performance_pending_usage_idx
    ON channel_performance_facts (usage_next_check_at, started_at)
    WHERE outcome = 'success' AND NOT usage_resolved;
ALTER TABLE channel_performance_dirty_hours
    ADD COLUMN IF NOT EXISTS priority SMALLINT NOT NULL DEFAULT 0;

-- Retry bookkeeping alone must not invalidate metric buckets.
CREATE OR REPLACE FUNCTION channel_performance_mark_fact_hours() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       ROW(OLD.api_key_id,OLD.request_id,OLD.group_id,OLD.model,OLD.service_tier,
           OLD.reasoning_effort,OLD.stream,OLD.outcome,OLD.started_at,OLD.completed_at,
           OLD.latency_breakdown,OLD.usage_request_id)
       IS NOT DISTINCT FROM
       ROW(NEW.api_key_id,NEW.request_id,NEW.group_id,NEW.model,NEW.service_tier,
           NEW.reasoning_effort,NEW.stream,NEW.outcome,NEW.started_at,NEW.completed_at,
           NEW.latency_breakdown,NEW.usage_request_id) THEN
        RETURN NEW;
    END IF;
    INSERT INTO channel_performance_dirty_hours(hour_start,priority)
    VALUES (date_trunc('hour',NEW.started_at),1)
    ON CONFLICT(hour_start) DO UPDATE
        SET revision=channel_performance_dirty_hours.revision+1,priority=1;
    IF TG_OP = 'UPDATE' AND date_trunc('hour',OLD.started_at) <> date_trunc('hour',NEW.started_at) THEN
        INSERT INTO channel_performance_dirty_hours(hour_start,priority)
        VALUES(date_trunc('hour',OLD.started_at),1)
        ON CONFLICT(hour_start) DO UPDATE
            SET revision=channel_performance_dirty_hours.revision+1,priority=1;
    END IF;
    RETURN NEW;
END;
$$;
