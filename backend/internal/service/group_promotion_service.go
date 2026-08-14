package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type GroupPromotionService struct {
	repo      GroupPromotionRepository
	groupRepo GroupRepository
	resolver  *GroupPromotionResolver
}

func NewGroupPromotionService(repo GroupPromotionRepository, groupRepo GroupRepository) *GroupPromotionService {
	resolver := NewGroupPromotionResolver(repo)
	setDefaultGroupPromotionResolver(resolver)
	return &GroupPromotionService{
		repo:      repo,
		groupRepo: groupRepo,
		resolver:  resolver,
	}
}

func (s *GroupPromotionService) Create(ctx context.Context, input *CreateGroupPromotionInput) (*GroupPromotion, error) {
	if input == nil {
		return nil, ErrGroupPromotionInvalid
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	promotion := &GroupPromotion{
		Name:        strings.TrimSpace(input.Name),
		Description: normalizePromotionDescription(input.Description),
		GroupID:     input.GroupID,
		Mode:        strings.TrimSpace(input.Mode),
		Value:       input.Value,
		StartsAt:    input.StartsAt,
		EndsAt:      input.EndsAt,
		Enabled:     enabled,
	}
	if input.ActorID != nil && *input.ActorID > 0 {
		promotion.CreatedBy = input.ActorID
		promotion.UpdatedBy = input.ActorID
	}
	if err := s.validate(ctx, promotion, 0); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, promotion); err != nil {
		return nil, fmt.Errorf("create group promotion: %w", err)
	}
	s.invalidateResolver()
	return promotion, nil
}

func (s *GroupPromotionService) Update(ctx context.Context, id int64, input *UpdateGroupPromotionInput) (*GroupPromotion, error) {
	if input == nil {
		return nil, ErrGroupPromotionInvalid
	}
	promotion, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		promotion.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		promotion.Description = normalizePromotionDescription(*input.Description)
	}
	if input.GroupID != nil {
		promotion.GroupID = *input.GroupID
	}
	if input.Mode != nil {
		promotion.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.Value != nil {
		promotion.Value = *input.Value
	}
	if input.StartsAt != nil {
		promotion.StartsAt = *input.StartsAt
	}
	if input.EndsAt != nil {
		promotion.EndsAt = *input.EndsAt
	}
	if input.Enabled != nil {
		promotion.Enabled = *input.Enabled
	}
	if input.ActorID != nil && *input.ActorID > 0 {
		promotion.UpdatedBy = input.ActorID
	}
	if err := s.validate(ctx, promotion, promotion.ID); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, promotion); err != nil {
		return nil, fmt.Errorf("update group promotion: %w", err)
	}
	s.invalidateResolver()
	return promotion, nil
}

func (s *GroupPromotionService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete group promotion: %w", err)
	}
	s.invalidateResolver()
	return nil
}

func (s *GroupPromotionService) GetByID(ctx context.Context, id int64) (*GroupPromotion, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *GroupPromotionService) List(ctx context.Context, params pagination.PaginationParams, filters GroupPromotionListFilters) ([]GroupPromotion, *pagination.PaginationResult, error) {
	return s.repo.List(ctx, params, filters)
}

func (s *GroupPromotionService) Resolver() *GroupPromotionResolver {
	if s == nil {
		return nil
	}
	return s.resolver
}

func (s *GroupPromotionService) invalidateResolver() {
	if s != nil && s.resolver != nil {
		s.resolver.Invalidate()
	}
}

func (s *GroupPromotionService) validate(ctx context.Context, promotion *GroupPromotion, excludeID int64) error {
	if s == nil || s.repo == nil || s.groupRepo == nil || promotion == nil {
		return ErrGroupPromotionInvalid
	}
	if promotion.Name == "" || len(promotion.Name) > 200 || promotion.GroupID <= 0 {
		return ErrGroupPromotionInvalid
	}
	if promotion.Description != nil && len(*promotion.Description) > 4000 {
		return ErrGroupPromotionInvalid
	}
	if promotion.StartsAt.IsZero() || promotion.EndsAt.IsZero() || !promotion.StartsAt.Before(promotion.EndsAt) {
		return ErrGroupPromotionInvalid
	}
	if _, err := s.groupRepo.GetByIDLite(ctx, promotion.GroupID); err != nil {
		return err
	}
	switch promotion.Mode {
	case GroupPromotionModeDiscountFactor:
		if math.IsNaN(promotion.Value) || math.IsInf(promotion.Value, 0) || promotion.Value < 0 || promotion.Value > 1 {
			return ErrGroupPromotionInvalid
		}
	case GroupPromotionModeFixedMultiplier:
		if math.IsNaN(promotion.Value) || math.IsInf(promotion.Value, 0) || promotion.Value < 0 || promotion.Value > 100 {
			return ErrGroupPromotionInvalid
		}
	default:
		return ErrGroupPromotionInvalid
	}
	if promotion.Enabled {
		overlap, err := s.repo.HasEnabledOverlap(ctx, promotion.GroupID, excludeID, promotion.StartsAt, promotion.EndsAt)
		if err != nil {
			return err
		}
		if overlap {
			return ErrGroupPromotionOverlap
		}
	}
	return nil
}

func normalizePromotionDescription(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
