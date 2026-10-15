-- Internal cost ledger for real channel-monitor probes.
--
-- Monitor calls are system work, not end-user API-key usage. Keeping them in a
-- dedicated append-only table avoids corrupting user revenue, ranking, and
-- billing aggregates while preserving an auditable daily monitoring cost.

CREATE TABLE IF NOT EXISTS channel_monitor_cost_events (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    account_id BIGINT NULL,
    provider VARCHAR(32) NOT NULL,
    api_mode VARCHAR(32) NOT NULL,
    model VARCHAR(200) NOT NULL,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    image_count INTEGER NOT NULL DEFAULT 0,
    estimated_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    account_cost DECIMAL(20,10) NOT NULL DEFAULT 0,
    cost_source VARCHAR(32) NOT NULL DEFAULT 'unavailable',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_cost_events_created_at
    ON channel_monitor_cost_events(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_cost_events_monitor_created_at
    ON channel_monitor_cost_events(monitor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_cost_events_account_created_at
    ON channel_monitor_cost_events(account_id, created_at DESC)
    WHERE account_id IS NOT NULL;
