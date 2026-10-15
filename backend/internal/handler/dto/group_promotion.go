package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type GroupPromotion struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	GroupID     int64     `json:"group_id"`
	Mode        string    `json:"mode"`
	Value       float64   `json:"value"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Enabled     bool      `json:"enabled"`
	Status      string    `json:"status"`
	CreatedBy   *int64    `json:"created_by,omitempty"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func GroupPromotionFromService(promotion *service.GroupPromotion) *GroupPromotion {
	if promotion == nil {
		return nil
	}
	return &GroupPromotion{
		ID:          promotion.ID,
		Name:        promotion.Name,
		Description: promotion.Description,
		GroupID:     promotion.GroupID,
		Mode:        promotion.Mode,
		Value:       promotion.Value,
		StartsAt:    promotion.StartsAt,
		EndsAt:      promotion.EndsAt,
		Enabled:     promotion.Enabled,
		Status:      groupPromotionStatus(promotion, time.Now()),
		CreatedBy:   promotion.CreatedBy,
		UpdatedBy:   promotion.UpdatedBy,
		CreatedAt:   promotion.CreatedAt,
		UpdatedAt:   promotion.UpdatedAt,
	}
}

func groupPromotionStatus(promotion *service.GroupPromotion, now time.Time) string {
	if promotion == nil || !promotion.Enabled {
		return "disabled"
	}
	if now.Before(promotion.StartsAt) {
		return "upcoming"
	}
	if !now.Before(promotion.EndsAt) {
		return "ended"
	}
	return "active"
}
