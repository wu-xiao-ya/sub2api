-- Add first-class Starlight subscription purchase attribution to usage logs.
-- Legacy subscription_id remains nullable for historical user_subscriptions rows.

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS subscription_purchase_id BIGINT
        REFERENCES subscription_purchases(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_usage_logs_subscription_purchase_id
    ON usage_logs(subscription_purchase_id);

CREATE INDEX IF NOT EXISTS idx_usage_logs_purchase_created
    ON usage_logs(subscription_purchase_id, created_at);
