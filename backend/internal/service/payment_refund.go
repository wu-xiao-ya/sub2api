package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundPending, OrderStatusRefundFailed}
	if !psSliceContains(ok, o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	ga := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: amt, GatewayAmount: ga, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		return s.prepSubscriptionDeduct(ctx, o, p, force)
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	p.BalanceToDeduct = math.Min(p.RefundAmount, u.Balance)
	return nil
}

// prepSubscriptionDeduct resolves what a subscription refund must withdraw.
//
// Two purchase targets exist, resolved in priority order:
//
//  1. The purchase this order created — keyed by (source='payment_order',
//     source_id=order.id) — is the authoritative target for plan orders: it
//     identifies the exact entitlement this payment granted, and full refunds
//     revoke it entirely.
//
//  2. Historical orders completed before the purchase model have no such row.
//     user_subscriptions is frozen by migration 233 and must never be written,
//     so the migrated purchase is resolved instead: source=
//     'legacy_user_subscription' pinned to the order's user/group through
//     subscription_purchase_groups, preferring the legacy "payment order *id*"
//     note when several candidates exist. The old day-deduction semantics
//     (reduce expiry by SubscriptionDays, revoke on would-expire) are applied to
//     that migrated purchase. Ambiguous attribution fails closed and requires
//     force — never revokes an unrelated entitlement.
func (s *PaymentService) prepSubscriptionDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if s.subscriptionSvc == nil {
		if !force {
			return &RefundResult{Success: false, Warning: "subscription service is unavailable for deduction, use force", RequireForce: true}
		}
		return nil
	}

	purchase, err := s.subscriptionSvc.FindPurchaseByPaymentOrder(ctx, o.ID)
	switch {
	case err == nil && purchase != nil:
		p.PurchaseID = purchase.ID
		p.PurchasePrior = purchase
		p.PurchaseLegacyDeduct = false
		// Day-deduction is not applicable to payment_order purchases:
		// entitlement is withdrawn by a full status transition instead.
		p.SubDaysToDeduct = 0
		p.SubscriptionID = 0
		return nil
	case err != nil && !errors.Is(err, ErrSharedPurchaseNotFound):
		if !force {
			return &RefundResult{Success: false, Warning: "cannot resolve subscription purchase for refund: " + psErrMsg(err) + ", use force", RequireForce: true}
		}
		return nil
	}

	// No payment_order purchase: a historical order. Legacy fields are the
	// signature of one fulfilled before the purchase model.
	if o.SubscriptionGroupID == nil || o.SubscriptionDays == nil {
		if !force {
			return &RefundResult{Success: false, Warning: "subscription order has no purchase and no legacy subscription fields, use force", RequireForce: true}
		}
		return nil
	}

	legacy, lerr := s.subscriptionSvc.ResolveLegacyPurchaseForPaymentOrder(ctx, o.UserID, *o.SubscriptionGroupID, o.ID)
	switch {
	case lerr == nil && legacy != nil:
		p.PurchaseID = legacy.ID
		p.PurchasePrior = legacy
		p.PurchaseLegacyDeduct = true
		p.SubDaysToDeduct = *o.SubscriptionDays
		p.SubscriptionID = 0
		return nil
	case errors.Is(lerr, ErrSharedPurchaseAmbiguous):
		// Fail closed: attributing the wrong purchase would revoke an unrelated
		// entitlement. Force deliberately proceeds WITHOUT touching a purchase,
		// so even the operator escape hatch never mutates unverified rows.
		if !force {
			return &RefundResult{Success: false, Warning: "cannot attribute historical order to a migrated legacy purchase: " + psErrMsg(lerr) + "; resolution requires force and will skip entitlement withdrawal", RequireForce: true}
		}
		return nil
	case lerr != nil && !errors.Is(lerr, ErrSharedPurchaseNotFound):
		if !force {
			return &RefundResult{Success: false, Warning: "cannot resolve migrated legacy purchase for refund: " + psErrMsg(lerr) + ", use force", RequireForce: true}
		}
		return nil
	default:
		// No migrated purchase covers this user/group: nothing can be attributed.
		if !force {
			return &RefundResult{Success: false, Warning: "no migrated legacy purchase found for historical subscription order (user=" + strconv.FormatInt(o.UserID, 10) + ", group=" + strconv.FormatInt(*o.SubscriptionGroupID, 10) + "); resolution requires force and will skip entitlement withdrawal", RequireForce: true}
		}
		return nil
	}
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundPending, OrderStatusRefundFailed)).SetStatus(OrderStatusRefunding).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if c == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		// Skip balance deduction on retry if previous attempt already deducted
		// but failed to roll back (REFUND_ROLLBACK_FAILED in audit log).
		if !s.hasAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED") {
			if err := s.userRepo.DeductBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
				s.restoreStatus(ctx, p)
				return nil, fmt.Errorf("deduction: %w", err)
			}
		} else {
			slog.Warn("skipping balance deduction on retry (previous rollback failed)", "orderID", p.OrderID)
			p.BalanceToDeduct = 0
		}
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.PurchaseID > 0 {
		// withdrawPurchaseForRefund handles both target kinds: full revocation of
		// a payment_order purchase and legacy day-deduction of a migrated
		// legacy_user_subscription purchase. user_subscriptions is never written.
		if !s.hasAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED") {
			if res, err := s.withdrawPurchaseForRefund(ctx, p); err != nil {
				s.restoreStatus(ctx, p)
				return nil, err
			} else if res != nil {
				s.restoreStatus(ctx, p)
				return res, nil
			}
		} else {
			slog.Warn("skipping purchase withdrawal on retry (previous rollback failed)", "orderID", p.OrderID, "purchaseID", p.PurchaseID)
		}
	}
	resp, err := s.gwRefund(ctx, p)
	if err != nil {
		return s.handleGwFail(ctx, p, err)
	}
	return s.finishRefund(ctx, p, resp)
}

