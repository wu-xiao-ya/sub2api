package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AdminPurchaseRecord is the management view of a purchase snapshot. It is
// intentionally independent from UserSubscription so the admin UI cannot
// accidentally revive the frozen native subscription model.
type AdminPurchaseRecord struct {
	ID                     int64                     `json:"id"`
	UserID                 int64                     `json:"user_id"`
	UserEmail              string                    `json:"user_email"`
	Username               string                    `json:"username"`
	PlanID                 *int64                    `json:"plan_id,omitempty"`
	Name                   string                    `json:"name"`
	TierCode               string                    `json:"tier_code"`
	Price                  float64                   `json:"price"`
	Currency               string                    `json:"currency"`
	StartsAt               time.Time                 `json:"starts_at"`
	ExpiresAt              time.Time                 `json:"expires_at"`
	Status                 string                    `json:"status"`
	ConcurrencyEntitlement int                       `json:"concurrency_entitlement"`
	LifetimeQuotaUSD       float64                   `json:"lifetime_quota_usd"`
	DailyQuotaUSD          float64                   `json:"daily_quota_usd"`
	WeeklyQuotaUSD         float64                   `json:"weekly_quota_usd"`
	MonthlyQuotaUSD        float64                   `json:"monthly_quota_usd"`
	LifetimeUsageUSD       float64                   `json:"lifetime_usage_usd"`
	DailyUsageUSD          float64                   `json:"daily_usage_usd"`
	WeeklyUsageUSD         float64                   `json:"weekly_usage_usd"`
	MonthlyUsageUSD        float64                   `json:"monthly_usage_usd"`
	BalanceTopupEnabled    bool                      `json:"balance_topup_enabled"`
	Source                 string                    `json:"source"`
	SourceID               *int64                    `json:"source_id,omitempty"`
	Notes                  string                    `json:"notes"`
	Groups                 []SharedSubscriptionGroup `json:"groups"`
}

type AdminPurchaseListQuery struct {
	Page     int
	PageSize int
	UserID   *int64
	PlanID   *int64
	Status   string
	Platform string
	Keyword  string
}

type AdminPurchaseListResult struct {
	Items    []AdminPurchaseRecord `json:"items"`
	Total    int                   `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// ListPurchaseRecords lists purchase snapshots and their immutable group
// authorizations. All filters are applied before pagination.
func (s *SubscriptionService) ListPurchaseRecords(ctx context.Context, q AdminPurchaseListQuery) (*AdminPurchaseListResult, error) {
	if s == nil || s.sqlDB == nil {
		return nil, errors.New("subscription service SQL database is unavailable")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 20
	}

	where, args := adminPurchaseWhere(q)
	countQuery := `SELECT COUNT(*)
		FROM subscription_purchases p
		LEFT JOIN users u ON u.id = p.user_id
		` + where
	var total int
	if err := s.sqlDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset := (q.Page - 1) * q.PageSize
	listArgs := append(append([]any{}, args...), q.PageSize, offset)
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT p.id, p.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),
		       p.plan_id, p.name, p.tier_code, p.price, p.currency,
		       p.starts_at, p.expires_at, p.status, p.concurrency_entitlement,
		       p.lifetime_quota_usd, p.daily_quota_usd, p.weekly_quota_usd,
		       p.monthly_quota_usd, p.lifetime_usage_usd, p.daily_usage_usd,
		       p.weekly_usage_usd, p.monthly_usage_usd, p.balance_topup_enabled,
		       p.source, p.source_id, p.notes,
		       COALESCE((
		         SELECT jsonb_agg(jsonb_build_object(
		           'id', pg.group_id, 'name', pg.group_name, 'platform', pg.platform
		         ) ORDER BY pg.group_id)
		         FROM subscription_purchase_groups pg
		         WHERE pg.purchase_id = p.id
		       ), '[]'::jsonb) AS groups
		FROM subscription_purchases p
		LEFT JOIN users u ON u.id = p.user_id
		`+where+`
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), listArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminPurchaseRecord, 0)
	for rows.Next() {
		var item AdminPurchaseRecord
		var groupsJSON []byte
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.UserEmail, &item.Username,
			&item.PlanID, &item.Name, &item.TierCode, &item.Price, &item.Currency,
			&item.StartsAt, &item.ExpiresAt, &item.Status, &item.ConcurrencyEntitlement,
			&item.LifetimeQuotaUSD, &item.DailyQuotaUSD, &item.WeeklyQuotaUSD,
			&item.MonthlyQuotaUSD, &item.LifetimeUsageUSD, &item.DailyUsageUSD,
			&item.WeeklyUsageUSD, &item.MonthlyUsageUSD, &item.BalanceTopupEnabled,
			&item.Source, &item.SourceID, &item.Notes, &groupsJSON,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(groupsJSON, &item.Groups); err != nil {
			return nil, fmt.Errorf("decode purchase groups: %w", err)
		}
		if item.Groups == nil {
			item.Groups = []SharedSubscriptionGroup{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &AdminPurchaseListResult{Items: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

func adminPurchaseWhere(q AdminPurchaseListQuery) (string, []any) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 6)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if q.UserID != nil && *q.UserID > 0 {
		add("p.user_id = $%d", *q.UserID)
	}
	if q.PlanID != nil && *q.PlanID > 0 {
		add("p.plan_id = $%d", *q.PlanID)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		add("p.status = $%d", status)
	}
	if platform := strings.TrimSpace(q.Platform); platform != "" {
		add(`EXISTS (
			SELECT 1 FROM subscription_purchase_groups fpg
			WHERE fpg.purchase_id = p.id AND fpg.platform = $%d
		)`, platform)
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		args = append(args, keyword)
		placeholder := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			u.email ILIKE '%%' || $%d || '%%'
			OR u.username ILIKE '%%' || $%d || '%%'
			OR p.name ILIKE '%%' || $%d || '%%'
			OR CAST(p.id AS TEXT) = $%d
		)`, placeholder, placeholder, placeholder, placeholder))
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

