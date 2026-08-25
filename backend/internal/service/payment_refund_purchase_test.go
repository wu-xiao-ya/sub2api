//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// errWithdrawTestGateway stands in for a provider-side refund failure.
var errWithdrawTestGateway = errors.New("gateway refund failed")

func newRefundPurchaseOrder(t *testing.T, ctx context.Context, client *dbent.Client, amount float64) *dbent.PaymentOrder {
	t.Helper()
	user, err := client.User.Create().
		SetEmail("refund-purchase-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-purchase-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(amount).
		SetPayAmount(amount).
		SetFeeRate(0).
		SetRechargeCode("REFUND-PURCHASE-" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetOutTradeNo("sub2_refund_purchase_" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-refund-purchase").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(100).
		SetSubscriptionGroupID(7).
		SetSubscriptionDays(30).
		SetStatus(OrderStatusCompleted).
		SetPaidAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	return order
}

// prepDeduct must target the purchase this order created, not "some active
// subscription for the order's group".
func TestPrepDeductResolvesPurchaseByOrderSource(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)

	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, order.UserID, PurchaseSourcePaymentOrder, &order.ID, PurchaseStatusActive, expires)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: order.Amount}

	early := svc.prepDeduct(ctx, order, plan, false)
	require.Nil(t, early)
	require.Equal(t, payment.DeductionTypeSubscription, plan.DeductionType)
	require.Equal(t, purchase.ID, plan.PurchaseID)
	require.NotNil(t, plan.PurchasePrior)
	require.Equal(t, PurchaseStatusActive, plan.PurchasePrior.Status)
	require.Zero(t, plan.SubDaysToDeduct, "purchases are withdrawn by status, not by day deduction")
	require.Zero(t, plan.SubscriptionID)
}

// A historical order that genuinely has no purchase keeps the legacy path.
func TestPrepDeductFallsBackToLegacyForHistoricalOrderWithoutPurchase(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        77,
		UserID:    order.UserID,
		GroupID:   *order.SubscriptionGroupID,
		StartsAt:  time.Now().Add(-time.Hour),
		ExpiresAt: expiresAt,
		Status:    SubscriptionStatusActive,
	})
	groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 7, Status: payment.EntityStatusActive}}
	svc := &PaymentService{
		entClient:       client,
		subscriptionSvc: NewSubscriptionService(groupRepo, subRepo, nil, client, nil),
	}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: order.Amount}

	early := svc.prepDeduct(ctx, order, plan, false)
	require.Nil(t, early)
	require.Zero(t, plan.PurchaseID, "no purchase exists for this historical order")
	require.Equal(t, 30, plan.SubDaysToDeduct)
	require.Equal(t, int64(77), plan.SubscriptionID)
}

// An order with neither a purchase nor legacy subscription fields is ambiguous
// and must require force rather than silently refunding nothing.
func TestPrepDeductRequiresForceWhenNeitherPurchaseNorLegacyFields(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		ClearSubscriptionGroupID().
		ClearSubscriptionDays().
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{OrderID: order.ID, Order: order, RefundAmount: order.Amount}

	early := svc.prepDeduct(ctx, order, plan, false)
	require.NotNil(t, early)
	require.True(t, early.RequireForce)
	require.False(t, early.Success)

	forced := svc.prepDeduct(ctx, order, plan, true)
	require.Nil(t, forced, "force lets an operator proceed deliberately")
}

func TestWithdrawPurchaseForRefundRevokesFullRefundIdempotently(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, order.UserID, PurchaseSourcePaymentOrder, &order.ID, PurchaseStatusActive, expires)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		DeductionType: payment.DeductionTypeSubscription,
		PurchaseID:    purchase.ID,
	}

	res, err := svc.withdrawPurchaseForRefund(ctx, plan)
	require.NoError(t, err)
	require.Nil(t, res)
	require.True(t, plan.PurchaseWithdrawn)

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusRevoked, reloaded.Status)

	revokedAudit, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PURCHASE_REVOKED")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, revokedAudit)

	// Replay: already revoked, so no second transition is recorded.
	replay := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		DeductionType: payment.DeductionTypeSubscription,
		PurchaseID:    purchase.ID,
	}
	res, err = svc.withdrawPurchaseForRefund(ctx, replay)
	require.NoError(t, err)
	require.Nil(t, res)
	require.False(t, replay.PurchaseWithdrawn, "already-withdrawn purchase must not be re-revoked")
}

