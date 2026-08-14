-- Time-bounded group billing promotions. The service rejects overlapping
-- enabled windows for a group; usage snapshots retain the historical detail.

CREATE TABLE IF NOT EXISTS group_promotions (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    mode VARCHAR(32) NOT NULL,
    value DECIMAL(10, 4) NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by BIGINT,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT group_promotions_time_range_check CHECK (starts_at < ends_at),
    CONSTRAINT group_promotions_mode_value_check CHECK (
        (mode = 'discount_factor' AND value >= 0 AND value <= 1)
        OR
        (mode = 'fixed_multiplier' AND value >= 0)
    )
);

CREATE INDEX IF NOT EXISTS idx_group_promotions_group_schedule
    ON group_promotions (group_id, starts_at, ends_at)
    WHERE enabled = TRUE;

CREATE INDEX IF NOT EXISTS idx_group_promotions_schedule
    ON group_promotions (starts_at, ends_at)
    WHERE enabled = TRUE;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS promotion_id BIGINT,
    ADD COLUMN IF NOT EXISTS promotion_name VARCHAR(200),
    ADD COLUMN IF NOT EXISTS base_rate_multiplier DECIMAL(10, 4);

CREATE INDEX IF NOT EXISTS idx_usage_logs_promotion_id_created_at
    ON usage_logs (promotion_id, created_at DESC)
    WHERE promotion_id IS NOT NULL;

ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS promotion_id BIGINT,
    ADD COLUMN IF NOT EXISTS promotion_name VARCHAR(200),
    ADD COLUMN IF NOT EXISTS promotion_base_rate_multiplier DECIMAL(10, 4);

COMMENT ON TABLE group_promotions IS 'Time-bounded billing promotions scoped to one API key group.';
COMMENT ON COLUMN usage_logs.base_rate_multiplier IS 'Effective user rate before an applied promotion.';
COMMENT ON COLUMN batch_image_jobs.promotion_base_rate_multiplier IS 'Effective image group rate before the frozen promotion, excluding batch discount.';
