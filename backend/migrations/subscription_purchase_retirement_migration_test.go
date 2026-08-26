package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readRetirementMigration(t *testing.T, name string) string {
	t.Helper()
	data, err := FS.ReadFile(name)
	require.NoError(t, err)
	return strings.ToLower(string(data))
}

func TestSubscriptionPurchaseAttributionMigration(t *testing.T) {
	sql := readRetirementMigration(t, "232_usage_log_subscription_purchase_attribution.sql")
	require.Contains(t, sql, "add column if not exists subscription_purchase_id")
	require.Contains(t, sql, "references subscription_purchases(id) on delete set null")
	require.Contains(t, sql, "idx_usage_logs_purchase_created")
}

func TestNativeSubscriptionBackfillAndFreezeMigration(t *testing.T) {
	sql := readRetirementMigration(t, "233_backfill_and_freeze_native_subscriptions.sql")
	require.Contains(t, sql, "source = 'legacy_user_subscription'")
	require.Contains(t, sql, "source_id")
	require.Contains(t, sql, "insert into subscription_purchase_groups")
	require.Contains(t, sql, "lock table user_subscriptions in share row exclusive mode")
	require.Contains(t, sql, "trg_user_subscriptions_read_only")
	require.Contains(t, sql, "when (pg_trigger_depth() = 0)")
	require.Contains(t, sql, "drop trigger if exists trg_user_subscriptions_read_only")
	require.NotContains(t, sql, "drop table user_subscriptions")
}

func TestLegacyUsageAttributionBackfillMigration(t *testing.T) {
	sql := readRetirementMigration(t, "234_backfill_legacy_usage_purchase_attribution.sql")
	require.Contains(t, sql, "set subscription_purchase_id = purchase.id")
	require.Contains(t, sql, "purchase.source = 'legacy_user_subscription'")
	require.Contains(t, sql, "usage_log.subscription_id = legacy_subscription.id")
	require.Contains(t, sql, "usage_log.subscription_purchase_id is null")
	require.NotContains(t, sql, "set subscription_id = null")
}

func TestSubscriptionPurchaseRetirementMigrationChain(t *testing.T) {
	names := []string{
		"231_subscription_purchases_and_multi_group_plans.sql",
		"232_usage_log_subscription_purchase_attribution.sql",
		"233_backfill_and_freeze_native_subscriptions.sql",
		"234_backfill_legacy_usage_purchase_attribution.sql",
		"235_align_subscription_plan_groups_ent_schema.sql",
		"236_subscription_billing_priority.sql",
	}
	contents := make([]string, 0, len(names))
	for _, name := range names {
		contents = append(contents, readRetirementMigration(t, name))
	}

	for i := 1; i < len(names); i++ {
		require.Less(t, names[i-1], names[i], "retirement migrations must run in numeric order")
	}
	require.Contains(t, contents[0], "create table if not exists subscription_purchases")
	require.Contains(t, contents[0], "create table if not exists subscription_purchase_groups")
	require.Contains(t, contents[1], "add column if not exists subscription_purchase_id")
	require.Contains(t, contents[2], "on conflict (source, source_id) do nothing")
	require.Contains(t, contents[2], "drop trigger if exists trg_user_subscriptions_read_only")
	require.Contains(t, contents[3], "where usage_log.subscription_purchase_id is null")

	// 233 and 234 are safe to retry: the purchase backfill is protected by its
	// source key, group snapshots by their composite key, and usage attribution
	// only updates rows that are still missing a purchase ID.
	require.Contains(t, contents[2], "on conflict (purchase_id, group_id) do nothing")
	require.NotContains(t, contents[2], "drop table user_subscriptions")
	require.NotContains(t, contents[3], "delete from user_subscriptions")
	require.NotContains(t, contents[3], "update user_subscriptions")
	require.Contains(t, contents[4], "add column if not exists id bigint")
	require.Contains(t, contents[4], "create unique index if not exists idx_subscription_plan_groups_id")
	require.NotContains(t, contents[4], "drop table subscription_plan_groups")
	require.Contains(t, contents[5], "add column if not exists billing_priority")
	require.Contains(t, contents[5], "default 'subscription'")
	require.Contains(t, contents[5], "where billing_priority is null")
	require.Contains(t, contents[5], "drop constraint if exists subscription_purchases_billing_priority_check")
	require.Contains(t, contents[5], "check (billing_priority in ('subscription', 'balance'))")
}

func TestSubscriptionPlanGroupsEntAlignmentMigration(t *testing.T) {
	sql := readRetirementMigration(t, "235_align_subscription_plan_groups_ent_schema.sql")
	require.Contains(t, sql, "create sequence if not exists subscription_plan_groups_id_seq")
	require.Contains(t, sql, "alter table subscription_plan_groups")
	require.Contains(t, sql, "set default nextval")
	require.Contains(t, sql, "where id is null")
	require.Contains(t, sql, "create unique index if not exists idx_subscription_plan_groups_id")
	require.NotContains(t, sql, "drop table")
}

func TestSubscriptionBillingPriorityMigration(t *testing.T) {
	sql := readRetirementMigration(t, "236_subscription_billing_priority.sql")
	require.Contains(t, sql, "add column if not exists billing_priority")
	require.Contains(t, sql, "default 'subscription'")
	require.Contains(t, sql, "where billing_priority is null")
	require.Contains(t, sql, "drop constraint if exists subscription_purchases_billing_priority_check")
	require.Contains(t, sql, "check (billing_priority in ('subscription', 'balance'))")
}
