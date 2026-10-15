-- Migration: 196_channel_monitor_adaptive_account_probe
-- Explicit account-group binding plus durable model-level sticky probe state.

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS account_group_id BIGINT NULL;

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS fk_channel_monitors_account_group_id;

ALTER TABLE channel_monitors
    ADD CONSTRAINT fk_channel_monitors_account_group_id
    FOREIGN KEY (account_group_id) REFERENCES groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_account_group_id
    ON channel_monitors (account_group_id)
    WHERE account_group_id IS NOT NULL;

ALTER TABLE channel_monitor_histories
    ADD COLUMN IF NOT EXISTS account_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS account_name VARCHAR(160) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS probe_mode VARCHAR(24) NOT NULL DEFAULT 'static',
    ADD COLUMN IF NOT EXISTS candidate_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS healthy_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_channel_monitor_histories_account_checked
    ON channel_monitor_histories (monitor_id, model, account_id, checked_at DESC)
    WHERE account_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS channel_monitor_account_probe_states (
    monitor_id BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    model VARCHAR(200) NOT NULL,
    account_id BIGINT NULL REFERENCES accounts(id) ON DELETE SET NULL,
    account_name VARCHAR(160) NOT NULL DEFAULT '',
    final_status VARCHAR(24) NOT NULL DEFAULT 'error',
    last_latency_ms INTEGER NULL,
    last_probe_mode VARCHAR(24) NOT NULL DEFAULT 'full',
    last_full_sweep_at TIMESTAMPTZ NULL,
    last_checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (monitor_id, model),
    CONSTRAINT channel_monitor_account_probe_states_status_check
        CHECK (final_status IN ('operational', 'degraded', 'failed', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_account_probe_states_account_id
    ON channel_monitor_account_probe_states (account_id)
    WHERE account_id IS NOT NULL;
