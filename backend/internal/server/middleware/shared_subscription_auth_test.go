package middleware

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedSubscriptionConcurrencyPlanUsesGlobalBalanceTopupPreference(t *testing.T) {
	tests := []struct {
		name            string
		globalEnabled   bool
		purchaseEnabled bool
		wantAllowTopup  bool
		wantEntitlement bool
	}{
		{name: "closed globally even when legacy purchase flag is true", globalEnabled: false, purchaseEnabled: true, wantAllowTopup: false, wantEntitlement: false},
		{name: "enabled globally", globalEnabled: true, purchaseEnabled: false, wantAllowTopup: true, wantEntitlement: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			startsAt := time.Now().Add(-time.Hour)
			expiresAt := time.Now().Add(time.Hour)
			mock.ExpectExec(`UPDATE subscription_purchases`).
				WithArgs(int64(42), sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(`SELECT p\.id, p\.user_id, p\.name, p\.tier_code`).
				WithArgs(int64(42), int64(9)).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "user_id", "name", "tier_code", "starts_at", "expires_at", "status",
					"concurrency_entitlement", "lifetime_quota_usd", "daily_quota_usd",
					"weekly_quota_usd", "monthly_quota_usd", "lifetime_usage_usd",
					"daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd",
					"balance_topup_enabled", "billing_priority",
				}).AddRow(
					int64(101), int64(42), "Weekly", "standard", startsAt, expiresAt, "active",
					5, 100.0, 10.0, 20.0, 40.0, 1.0, 1.0, 1.0, 1.0,
					tt.purchaseEnabled, "subscription",
				))
			mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT balance_topup_enabled
		FROM user_subscription_preferences
		WHERE user_id = $1
	`)).WithArgs(int64(42)).
				WillReturnRows(sqlmock.NewRows([]string{"balance_topup_enabled"}).AddRow(tt.globalEnabled))

			svc := service.NewSubscriptionService(nil, nil, nil, nil, nil, db)
			t.Cleanup(svc.Stop)
			plan, err := sharedSubscriptionConcurrencyPlan(context.Background(), svc, 42, 9, 10)
			require.NoError(t, err)
			require.Equal(t, tt.wantAllowTopup, plan.AllowBalanceTopup)
			require.Len(t, plan.Entitlements, 1)
			require.Equal(t, tt.wantEntitlement, plan.Entitlements[0].BalanceTopupEnabled)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSharedSubscriptionBalanceTopupPreferenceReadWithoutDatabaseFailsClosed(t *testing.T) {
	svc := &service.SubscriptionService{}
	enabled, err := svc.GetUserSubscriptionBalanceTopupPreference(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, enabled)
	require.ErrorIs(t, svc.SetUserSubscriptionBalanceTopupPreference(context.Background(), 42, true), service.ErrSharedSubscriptionNotFound)
}
