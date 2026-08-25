package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionpurchase"
)

// SharedSubscriptionEntitlement is the request-scoped snapshot selected for a
// group. It is deliberately separate from UserSubscription: the latter is the
// legacy one-group subscription record.
type SharedSubscriptionEntitlement struct {
	ID                     int64
	UserID                 int64
	GroupID                int64
	Name                   string
	TierCode               string
	StartsAt               time.Time
	ExpiresAt              time.Time
	Status                 string
	ConcurrencyEntitlement int
	LifetimeQuotaUSD       float64
	DailyQuotaUSD          float64
	WeeklyQuotaUSD         float64
	MonthlyQuotaUSD        float64
	LifetimeUsageUSD       float64
	DailyUsageUSD          float64
	WeeklyUsageUSD         float64
	MonthlyUsageUSD        float64
	BalanceTopupEnabled    bool
}

type SharedSubscriptionGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// AsLegacySubscription is a compatibility adapter for existing gateway handlers.
// Purchase identity is carried explicitly and never encoded as a synthetic legacy ID.
func (s *SharedSubscriptionEntitlement) AsLegacySubscription(group *Group) *UserSubscription {
	if s == nil {
		return nil
	}
	var daily, weekly, monthly *float64
	if s.DailyQuotaUSD > 0 {
		daily = &s.DailyQuotaUSD
	}
	if s.WeeklyQuotaUSD > 0 {
		weekly = &s.WeeklyQuotaUSD
	}
	if s.MonthlyQuotaUSD > 0 {
		monthly = &s.MonthlyQuotaUSD
	}
	adaptedGroup := group
	if group != nil {
		copy := *group
		copy.SubscriptionType = SubscriptionTypeSubscription
		copy.DailyLimitUSD, copy.WeeklyLimitUSD, copy.MonthlyLimitUSD = daily, weekly, monthly
		adaptedGroup = &copy
	}
	purchaseID := s.ID
	return &UserSubscription{
		SubscriptionPurchaseID: &purchaseID,
		UserID:                 s.UserID,
		GroupID:                s.GroupID,
		StartsAt:               s.StartsAt,
		ExpiresAt:              s.ExpiresAt,
		Status:                 SubscriptionStatusActive,
		DailyUsageUSD:          s.DailyUsageUSD,
		WeeklyUsageUSD:         s.WeeklyUsageUSD,
		MonthlyUsageUSD:        s.MonthlyUsageUSD,
		Group:                  adaptedGroup,
		CreatedAt:              s.StartsAt,
		UpdatedAt:              time.Now().UTC(),
	}
}

func (s *SubscriptionService) ValidateSharedPurchase(purchase *SharedSubscriptionEntitlement, additionalCost float64) error {
	if purchase == nil || !purchase.ExpiresAt.After(time.Now()) {
		return ErrSharedSubscriptionNotFound
	}
	if purchase.LifetimeQuotaUSD > 0 && purchase.LifetimeUsageUSD+additionalCost > purchase.LifetimeQuotaUSD {
		return ErrMonthlyLimitExceeded
	}
	if purchase.DailyQuotaUSD > 0 && purchase.DailyUsageUSD+additionalCost > purchase.DailyQuotaUSD {
		return ErrDailyLimitExceeded
	}
	if purchase.WeeklyQuotaUSD > 0 && purchase.WeeklyUsageUSD+additionalCost > purchase.WeeklyQuotaUSD {
		return ErrWeeklyLimitExceeded
	}
	if purchase.MonthlyQuotaUSD > 0 && purchase.MonthlyUsageUSD+additionalCost > purchase.MonthlyQuotaUSD {
		return ErrMonthlyLimitExceeded
	}
	return nil
}

var ErrSharedSubscriptionNotFound = errors.New("shared subscription not found")

