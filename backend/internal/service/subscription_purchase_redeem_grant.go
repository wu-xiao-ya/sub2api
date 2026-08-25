package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionpurchase"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// RedeemPurchaseSource is the source discriminator for purchases created by a
// redeemed subscription redeem code. Combined with source_id = redeem code ID
// it is the stable, idempotency key (uq_subscription_purchases_source_id).
const RedeemPurchaseSource = "redeem_code"

// GrantRedeemCodePurchase creates (or returns) the idempotent immutable purchase
// snapshot for a redeemed subscription redeem code.
//
// Plan-backed codes (PlanID set) authorize every group in the plan — single or
// multi-group — on any supported group platform (not just OpenAI). Each group
// must exist, must not be soft-deleted, and must be active.
//
// Legacy group-only codes (PlanID nil, GroupID set) produce a single-group
// snapshot that copies the group's quota limits, uses the code's specified
// validity, and sets concurrency_entitlement = 0 (inherit user concurrency).
// No user_subscriptions row is written from either path: entitlement is fully
// represented by the purchase snapshot. Negative-validity legacy codes are
// rejected because purchase entitlements are immutable snapshots and can no
// longer "reduce" an existing native subscription.
func (s *SubscriptionService) GrantRedeemCodePurchase(ctx context.Context, userID int64, code *RedeemCode) (*SharedSubscriptionEntitlement, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if code == nil {
		return nil, infraerrors.BadRequest("REDEEM_CODE_INVALID", "subscription redeem code is required")
	}
	if code.ValidityDays < 0 {
		return nil, infraerrors.BadRequest("REDEEM_CODE_INVALID", "subscription redeem codes cannot reduce validity")
	}
	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	if code.PlanID != nil && *code.PlanID > 0 {
		return s.grantPlanRedeemPurchase(ctx, client, userID, code)
	}
	if code.GroupID != nil && *code.GroupID > 0 {
		return s.grantLegacyGroupRedeemPurchase(ctx, client, userID, code)
	}
	return nil, infraerrors.BadRequest("REDEEM_CODE_INVALID", "invalid subscription redeem code: missing plan_id or group_id")
}

// grantPlanRedeemPurchase resolves and validates the plan's groups then creates
// an idempotent purchase snapshot carrying the plan's quota/validity/concurrency.
func (s *SubscriptionService) grantPlanRedeemPurchase(ctx context.Context, client *dbent.Client, userID int64, code *RedeemCode) (*SharedSubscriptionEntitlement, error) {
	plan, err := client.SubscriptionPlan.Get(ctx, *code.PlanID)
	if err != nil {
		return nil, err
	}
	ids, err := s.listPlanGroupIDsWithClient(ctx, client, plan.ID, plan.GroupID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("subscription plan has no groups")
	}

	groupSnapshots := make([]*dbent.Group, 0, len(ids))
	for _, id := range ids {
		g, gErr := client.Group.Get(ctx, id)
		if gErr != nil {
			return nil, gErr
		}
		if g.DeletedAt != nil {
			return nil, fmt.Errorf("subscription plan group %d no longer exists", id)
		}
		if g.Status != StatusActive {
			return nil, fmt.Errorf("subscription plan group %d is not active", id)
		}
		groupSnapshots = append(groupSnapshots, g)
	}

	sourceID := code.ID
	if existing, findErr := client.SubscriptionPurchase.Query().
		Where(
			subscriptionpurchase.SourceEQ(RedeemPurchaseSource),
			subscriptionpurchase.SourceIDEQ(sourceID),
		).
		Only(ctx); findErr == nil {
		return sharedEntitlementFromEntity(existing, plan.GroupID)
	} else if !dbent.IsNotFound(findErr) {
		return nil, findErr
	}

	now := time.Now().UTC()
	validityDays := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	expires := now.AddDate(0, 0, validityDays)
	snapshot := map[string]interface{}{
		"plan_id": plan.ID, "group_ids": ids, "tier_code": plan.TierCode,
		"price": plan.Price, "currency": plan.Currency,
		"validity_days":           validityDays,
		"lifetime_quota_usd":      plan.LifetimeQuotaUsd,
		"daily_quota_usd":         plan.DailyQuotaUsd,
		"weekly_quota_usd":        plan.WeeklyQuotaUsd,
		"monthly_quota_usd":       plan.MonthlyQuotaUsd,
		"concurrency_entitlement": plan.ConcurrencyEntitlement,
	}
	purchase, createErr := client.SubscriptionPurchase.Create().
		SetUserID(userID).
		SetNillablePlanID(&plan.ID).
		SetName(plan.Name).
		SetTierCode(plan.TierCode).
		SetPrice(plan.Price).
		SetCurrency(plan.Currency).
		SetStartsAt(now).
		SetExpiresAt(expires).
		SetStatus(PurchaseStatusActive).
		SetConcurrencyEntitlement(plan.ConcurrencyEntitlement).
		SetLifetimeQuotaUsd(plan.LifetimeQuotaUsd).
		SetDailyQuotaUsd(plan.DailyQuotaUsd).
		SetWeeklyQuotaUsd(plan.WeeklyQuotaUsd).
		SetMonthlyQuotaUsd(plan.MonthlyQuotaUsd).
		SetSource(RedeemPurchaseSource).
		SetNillableSourceID(&sourceID).
		SetSnapshot(snapshot).
		Save(ctx)
	if createErr != nil {
		return nil, createErr
	}
	for _, g := range groupSnapshots {
		if _, groupErr := client.SubscriptionPurchaseGroup.Create().
			SetPurchaseID(purchase.ID).
			SetGroupID(g.ID).
			SetGroupName(g.Name).
			SetPlatform(g.Platform).
			Save(ctx); groupErr != nil {
			return nil, groupErr
		}
	}
	return sharedEntitlementFromEntity(purchase, plan.GroupID)
}

