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
	// REFUND_PENDING 只能通过 QueryAndFinalizeRefund 收敛：重新发起会重复扣减并向网关重复退款。
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed}
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
	if u.Balance < p.RefundAmount && !force {
		return &RefundResult{Success: false, Warning: "user balance is insufficient for deduction, use force", RequireForce: true}
	}
	// 扣减上限是"当前可用余额"，实际扣减金额由 deductAvailableBalance 返回，
	// 避免并发充值/扣费后把余额扣成负数。
	p.BalanceToDeduct = math.Max(0, math.Min(p.RefundAmount, u.Balance))
	return nil
}

// availableBalanceDeductor 只扣可用余额（最多扣到 0）。UserRepository 的基础实现
// 允许透支，退款扣减必须走这条能力，否则会把用户余额打成负数。
type availableBalanceDeductor interface {
	DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error)
}

func (s *PaymentService) deductAvailableBalance(ctx context.Context, userID int64, amount float64) (float64, error) {
	deductor, ok := s.userRepo.(availableBalanceDeductor)
	if !ok {
		return 0, errors.New("user repository does not support available balance deduction")
	}
	return deductor.DeductAvailableBalance(ctx, userID, amount)
}

// invalidateRefundBalanceCache 退款扣减或归还余额后失效计费缓存，
// 否则网关在缓存的 TTL 内仍会按旧余额放行。
func (s *PaymentService) invalidateRefundBalanceCache(ctx context.Context, userID int64) {
	if s == nil || s.redeemService == nil {
		return
	}
	s.redeemService.invalidateRedeemCaches(ctx, userID, &RedeemCode{Type: RedeemTypeBalance})
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
	// REFUND_PENDING 不在白名单内：挂起中的退款只能由 QueryAndFinalizeRefund 收敛，
	// 重新发起会重复扣减并向网关重复退款。
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed)).SetStatus(OrderStatusRefunding).Save(ctx)
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
			deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
			if err != nil {
				s.restoreStatus(ctx, p)
				return nil, fmt.Errorf("deduction: %w", err)
			}
			p.BalanceToDeduct = deducted
			s.invalidateRefundBalanceCache(ctx, p.Order.UserID)
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

	pendingDetail, err := s.latestRefundPendingDetail(ctx, oid)
	if err != nil {
		return nil, err
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:     o.PaymentTradeNo,
		OrderID:     o.OutTradeNo,
		RefundID:    pendingDetail.RefundID,
		Amount:      formatGatewayRefundAmount(o.RefundAmount, o),
		RequestedAt: pendingDetail.requestedAt,
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}

	plan := s.refundFinalizePlan(o)
	switch {
	case pendingDetail.DeductionHeld:
		// 进入 pending 时扣减（余额/订阅权益）已经保留，终态直接复用，不再重复扣减。
		pendingDetail.applyTo(plan)
	case !pendingDetail.DeductionRollbackOK:
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	case o.OrderType == payment.OrderTypeSubscription:
		if early := s.prepDeduct(ctx, o, plan, true); early != nil {
			return early, nil
		}
	}

	if err := validateRefundProviderResponse(resp); err != nil {
		return s.finalizeRefundFailed(ctx, plan, err)
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.finalizePendingRefundSuccess(ctx, plan)
	case payment.ProviderStatusPending:
		s.writeAuditLog(ctx, oid, "REFUND_QUERY_PENDING", "admin", map[string]any{"refundID": resp.RefundID})
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	default:
		return s.finalizeRefundFailed(ctx, plan, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
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

// finalizePendingRefundSuccess 把挂起中的退款收敛为成功。整个过程在一个事务里
// 完成状态认领、最终扣减与成功审计，避免只完成一半时重试重复扣减。
func (s *PaymentService) finalizePendingRefundSuccess(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund finalize tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)
	claimed, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefundPending)).
		SetStatus(OrderStatusRefunding).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("claim pending refund: %w", err)
	}
	if claimed == 0 {
		err = infraerrors.Conflict("CONFLICT", "order status changed")
		return nil, err
	}
	if err = s.applyRefundFinalDeduction(txCtx, p); err != nil {
		return nil, err
	}
	result, err := s.markRefundOkTx(txCtx, tx.Client(), p)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund finalize tx: %w", err)
	}
	// 挂起时扣减已保留，这里只补偿"直接成功"路径的余额缓存。
	if !p.deductionHeld && p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		s.invalidateRefundBalanceCache(ctx, p.Order.UserID)
	}
	return result, nil
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if p.deductionHeld {
		// 进入 pending 时扣减已保留，终态成功无需再扣。
		return nil
	}
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
		deducted, err := s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		if err != nil {
			return fmt.Errorf("deduction: %w", err)
		}
		p.BalanceToDeduct = deducted
	}
	// The subscription withdrawal already ran through withdrawPurchaseForRefund
	// above; there is no legacy user_subscriptions path left on this route.
	return nil
}

