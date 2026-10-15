package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	GroupPromotionModeDiscountFactor  = "discount_factor"
	GroupPromotionModeFixedMultiplier = "fixed_multiplier"
)

var (
	ErrGroupPromotionNotFound = infraerrors.NotFound("GROUP_PROMOTION_NOT_FOUND", "group promotion not found")
	ErrGroupPromotionInvalid  = infraerrors.BadRequest("GROUP_PROMOTION_INVALID", "group promotion is invalid")
	ErrGroupPromotionOverlap  = infraerrors.Conflict("GROUP_PROMOTION_OVERLAP", "this group already has an overlapping enabled promotion")
)

// GroupPromotion is the editable definition of a discount for one sales group.
type GroupPromotion struct {
	ID          int64
	Name        string
	Description *string
	GroupID     int64
	Mode        string
	Value       float64
	StartsAt    time.Time
	EndsAt      time.Time
	Enabled     bool
	CreatedBy   *int64
	UpdatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsActiveAt uses a left-closed, right-open activity window: [starts_at, ends_at).
func (p GroupPromotion) IsActiveAt(now time.Time) bool {
	return p.Enabled && !now.Before(p.StartsAt) && now.Before(p.EndsAt)
}

type GroupPromotionListFilters struct {
	GroupID *int64
	Enabled *bool
	Search  string
}

type GroupPromotionRepository interface {
	Create(ctx context.Context, promotion *GroupPromotion) error
	GetByID(ctx context.Context, id int64) (*GroupPromotion, error)
	Update(ctx context.Context, promotion *GroupPromotion) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params pagination.PaginationParams, filters GroupPromotionListFilters) ([]GroupPromotion, *pagination.PaginationResult, error)
	ListEnabled(ctx context.Context) ([]GroupPromotion, error)
	HasEnabledOverlap(ctx context.Context, groupID, excludeID int64, startsAt, endsAt time.Time) (bool, error)
}

type CreateGroupPromotionInput struct {
	Name        string
	Description *string
	GroupID     int64
	Mode        string
	Value       float64
	StartsAt    time.Time
	EndsAt      time.Time
	Enabled     *bool
	ActorID     *int64
}

type UpdateGroupPromotionInput struct {
	Name        *string
	Description **string
	GroupID     *int64
	Mode        *string
	Value       *float64
	StartsAt    *time.Time
	EndsAt      *time.Time
	Enabled     *bool
	ActorID     *int64
}

// AppliedGroupPromotion is frozen into a usage log whenever the promotion
// actually lowers a charge. BaseRateMultiplier is the true rate immediately
// before the activity, after user, peak, image, or video logic is resolved.
type AppliedGroupPromotion struct {
	ID                 int64
	Name               string
	Mode               string
	Value              float64
	BaseRateMultiplier float64
	RateMultiplier     float64
}