// grantLegacyGroupRedeemPurchase creates a single-group purchase snapshot from a
// historical group-only redeem code, copying the group's quota limits and using
// the code's specified validity with concurrency_entitlement = 0 (inherit user
// concurrency). Idempotent per (source, source_id); never writes user_subscriptions.
func (s *SubscriptionService) grantLegacyGroupRedeemPurchase(ctx context.Context, client *dbent.Client, userID int64, code *RedeemCode) (*SharedSubscriptionEntitlement, error) {
	g, err := client.Group.Get(ctx, *code.GroupID)
	if err != nil {
		return nil, err
	}
	if g.DeletedAt != nil {
		return nil, fmt.Errorf("group %d no longer exists", *code.GroupID)
	}
	if g.Status != StatusActive {
		return nil, fmt.Errorf("group %d is not active", *code.GroupID)
	}

	sourceID := code.ID
	if existing, findErr := client.SubscriptionPurchase.Query().
		Where(
			subscriptionpurchase.SourceEQ(RedeemPurchaseSource),
			subscriptionpurchase.SourceIDEQ(sourceID),
		).
		Only(ctx); findErr == nil {
		return sharedEntitlementFromEntity(existing, g.ID)
	} else if !dbent.IsNotFound(findErr) {
		return nil, findErr
	}

	validityDays := code.ValidityDays
	if validityDays <= 0 {
		validityDays = 30 // 历史默认值：正数增加
	}
	now := time.Now().UTC()
	expires := now.AddDate(0, 0, validityDays)

	var daily, weekly, monthly, lifetime float64
	if g.DailyLimitUsd != nil {
		daily = *g.DailyLimitUsd
	}
	if g.WeeklyLimitUsd != nil {
		weekly = *g.WeeklyLimitUsd
	}
	if g.MonthlyLimitUsd != nil {
		monthly = *g.MonthlyLimitUsd
	}
	snapshot := map[string]interface{}{
		"group_id":                g.ID,
		"group_name":              g.Name,
		"platform":                g.Platform,
		"validity_days":           validityDays,
		"lifetime_quota_usd":      lifetime,
		"daily_quota_usd":         daily,
		"weekly_quota_usd":        weekly,
		"monthly_quota_usd":       monthly,
		"concurrency_entitlement": 0,
	}
	purchase, createErr := client.SubscriptionPurchase.Create().
		SetUserID(userID).
		SetName(g.Name).
		SetTierCode(g.SubscriptionType).
		SetStartsAt(now).
		SetExpiresAt(expires).
		SetStatus(PurchaseStatusActive).
		SetConcurrencyEntitlement(0).
		SetLifetimeQuotaUsd(lifetime).
		SetDailyQuotaUsd(daily).
		SetWeeklyQuotaUsd(weekly).
		SetMonthlyQuotaUsd(monthly).
		SetSource(RedeemPurchaseSource).
		SetNillableSourceID(&sourceID).
		SetSnapshot(snapshot).
		Save(ctx)
	if createErr != nil {
		return nil, createErr
	}
	if _, groupErr := client.SubscriptionPurchaseGroup.Create().
		SetPurchaseID(purchase.ID).
		SetGroupID(g.ID).
		SetGroupName(g.Name).
		SetPlatform(g.Platform).
		Save(ctx); groupErr != nil {
		return nil, groupErr
	}
	return sharedEntitlementFromEntity(purchase, g.ID)
}