// Partial refunds intentionally leave the purchase intact: an entitlement
// snapshot has no partial representation. The refund proceeds and an audit row
// flags it for manual handling.
func TestWithdrawPurchaseForRefundSkipsPartialRefund(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, order.UserID, PurchaseSourcePaymentOrder, &order.ID, PurchaseStatusActive, expires)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount / 2,
		DeductionType: payment.DeductionTypeSubscription,
		PurchaseID:    purchase.ID,
	}

	res, err := svc.withdrawPurchaseForRefund(ctx, plan)
	require.NoError(t, err)
	require.Nil(t, res, "partial refund must not block the gateway refund")
	require.False(t, plan.PurchaseWithdrawn)

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusActive, reloaded.Status, "partial refund leaves entitlement in place")

	skipped, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PURCHASE_PARTIAL_SKIPPED")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, skipped, "partial refunds must be auditable for manual follow-up")
}

// Gateway failure after withdrawal must restore the exact prior status/expiry.
func TestRollbackRefundRestoresWithdrawnPurchase(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, order.UserID, PurchaseSourcePaymentOrder, &order.ID, PurchaseStatusActive, expires)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		DeductionType: payment.DeductionTypeSubscription,
		PurchaseID:    purchase.ID,
	}

	_, err := svc.withdrawPurchaseForRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, plan.PurchaseWithdrawn)

	require.True(t, svc.RollbackRefund(ctx, plan, errWithdrawTestGateway))
	require.False(t, plan.PurchaseWithdrawn)

	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusActive, reloaded.Status)
	require.True(t, reloaded.ExpiresAt.Equal(expires), "rollback restores the original expiry")

	restoredAudit, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PURCHASE_RESTORED")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, restoredAudit)
}

// A purchase revoked by an earlier settled refund must not be resurrected by a
// later rollback that did not withdraw it.
func TestRollbackRefundLeavesUntouchedPurchaseAlone(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := newRefundPurchaseOrder(t, ctx, client, 80)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	purchase := seedLifecyclePurchase(t, ctx, client, order.UserID, PurchaseSourcePaymentOrder, &order.ID, PurchaseStatusRevoked, expires)

	svc := &PaymentService{entClient: client, subscriptionSvc: &SubscriptionService{entClient: client}}
	plan := &RefundPlan{
		OrderID:       order.ID,
		Order:         order,
		RefundAmount:  order.Amount,
		DeductionType: payment.DeductionTypeSubscription,
		PurchaseID:    purchase.ID,
	}

	// withdraw is a no-op because the purchase is already revoked
	_, err := svc.withdrawPurchaseForRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, plan.PurchaseWithdrawn)

	require.True(t, svc.RollbackRefund(ctx, plan, errWithdrawTestGateway))
	reloaded, err := client.SubscriptionPurchase.Get(ctx, purchase.ID)
	require.NoError(t, err)
	require.Equal(t, PurchaseStatusRevoked, reloaded.Status, "rollback must not resurrect a previously revoked purchase")
}

func TestRefundIsFullOrderAmountUsesCurrencyTolerance(t *testing.T) {
	t.Parallel()
	order := &dbent.PaymentOrder{Amount: 80}
	require.True(t, refundIsFullOrderAmount(&RefundPlan{Order: order, RefundAmount: 80}))
	require.True(t, refundIsFullOrderAmount(&RefundPlan{Order: order, RefundAmount: 79.999}), "float noise must not read as partial")
	require.False(t, refundIsFullOrderAmount(&RefundPlan{Order: order, RefundAmount: 40}))
	require.False(t, refundIsFullOrderAmount(nil))
}
