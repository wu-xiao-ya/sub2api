-- Durable liveness only; no request bodies, user identities or billing writes.
CREATE TABLE IF NOT EXISTS channel_performance_runtimes (
    instance_id TEXT PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS channel_performance_runtimes_open_idx
    ON channel_performance_runtimes(last_seen_at) WHERE closed_at IS NULL;
