package service

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

func normalizePlanTier(raw string) (string, error) {
	tier := strings.ToLower(strings.TrimSpace(raw))
	if tier == "" {
		return "standard", nil
	}
	switch tier {
	case "standard", "pro", "plus":
		return tier, nil
	default:
		return "", infraerrors.BadRequest("PLAN_TIER_INVALID", "tier_code must be standard, pro, or plus")
	}
}

func normalizePlanGroupIDs(primary int64, ids []int64) []int64 {
	out := make([]int64, 0, len(ids)+1)
	seen := make(map[int64]struct{}, len(ids)+1)
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(primary)
	for _, id := range ids {
		add(id)
	}
	return out
}

func (s *PaymentConfigService) validatePlanGroups(ctx context.Context, primary int64, requested []int64) ([]int64, error) {
	ids := normalizePlanGroupIDs(primary, requested)
	if len(ids) == 0 {
		return nil, infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "at least one group is required")
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load plan groups: %w", err)
	}
	byID := make(map[int64]*dbent.Group, len(groups))
	for _, item := range groups {
		byID[int64(item.ID)] = item
	}
	for _, id := range ids {
		item, ok := byID[id]
		if !ok || item.DeletedAt != nil {
			return nil, infraerrors.NotFound("PLAN_GROUP_NOT_FOUND", fmt.Sprintf("group %d not found", id))
		}
		if !strings.EqualFold(strings.TrimSpace(item.Platform), PlatformOpenAI) {
			return nil, infraerrors.BadRequest("PLAN_GROUP_PLATFORM_INVALID", fmt.Sprintf("group %d must use the openai platform", id))
		}
	}
	return ids, nil
}

func (s *PaymentConfigService) replacePlanGroups(ctx context.Context, planID int64, ids []int64) error {
	if s.sqlDB == nil {
		return fmt.Errorf("payment config SQL database is unavailable")
	}
	if _, err := s.sqlDB.ExecContext(ctx, "DELETE FROM subscription_plan_groups WHERE plan_id = $1", planID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.sqlDB.ExecContext(ctx,
			"INSERT INTO subscription_plan_groups (plan_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			planID, id,
		); err != nil {
			return err
		}
	}
	return nil
}

// ListPlanGroupIDs returns the administrator-selected group list. Older plans
// without rows fall back to their legacy group_id.
func (s *PaymentConfigService) ListPlanGroupIDs(ctx context.Context, plan *dbent.SubscriptionPlan) ([]int64, error) {
	if plan == nil {
		return nil, nil
	}
	if s.sqlDB == nil {
		return []int64{plan.GroupID}, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx,
		"SELECT group_id FROM subscription_plan_groups WHERE plan_id = $1 ORDER BY group_id",
		plan.ID)
	if err != nil {
		return []int64{plan.GroupID}, nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []int64{plan.GroupID}
	}
	return ids, nil
}

// HasExplicitPlanGroups distinguishes new multi-group plans from legacy plans.
// Legacy plans have no rows in subscription_plan_groups and continue using the
// original user_subscriptions fulfillment path.
func (s *PaymentConfigService) HasExplicitPlanGroups(ctx context.Context, planID int64) bool {
	if s == nil || s.sqlDB == nil || planID <= 0 {
		return false
	}
	var exists bool
	if err := s.sqlDB.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM subscription_plan_groups WHERE plan_id = $1)",
		planID,
	).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if req.GroupID != nil && *req.GroupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		if !seen[p.GroupID] {
			seen[p.GroupID] = true
			ids = append(ids, p.GroupID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			Name:               g.Name,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	groupIDs, err := s.validatePlanGroups(ctx, req.GroupID, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if err := validatePlanRequired(req.Name, groupIDs[0], req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice); err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	tier, err := normalizePlanTier(req.TierCode)
	if err != nil {
		return nil, err
	}
	b := s.entClient.SubscriptionPlan.Create().
		SetGroupID(groupIDs[0]).SetTierCode(tier).SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder).
		SetConcurrencyEntitlement(maxPlanInt(req.ConcurrencyEntitlement, 0)).
		SetLifetimeQuotaUsd(maxPlanFloat(req.LifetimeQuotaUSD, 0)).
		SetDailyQuotaUsd(maxPlanFloat(req.DailyQuotaUSD, 0)).
		SetWeeklyQuotaUsd(maxPlanFloat(req.WeeklyQuotaUSD, 0)).
		SetMonthlyQuotaUsd(maxPlanFloat(req.MonthlyQuotaUSD, 0))
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	plan, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.replacePlanGroups(ctx, int64(plan.ID), groupIDs); err != nil {
		_ = s.entClient.SubscriptionPlan.DeleteOneID(plan.ID).Exec(ctx)
		return nil, fmt.Errorf("save plan groups: %w", err)
	}
	return plan, nil
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	current, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	if req.GroupIDs != nil || req.GroupID != nil {
		primary := current.GroupID
		requested := []int64(nil)
		if req.GroupID != nil {
			primary = *req.GroupID
		}
		if req.GroupIDs != nil {
			requested = *req.GroupIDs
		}
		ids, groupErr := s.validatePlanGroups(ctx, primary, requested)
		if groupErr != nil {
			return nil, groupErr
		}
		u.SetGroupID(ids[0])
		if err := s.replacePlanGroups(ctx, id, ids); err != nil {
			return nil, fmt.Errorf("save plan groups: %w", err)
		}
	}
	if req.GroupID != nil {
		u.SetGroupID(*req.GroupID)
	}
	if req.TierCode != nil {
		tier, tierErr := normalizePlanTier(*req.TierCode)
		if tierErr != nil {
			return nil, tierErr
		}
		u.SetTierCode(tier)
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	if req.ConcurrencyEntitlement != nil {
		u.SetConcurrencyEntitlement(maxPlanInt(*req.ConcurrencyEntitlement, 0))
	}
	if req.LifetimeQuotaUSD != nil {
		u.SetLifetimeQuotaUsd(maxPlanFloat(*req.LifetimeQuotaUSD, 0))
	}
	if req.DailyQuotaUSD != nil {
		u.SetDailyQuotaUsd(maxPlanFloat(*req.DailyQuotaUSD, 0))
	}
	if req.WeeklyQuotaUSD != nil {
		u.SetWeeklyQuotaUsd(maxPlanFloat(*req.WeeklyQuotaUSD, 0))
	}
	if req.MonthlyQuotaUSD != nil {
		u.SetMonthlyQuotaUsd(maxPlanFloat(*req.MonthlyQuotaUSD, 0))
	}
	return u.Save(ctx)
}

func maxPlanInt(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}

func maxPlanFloat(v, floor float64) float64 {
	if v < floor {
		return floor
	}
	return v
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}
