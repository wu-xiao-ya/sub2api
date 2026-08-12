package repository

import (
	"context"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/grouppromotion"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type groupPromotionRepository struct {
	client *dbent.Client
}

func NewGroupPromotionRepository(client *dbent.Client) service.GroupPromotionRepository {
	return &groupPromotionRepository{client: client}
}

func (r *groupPromotionRepository) Create(ctx context.Context, promotion *service.GroupPromotion) error {
	client := clientFromContext(ctx, r.client)
	builder := client.GroupPromotion.Create().
		SetName(promotion.Name).
		SetGroupID(promotion.GroupID).
		SetMode(promotion.Mode).
		SetValue(promotion.Value).
		SetStartsAt(promotion.StartsAt).
		SetEndsAt(promotion.EndsAt).
		SetEnabled(promotion.Enabled)
	if promotion.Description != nil {
		builder.SetDescription(*promotion.Description)
	}
	if promotion.CreatedBy != nil {
		builder.SetCreatedBy(*promotion.CreatedBy)
	}
	if promotion.UpdatedBy != nil {
		builder.SetUpdatedBy(*promotion.UpdatedBy)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	applyGroupPromotionEntity(promotion, created)
	return nil
}

func (r *groupPromotionRepository) GetByID(ctx context.Context, id int64) (*service.GroupPromotion, error) {
	item, err := r.client.GroupPromotion.Query().
		Where(grouppromotion.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrGroupPromotionNotFound, nil)
	}
	return groupPromotionEntityToService(item), nil
}

func (r *groupPromotionRepository) Update(ctx context.Context, promotion *service.GroupPromotion) error {
	client := clientFromContext(ctx, r.client)
	builder := client.GroupPromotion.UpdateOneID(promotion.ID).
		SetName(promotion.Name).
		SetGroupID(promotion.GroupID).
		SetMode(promotion.Mode).
		SetValue(promotion.Value).
		SetStartsAt(promotion.StartsAt).
		SetEndsAt(promotion.EndsAt).
		SetEnabled(promotion.Enabled)
	if promotion.Description != nil {
		builder.SetDescription(*promotion.Description)
	} else {
		builder.ClearDescription()
	}
	if promotion.CreatedBy != nil {
		builder.SetCreatedBy(*promotion.CreatedBy)
	} else {
		builder.ClearCreatedBy()
	}
	if promotion.UpdatedBy != nil {
		builder.SetUpdatedBy(*promotion.UpdatedBy)
	} else {
		builder.ClearUpdatedBy()
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrGroupPromotionNotFound, nil)
	}
	applyGroupPromotionEntity(promotion, updated)
	return nil
}

func (r *groupPromotionRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	affected, err := client.GroupPromotion.Delete().
		Where(grouppromotion.IDEQ(id)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrGroupPromotionNotFound
	}
	return nil
}

func (r *groupPromotionRepository) List(
	ctx context.Context,
	params pagination.PaginationParams,
	filters service.GroupPromotionListFilters,
) ([]service.GroupPromotion, *pagination.PaginationResult, error) {
	q := r.client.GroupPromotion.Query()
	if filters.GroupID != nil && *filters.GroupID > 0 {
		q = q.Where(grouppromotion.GroupIDEQ(*filters.GroupID))
	}
	if filters.Enabled != nil {
		q = q.Where(grouppromotion.EnabledEQ(*filters.Enabled))
	}
	if search := strings.TrimSpace(filters.Search); search != "" {
		q = q.Where(
			grouppromotion.Or(
				grouppromotion.NameContainsFold(search),
				grouppromotion.DescriptionContainsFold(search),
			),
		)
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	itemsQuery := q.Offset(params.Offset()).Limit(params.Limit())
	for _, order := range groupPromotionListOrders(params) {
		itemsQuery = itemsQuery.Order(order)
	}
	items, err := itemsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]service.GroupPromotion, 0, len(items))
	for i := range items {
		if item := groupPromotionEntityToService(items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out, paginationResultFromTotal(int64(total), params), nil
}

func (r *groupPromotionRepository) ListEnabled(ctx context.Context) ([]service.GroupPromotion, error) {
	items, err := r.client.GroupPromotion.Query().
		Where(grouppromotion.EnabledEQ(true)).
		Order(dbent.Asc(grouppromotion.FieldStartsAt), dbent.Asc(grouppromotion.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.GroupPromotion, 0, len(items))
	for i := range items {
		if item := groupPromotionEntityToService(items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}

func (r *groupPromotionRepository) HasEnabledOverlap(
	ctx context.Context,
	groupID, excludeID int64,
	startsAt, endsAt time.Time,
) (bool, error) {
	q := r.client.GroupPromotion.Query().
		Where(
			grouppromotion.GroupIDEQ(groupID),
			grouppromotion.EnabledEQ(true),
			grouppromotion.StartsAtLT(endsAt),
			grouppromotion.EndsAtGT(startsAt),
		)
	if excludeID > 0 {
		q = q.Where(grouppromotion.IDNEQ(excludeID))
	}
	return q.Exist(ctx)
}

func groupPromotionListOrder(params pagination.PaginationParams) (string, string) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)
	switch sortBy {
	case "name":
		return grouppromotion.FieldName, sortOrder
	case "group_id":
		return grouppromotion.FieldGroupID, sortOrder
	case "enabled":
		return grouppromotion.FieldEnabled, sortOrder
	case "starts_at":
		return grouppromotion.FieldStartsAt, sortOrder
	case "ends_at":
		return grouppromotion.FieldEndsAt, sortOrder
	case "id":
		return grouppromotion.FieldID, sortOrder
	case "", "created_at":
		return grouppromotion.FieldCreatedAt, sortOrder
	default:
		return grouppromotion.FieldCreatedAt, pagination.SortOrderDesc
	}
}

func groupPromotionListOrders(params pagination.PaginationParams) []func(*entsql.Selector) {
	field, sortOrder := groupPromotionListOrder(params)
	if sortOrder == pagination.SortOrderAsc {
		if field == grouppromotion.FieldID {
			return []func(*entsql.Selector){dbent.Asc(field)}
		}
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(grouppromotion.FieldID)}
	}
	if field == grouppromotion.FieldID {
		return []func(*entsql.Selector){dbent.Desc(field)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(grouppromotion.FieldID)}
}

func applyGroupPromotionEntity(dst *service.GroupPromotion, src *dbent.GroupPromotion) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func groupPromotionEntityToService(src *dbent.GroupPromotion) *service.GroupPromotion {
	if src == nil {
		return nil
	}
	return &service.GroupPromotion{
		ID:          src.ID,
		Name:        src.Name,
		Description: src.Description,
		GroupID:     src.GroupID,
		Mode:        src.Mode,
		Value:       src.Value,
		StartsAt:    src.StartsAt,
		EndsAt:      src.EndsAt,
		Enabled:     src.Enabled,
		CreatedBy:   src.CreatedBy,
		UpdatedBy:   src.UpdatedBy,
		CreatedAt:   src.CreatedAt,
		UpdatedAt:   src.UpdatedAt,
	}
}

var _ service.GroupPromotionRepository = (*groupPromotionRepository)(nil)
