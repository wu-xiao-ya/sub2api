-- Backfill native user_subscriptions into immutable Starlight purchase snapshots,
-- then freeze the native table during the compatibility/rollback window.
--
-- Rollout order:
--   1. Deploy application code that no longer creates or mutates user_subscriptions.
--   2. Apply migrations 231 and 232, then this migration.
--   3. Verify every eligible legacy row has a legacy_user_subscription purchase and
--      every such purchase has its immutable group snapshot.
--   4. Keep user_subscriptions and usage_logs.subscription_id for historical reads/FKs.
--      Their DROP is intentionally deferred to a later forward migration after the
--      rollback window and legacy-fallback metrics reach zero.
--
-- This regular migration runs in one transaction. The table lock closes the race
-- between the backfill SELECT and trigger installation while continuing to allow reads.
LOCK TABLE user_subscriptions IN SHARE ROW EXCLUSIVE MODE;

-- Broken/missing FK targets are not expected on a healthy database. Soft-deleted
-- groups are NOT missing: their rows remain present and are deliberately snapshotted.
-- If historical corruption left a physically missing user/group row, skip that legacy
-- row rather than aborting the entire rollout, and emit a PostgreSQL warning containing
-- the skipped IDs so operators can repair and rerun this migration SQL manually during
-- the rollback window.
DO $$
DECLARE
    missing_target_count BIGINT;
    missing_target_ids TEXT;
BEGIN
    SELECT COUNT(*), string_agg(us.id::TEXT, ',' ORDER BY us.id)
    INTO missing_target_count, missing_target_ids
    FROM user_subscriptions us
    LEFT JOIN users u ON u.id = us.user_id
    LEFT JOIN groups g ON g.id = us.group_id
    WHERE us.deleted_at IS NULL
      AND (u.id IS NULL OR g.id IS NULL);

    IF missing_target_count > 0 THEN
        RAISE WARNING
            'migration 233 skipped % non-deleted user_subscriptions with physically missing user/group rows; legacy_ids=%',
            missing_target_count,
            missing_target_ids;
    END IF;
END
$$;

WITH eligible_legacy AS (
    SELECT
        us.*,
        g.name AS legacy_group_name,
        g.platform AS legacy_group_platform,
        g.deleted_at AS legacy_group_deleted_at,
        CASE
            WHEN us.expires_at <= NOW()
                 AND LOWER(COALESCE(NULLIF(BTRIM(us.status), ''), 'active')) = 'active'
                THEN 'expired'
            ELSE LOWER(COALESCE(NULLIF(BTRIM(us.status), ''), 'active'))
        END AS normalized_status,
        COALESCE(g.daily_limit_usd, 0)::DECIMAL(20,10) AS purchase_daily_quota_usd,
        COALESCE(g.weekly_limit_usd, 0)::DECIMAL(20,10) AS purchase_weekly_quota_usd,
        COALESCE(g.monthly_limit_usd, 0)::DECIMAL(20,10) AS purchase_monthly_quota_usd
    FROM user_subscriptions us
    JOIN users u ON u.id = us.user_id
    JOIN groups g ON g.id = us.group_id
    WHERE us.deleted_at IS NULL
)
INSERT INTO subscription_purchases (
    user_id,
    plan_id,
    name,
    tier_code,
    price,
    currency,
    starts_at,
    expires_at,
    status,
    concurrency_entitlement,
    lifetime_quota_usd,
    daily_quota_usd,
    weekly_quota_usd,
    monthly_quota_usd,
    lifetime_usage_usd,
    daily_usage_usd,
    weekly_usage_usd,
    monthly_usage_usd,
    daily_window_start,
    weekly_window_start,
    monthly_window_start,
    balance_topup_enabled,
    source,
    source_id,
    snapshot,
    notes,
    created_at,
    updated_at
)
SELECT
    legacy.user_id,
    NULL::BIGINT,
    legacy.legacy_group_name,
    'legacy',
    0::DECIMAL(20,10),
    '',
    legacy.starts_at,
    legacy.expires_at,
    legacy.normalized_status,
    0, -- Starlight application semantics: inherit the user's base concurrency.
    0::DECIMAL(20,10),
    legacy.purchase_daily_quota_usd,
    legacy.purchase_weekly_quota_usd,
    legacy.purchase_monthly_quota_usd,
    0::DECIMAL(20,10), -- Native subscriptions did not track lifetime usage.
    COALESCE(legacy.daily_usage_usd, 0)::DECIMAL(20,10),
    COALESCE(legacy.weekly_usage_usd, 0)::DECIMAL(20,10),
    COALESCE(legacy.monthly_usage_usd, 0)::DECIMAL(20,10),
    legacy.daily_window_start,
    legacy.weekly_window_start,
    legacy.monthly_window_start,
    FALSE,
    'legacy_user_subscription',
    legacy.id,
    jsonb_build_object(
        'migration', 233,
        'legacy_user_subscription_id', legacy.id,
        'legacy_user_id', legacy.user_id,
        'legacy_group_id', legacy.group_id,
        'legacy_group_name', legacy.legacy_group_name,
        'legacy_group_platform', legacy.legacy_group_platform,
        'legacy_group_deleted_at', legacy.legacy_group_deleted_at,
        'legacy_original_status', legacy.status,
        'legacy_normalized_status', legacy.normalized_status,
        'legacy_assigned_by', legacy.assigned_by,
        'legacy_assigned_at', legacy.assigned_at,
        'legacy_notes', legacy.notes,
        'legacy_created_at', legacy.created_at,
        'legacy_updated_at', legacy.updated_at,
        'quota_source', 'groups',
        'concurrency_semantics', 'inherit_user_concurrency'
    ),
    COALESCE(legacy.notes, ''),
    legacy.created_at,
    legacy.updated_at