func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	if p.Order.PaymentTradeNo == "" {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"detail": "skipped"})
		return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
	}

	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: p.Order.PaymentTradeNo,
		OrderID: p.Order.OutTradeNo,
		Amount:  formatGatewayRefundAmount(p.GatewayAmount, p.Order),
		Reason:  p.Reason,
	})
	finishProviderCall()
	if err != nil {
		if resp != nil && strings.TrimSpace(resp.Status) == payment.ProviderStatusPending {
			return resp, nil
		}
		return nil, err
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func validateRefundProviderResponse(resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("payment refund response missing")
	}
	status := strings.TrimSpace(resp.Status)
	switch status {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded, payment.ProviderStatusPending:
		return nil
	case payment.ProviderStatusFailed:
		return fmt.Errorf("payment refund failed: status %s", status)
	default:
		return fmt.Errorf("payment refund returned unknown status: %s", status)
	}
}

func (s *PaymentService) finishRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.handleGwFail(ctx, p, err)
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.markRefundOk(ctx, p)
	case payment.ProviderStatusPending:
		return s.markRefundPending(ctx, p, resp)
	default:
		return s.handleGwFail(ctx, p, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusRefundPending {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only refund pending orders can be finalized")
	}

	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED", "this payment provider does not support refund status query; please verify manually")
	}

	pendingDetail := s.latestRefundPendingDetail(ctx, oid)
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:  o.PaymentTradeNo,
		OrderID:  o.OutTradeNo,
		RefundID: pendingDetail.RefundID,
		Amount:   formatGatewayRefundAmount(o.RefundAmount, o),
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.finalizeRefundFailed(ctx, o, err)
	}

	plan := s.refundFinalizePlan(o)
	if !pendingDetail.DeductionRollbackOK {
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	} else if o.OrderType == payment.OrderTypeSubscription {
		if early := s.prepDeduct(ctx, o, plan, true); early != nil {
			return early, nil
		}
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		if err := s.applyRefundFinalDeduction(ctx, plan); err != nil {
			return nil, err
		}
		return s.markRefundOk(ctx, plan)
	case payment.ProviderStatusPending:
		s.writeAuditLog(ctx, oid, "REFUND_QUERY_PENDING", "admin", map[string]any{"refundID": resp.RefundID})
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	default:
		return s.finalizeRefundFailed(ctx, o, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) refundFinalizePlan(o *dbent.PaymentOrder) *RefundPlan {
	refundAmount := o.RefundAmount
	reason := strings.TrimSpace(psStringValue(o.RefundReason))
	if reason == "" {
		reason = fmt.Sprintf("refund order:%d", o.ID)
	}
	return &RefundPlan{
		OrderID:       o.ID,
		Order:         o,
		RefundAmount:  refundAmount,
		GatewayAmount: calculateGatewayRefundAmount(o.Amount, o.PayAmount, refundAmount, PaymentOrderCurrency(o)),
		Reason:        reason,
		Force:         o.ForceRefund,
		DeductBalance: true,
		DeductionType: func() string {
			// Subscription orders must finalize through the subscription path so
			// the purchase is resolved and withdrawn; defaulting to the balance
			// type would silently skip entitlement withdrawal on the pending
			// refund confirmation path.
			if o.OrderType == payment.OrderTypeSubscription {
				return payment.DeductionTypeSubscription
			}
			return payment.DeductionTypeBalance
		}(),
		BalanceToDeduct: func() float64 {
			if o.OrderType == payment.OrderTypeBalance {
				return refundAmount
			}
			return 0
		}(),
	}
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if s.hasAuditLog(ctx, p.OrderID, "REFUND_SUCCESS") {
		p.BalanceToDeduct = 0
		p.SubDaysToDeduct = 0
		p.PurchaseID = 0
		return nil
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.PurchaseID > 0 {
		if res, err := s.withdrawPurchaseForRefund(ctx, p); err != nil {
			return err
		} else if res != nil {
			return errors.New(res.Warning)
		}
	}
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.DeductBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			return fmt.Errorf("deduction: %w", err)
		}
	}
	// The subscription withdrawal already ran through withdrawPurchaseForRefund
	// above; there is no legacy user_subscriptions path left on this route.
	return nil
}

