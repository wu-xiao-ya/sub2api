package repository

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type contributorRewardRepository struct {
	db *sql.DB
}

func NewContributorRewardRepository(_ *dbent.Client, sqlDB *sql.DB) service.ContributorRewardRepository {
	return &contributorRewardRepository{db: sqlDB}
}

func (r *contributorRewardRepository) ListByOwner(ctx context.Context, ownerUserID int64, params pagination.PaginationParams) ([]service.ContributorRewardLog, *pagination.PaginationResult, error) {
	if r == nil || r.db == nil {
		return nil, nil, sql.ErrConnDone
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contributor_reward_logs WHERE owner_user_id = $1`, ownerUserID).Scan(&total); err != nil {
		return nil, nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_id, api_key_id, owner_user_id, consumer_user_id, account_id, group_id,
		       total_cost, actual_cost, reward_multiplier, reward_amount, created_at
		FROM contributor_reward_logs
		WHERE owner_user_id = $1
		ORDER BY created_at DESC, id DESC
		OFFSET $2 LIMIT $3
	`, ownerUserID, params.Offset(), params.Limit())
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.ContributorRewardLog, 0)
	for rows.Next() {
		var item service.ContributorRewardLog
		if err := rows.Scan(
			&item.ID,
			&item.RequestID,
			&item.APIKeyID,
			&item.OwnerUserID,
			&item.ConsumerUserID,
			&item.AccountID,
			&item.GroupID,
			&item.TotalCost,
			&item.ActualCost,
			&item.RewardMultiplier,
			&item.RewardAmount,
			&item.CreatedAt,
		); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *contributorRewardRepository) SummaryByOwner(ctx context.Context, ownerUserID int64, now time.Time) (service.ContributorRewardSummary, error) {
	if r == nil || r.db == nil {
		return service.ContributorRewardSummary{}, sql.ErrConnDone
	}
	if now.IsZero() {
		now = time.Now()
	}
	localNow := now.In(time.Local)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	last7dStart := localNow.AddDate(0, 0, -7)

	var summary service.ContributorRewardSummary
	if err := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(reward_amount), 0),
			COALESCE(SUM(CASE WHEN created_at >= $2 THEN reward_amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN created_at >= $3 THEN reward_amount ELSE 0 END), 0)
		FROM contributor_reward_logs
		WHERE owner_user_id = $1
	`, ownerUserID, todayStart, last7dStart).Scan(&summary.TotalReward, &summary.TodayReward, &summary.Last7dReward); err != nil {
		return service.ContributorRewardSummary{}, err
	}
	return summary, nil
}
