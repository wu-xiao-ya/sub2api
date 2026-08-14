-- Keep exactly one successful generated image per channel monitor.
ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_api_mode_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'models', 'images'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_api_mode_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_api_mode_check
    CHECK (api_mode IN ('chat_completions', 'responses', 'models', 'images'));

CREATE TABLE IF NOT EXISTS channel_monitor_latest_images (
    monitor_id BIGINT PRIMARY KEY
        REFERENCES channel_monitors(id) ON DELETE CASCADE,
    content_type VARCHAR(100) NOT NULL DEFAULT 'image/png',
    image_data BYTEA NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS channel_monitor_latest_images_generated_at_idx
    ON channel_monitor_latest_images (generated_at DESC);