// GetActiveSharedSubscriptionForGroup returns the earliest expiring active
// purchase that authorizes the requested group.
func (s *SubscriptionService) GetActiveSharedSubscriptionForGroup(ctx context.Context, userID, groupID int64) (*SharedSubscriptionEntitlement, error) {
	if s == nil || s.sqlDB == nil || userID <= 0 || groupID <= 0 {
		return nil, ErrSharedSubscriptionNotFound
	}
	// Keep rolling quota windows consistent with the legacy subscription
	// semantics before reading the entitlement used for this request.
	_, _ = s.sqlDB.ExecContext(ctx, `
		UPDATE subscription_purchases
		SET daily_usage_usd = CASE
				WHEN daily_window_start IS NULL OR daily_window_start < date_trunc('day', NOW())
				THEN 0 ELSE daily_usage_usd END,
			daily_window_start = CASE
				WHEN daily_window_start IS NULL OR daily_window_start < date_trunc('day', NOW())
				THEN date_trunc('day', NOW()) ELSE daily_window_start END,
			weekly_usage_usd = CASE
				WHEN weekly_window_start IS NULL OR weekly_window_start < NOW() - INTERVAL '7 days'
				THEN 0 ELSE weekly_usage_usd END,
			weekly_window_start = CASE
				WHEN weekly_window_start IS NULL OR weekly_window_start < NOW() - INTERVAL '7 days'
				THEN NOW() ELSE weekly_window_start END,
			monthly_usage_usd = CASE
				WHEN monthly_window_start IS NULL OR monthly_window_start < NOW() - INTERVAL '30 days'
				THEN 0 ELSE monthly_usage_usd END,
			monthly_window_start = CASE
				WHEN monthly_window_start IS NULL OR monthly_window_start < NOW() - INTERVAL '30 days'
				THEN NOW() ELSE monthly_window_start END,
			updated_at = NOW()
		WHERE user_id = $1 AND status = 'active' AND starts_at <= NOW() AND expires_at > NOW()
	`, userID)
	row := s.sqlDB.QueryRowContext(ctx, `
		SELECT p.id, p.user_id, g.group_id, p.name, p.tier_code,
		       p.starts_at, p.expires_at, p.status,
		       p.concurrency_entitlement, p.lifetime_quota_usd,
		       p.daily_quota_usd, p.weekly_quota_usd, p.monthly_quota_usd,
		       p.lifetime_usage_usd, p.daily_usage_usd, p.weekly_usage_usd,
		       p.monthly_usage_usd, p.balance_topup_enabled
		FROM subscription_purchases p
		JOIN subscription_purchase_groups g ON g.purchase_id = p.id
		WHERE p.user_id = $1 AND g.group_id = $2
		  AND p.status = 'active'
		  AND p.starts_at <= NOW() AND p.expires_at > NOW()
		ORDER BY p.expires_at ASC, p.id ASC
		LIMIT 1
	`, userID, groupID)
	var out SharedSubscriptionEntitlement
	if err := row.Scan(
		&out.ID, &out.UserID, &out.GroupID, &out.Name, &out.TierCode,
		&out.StartsAt, &out.ExpiresAt, &out.Status,
		&out.ConcurrencyEntitlement, &out.LifetimeQuotaUSD,
		&out.DailyQuotaUSD, &out.WeeklyQuotaUSD, &out.MonthlyQuotaUSD,
		&out.LifetimeUsageUSD, &out.DailyUsageUSD, &out.WeeklyUsageUSD,
		&out.MonthlyUsageUSD, &out.BalanceTopupEnabled,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSharedSubscriptionNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListActiveSharedSubscriptions returns all active purchases for display and
// concurrency aggregation.
func (s *SubscriptionService) ListActiveSharedSubscriptions(ctx context.Context, userID int64) ([]SharedSubscriptionEntitlement, error) {
	if s == nil || s.sqlDB == nil || userID <= 0 {
		return []SharedSubscriptionEntitlement{}, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, user_id, name, tier_code, starts_at, expires_at, status,
		       concurrency_entitlement, lifetime_quota_usd, daily_quota_usd,
		       weekly_quota_usd, monthly_quota_usd, lifetime_usage_usd,
		       daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
		       balance_topup_enabled
		FROM subscription_purchases
		WHERE user_id = $1 AND status = 'active'
		  AND starts_at <= NOW() AND expires_at > NOW()
		ORDER BY expires_at ASC, id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SharedSubscriptionEntitlement
	for rows.Next() {
		var item SharedSubscriptionEntitlement
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Name, &item.TierCode,
			&item.StartsAt, &item.ExpiresAt, &item.Status,
			&item.ConcurrencyEntitlement, &item.LifetimeQuotaUSD,
			&item.DailyQuotaUSD, &item.WeeklyQuotaUSD, &item.MonthlyQuotaUSD,
			&item.LifetimeUsageUSD, &item.DailyUsageUSD, &item.WeeklyUsageUSD,
			&item.MonthlyUsageUSD, &item.BalanceTopupEnabled,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListActiveSharedSubscriptionsForGroup returns all active purchases that
// authorize one group, ordered by earliest expiry for deterministic allocation.
func (s *SubscriptionService) ListActiveSharedSubscriptionsForGroup(ctx context.Context, userID, groupID int64) ([]SharedSubscriptionEntitlement, error) {
	if s == nil || s.sqlDB == nil || userID <= 0 || groupID <= 0 {
		return []SharedSubscriptionEntitlement{}, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT p.id, p.user_id, p.name, p.tier_code, p.starts_at, p.expires_at, p.status,
		       p.concurrency_entitlement, p.lifetime_quota_usd, p.daily_quota_usd,
		       p.weekly_quota_usd, p.monthly_quota_usd, p.lifetime_usage_usd,
		       p.daily_usage_usd, p.weekly_usage_usd, p.monthly_usage_usd,
		       p.balance_topup_enabled
		FROM subscription_purchases p
		JOIN subscription_purchase_groups g ON g.purchase_id = p.id
		WHERE p.user_id = $1 AND g.group_id = $2 AND p.status = 'active'
		  AND p.starts_at <= NOW() AND p.expires_at > NOW()
		ORDER BY p.expires_at ASC, p.id ASC
	`, userID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SharedSubscriptionEntitlement
	for rows.Next() {
		var item SharedSubscriptionEntitlement
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Name, &item.TierCode,
			&item.StartsAt, &item.ExpiresAt, &item.Status,
			&item.ConcurrencyEntitlement, &item.LifetimeQuotaUSD,
			&item.DailyQuotaUSD, &item.WeeklyQuotaUSD, &item.MonthlyQuotaUSD,
			&item.LifetimeUsageUSD, &item.DailyUsageUSD, &item.WeeklyUsageUSD,
			&item.MonthlyUsageUSD, &item.BalanceTopupEnabled,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListActiveSharedGroupIDs returns the groups authorized by any active shared
// purchase. It is intentionally a single query because this method is used by
// the API key creation page.
func (s *SubscriptionService) ListActiveSharedGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	if s == nil || s.sqlDB == nil || userID <= 0 {
		return []int64{}, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT DISTINCT g.group_id
		FROM subscription_purchase_groups g
		JOIN subscription_purchases p ON p.id = g.purchase_id
		WHERE p.user_id = $1 AND p.status = 'active'
		  AND p.starts_at <= NOW() AND p.expires_at > NOW()
		ORDER BY g.group_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		out = append(out, groupID)
	}
	return out, rows.Err()
}

func (s *SubscriptionService) ListSharedSubscriptionGroups(ctx context.Context, purchaseID int64) ([]SharedSubscriptionGroup, error) {
	if s == nil || s.sqlDB == nil || purchaseID <= 0 {
		return []SharedSubscriptionGroup{}, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT group_id, group_name, platform
		FROM subscription_purchase_groups
		WHERE purchase_id = $1
		ORDER BY group_id
	`, purchaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []SharedSubscriptionGroup
	for rows.Next() {
		var item SharedSubscriptionGroup
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform); err != nil {
			return nil, err
		}
		groups = append(groups, item)
	}
	return groups, rows.Err()
}

// SetSharedSubscriptionBalanceTopup toggles the per-purchase balance fallback.
func (s *SubscriptionService) SetSharedSubscriptionBalanceTopup(ctx context.Context, userID, purchaseID int64, enabled bool) error {
	if s == nil || s.sqlDB == nil {
		return ErrSharedSubscriptionNotFound
	}
	res, err := s.sqlDB.ExecContext(ctx,
		"UPDATE subscription_purchases SET balance_topup_enabled = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3 AND status = 'active'",
		enabled, purchaseID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSharedSubscriptionNotFound
	}
	return nil
}

// CreateSharedPurchaseFromPlan creates an immutable plan snapshot and group
// grants. It is idempotent for a source/source_id pair.
func (s *SubscriptionService) CreateSharedPurchaseFromPlan(ctx context.Context, userID, planID int64, source string, sourceID *int64) (*SharedSubscriptionEntitlement, error) {
	if s == nil || s.entClient == nil {
		return nil, errors.New("subscription service is unavailable")
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return s.createSharedPurchaseWithClient(ctx, tx.Client(), userID, planID, source, sourceID)
	}
	if s.sqlDB == nil {
		return nil, errors.New("subscription service SQL database is unavailable")
	}
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		return nil, err
	}
	ids, err := s.listPlanGroupIDsWithClient(ctx, s.entClient, plan.ID, plan.GroupID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []int64{plan.GroupID}
	}
	now := time.Now().UTC()
	expires := now.AddDate(0, 0, psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit))
	snapshot, _ := json.Marshal(map[string]any{
		"plan_id": plan.ID, "group_ids": ids, "tier_code": plan.TierCode,
		"price": plan.Price, "currency": plan.Currency,
		"validity_days":      psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit),
		"lifetime_quota_usd": plan.LifetimeQuotaUsd, "daily_quota_usd": plan.DailyQuotaUsd,
		"weekly_quota_usd": plan.WeeklyQuotaUsd, "monthly_quota_usd": plan.MonthlyQuotaUsd,
		"concurrency_entitlement": plan.ConcurrencyEntitlement,
	})
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if sourceID != nil {
		var existingID int64
		err := tx.QueryRowContext(ctx,
			"SELECT id FROM subscription_purchases WHERE source = $1 AND source_id = $2 LIMIT 1",
			source, *sourceID).Scan(&existingID)
		if err == nil {
			_ = tx.Rollback()
			return s.getSharedPurchaseByID(ctx, existingID, plan.GroupID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	var purchaseID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO subscription_purchases
		  (user_id, plan_id, name, tier_code, price, currency, starts_at, expires_at,
		   status, concurrency_entitlement, lifetime_quota_usd, daily_quota_usd,
		   weekly_quota_usd, monthly_quota_usd, source, source_id, snapshot)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id
	`, userID, plan.ID, plan.Name, plan.TierCode, plan.Price, plan.Currency, now, expires,
		plan.ConcurrencyEntitlement, plan.LifetimeQuotaUsd, plan.DailyQuotaUsd,
		plan.WeeklyQuotaUsd, plan.MonthlyQuotaUsd, source, sourceID, snapshot).Scan(&purchaseID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var name, platform string
		if err := tx.QueryRowContext(ctx, "SELECT name, platform FROM groups WHERE id = $1", id).Scan(&name, &platform); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO subscription_purchase_groups (purchase_id, group_id, group_name, platform) VALUES ($1,$2,$3,$4)",
			purchaseID, id, name, platform); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getSharedPurchaseByID(ctx, purchaseID, plan.GroupID)
}

// createSharedPurchaseWithClient is the transaction-aware path used by
// payment and redeem fulfillment. It keeps the entitlement and the source
// record in the same database transaction as the caller.
func (s *SubscriptionService) createSharedPurchaseWithClient(ctx context.Context, client *dbent.Client, userID, planID int64, source string, sourceID *int64) (*SharedSubscriptionEntitlement, error) {
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
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
		group, groupErr := client.Group.Get(ctx, id)
		if groupErr != nil || group.DeletedAt != nil {
			if groupErr != nil {
				return nil, groupErr
			}
			return nil, fmt.Errorf("subscription plan group %d no longer exists", id)
		}
		groupSnapshots = append(groupSnapshots, group)
	}

	if sourceID != nil {
		existing, queryErr := client.SubscriptionPurchase.Query().
			Where(subscriptionpurchase.SourceEQ(source), subscriptionpurchase.SourceIDEQ(*sourceID)).
			WithGroups().
			Only(ctx)
		if queryErr == nil {
			return sharedEntitlementFromEntity(existing, plan.GroupID)
		}
		if !dbent.IsNotFound(queryErr) {
			return nil, queryErr
		}
	}

	now := time.Now().UTC()
	validityDays := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	expires := now.AddDate(0, 0, validityDays)
	snapshot := map[string]interface{}{
		"plan_id": plan.ID, "group_ids": ids, "tier_code": plan.TierCode,
		"price": plan.Price, "currency": plan.Currency,
		"validity_days":      validityDays,
		"lifetime_quota_usd": plan.LifetimeQuotaUsd, "daily_quota_usd": plan.DailyQuotaUsd,
		"weekly_quota_usd": plan.WeeklyQuotaUsd, "monthly_quota_usd": plan.MonthlyQuotaUsd,
		"concurrency_entitlement": plan.ConcurrencyEntitlement,
	}
	purchase, err := client.SubscriptionPurchase.Create().
		SetUserID(userID).
		SetPlanID(plan.ID).
		SetName(plan.Name).
		SetTierCode(plan.TierCode).
		SetPrice(plan.Price).
		SetCurrency(plan.Currency).
		SetStartsAt(now).
		SetExpiresAt(expires).
		SetStatus("active").
		SetConcurrencyEntitlement(plan.ConcurrencyEntitlement).
		SetLifetimeQuotaUsd(plan.LifetimeQuotaUsd).
		SetDailyQuotaUsd(plan.DailyQuotaUsd).
		SetWeeklyQuotaUsd(plan.WeeklyQuotaUsd).
		SetMonthlyQuotaUsd(plan.MonthlyQuotaUsd).
		SetSource(source).
		SetNillableSourceID(sourceID).
		SetSnapshot(snapshot).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	for _, group := range groupSnapshots {
		if _, err := client.SubscriptionPurchaseGroup.Create().
			SetPurchaseID(purchase.ID).
			SetGroupID(group.ID).
			SetGroupName(group.Name).
			SetPlatform(group.Platform).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	return sharedEntitlementFromEntity(purchase, plan.GroupID)
}

func sharedEntitlementFromEntity(purchase *dbent.SubscriptionPurchase, groupID int64) (*SharedSubscriptionEntitlement, error) {
	if purchase == nil {
		return nil, ErrSharedSubscriptionNotFound
	}
	return &SharedSubscriptionEntitlement{
		ID: purchase.ID, UserID: purchase.UserID, GroupID: groupID,
		Name: purchase.Name, TierCode: purchase.TierCode,
		StartsAt: purchase.StartsAt, ExpiresAt: purchase.ExpiresAt, Status: purchase.Status,
		ConcurrencyEntitlement: purchase.ConcurrencyEntitlement,
		LifetimeQuotaUSD:       purchase.LifetimeQuotaUsd, DailyQuotaUSD: purchase.DailyQuotaUsd,
		WeeklyQuotaUSD: purchase.WeeklyQuotaUsd, MonthlyQuotaUSD: purchase.MonthlyQuotaUsd,
		LifetimeUsageUSD: purchase.LifetimeUsageUsd, DailyUsageUSD: purchase.DailyUsageUsd,
		WeeklyUsageUSD: purchase.WeeklyUsageUsd, MonthlyUsageUSD: purchase.MonthlyUsageUsd,
		BalanceTopupEnabled: purchase.BalanceTopupEnabled,
	}, nil
}

func (s *SubscriptionService) getSharedPurchaseByID(ctx context.Context, purchaseID, groupID int64) (*SharedSubscriptionEntitlement, error) {
	row := s.sqlDB.QueryRowContext(ctx, `
		SELECT p.id, p.user_id, g.group_id, p.name, p.tier_code, p.starts_at, p.expires_at,
		       p.status, p.concurrency_entitlement, p.lifetime_quota_usd, p.daily_quota_usd,
		       p.weekly_quota_usd, p.monthly_quota_usd, p.lifetime_usage_usd, p.daily_usage_usd,
		       p.weekly_usage_usd, p.monthly_usage_usd, p.balance_topup_enabled
		FROM subscription_purchases p
		JOIN subscription_purchase_groups g ON g.purchase_id = p.id
		WHERE p.id = $1 AND g.group_id = $2
	`, purchaseID, groupID)
	var item SharedSubscriptionEntitlement
	err := row.Scan(&item.ID, &item.UserID, &item.GroupID, &item.Name, &item.TierCode,
		&item.StartsAt, &item.ExpiresAt, &item.Status, &item.ConcurrencyEntitlement,
		&item.LifetimeQuotaUSD, &item.DailyQuotaUSD, &item.WeeklyQuotaUSD, &item.MonthlyQuotaUSD,
		&item.LifetimeUsageUSD, &item.DailyUsageUSD, &item.WeeklyUsageUSD, &item.MonthlyUsageUSD,
		&item.BalanceTopupEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSharedSubscriptionNotFound
	}
	return &item, err
}

func (s *SubscriptionService) listPlanGroupIDs(ctx context.Context, planID, legacyGroupID int64) ([]int64, error) {
	return s.listPlanGroupIDsWithClient(ctx, nil, planID, legacyGroupID)
}

func (s *SubscriptionService) listPlanGroupIDsWithClient(ctx context.Context, client *dbent.Client, planID, legacyGroupID int64) ([]int64, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case client != nil:
		rows, err = client.QueryContext(ctx,
			"SELECT group_id FROM subscription_plan_groups WHERE plan_id = $1 ORDER BY group_id",
			planID,
		)
	case s.sqlDB != nil:
		rows, err = s.sqlDB.QueryContext(ctx,
			"SELECT group_id FROM subscription_plan_groups WHERE plan_id = $1 ORDER BY group_id",
			planID,
		)
	default:
		return []int64{legacyGroupID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load subscription plan groups: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, 1)
	seen := make(map[int64]struct{}, 1)
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(legacyGroupID)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		add(id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("subscription plan has no groups")
	}
	return ids, nil
}
