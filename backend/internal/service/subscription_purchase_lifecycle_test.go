//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func newPurchaseLifecycleService(t *testing.T) (*SubscriptionService, *dbent.Client) {
	t.Helper()
	client := newPaymentConfigServiceTestClient(t)
	return &SubscriptionService{entClient: client}, client
}

func seedLifecyclePurchase(t *testing.T, ctx context.Context, client *dbent.Client, userID int64, source string, sourceID *int64, status string, expiresAt time.Time) *dbent.SubscriptionPurchase {
	t.Helper()
	purchase, err := client.SubscriptionPurchase.Create().
		SetUserID(userID).
		SetName("Pro Monthly").
		SetTierCode("pro").
		SetPrice(80).
		SetCurrency("CNY").
		SetStartsAt(time.Now().UTC().Add(-time.Hour)).
		SetExpiresAt(expiresAt).
		SetStatus(status).
		SetSource(source).
		SetNillableSourceID(sourceID).
		Save(ctx)
	require.NoError(t, err)
	return purchase
}

func TestFindPurchaseByPaymentOrderResolvesExactSource(t *testing.T) {
	ctx := context.Background()
	svc, client := newPurchaseLifecycleService(t)

	orderID := int64(4242)
	otherOrderID := int64(4343)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	want := seedLifecyclePurchase(t, ctx, client, 7, PurchaseSourcePaymentOrder, &orderID, PurchaseStatusActive, expires)
	// A second purchase for the same user must not be selected.
	seedLifecyclePurchase(t, ctx, client, 7, PurchaseSourcePaymentOrder, &otherOrderID, PurchaseStatusActive, expires.Add(-24*time.Hour))

	got, err := svc.FindPurchaseByPaymentOrder(ctx, orderID)
	require.NoError(t, err)
	require.Equal(t, want.ID, got.ID)
	require.Equal(t, PurchaseSourcePaymentOrder, got.Source)
	require.NotNil(t, got.SourceID)
	require.Equal(t, orderID, *got.SourceID)
	require.True(t, got.IsActive())
}

func TestFindPurchaseByPaymentOrderReportsMissingDistinctly(t *testing.T) {
	ctx := context.Background()
	svc, _ := newPurchaseLifecycleService(t)

	_, err := svc.FindPurchaseByPaymentOrder(ctx, 999)
	require.ErrorIs(t, err, ErrSharedPurchaseNotFound)
}

func TestRevokePurchaseIsIdempotentAndReportsPriorState(t *testing.T) {
	ctx := context.Background()
	svc, client := newPurchaseLifecycleService(t)

	orderID := int64(5150)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, 11, PurchaseSourcePaymentOrder, &orderID, PurchaseStatusActive, expires)

	changed, prior, err := svc.RevokePurchase(ctx, purchase.ID)
	require.NoError(t, err)
	require.True(t, changed, "first revoke must transition the purchase")
	require.Equal(t, PurchaseStatusActive, prior.Status, "prior state must describe what to restore")
	require.True(t, prior.ExpiresAt.Equal(expires))

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusRevoked, reloaded.Status)
	require.True(t, reloaded.ExpiresAt.Before(expires), "revoke pulls expiry back so the shown value matches the refund")

	// Second revoke is a no-op: no double withdrawal, no error.
	changedAgain, priorAgain, err := svc.RevokePurchase(ctx, purchase.ID)
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.Equal(t, PurchaseStatusRevoked, priorAgain.Status)
}

func TestRestorePurchaseStateReturnsExactPriorStatusAndExpiry(t *testing.T) {
	ctx := context.Background()
	svc, client := newPurchaseLifecycleService(t)

	orderID := int64(6161)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, 12, PurchaseSourcePaymentOrder, &orderID, PurchaseStatusActive, expires)

	_, prior, err := svc.RevokePurchase(ctx, purchase.ID)
	require.NoError(t, err)

	restored, err := svc.RestorePurchaseState(ctx, prior)
	require.NoError(t, err)
	require.True(t, restored)

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusActive, reloaded.Status)
	require.True(t, reloaded.ExpiresAt.Equal(expires), "expiry must be restored exactly, not recomputed")

	// Restoring an already-restored purchase changes nothing.
	again, err := svc.RestorePurchaseState(ctx, prior)
	require.NoError(t, err)
	require.False(t, again)
}

func TestSuspendPurchaseWithdrawsWithoutChangingExpiry(t *testing.T) {
	ctx := context.Background()
	svc, client := newPurchaseLifecycleService(t)

	orderID := int64(7171)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, 13, PurchaseSourcePaymentOrder, &orderID, PurchaseStatusActive, expires)

	changed, prior, err := svc.SuspendPurchase(ctx, purchase.ID)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, PurchaseStatusActive, prior.Status)

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusSuspended, reloaded.Status)
	require.True(t, reloaded.ExpiresAt.Equal(expires), "suspend must leave expiry intact so restore is lossless")

	changedAgain, _, err := svc.SuspendPurchase(ctx, purchase.ID)
	require.NoError(t, err)
	require.False(t, changedAgain)
}

func TestRestorePurchaseStateRejectsUnknownPurchase(t *testing.T) {
	ctx := context.Background()
	svc, _ := newPurchaseLifecycleService(t)

	_, err := svc.RestorePurchaseState(ctx, &SharedPurchaseRecord{ID: 4242, Status: PurchaseStatusActive})
	require.True(t, errors.Is(err, ErrSharedPurchaseNotFound))
}
