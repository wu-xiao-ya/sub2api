package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionpurchase"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionpurchasegroup"
)

// Purchase lifecycle statuses. Only PurchaseStatusActive grants entitlement:
// every read path in subscription_purchase_service.go filters status = 'active',
// so moving a purchase out of that value is what actually withdraws access.
const (
	PurchaseStatusActive    = "active"
	PurchaseStatusRevoked   = "revoked"
	PurchaseStatusSuspended = "suspended"
)

// PurchaseSourcePaymentOrder is the source discriminator written by payment
// fulfillment. Combined with source_id = payment order ID it is unique
// (uq_subscription_purchases_source_id), which makes it the stable key for
// resolving "the purchase this order created".
const PurchaseSourcePaymentOrder = "payment_order"

// PurchaseSourceLegacyUserSubscription is the source discriminator migration 233
// wrote onto every backfilled subscription_purchases row that originated from a
// frozen user_subscriptions record (source_id = legacy user_subscriptions.id).
// Historical-order refunds resolve through this source so the entitlement they
// withdraw lives on the purchase model and the frozen native table is never
// written.
const PurchaseSourceLegacyUserSubscription = "legacy_user_subscription"

// ErrSharedPurchaseAmbiguous reports that several migrated legacy purchases
// could stand for the entitlement a historical order granted, and none of the
// deterministic evidence rules singled one out. Callers must fail closed before
// touching any entitlement rather than guessing.
var ErrSharedPurchaseAmbiguous = errors.New("shared subscription purchase attribution is ambiguous")

// ErrSharedPurchaseNotFound is returned when no purchase matches a source key.
// It is deliberately distinct from ErrSharedSubscriptionNotFound, which means
// "no *active entitlement* for this group" — a purchase may exist but be
// revoked, and refund paths must be able to tell those two cases apart.
var ErrSharedPurchaseNotFound = errors.New("shared subscription purchase not found")

// SharedPurchaseRecord is the lifecycle view of a purchase row: identity plus
// the mutable fields refunds need to withdraw and restore. It intentionally
// omits usage/quota columns, which lifecycle transitions never touch.
type SharedPurchaseRecord struct {
	ID        int64
	UserID    int64
	PlanID    *int64
	Name      string
	TierCode  string
	Status    string
	StartsAt  time.Time
	ExpiresAt time.Time
	Source    string
	SourceID  *int64
}

// IsActive reports whether the record currently grants entitlement.
func (r *SharedPurchaseRecord) IsActive() bool {
	return r != nil && r.Status == PurchaseStatusActive
}

func sharedPurchaseRecordFromEntity(purchase *dbent.SubscriptionPurchase) *SharedPurchaseRecord {
	if purchase == nil {
		return nil
	}
	return &SharedPurchaseRecord{
		ID:        purchase.ID,
		UserID:    purchase.UserID,
		PlanID:    purchase.PlanID,
		Name:      purchase.Name,
		TierCode:  purchase.TierCode,
		Status:    purchase.Status,
		StartsAt:  purchase.StartsAt,
		ExpiresAt: purchase.ExpiresAt,
		Source:    purchase.Source,
		SourceID:  purchase.SourceID,
	}
}

// FindPurchaseBySource resolves the purchase created by a given source record.
// Returns ErrSharedPurchaseNotFound when the pair has no row, so callers can
// distinguish "never fulfilled as a purchase" from a query failure.
func (s *SubscriptionService) FindPurchaseBySource(ctx context.Context, source string, sourceID int64) (*SharedPurchaseRecord, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if source == "" || sourceID <= 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	purchase, err := s.entClient.SubscriptionPurchase.Query().
		Where(
			subscriptionpurchase.SourceEQ(source),
			subscriptionpurchase.SourceIDEQ(sourceID),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSharedPurchaseNotFound
		}
		return nil, err
	}
	return sharedPurchaseRecordFromEntity(purchase), nil
}

// FindPurchaseByPaymentOrder resolves the purchase created by a payment order.
func (s *SubscriptionService) FindPurchaseByPaymentOrder(ctx context.Context, orderID int64) (*SharedPurchaseRecord, error) {
	return s.FindPurchaseBySource(ctx, PurchaseSourcePaymentOrder, orderID)
}

