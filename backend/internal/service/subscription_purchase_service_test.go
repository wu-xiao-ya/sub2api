package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestSharedSubscriptionAsLegacySubscriptionUsesExplicitPurchaseID(t *testing.T) {
	shared := &SharedSubscriptionEntitlement{ID: 101, UserID: 42}

	legacy := shared.AsLegacySubscription(nil)

	require.NotNil(t, legacy.SubscriptionPurchaseID)
	require.Equal(t, int64(101), *legacy.SubscriptionPurchaseID)
	require.Zero(t, legacy.ID)
	require.Nil(t, optionalSubscriptionID(legacy))
	require.Equal(t, int64(101), *optionalSubscriptionPurchaseID(legacy))
}

func TestListActiveSharedGroupIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT DISTINCT g\.group_id`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}).
			AddRow(int64(3)).
			AddRow(int64(9)))

	svc := &SubscriptionService{sqlDB: db}
	groupIDs, err := svc.ListActiveSharedGroupIDs(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 9}, groupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListActiveSharedGroupIDsWithoutDatabaseIsEmpty(t *testing.T) {
	svc := &SubscriptionService{}

	groupIDs, err := svc.ListActiveSharedGroupIDs(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, groupIDs)
}

func TestListActiveSharedSubscriptionsForGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	startsAt := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	expiresAt := startsAt.Add(7 * 24 * time.Hour)
	mock.ExpectExec(`UPDATE subscription_purchases`).
		WithArgs(int64(42), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT p\.id, p\.user_id, p\.name, p\.tier_code`).
		WithArgs(int64(42), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "name", "tier_code", "starts_at", "expires_at", "status",
			"concurrency_entitlement", "lifetime_quota_usd", "daily_quota_usd",
			"weekly_quota_usd", "monthly_quota_usd", "lifetime_usage_usd",
			"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd", "balance_topup_enabled", "billing_priority",
		}).AddRow(
			int64(101), int64(42), "Weekly", "standard", startsAt, expiresAt, "active",
			5, 36.0, 8.0, 20.0, 36.0, 1.2, 0.4, 0.8, 1.2, false, "subscription",
		))

	svc := &SubscriptionService{sqlDB: db}
	items, err := svc.ListActiveSharedSubscriptionsForGroup(context.Background(), 42, 9)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(101), items[0].ID)
	require.Equal(t, 5, items[0].ConcurrencyEntitlement)
	require.False(t, items[0].BalanceTopupEnabled)
	require.Equal(t, "subscription", items[0].BillingPriority)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshSharedPurchaseDailyWindowsUsesConfiguredTimezone(t *testing.T) {
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { require.NoError(t, timezone.Init("UTC")) })

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`UPDATE subscription_purchases`).
		WithArgs(int64(42), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, refreshSharedPurchaseQuotaWindows(context.Background(), db, 42))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserSubscriptionBalanceTopupPreferenceDefaultsClosedAndUpserts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT balance_topup_enabled\s+FROM user_subscription_preferences`).
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	svc := &SubscriptionService{sqlDB: db}
	enabled, err := svc.GetUserSubscriptionBalanceTopupPreference(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, enabled)

	mock.ExpectExec(`INSERT INTO user_subscription_preferences`).
		WithArgs(int64(42), true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, svc.SetUserSubscriptionBalanceTopupPreference(context.Background(), 42, true))

	mock.ExpectQuery(`SELECT balance_topup_enabled\s+FROM user_subscription_preferences`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance_topup_enabled"}).AddRow(true))
	enabled, err = svc.GetUserSubscriptionBalanceTopupPreference(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, mock.ExpectationsWereMet())
}
