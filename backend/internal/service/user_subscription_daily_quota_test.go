package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAssignOrExtendSubscriptionFailClosedWithoutRepositoryCalls asserts the
// retired native assign-or-extend endpoint fails closed without repository I/O.
func TestAssignOrExtendSubscriptionFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	renewed, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       200,
		GroupID:      1,
		ValidityDays: 1,
		Notes:        "new",
	})
	require.Nil(t, renewed)
	require.False(t, reused)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// TestCheckAndResetWindowsFailClosedWithoutRepositoryCalls asserts the retired
// window-reset method fails closed without calling the repository.
func TestCheckAndResetWindowsFailClosedWithoutRepositoryCalls(t *testing.T) {
	sub := &UserSubscription{ID: 1, UserID: 10, GroupID: 20}
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	err := svc.CheckAndResetWindows(context.Background(), sub)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// TestValidateAndCheckLimitsFailClosedWithoutRepositoryCalls asserts the retired
// limits check fails closed without mutating the subscription or the repository.
func TestValidateAndCheckLimitsFailClosedWithoutRepositoryCalls(t *testing.T) {
	sub := &UserSubscription{ID: 1, UserID: 10, GroupID: 20}
	group := &Group{SubscriptionType: SubscriptionTypeSubscription}
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	needsMaintenance, err := svc.ValidateAndCheckLimits(sub, group)
	require.False(t, needsMaintenance)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// --- pure helper tests kept (helpers HasOneTimeDailyQuota / NeedsDailyResetAt / DailyResetTime remain harmless) ---

func TestUserSubscriptionNeedsDailyReset_DailyCardKeepsOneTimeQuota(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.Add(24 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
		DailyUsageUSD:    10,
	}

	require.True(t, sub.HasOneTimeDailyQuota())
	require.False(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(25*time.Hour)), "日卡应作为一次性配额，跨 0 点后不再刷新日额度")
}

func TestUserSubscriptionNeedsDailyReset_MultiDaySubscriptionStillRefreshes(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.AddDate(0, 0, 2),
		DailyWindowStart: &dailyWindowStart,
	}

	require.False(t, sub.HasOneTimeDailyQuota())
	require.True(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(24*time.Hour)), "多日订阅仍应按 24 小时日窗口刷新")
}

func TestUserSubscriptionDailyResetTime_DailyCardReturnsExpiry(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	expiresAt := start.Add(24 * time.Hour)
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        expiresAt,
		DailyWindowStart: &dailyWindowStart,
	}

	resetAt := sub.DailyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, expiresAt, *resetAt, "日卡展示的日额度结束时间应为订阅过期时间")
}
