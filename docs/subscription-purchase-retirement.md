# Subscription Purchase Retirement

## Scope

The Starlight purchase model is the only runtime subscription entitlement model:

- `subscription_plans` remains the product catalog.
- `subscription_purchases` is the immutable entitlement snapshot.
- `subscription_purchase_groups` is the immutable group authorization snapshot.
- `user_subscriptions` is retained temporarily for historical foreign keys and rollback inspection only.

New requests attribute usage through `usage_logs.subscription_purchase_id`. The legacy `subscription_id` column remains nullable and is not populated by purchase-backed requests.

## Deployment Order

1. Deploy the application build that supports purchase attribution, purchase-only authentication, purchase fulfillment, and purchase-aware refunds.
2. Apply migrations 231 and 232.
3. Verify new payment and redeem flows create an idempotent purchase keyed by their source and source ID.
4. Apply migration 233. It backfills non-deleted native subscriptions into `subscription_purchases`, snapshots their groups and quota usage, and installs a read-only trigger on `user_subscriptions`.
5. Apply migration 234. It maps historical `usage_logs.subscription_id` values to the migrated purchase IDs while preserving the legacy column for audit and rollback.
6. Run the focused backend and frontend checks listed below, then perform a staged smoke test with a single user and a single multi-group plan.

## Compatibility Rules

- A purchase with a positive `concurrency_entitlement` uses its own purchase slot pool.
- A purchase with `concurrency_entitlement = 0` inherits the user's concurrency ceiling. Multiple inherited purchases share one user pool and do not multiply the limit.
- Historical native rows are copied with source `legacy_user_subscription` and source ID equal to the old row ID.
- Historical group-only subscription redeem codes are converted to one-group purchases when redeemed. New subscription redeem codes require a plan ID.
- Full subscription refunds revoke the exact purchase created by the payment order. Failed or pending refunds restore the captured purchase status and expiry. Ambiguous historical attribution must fail closed.
- Native user endpoints (`/subscriptions`, `/subscriptions/active`, `/subscriptions/progress`, and `/subscriptions/summary`) and all native admin assignment/revoke/restore routes are retired. The user-facing purchase endpoints remain `/subscriptions/shared` and `/subscriptions/shared/:id/balance-topup`.
- Native service compatibility methods fail closed with `NATIVE_SUBSCRIPTION_RETIRED`; the legacy expiry/reminder worker is inert during the rollback window.
- Request-time subscription billing requires a positive `subscription_purchase_id`. A legacy `subscription_id` may be retained in historical logs but must never select or update `user_subscriptions`.

## Validation

Backend compile-only check:

```go
cd backend
go test ./internal/service ./internal/server/middleware ./internal/handler ./internal/repository -run '^$'
```

Focused backend tests should cover payment fulfillment/refund, redeem purchase creation, shared authorization, purchase-only billing attribution, retired native routes/services, and migration SQL regression checks. Frontend validation is:

```sh
pnpm run typecheck
pnpm run test:run
```

Do not treat a compile-only pass as sufficient: verify that no production runtime path writes through `UserSubscriptionRepository` after migration 233.

## Rollback Window

During the compatibility window, remove only the read-only trigger if a rollback to the previous application is explicitly authorized:

```sql
BEGIN;
DROP TRIGGER IF EXISTS trg_user_subscriptions_read_only ON user_subscriptions;
DROP FUNCTION IF EXISTS sub2api_reject_native_subscription_write();
COMMIT;
```

The rollback does not delete purchase snapshots. Restore the previous application only after confirming whether its writes should be reconciled back into the legacy table. Keep migrations 233 and 234 applied; never edit or delete an applied migration file.

## Deferred Forward Migration

After the rollback window, once unresolved legacy usage IDs and legacy runtime references are zero, create a later forward migration to remove the old table and its generated code. That migration must first remove or replace the remaining historical foreign keys and archive the old table according to the production backup policy. Do not drop `user_subscriptions` as part of 233 or 234.