func (s *PaymentService) finalizeRefundFailed(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	warning := "gateway refund failed: " + psErrMsg(gErr)
	returnErr, err := s.finalizeRefundFailedTx(ctx, p, gErr, nil)
	if err != nil {
		return nil, err
	}
	if returnErr == nil {
		if p.deductionHeld && p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
			s.invalidateRefundBalanceCache(ctx, p.Order.UserID)
		}
		return &RefundResult{Success: false, Warning: warning}, nil
	}
	// 归还保留的扣减失败：先落一条 REFUND_ROLLBACK_FAILED 审计再重试，
	// 让运维可追溯，同时避免后续重试重复归还。
	slog.Error("[CRITICAL] refund failed but held deduction could not be returned", "orderID", p.OrderID, "error", returnErr)
	if _, retryErr := s.finalizeRefundFailedTx(ctx, p, gErr, returnErr); retryErr != nil {
		return nil, retryErr
	}
	return &RefundResult{Success: false, Warning: warning + "; refund deduction rollback failed"}, nil
}

// finalizeRefundFailedTx 在一个事务内把挂起退款收敛为失败并归还保留的扣减。
// 归还失败时整笔回滚（状态仍是 REFUND_PENDING），由调用方带 rollbackErr 重试，
// 保证"状态已失败但扣减没归还"不会留下只在重试时才暴露的双重扣减窗口。
func (s *PaymentService) finalizeRefundFailedTx(ctx context.Context, p *RefundPlan, gErr error, rollbackErr error) (returnErr error, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund failed tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)
	now := time.Now()
	claimed, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefundPending)).
		SetStatus(OrderStatusRefundFailed).
		SetFailedAt(now).
		SetFailedReason(psErrMsg(gErr)).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("claim pending refund failure: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	if p.deductionHeld && rollbackErr == nil {
		if err := s.returnHeldRefundDeduction(txCtx, p); err != nil {
			return err, nil
		}
	}
	if rollbackErr != nil {
		if err := createPaymentAuditLogTx(txCtx, tx.Client(), p.OrderID, "REFUND_ROLLBACK_FAILED", map[string]any{
			"gatewayError":    psErrMsg(gErr),
			"rollbackError":   psErrMsg(rollbackErr),
			"balanceDeducted": p.BalanceToDeduct,
			"subDaysDeducted": p.SubDaysToDeduct,
			"purchaseID":      p.PurchaseID,
		}); err != nil {
			return nil, err
		}
	}
	if err := createPaymentAuditLogTx(txCtx, tx.Client(), p.OrderID, "REFUND_FAILED", map[string]any{"detail": psErrMsg(gErr)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund failed tx: %w", err)
	}
	committed = true
	return nil, nil
}

// returnHeldRefundDeduction 归还进入 pending 时保留的扣减（余额/订阅权益）。
func (s *PaymentService) returnHeldRefundDeduction(ctx context.Context, p *RefundPlan) error {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.UpdateBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			return fmt.Errorf("return held balance deduction: %w", err)
		}
	}
	if p.PurchaseWithdrawn && p.PurchasePrior != nil && s.subscriptionSvc != nil {
		if _, err := s.subscriptionSvc.RestorePurchaseState(ctx, p.PurchasePrior); err != nil {
			return fmt.Errorf("return held subscription deduction: %w", err)
		}
		p.PurchaseWithdrawn = false
	}
	return nil
}

func createPaymentAuditLogTx(ctx context.Context, client *dbent.Client, oid int64, action string, detail map[string]any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal %s audit: %w", action, err)
	}
	if err := client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(oid, 10)).
		SetAction(action).
		SetDetail(string(raw)).
		SetOperator("admin").
		Exec(ctx); err != nil {
		return fmt.Errorf("write %s audit: %w", action, err)
	}
	return nil
}

type refundPendingAuditDetail struct {
	RefundID            string  `json:"refundID"`
	RefundAmount        float64 `json:"refundAmount"`
	Reason              string  `json:"reason"`
	Force               bool    `json:"force"`
	DeductionRollbackOK bool    `json:"deductionRollbackOK,omitempty"`
	// DeductionHeld 表示扣减在进入 pending 时已保留、终态成功时无需再扣。
	DeductionHeld   bool    `json:"deductionHeld,omitempty"`
	DeductionType   string  `json:"deductionType,omitempty"`
	BalanceDeducted float64 `json:"balanceDeducted"`
	SubDaysDeducted int     `json:"subDaysDeducted"`
	PurchaseID      int64   `json:"purchaseID,omitempty"`
	// 订阅权益的扣减同样保留在挂起期，因此必须记录被撤回 purchase 的
	// 用户与前态（status/expires_at），网关最终失败时才能精确还原。
	PurchaseUserID      int64      `json:"purchaseUserID,omitempty"`
	PurchaseStatus      string     `json:"purchaseStatus,omitempty"`
	PurchaseExpiresAt   *time.Time `json:"purchaseExpiresAt,omitempty"`
	SubscriptionID      int64      `json:"subscriptionID,omitempty"`

	// requestedAt 取自审计记录的创建时间，用于判断网关侧"查无退款记录"是否已过宽限期。
	// 不参与序列化。
	requestedAt time.Time
}

