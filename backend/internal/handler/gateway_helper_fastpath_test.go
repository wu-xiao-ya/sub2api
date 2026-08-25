package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type concurrencyCacheMock struct {
	acquireUserSlotFn         func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error)
	acquireAccountSlotFn      func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error)
	acquireSubscriptionFn     func(ctx context.Context, purchaseID int64, maxConcurrency int, requestID string) (bool, error)
	acquireIngressLeaseFn     func(ctx context.Context, apiKeyID int64, maxConnections int, leaseID string) (bool, error)
	releaseIngressLeaseFn     func(ctx context.Context, apiKeyID int64, leaseID string) error
	releaseUserCalled         int32
	releaseAccountCalled      int32
	releaseSubscriptionCalled int32
	releaseIngressCalled      int32
}

func (m *concurrencyCacheMock) AcquireSubscriptionSlot(ctx context.Context, purchaseID int64, maxConcurrency int, requestID string) (bool, error) {
	if m.acquireSubscriptionFn != nil {
		return m.acquireSubscriptionFn(ctx, purchaseID, maxConcurrency, requestID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) ReleaseSubscriptionSlot(context.Context, int64, string) error {
	atomic.AddInt32(&m.releaseSubscriptionCalled, 1)
	return nil
}

func (m *concurrencyCacheMock) GetSubscriptionConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	if m.acquireAccountSlotFn != nil {
		return m.acquireAccountSlotFn(ctx, accountID, maxConcurrency, requestID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	atomic.AddInt32(&m.releaseAccountCalled, 1)
	return nil
}

func (m *concurrencyCacheMock) GetAccountConcurrency(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = 0
	}
	return result, nil
}

func (m *concurrencyCacheMock) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) DecrementAccountWaitCount(ctx context.Context, accountID int64) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	if m.acquireUserSlotFn != nil {
		return m.acquireUserSlotFn(ctx, userID, maxConcurrency, requestID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	atomic.AddInt32(&m.releaseUserCalled, 1)
	return nil
}

func (m *concurrencyCacheMock) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) DecrementWaitCount(ctx context.Context, userID int64) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountsLoadBatch(ctx context.Context, accounts []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	return map[int64]*service.AccountLoadInfo{}, nil
}

func (m *concurrencyCacheMock) GetUsersLoadBatch(ctx context.Context, users []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	return map[int64]*service.UserLoadInfo{}, nil
}

func (m *concurrencyCacheMock) CleanupExpiredAccountSlots(ctx context.Context, accountID int64) error {
	return nil
}

func (m *concurrencyCacheMock) CleanupExpiredAccountSlotKeys(ctx context.Context) error {
	return nil
}

func (m *concurrencyCacheMock) CleanupStaleProcessSlots(ctx context.Context, activeRequestPrefix string) error {
	return nil
}

func (m *concurrencyCacheMock) AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int, leaseID string) (bool, error) {
	if m.acquireIngressLeaseFn != nil {
		return m.acquireIngressLeaseFn(ctx, apiKeyID, maxConnections, leaseID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) RefreshOpenAIWSIngressLease(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) ReleaseOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, leaseID string) error {
	atomic.AddInt32(&m.releaseIngressCalled, 1)
	if m.releaseIngressLeaseFn != nil {
		return m.releaseIngressLeaseFn(ctx, apiKeyID, leaseID)
	}
	return nil
}

func TestConcurrencyHelper_TryAcquireUserSlot(t *testing.T) {
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)

	release, acquired, err := helper.TryAcquireUserSlot(context.Background(), 101, 2)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)

	release()
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseUserCalled))
}

func TestConcurrencyHelper_TryAcquireAccountSlot_NotAcquired(t *testing.T) {
	cache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return false, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)

	release, acquired, err := helper.TryAcquireAccountSlot(context.Background(), 201, 1)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Nil(t, release)
	require.Equal(t, int32(0), atomic.LoadInt32(&cache.releaseAccountCalled))
}

func TestConcurrencyHelper_PlannedSubscriptionSlotUpdatesBillingSource(t *testing.T) {
	cache := &concurrencyCacheMock{
		acquireSubscriptionFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			t.Fatal("subscription request must not acquire a balance user slot")
			return false, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)
	shared := &service.SharedSubscriptionEntitlement{ID: 22, UserID: 7}
	legacy := shared.AsLegacySubscription(nil)
	plan := &service.SubscriptionConcurrencyPlan{
		UserID:                7,
		UserLimit:             10,
		HasSharedSubscription: true,
		Entitlements: []service.SubscriptionConcurrencyEntitlement{
			{PurchaseID: 22, Concurrency: 5, Subscription: legacy, SharedSubscription: shared},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request = c.Request.WithContext(service.WithSubscriptionConcurrencyPlan(c.Request.Context(), plan))

	release, acquired, err := helper.TryAcquireUserSlotForAPIKeyFromGin(c, 7, 10, 99)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)
	got, ok := middleware2.GetSubscriptionFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(-22), got.ID)
	source, ok := service.SubscriptionConcurrencySourceFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, int64(22), source.PurchaseID)
	require.False(t, source.UseBalance)

	release()
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseSubscriptionCalled))
}

func TestConcurrencyHelper_PlannedBalanceTopupClearsSubscriptionBilling(t *testing.T) {
	cache := &concurrencyCacheMock{
		acquireSubscriptionFn: func(context.Context, int64, int, string) (bool, error) {
			return false, nil
		},
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)
	shared := &service.SharedSubscriptionEntitlement{ID: 22, UserID: 7}
	plan := &service.SubscriptionConcurrencyPlan{
		UserID:                 7,
		UserLimit:              10,
		HasSharedSubscription:  true,
		AllowBalanceTopup:      true,
		BalanceTopupPurchaseID: 22,
		Entitlements: []service.SubscriptionConcurrencyEntitlement{
			{PurchaseID: 22, Concurrency: 5, BalanceTopupEnabled: true, Subscription: shared.AsLegacySubscription(nil), SharedSubscription: shared},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request = c.Request.WithContext(service.WithSubscriptionConcurrencyPlan(c.Request.Context(), plan))
	c.Set(string(middleware2.ContextKeySubscription), shared.AsLegacySubscription(nil))

	release, acquired, err := helper.TryAcquireUserSlotForAPIKeyFromGin(c, 7, 10, 99)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, release)
	_, ok := middleware2.GetSubscriptionFromContext(c)
	require.False(t, ok)
	source, ok := service.SubscriptionConcurrencySourceFromContext(c.Request.Context())
	require.True(t, ok)
	require.True(t, source.UseBalance)

	release()
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseUserCalled))
}
