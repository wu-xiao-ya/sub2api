-- Upstream new-api ratio sync (国模 new-api 上游倍率同步).
-- One sync run stores one row per model with the ratios in effect at
-- observed_at, so upstream cost can be recomputed from the timeline later.
-- Ratio semantics follow new-api: USD = group_ratio * model_ratio *
-- (uncached_prompt + cached_prompt * cache_ratio + completion * completion_ratio)
-- / quota_per_unit; per-call models charge model_price * group_ratio.

CREATE TABLE IF NOT EXISTS upstream_model_ratio_snapshots (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    model_name VARCHAR(300) NOT NULL,
    quota_type SMALLINT NOT NULL DEFAULT 0,
    model_ratio NUMERIC(14, 6) NOT NULL DEFAULT 0,
    completion_ratio NUMERIC(14, 6) NOT NULL DEFAULT 0,
    model_price NUMERIC(14, 6) NOT NULL DEFAULT 0,
    cache_ratio NUMERIC(14, 6),
    group_ratio NUMERIC(14, 6) NOT NULL DEFAULT 1,
    observed_at TIMESTAMPTZ NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    raw JSONB
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_upstream_model_ratio_snapshots_identity
    ON upstream_model_ratio_snapshots (account_id, model_name, observed_at);

CREATE INDEX IF NOT EXISTS idx_upstream_model_ratio_snapshots_lookup
    ON upstream_model_ratio_snapshots (account_id, model_name, observed_at DESC);