func (d refundPendingAuditDetail) applyTo(p *RefundPlan) {
	p.deductionHeld = true
	p.DeductionType = d.DeductionType
	if p.DeductionType == "" {
		p.DeductionType = payment.DeductionTypeNone
	}
	p.BalanceToDeduct = d.BalanceDeducted
	p.SubDaysToDeduct = d.SubDaysDeducted
	p.PurchaseID = d.PurchaseID
	p.SubscriptionID = d.SubscriptionID
	if d.PurchaseID > 0 && d.PurchaseStatus != "" {
		prior := &SharedPurchaseRecord{
			ID:      d.PurchaseID,
			UserID:  d.PurchaseUserID,
			Status:  d.PurchaseStatus,
			ExpiresAt: func() time.Time {
				if d.PurchaseExpiresAt != nil {
					return *d.PurchaseExpiresAt
				}
				return time.Time{}
			}(),
		}
		p.PurchasePrior = prior
		p.PurchaseWithdrawn = true
	}
}

func (s *PaymentService) latestRefundPendingDetail(ctx context.Context, oid int64) (refundPendingAuditDetail, error) {
	detail := refundPendingAuditDetail{DeductionRollbackOK: true}
	logEntry, err := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(oid, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
		Order(paymentauditlog.ByCreatedAt(sql.OrderDesc()), paymentauditlog.ByID(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return detail, nil
		}
		return detail, fmt.Errorf("load refund pending audit: %w", err)
	}
	if err := json.Unmarshal([]byte(logEntry.Detail), &detail); err != nil {
		return detail, fmt.Errorf("parse refund pending audit: %w", err)
	}
	detail.RefundID = strings.TrimSpace(detail.RefundID)
	detail.requestedAt = logEntry.CreatedAt
	return detail, nil
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
	return s.markRefundOkTx(ctx, s.entClient, p)
}

func (s *PaymentService) markRefundOkTx(ctx context.Context, client *dbent.Client, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	if err := createPaymentAuditLogTx(ctx, client, p.OrderID, "REFUND_SUCCESS", map[string]any{
		"refundAmount":    p.RefundAmount,
		"reason":          p.Reason,
		"balanceDeducted": p.BalanceToDeduct,
		"force":           p.Force,
	}); err != nil {
		return nil, err
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

// markRefundPending 把订单标记为"网关退款待确认"，并保留本次扣减。
//
// 关键点：扣减不再回滚。挂起期间用户余额/订阅权益保持已扣状态，
// REFUND_PENDING 审计里记录扣减明细，后续由 QueryAndFinalizeRefund 依据它
// 收敛：成功则沿用扣减，失败则归还一次。若此时回滚，收敛成功时会重复扣减
// 用户余额，或让"已退款但权益仍在"的窗口被反复放大。
func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	detail := refundPendingAuditDetail{
		RefundID:      refundResponseID(resp),
		RefundAmount:  p.RefundAmount,
		Reason:        p.Reason,
		Force:         p.Force,
		DeductionHeld: true,
		DeductionType: p.DeductionType,
		BalanceDeducted: func() float64 {
			if p.DeductionType == payment.DeductionTypeBalance {
				return p.BalanceToDeduct
			}
			return 0
		}(),
		SubDaysDeducted: p.SubDaysToDeduct,
		PurchaseID:      p.PurchaseID,
		SubscriptionID:  p.SubscriptionID,
	}
	if p.PurchaseWithdrawn && p.PurchasePrior != nil {
		detail.PurchaseUserID = p.PurchasePrior.UserID
		detail.PurchaseStatus = p.PurchasePrior.Status
		expiresAt := p.PurchasePrior.ExpiresAt
		detail.PurchaseExpiresAt = &expiresAt
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return nil, fmt.Errorf("marshal refund pending audit: %w", err)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund pending tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err = tx.PaymentOrder.UpdateOneID(p.OrderID).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		ClearRefundAt().
		SetForceRefund(p.Force).
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}
	if _, err := tx.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).
		SetAction("REFUND_PENDING").
		SetDetail(string(raw)).
		SetOperator("admin").
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("write refund pending audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		slog.Error("[CRITICAL] gateway refund pending but order not marked", "orderID", p.OrderID, "error", err)
		return nil, fmt.Errorf("commit refund pending: %w", err)
	}
	committed = true
	p.deductionHeld = true

	return &RefundResult{Success: false, Warning: "gateway refund is pending confirmation"}, nil
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
