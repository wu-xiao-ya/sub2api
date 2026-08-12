package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type ContributorRewardLog struct {
	ID               int64     `json:"id"`
	RequestID        string    `json:"request_id"`
	APIKeyID         int64     `json:"api_key_id"`
	OwnerUserID      int64     `json:"owner_user_id"`
	ConsumerUserID   int64     `json:"consumer_user_id"`
	AccountID        int64     `json:"account_id"`
	GroupID          int64     `json:"group_id"`
	TotalCost        float64   `json:"total_cost"`
	ActualCost       float64   `json:"actual_cost"`
	RewardMultiplier float64   `json:"reward_multiplier"`
	RewardAmount     float64   `json:"reward_amount"`
	CreatedAt        time.Time `json:"created_at"`
}

type ContributorRewardSummary struct {
	TotalReward  float64 `json:"total_reward"`
	TodayReward  float64 `json:"today_reward"`
	Last7dReward float64 `json:"last_7d_reward"`
}

type ContributorRewardRepository interface {
	ListByOwner(ctx context.Context, ownerUserID int64, params pagination.PaginationParams) ([]ContributorRewardLog, *pagination.PaginationResult, error)
	SummaryByOwner(ctx context.Context, ownerUserID int64, now time.Time) (ContributorRewardSummary, error)
}
