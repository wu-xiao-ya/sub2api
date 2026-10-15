-- Station-owned API keys for in-process channel monitoring.
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS source_mode VARCHAR(24) NOT NULL DEFAULT 'direct_upstream',
    ADD COLUMN IF NOT EXISTS internal_api_key_id BIGINT NULL REFERENCES api_keys(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS internal_group_id BIGINT NULL REFERENCES groups(id) ON DELETE SET NULL;

UPDATE channel_monitors
SET source_mode = 'direct_upstream'
WHERE source_mode IS NULL OR source_mode = '';

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_source_mode_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_source_mode_check
    CHECK (source_mode IN ('direct_upstream', 'internal_gateway'));

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_monitoring_user BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_internal_key
    ON channel_monitors (source_mode, internal_api_key_id)
    WHERE internal_api_key_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_monitoring_user
    ON users (is_monitoring_user)
    WHERE is_monitoring_user = TRUE AND deleted_at IS NULL;