func (s *PaymentService) finalizeRefundFailed(ctx context.Context, o *dbent.PaymentOrder, gErr error) (*RefundResult, error) {
	now := time.Now()
	_, _ = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetStatus(OrderStatusRefundFailed).SetFailedAt(now).SetFailedReason(psErrMsg(gErr)).Save(ctx)
	s.writeAuditLog(ctx, o.ID, "REFUND_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
	return &RefundResult{Success: false, Warning: "gateway refund failed: " + psErrMsg(gErr)}, nil
}

type refundPendingAuditDetail struct {
	RefundID            string `json:"refundID"`
	DeductionRollbackOK bool   `json:"deductionRollbackOK"`
}

func (s *PaymentService) latestRefundPendingDetail(ctx context.Context, oid int64) refundPendingAuditDetail {
	logEntry, err := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(oid, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
		Order(paymentauditlog.ByCreatedAt(sql.OrderDesc())).
		First(ctx)
	if err != nil || logEntry == nil {
		return refundPendingAuditDetail{DeductionRollbackOK: true}
	}
	detail := refundPendingAuditDetail{DeductionRollbackOK: true}
	_ = json.Unmarshal([]byte(logEntry.Detail), &detail)
	detail.RefundID = strings.TrimSpace(detail.RefundID)
	return detail
}

// getRefundProvider creates a provider using the order's original instance config.
// Delegates to getOrderProvider which handles instance lookup and fallback.
func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) handleGwFail(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	if s.RollbackRefund(ctx, p, gErr) {
		s.restoreStatus(ctx, p)
		s.writeAuditLog(ctx, p.OrderID, "REFUND_GATEWAY_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
		return &RefundResult{Success: false, Warning: "gateway failed: " + psErrMsg(gErr) + ", rolled back"}, nil
	}
	now := time.Now()
	_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(OrderStatusRefundFailed).SetFailedAt(now).SetFailedReason(psErrMsg(gErr)).Save(ctx)
	s.writeAuditLog(ctx, p.OrderID, "REFUND_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
	return nil, infraerrors.InternalServer("REFUND_FAILED", psErrMsg(gErr))
}

func (s *PaymentService) markRefundOk(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	s.writeAuditLog(ctx, p.OrderID, "REFUND_SUCCESS", "admin", map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	balanceDeducted := p.BalanceToDeduct
	subDaysDeducted := p.SubDaysToDeduct
	rollbackOK := s.RollbackRefund(ctx, p, nil)
	if rollbackOK {
		p.BalanceToDeduct = 0
		p.SubDaysToDeduct = 0
	}

	_, err := s.entClient.PaymentOrder.UpdateOneID(p.OrderID).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		ClearRefundAt().
		SetForceRefund(p.Force).
		ClearFailedAt().
		ClearFailedReason().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}

	detail := map[string]any{
		"refundID":            refundResponseID(resp),
		"refundAmount":        p.RefundAmount,
		"reason":              p.Reason,
		"force":               p.Force,
		"balanceDeducted":     p.BalanceToDeduct,
		"subDaysDeducted":     p.SubDaysToDeduct,
		"balanceRolledBack":   balanceDeducted,
		"subDaysRolledBack":   subDaysDeducted,
		"deductionRollbackOK": rollbackOK,
	}
	s.writeAuditLog(ctx, p.OrderID, "REFUND_PENDING", "admin", detail)

	warning := "gateway refund is pending confirmation"
	if !rollbackOK {
		warning += "; refund deduction rollback failed"
	}
	return &RefundResult{Success: false, Warning: warning}, nil
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}

func (s *PaymentService) RollbackRefund(ctx context.Context, p *RefundPlan, gErr error) bool {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.UpdateBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			slog.Error("[CRITICAL] rollback failed", "orderID", p.OrderID, "amount", p.BalanceToDeduct, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{"gatewayError": psErrMsg(gErr), "rollbackError": psErrMsg(err), "balanceDeducted": p.BalanceToDeduct})
			return false
		}
	}
	// Subscription withdrawal lives entirely on subscription_purchases: a
	// migrated legacy purchase deducted by day is restored through
	// RestorePurchaseState below, exactly like a revoked payment_order purchase.
	// user_subscriptions has no rollback path here.
	//
	// Restore the purchase only when this attempt withdrew it. An
	// already-revoked purchase was withdrawn by an earlier settled refund and
	// must not be resurrected by this rollback.
	if p.PurchaseWithdrawn && p.PurchasePrior != nil && s.subscriptionSvc != nil {
		if _, err := s.subscriptionSvc.RestorePurchaseState(ctx, p.PurchasePrior); err != nil {
			slog.Error("[CRITICAL] purchase rollback failed", "orderID", p.OrderID, "purchaseID", p.PurchaseID, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{
				"gatewayError":  psErrMsg(gErr),
				"rollbackError": psErrMsg(err),
				"purchaseID":    p.PurchaseID,
			})
			return false
		}
		p.PurchaseWithdrawn = false
		s.writeAuditLog(ctx, p.OrderID, "REFUND_PURCHASE_RESTORED", "admin", map[string]any{
			"purchaseID":     p.PurchaseID,
			"restoredStatus": p.PurchasePrior.Status,
			"restoredExpiry": p.PurchasePrior.ExpiresAt,
		})
	}
	return true
}

// refundIsFullOrderAmount reports whether the refund covers the whole order.
// Currency tolerance is reused so float representation does not misclassify an
// exact full refund as partial.
func refundIsFullOrderAmount(p *RefundPlan) bool {
	if p == nil || p.Order == nil {
		return false
	}
	return p.RefundAmount >= p.Order.Amount-paymentAmountToleranceForCurrency(PaymentOrderCurrency(p.Order))
}

// withdrawPurchaseForRefund withdraws entitlement behind a successful full
// subscription refund. Two withdrawal kinds exist, both idempotent:
//
//   - Payment-order purchases (PurchaseLegacyDeduct=false) are revoked entirely:
//     status flips to 'revoked' and expiry is pulled back so the value shown to
//     the user matches the refund.
//
//   - Migrated legacy_user_subscription purchases (PurchaseLegacyDeduct=true)
//     keep the historical day-deduction semantics: expires_at shortens by
//     SubDaysToDeduct and, if that would expire the purchase, it is revoked.
//
// Both capture exact prior status/expiry on the plan so a gateway failure or
// pending-refund reversal restores precisely that state.
//
// Partial refunds deliberately do not touch the purchase either way. A purchase
// is an all-or-nothing entitlement snapshot with no partial representation, so
// withdrawing it for a partial refund would withdraw more than was refunded, and
// silently prorating quotas or expiry would invent policy. The refund proceeds
// and an audit row flags it for manual handling.
func (s *PaymentService) withdrawPurchaseForRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	if s.subscriptionSvc == nil || p.PurchaseID <= 0 {
		return nil, nil
	}
	if !refundIsFullOrderAmount(p) {
		s.writeAuditLog(ctx, p.OrderID, "REFUND_PURCHASE_PARTIAL_SKIPPED", "admin", map[string]any{
			"purchaseID":   p.PurchaseID,
			"refundAmount": p.RefundAmount,
			"orderAmount":  p.Order.Amount,
			"refundDays":   p.SubDaysToDeduct,
			"detail":       "partial subscription refund does not withdraw the purchase; adjust entitlement manually",
		})
		p.SubDaysToDeduct = 0
		return nil, nil
	}

	var (
		changed  bool
		prior    *SharedPurchaseRecord
		err      error
		auditKey string
		auditAt  map[string]any
	)
	if p.PurchaseLegacyDeduct {
		changed, prior, err = s.subscriptionSvc.DeductLegacyPurchaseDays(ctx, p.PurchaseID, p.SubDaysToDeduct)
		auditKey = "REFUND_PURCHASE_LEGACY_DAYS_DEDUCTED"
		auditAt = map[string]any{
			"purchaseID":    p.PurchaseID,
			"refundDays":    p.SubDaysToDeduct,
			"refundAmount":  p.RefundAmount,
			"deductionType": p.DeductionType,
		}
		if err != nil {
			return nil, fmt.Errorf("deduct legacy subscription purchase: %w", err)
		}
	} else {
		changed, prior, err = s.subscriptionSvc.RevokePurchase(ctx, p.PurchaseID)
		auditKey = "REFUND_PURCHASE_REVOKED"
		auditAt = map[string]any{
			"purchaseID":    p.PurchaseID,
			"refundAmount":  p.RefundAmount,
			"deductionType": p.DeductionType,
		}
		if err != nil {
			return nil, fmt.Errorf("revoke subscription purchase: %w", err)
		}
	}
	if prior != nil {
		p.PurchasePrior = prior
		auditAt["priorStatus"] = prior.Status
		auditAt["priorExpiry"] = prior.ExpiresAt
	}
	p.PurchaseWithdrawn = changed
	if changed {
		s.writeAuditLog(ctx, p.OrderID, auditKey, "admin", auditAt)
	} else {
		slog.Info("subscription purchase already without entitlement, refund is idempotent", "orderID", p.OrderID, "purchaseID", p.PurchaseID, "legacyDeduct", p.PurchaseLegacyDeduct)
	}
	return nil, nil
}

func (s *PaymentService) restoreStatus(ctx context.Context, p *RefundPlan) {
	rs := OrderStatusCompleted
	if p.Order.Status == OrderStatusRefundRequested {
		rs = OrderStatusRefundRequested
	}
	_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(rs).Save(ctx)
}