// ListPurchaseGroupIDs returns the groups a purchase authorizes, ordered for
// deterministic cache invalidation.
func (s *SubscriptionService) ListPurchaseGroupIDs(ctx context.Context, purchaseID int64) ([]int64, error) {
	if s == nil || s.entClient == nil || purchaseID <= 0 {
		return nil, nil
	}
	groups, err := s.ListSharedSubscriptionGroups(ctx, purchaseID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(groups))
	for _, item := range groups {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

// invalidatePurchaseCaches refreshes entitlement caches for every group the
// purchase authorizes. Best-effort by design: a lifecycle transition is already
// committed by the time this runs, and failing the caller would misreport a
// completed refund. Errors are joined so callers may log them.
func (s *SubscriptionService) invalidatePurchaseCaches(ctx context.Context, purchase *SharedPurchaseRecord) error {
	if s == nil || purchase == nil {
		return nil
	}
	groupIDs, err := s.ListPurchaseGroupIDs(ctx, purchase.ID)
	if err != nil {
		return err
	}
	var errs []error
	for _, groupID := range groupIDs {
		if cacheErr := s.invalidateSubscriptionCaches(purchase.UserID, groupID); cacheErr != nil {
			errs = append(errs, cacheErr)
		}
	}
	return errors.Join(errs...)
}

// setPurchaseStatusFrom performs a compare-and-set status transition. The
// expectedStatus guard is what makes every lifecycle call idempotent and safe
// against concurrent refund workers: a second attempt updates zero rows and
// returns changed=false rather than clobbering a newer state.
func (s *SubscriptionService) setPurchaseStatusFrom(
	ctx context.Context,
	purchaseID int64,
	expectedStatus string,
	nextStatus string,
	expiresAt *time.Time,
) (changed bool, err error) {
	if s == nil || s.entClient == nil {
		return false, errors.New("subscription service is unavailable")
	}
	if purchaseID <= 0 {
		return false, ErrSharedPurchaseNotFound
	}
	update := s.entClient.SubscriptionPurchase.Update().
		Where(
			subscriptionpurchase.IDEQ(purchaseID),
			subscriptionpurchase.StatusEQ(expectedStatus),
		).
		SetStatus(nextStatus).
		SetUpdatedAt(time.Now().UTC())
	if expiresAt != nil {
		update = update.SetExpiresAt(*expiresAt)
	}
	affected, err := update.Save(ctx)
	if err != nil {
		return false, fmt.Errorf("set purchase %d status to %s: %w", purchaseID, nextStatus, err)
	}
	return affected > 0, nil
}

// RevokePurchase withdraws entitlement for a purchase, idempotently.
//
// Revocation both flips status and pulls expires_at back to now: status alone
// is enough for the entitlement queries, but a truthful expiry keeps the value
// shown to users and any expiry-ordered reporting consistent with the refund.
// The pre-revocation status and expiry are returned so a caller that later has
// to roll back (gateway failure, pending-refund reversal) can restore exactly
// what was there before rather than guessing "active + original expiry".
func (s *SubscriptionService) RevokePurchase(ctx context.Context, purchaseID int64) (changed bool, prior *SharedPurchaseRecord, err error) {
	prior, err = s.getPurchaseByID(ctx, purchaseID)
	if err != nil {
		return false, nil, err
	}
	if prior.Status != PurchaseStatusActive {
		// Already withdrawn — nothing to do, and prior still describes the
		// state a rollback should restore.
		return false, prior, nil
	}
	now := time.Now().UTC()
	changed, err = s.setPurchaseStatusFrom(ctx, purchaseID, PurchaseStatusActive, PurchaseStatusRevoked, &now)
	if err != nil {
		return false, prior, err
	}
	if changed {
		if cacheErr := s.invalidatePurchaseCaches(ctx, prior); cacheErr != nil {
			return changed, prior, fmt.Errorf("invalidate purchase caches after revoke: %w", cacheErr)
		}
	}
	return changed, prior, nil
}

// SuspendPurchase pauses entitlement without asserting the purchase is spent.
// Used for refunds that are not yet gateway-confirmed, where access must stop
// but the row should remain restorable.
func (s *SubscriptionService) SuspendPurchase(ctx context.Context, purchaseID int64) (changed bool, prior *SharedPurchaseRecord, err error) {
	prior, err = s.getPurchaseByID(ctx, purchaseID)
	if err != nil {
		return false, nil, err
	}
	if prior.Status != PurchaseStatusActive {
		return false, prior, nil
	}
	changed, err = s.setPurchaseStatusFrom(ctx, purchaseID, PurchaseStatusActive, PurchaseStatusSuspended, nil)
	if err != nil {
		return false, prior, err
	}
	if changed {
		if cacheErr := s.invalidatePurchaseCaches(ctx, prior); cacheErr != nil {
			return changed, prior, fmt.Errorf("invalidate purchase caches after suspend: %w", cacheErr)
		}
	}
	return changed, prior, nil
}

// RestorePurchaseState puts a purchase back to a previously captured status and
// expiry. It is the inverse of Revoke/Suspend and is idempotent: restoring a
// purchase that already holds the target status is a no-op.
//
// Restore is intentionally unconditional on the current status rather than
// compare-and-set from 'revoked'. A rollback must succeed regardless of which
// withdrawal path ran (revoke or suspend), and the caller supplies the exact
// prior state, so there is no ambiguity about the target.
func (s *SubscriptionService) RestorePurchaseState(ctx context.Context, prior *SharedPurchaseRecord) (changed bool, err error) {
	if s == nil || s.entClient == nil {
		return false, errors.New("subscription service is unavailable")
	}
	if prior == nil || prior.ID <= 0 {
		return false, ErrSharedPurchaseNotFound
	}
	current, err := s.getPurchaseByID(ctx, prior.ID)
	if err != nil {
		return false, err
	}
	if current.Status == prior.Status && current.ExpiresAt.Equal(prior.ExpiresAt) {
		return false, nil
	}
	affected, err := s.entClient.SubscriptionPurchase.Update().
		Where(subscriptionpurchase.IDEQ(prior.ID)).
		SetStatus(prior.Status).
		SetExpiresAt(prior.ExpiresAt).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("restore purchase %d state: %w", prior.ID, err)
	}
	if affected > 0 {
		if cacheErr := s.invalidatePurchaseCaches(ctx, prior); cacheErr != nil {
			return true, fmt.Errorf("invalidate purchase caches after restore: %w", cacheErr)
		}
	}
	return affected > 0, nil
}

func (s *SubscriptionService) getPurchaseByID(ctx context.Context, purchaseID int64) (*SharedPurchaseRecord, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if purchaseID <= 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	purchase, err := s.entClient.SubscriptionPurchase.Get(ctx, purchaseID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSharedPurchaseNotFound
		}
		return nil, err
	}
	return sharedPurchaseRecordFromEntity(purchase), nil
}

