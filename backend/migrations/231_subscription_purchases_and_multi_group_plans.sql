-- First-generation shared-quota subscription purchases.
-- Existing subscription_plans/user_subscriptions remain backward compatible.

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_redeem_codes_plan_id ON redeem_codes(plan_id);

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS tier_code VARCHAR(20) NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS concurrency_entitlement INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS lifetime_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS weekly_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS monthly_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS subscription_plan_groups (
    plan_id BIGINT NOT NULL REFERENCES subscription_plans(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_id, group_id)
);
CREATE INDEX IF NOT EXISTS idx_subscription_plan_groups_group_id
    ON subscription_plan_groups(group_id);

CREATE TABLE IF NOT EXISTS subscription_purchases (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL,
    name VARCHAR(100) NOT NULL DEFAULT '',
    tier_code VARCHAR(20) NOT NULL DEFAULT 'standard',
    price DECIMAL(20,10) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT '',
    starts_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    concurrency_entitlement INT NOT NULL DEFAULT 0,
    lifetime_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    daily_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    weekly_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    monthly_quota_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    lifetime_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    daily_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    weekly_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    monthly_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    daily_window_start TIMESTAMPTZ,
    weekly_window_start TIMESTAMPTZ,
    monthly_window_start TIMESTAMPTZ,
    balance_topup_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    source VARCHAR(30) NOT NULL DEFAULT 'payment',
    source_id BIGINT,
    snapshot JSONB,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_subscription_purchases_user_status_expires
    ON subscription_purchases(user_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_subscription_purchases_plan_id
    ON subscription_purchases(plan_id);
CREATE INDEX IF NOT EXISTS idx_subscription_purchases_source
    ON subscription_purchases(source, source_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_subscription_purchases_source_id
    ON subscription_purchases(source, source_id);

CREATE TABLE IF NOT EXISTS subscription_purchase_groups (
    id BIGSERIAL PRIMARY KEY,
    purchase_id BIGINT NOT NULL REFERENCES subscription_purchases(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    group_name VARCHAR(100) NOT NULL DEFAULT '',
    platform VARCHAR(50) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (purchase_id, group_id)
);
CREATE INDEX IF NOT EXISTS idx_subscription_purchase_groups_group_id
    ON subscription_purchase_groups(group_id);
