-- Archive old usage_logs rows into compact hourly buckets before deleting details.
-- This keeps long-term accounting available without retaining every request row forever.

CREATE TABLE IF NOT EXISTS usage_log_hourly_archives (
    id BIGSERIAL PRIMARY KEY,
    bucket_start TIMESTAMPTZ NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    channel_id BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(100) NOT NULL DEFAULT '',
    requested_model VARCHAR(100) NOT NULL DEFAULT '',
    upstream_model VARCHAR(100) NOT NULL DEFAULT '',
    billing_tier VARCHAR(50) NOT NULL DEFAULT '',
    billing_mode VARCHAR(20) NOT NULL DEFAULT '',
    request_type SMALLINT NOT NULL DEFAULT 0,
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    billing_type SMALLINT NOT NULL DEFAULT 0,

    request_count BIGINT NOT NULL DEFAULT 0,
    success_count BIGINT NOT NULL DEFAULT 0,
    zero_cost_count BIGINT NOT NULL DEFAULT 0,

    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_5m_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_1h_tokens BIGINT NOT NULL DEFAULT 0,

    image_count BIGINT NOT NULL DEFAULT 0,
    image_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_output_tokens BIGINT NOT NULL DEFAULT 0,
    video_count BIGINT NOT NULL DEFAULT 0,
    video_duration_seconds BIGINT NOT NULL DEFAULT 0,

    input_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    output_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    cache_creation_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    cache_read_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    image_input_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    image_output_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    total_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    account_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,

    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    total_first_token_ms BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_log_hourly_archives_unique_bucket
    ON usage_log_hourly_archives (
        bucket_start,
        user_id,
        api_key_id,
        account_id,
        group_id,
        channel_id,
        model,
        requested_model,
        upstream_model,
        billing_tier,
        billing_mode,
        request_type,
        stream,
        billing_type
    );

CREATE INDEX IF NOT EXISTS idx_usage_log_hourly_archives_bucket_start
    ON usage_log_hourly_archives (bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_usage_log_hourly_archives_user_bucket
    ON usage_log_hourly_archives (user_id, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_usage_log_hourly_archives_group_bucket
    ON usage_log_hourly_archives (group_id, bucket_start DESC);

COMMENT ON TABLE usage_log_hourly_archives IS 'Hourly compact archive of old usage_logs rows; detail rows may be deleted after this table is populated.';
COMMENT ON COLUMN usage_log_hourly_archives.bucket_start IS 'UTC start timestamp of the archived hour bucket.';
COMMENT ON COLUMN usage_log_hourly_archives.group_id IS '0 represents NULL group_id from usage_logs.';
COMMENT ON COLUMN usage_log_hourly_archives.channel_id IS '0 represents NULL channel_id from usage_logs.';