// GrantPurchaseFromPlan creates a manual admin purchase with a full plan
// snapshot. It never writes user_subscriptions.
func (s *SubscriptionService) GrantPurchaseFromPlan(ctx context.Context, userID, planID int64, notes string) (*AdminPurchaseRecord, error) {
	purchase, err := s.CreateSharedPurchaseFromPlan(ctx, userID, planID, "admin_grant", nil)
	if err != nil {
		return nil, err
	}
	if notes != "" {
		_, err = s.sqlDB.ExecContext(ctx,
			"UPDATE subscription_purchases SET notes = $1, updated_at = NOW() WHERE id = $2",
			notes, purchase.ID)
		if err != nil {
			return nil, err
		}
	}
	return s.GetPurchaseRecord(ctx, purchase.ID)
}

func (s *SubscriptionService) BulkGrantPurchaseFromPlan(ctx context.Context, userIDs []int64, planID int64, notes string) (*AdminPurchaseBulkResult, error) {
	result := &AdminPurchaseBulkResult{Items: []AdminPurchaseRecord{}, Errors: []string{}}
	for _, userID := range userIDs {
		item, err := s.GrantPurchaseFromPlan(ctx, userID, planID, notes)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			continue
		}
		result.Items = append(result.Items, *item)
	}
	result.SuccessCount = len(result.Items)
	result.FailedCount = len(result.Errors)
	return result, nil
}

type AdminPurchaseBulkResult struct {
	SuccessCount int                   `json:"success_count"`
	FailedCount  int                   `json:"failed_count"`
	Items        []AdminPurchaseRecord `json:"items"`
	Errors       []string              `json:"errors"`
}