FROM eligible_legacy legacy
ON CONFLICT (source, source_id) DO NOTHING;

-- Fill the immutable authorization snapshot for both newly inserted purchases and
-- pre-existing idempotency matches. Soft-deleted groups are included because the
-- physical group row still satisfies the FK and accurately preserves history.
INSERT INTO subscription_purchase_groups (
    purchase_id,
    group_id,
    group_name,
    platform,
    created_at
)
SELECT
    purchase.id,
    us.group_id,
    g.name,
    COALESCE(g.platform, ''),
    COALESCE(us.created_at, NOW())
FROM user_subscriptions us
JOIN users u ON u.id = us.user_id
JOIN groups g ON g.id = us.group_id
JOIN subscription_purchases purchase
  ON purchase.source = 'legacy_user_subscription'
 AND purchase.source_id = us.id
WHERE us.deleted_at IS NULL
ON CONFLICT (purchase_id, group_id) DO NOTHING;

-- Surface any idempotency rows that could not receive a group snapshot. This is a
-- warning rather than a migration failure so one corrupt historical FK target does
-- not block conversion of every healthy subscription.
DO $$
DECLARE
    missing_snapshot_count BIGINT;
    missing_snapshot_ids TEXT;
BEGIN
    SELECT COUNT(*), string_agg(us.id::TEXT, ',' ORDER BY us.id)
    INTO missing_snapshot_count, missing_snapshot_ids
    FROM user_subscriptions us
    JOIN subscription_purchases purchase
      ON purchase.source = 'legacy_user_subscription'
     AND purchase.source_id = us.id
    LEFT JOIN subscription_purchase_groups purchase_group
      ON purchase_group.purchase_id = purchase.id
     AND purchase_group.group_id = us.group_id
    WHERE us.deleted_at IS NULL
      AND purchase_group.id IS NULL;

    IF missing_snapshot_count > 0 THEN
        RAISE WARNING
            'migration 233 found % legacy purchases without group snapshots; legacy_ids=%',
            missing_snapshot_count,
            missing_snapshot_ids;
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION sub2api_reject_native_subscription_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = FORMAT(
            'user_subscriptions is read-only after migration 233; rejected %',
            TG_OP
        ),
        HINT = 'Write subscription_purchases instead, or use the documented rollback-window trigger removal SQL.';
END
$$;

DROP TRIGGER IF EXISTS trg_user_subscriptions_read_only
    ON user_subscriptions;

CREATE TRIGGER trg_user_subscriptions_read_only
BEFORE INSERT OR UPDATE OR DELETE ON user_subscriptions
FOR EACH ROW
WHEN (pg_trigger_depth() = 0)
EXECUTE FUNCTION sub2api_reject_native_subscription_write();

-- ROLLBACK WINDOW SQL BLOCK (MANUAL ONLY; comments are intentionally non-executable):
-- Name: remove_user_subscriptions_read_only_guard
-- Use only while the legacy rollback path is explicitly authorized.
--
--     BEGIN;
--     DROP TRIGGER IF EXISTS trg_user_subscriptions_read_only
--         ON user_subscriptions;
--     DROP FUNCTION IF EXISTS sub2api_reject_native_subscription_write();
--     COMMIT;
--
-- Removing the guard does not delete backfilled purchases and does not restore any
-- overwritten data because this migration never updates existing source/source_id rows.