// EnsurePurchaseForPlanOrder creates (or returns) the purchase for a plan-backed
// order. It routes through the transaction-aware creation path when the caller
// already owns a transaction, and otherwise uses the ent client directly so the
// call does not depend on a separately wired *sql.DB.
//
// Idempotency is inherited from CreateSharedPurchaseFromPlan, which resolves an
// existing row for the same (source, source_id) instead of inserting a second.
func (s *SubscriptionService) EnsurePurchaseForPlanOrder(ctx context.Context, userID, planID int64, source string, sourceID *int64) (*SharedSubscriptionEntitlement, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return s.createSharedPurchaseWithClient(ctx, tx.Client(), userID, planID, source, sourceID)
	}
	return s.createSharedPurchaseWithClient(ctx, s.entClient, userID, planID, source, sourceID)
}

// legacyPurchaseHasOrderNote reports whether a migrated legacy purchase carries
// the "payment order <id>" note the legacy fulfill path wrote. Evidence is
// checked on both surfaces migration 233 preserved it on: the purchase notes
// column and the snapshot.legacy_notes copy of the original row.
func legacyPurchaseHasOrderNote(purchase *dbent.SubscriptionPurchase, orderID int64) bool {
	if purchase == nil {
		return false
	}
	orderNote := paymentSubscriptionOrderNote(orderID)
	if hasPaymentSubscriptionOrderNote(purchase.Notes, orderNote) {
		return true
	}
	if raw, ok := purchase.Snapshot["legacy_notes"].(string); ok && hasPaymentSubscriptionOrderNote(raw, orderNote) {
		return true
	}
	return false
}