func (s *SubscriptionService) GetPurchaseRecord(ctx context.Context, purchaseID int64) (*AdminPurchaseRecord, error) {
	if purchaseID <= 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	// A direct filtered lookup avoids depending on pagination order.
	if s.sqlDB == nil {
		return nil, errors.New("subscription service SQL database is unavailable")
	}
	var item AdminPurchaseRecord
	var groupsJSON []byte
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT p.id, p.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),
		       p.plan_id, p.name, p.tier_code, p.price, p.currency, p.starts_at,
		       p.expires_at, p.status, p.concurrency_entitlement,
		       p.lifetime_quota_usd, p.daily_quota_usd, p.weekly_quota_usd,
		       p.monthly_quota_usd, p.lifetime_usage_usd, p.daily_usage_usd,
		       p.weekly_usage_usd, p.monthly_usage_usd, p.balance_topup_enabled,
		       p.source, p.source_id, p.notes,
		       COALESCE((SELECT jsonb_agg(jsonb_build_object(
		         'id', pg.group_id, 'name', pg.group_name, 'platform', pg.platform
		       ) ORDER BY pg.group_id) FROM subscription_purchase_groups pg
		       WHERE pg.purchase_id = p.id), '[]'::jsonb)
		FROM subscription_purchases p
		LEFT JOIN users u ON u.id = p.user_id
		WHERE p.id = $1
	`, purchaseID).Scan(
		&item.ID, &item.UserID, &item.UserEmail, &item.Username, &item.PlanID,
		&item.Name, &item.TierCode, &item.Price, &item.Currency, &item.StartsAt,
		&item.ExpiresAt, &item.Status, &item.ConcurrencyEntitlement,
		&item.LifetimeQuotaUSD, &item.DailyQuotaUSD, &item.WeeklyQuotaUSD,
		&item.MonthlyQuotaUSD, &item.LifetimeUsageUSD, &item.DailyUsageUSD,
		&item.WeeklyUsageUSD, &item.MonthlyUsageUSD, &item.BalanceTopupEnabled,
		&item.Source, &item.SourceID, &item.Notes, &groupsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSharedPurchaseNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(groupsJSON, &item.Groups); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *SubscriptionService) ExtendPurchase(ctx context.Context, purchaseID int64, days int) (*AdminPurchaseRecord, error) {
	if days == 0 {
		return s.GetPurchaseRecord(ctx, purchaseID)
	}
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE subscription_purchases
		SET expires_at = GREATEST(expires_at, NOW()) + ($1 * INTERVAL '1 day'),
		    status = CASE WHEN status = 'expired' THEN 'active' ELSE status END,
		    updated_at = NOW()
		WHERE id = $2
	`, days, purchaseID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	return s.GetPurchaseRecord(ctx, purchaseID)
}

func (s *SubscriptionService) RevokeAdminPurchase(ctx context.Context, purchaseID int64) (*AdminPurchaseRecord, error) {
	res, err := s.sqlDB.ExecContext(ctx, "UPDATE subscription_purchases SET status = 'revoked', updated_at = NOW() WHERE id = $1", purchaseID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	item, err := s.GetPurchaseRecord(ctx, purchaseID)
	if err == nil {
		_ = s.invalidateSubscriptionCaches(item.UserID, firstPurchaseGroupID(item.Groups))
	}
	return item, err
}

func (s *SubscriptionService) RestoreAdminPurchase(ctx context.Context, purchaseID int64) (*AdminPurchaseRecord, error) {
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE subscription_purchases
		SET status = 'active', updated_at = NOW()
		WHERE id = $1 AND expires_at > NOW()
	`, purchaseID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, ErrSubscriptionNotFound
	}
	return s.GetPurchaseRecord(ctx, purchaseID)
}

func (s *SubscriptionService) ResetPurchaseQuota(ctx context.Context, purchaseID int64, daily, weekly, monthly bool) (*AdminPurchaseRecord, error) {
	sets := make([]string, 0, 3)
	if daily {
		sets = append(sets, "daily_usage_usd = 0, daily_window_start = NOW()")
	}
	if weekly {
		sets = append(sets, "weekly_usage_usd = 0, weekly_window_start = NOW()")
	}
	if monthly {
		sets = append(sets, "monthly_usage_usd = 0, monthly_window_start = NOW()")
	}
	if len(sets) == 0 {
		return nil, ErrInvalidInput
	}
	res, err := s.sqlDB.ExecContext(ctx,
		"UPDATE subscription_purchases SET "+strings.Join(sets, ", ")+" , updated_at = NOW() WHERE id = $1",
		purchaseID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return nil, ErrSharedPurchaseNotFound
	}
	return s.GetPurchaseRecord(ctx, purchaseID)
}

func firstPurchaseGroupID(groups []SharedSubscriptionGroup) int64 {
	if len(groups) == 0 {
		return 0
	}
	return groups[0].ID
}
