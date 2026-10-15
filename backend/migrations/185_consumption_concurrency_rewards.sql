-- Lifetime billing rewards:
--   >= $50  -> +4 concurrency
--   >= $100 -> +8 concurrency
--   >= $200 -> +12 concurrency
--   >= $500 -> +16 concurrency
--
-- The existing users.concurrency value remains the effective limit. The
-- reward columns make the incremental grants durable and idempotent without
-- overwriting administrator or redeem-code adjustments.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS lifetime_consumption_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS consumption_concurrency_tier SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS consumption_concurrency_bonus INTEGER NOT NULL DEFAULT 0;

WITH historical AS (
    SELECT
        u.id,
        GREATEST(COALESCE(SUM(ul.actual_cost), 0), 0)::DECIMAL(20, 10) AS lifetime_usd
    FROM users u
    LEFT JOIN usage_logs ul
        ON ul.user_id = u.id
       AND ul.actual_cost > 0
    WHERE u.deleted_at IS NULL
    GROUP BY u.id
),
tiers AS (
    SELECT
        id,
        lifetime_usd,
        CASE
            WHEN lifetime_usd >= 500 THEN 4
            WHEN lifetime_usd >= 200 THEN 3
            WHEN lifetime_usd >= 100 THEN 2
            WHEN lifetime_usd >= 50 THEN 1
            ELSE 0
        END AS tier
    FROM historical
)
UPDATE users u
SET
    lifetime_consumption_usd = t.lifetime_usd,
    consumption_concurrency_tier = t.tier,
    consumption_concurrency_bonus = t.tier * 4,
    concurrency = u.concurrency + (t.tier * 4),
    updated_at = NOW()
FROM tiers t
WHERE u.id = t.id;

INSERT INTO settings (key, value)
VALUES
    ('default_concurrency', '10'),
    ('auth_source_default_email_concurrency', '10'),
    ('auth_source_default_linuxdo_concurrency', '10'),
    ('auth_source_default_oidc_concurrency', '10'),
    ('auth_source_default_wechat_concurrency', '10'),
    ('auth_source_default_github_concurrency', '10'),
    ('auth_source_default_google_concurrency', '10'),
    ('auth_source_default_dingtalk_concurrency', '10')
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = NOW();