// ResolveLegacyPurchaseForPaymentOrder resolves the migrated purchase that a
// historical (pre-purchase-model) subscription order granted. Migration 233
// copied every frozen user_subscriptions row into subscription_purchases with
// source='legacy_user_subscription' and wrote the immutable group snapshot into
// subscription_purchase_groups, so candidates are narrowed by the order's
// user/group and then pinned by the legacy order note when it exists.
//
// Resolution is deliberately deterministic and safe:
//   - A single candidate for (user, group) is unambiguous: it cannot be confused
//     with an unrelated entitlement.
//   - Several candidates require the "payment order <id>" note (notes or
//     snapshot.legacy_notes) to single exactly one out.
//   - Anything else returns ErrSharedPurchaseAmbiguous so the refund caller
//     fails closed with an actionable error before touching entitlement.
//
// Returns ErrSharedPurchaseNotFound when no migrated purchase covers the pair.
func (s *SubscriptionService) ResolveLegacyPurchaseForPaymentOrder(ctx context.Context, userID, groupID, orderID int64) (*SharedPurchaseRecord, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if userID <= 0 || groupID <= 0 || orderID <= 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	candidates, err := s.entClient.SubscriptionPurchase.Query().
		Where(
			subscriptionpurchase.SourceEQ(PurchaseSourceLegacyUserSubscription),
			subscriptionpurchase.UserIDEQ(userID),
			subscriptionpurchase.HasGroupsWith(subscriptionpurchasegroup.GroupIDEQ(groupID)),
		).
		Order(subscriptionpurchase.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	switch len(candidates) {
	case 0:
		return nil, ErrSharedPurchaseNotFound
	case 1:
		return sharedPurchaseRecordFromEntity(candidates[0]), nil
	}
	var evidenced []*dbent.SubscriptionPurchase
	for _, c := range candidates {
		if legacyPurchaseHasOrderNote(c, orderID) {
			evidenced = append(evidenced, c)
		}
	}
	if len(evidenced) == 1 {
		return sharedPurchaseRecordFromEntity(evidenced[0]), nil
	}
	return nil, fmt.Errorf(
		"%w: %d legacy purchases match (user=%d, group=%d, order=%d), %d carry the order note",
		ErrSharedPurchaseAmbiguous, len(candidates), userID, groupID, orderID, len(evidenced),
	)
}

// DeductLegacyPurchaseDays applies the historical day-deduction semantics to a
// migrated legacy_user_subscription purchase: subtract the refunded days from
// expires_at and, if that would push expiry into the past, revoke the purchase.
// The pre-deduction status/expiry are returned so a gateway failure or
// pending-refund reversal can restore the exact prior state.
//
// A purchase that is not currently granting entitlement (non-active status, or
// already expired) is left untouched: deduction would change nothing real and
// only risk diverging the snapshot from its frozen legacy source. changed=false
// makes the withdrawal idempotent across refund retries.
func (s *SubscriptionService) DeductLegacyPurchaseDays(ctx context.Context, purchaseID int64, days int) (changed bool, prior *SharedPurchaseRecord, err error) {
	prior, err = s.getPurchaseByID(ctx, purchaseID)
	if err != nil {
		return false, nil, err
	}
	if prior == nil {
		return false, nil, ErrSharedPurchaseNotFound
	}
	now := time.Now().UTC()
	if days <= 0 || prior.Status != PurchaseStatusActive || !prior.ExpiresAt.After(now) {
		return false, prior, nil
	}
	newExpires := prior.ExpiresAt.AddDate(0, 0, -days)
	if newExpires.After(now) {
		changed, err = s.setPurchaseStatusFrom(ctx, purchaseID, PurchaseStatusActive, PurchaseStatusActive, &newExpires)
	} else {
		// Day deduction would expire the purchase — withdraw it entirely, pulling
		// expiry back to now so the value shown to users matches the refund.
		changed, err = s.setPurchaseStatusFrom(ctx, purchaseID, PurchaseStatusActive, PurchaseStatusRevoked, &now)
	}
	if err != nil {
		return false, prior, err
	}
	if changed {
		if cacheErr := s.invalidatePurchaseCaches(ctx, prior); cacheErr != nil {
			return changed, prior, fmt.Errorf("invalidate purchase caches after legacy day deduction: %w", cacheErr)
		}
	}
	return changed, prior, nil
}
