-- Backfill historical usage-log attribution after migration 232/233.
-- New requests already write subscription_purchase_id directly. This migration
-- preserves subscription_id for the rollback window while making the purchase
-- snapshot the canonical historical attribution.
--
-- The update is idempotent and does not write user_subscriptions, so it is safe
-- to run after the migration 233 read-only trigger has been installed.
UPDATE usage_logs AS usage_log
SET subscription_purchase_id = purchase.id
FROM user_subscriptions AS legacy_subscription
JOIN subscription_purchases AS purchase
  ON purchase.source = 'legacy_user_subscription'
 AND purchase.source_id = legacy_subscription.id
WHERE usage_log.subscription_purchase_id IS NULL
  AND usage_log.subscription_id = legacy_subscription.id
  AND usage_log.subscription_id > 0;

-- Keep unresolved legacy IDs visible during the rollback window. They are not
-- cleared because subscription_id remains the historical foreign-key record.
DO $$
DECLARE
    unresolved_count BIGINT;
BEGIN
    SELECT COUNT(*)
      INTO unresolved_count
      FROM usage_logs AS usage_log
     WHERE usage_log.subscription_purchase_id IS NULL
       AND usage_log.subscription_id IS NOT NULL
       AND usage_log.subscription_id > 0;

    IF unresolved_count > 0 THEN
        RAISE WARNING
            'migration 234 found % usage_logs with unresolved legacy subscription_id values',
            unresolved_count;
    END IF;
END
$$;
