-- Per-purchase billing source preference.
-- The default preserves the existing subscription-first behavior.
ALTER TABLE subscription_purchases
    ADD COLUMN IF NOT EXISTS billing_priority VARCHAR(20) NOT NULL DEFAULT 'subscription';

UPDATE subscription_purchases
SET billing_priority = 'subscription'
WHERE billing_priority IS NULL
   OR billing_priority NOT IN ('subscription', 'balance');

ALTER TABLE subscription_purchases
    DROP CONSTRAINT IF EXISTS subscription_purchases_billing_priority_check;

ALTER TABLE subscription_purchases
    ADD CONSTRAINT subscription_purchases_billing_priority_check
    CHECK (billing_priority IN ('subscription', 'balance'));
