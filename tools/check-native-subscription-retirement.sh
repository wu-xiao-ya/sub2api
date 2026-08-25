#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
migration_root="$repo_root/backend/migrations"

for migration in \
  231_subscription_purchases_and_multi_group_plans.sql \
  232_usage_log_subscription_purchase_attribution.sql \
  233_backfill_and_freeze_native_subscriptions.sql \
  234_backfill_legacy_usage_purchase_attribution.sql; do
  test -f "$migration_root/$migration"
done

if rg -n \
  --glob '*.go' \
  --glob '!**/*_test.go' \
  'UPDATE[[:space:]]+user_subscriptions|INSERT[[:space:]]+INTO[[:space:]]+user_subscriptions|DELETE[[:space:]]+FROM[[:space:]]+user_subscriptions|UserSubscription\.(Create|Update|Delete)' \
  "$repo_root/backend/internal"; then
  echo "retired native subscription write path detected" >&2
  exit 1
fi

if ! rg -n "subscription_purchase_id" "$repo_root/backend/internal/service" "$repo_root/backend/internal/repository" >/dev/null; then
  echo "purchase attribution path is missing" >&2
  exit 1
fi

echo "native subscription retirement static checks passed"
