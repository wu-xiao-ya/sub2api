package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type groupPromotionResolverRepoStub struct {
	mu        sync.Mutex
	items     []GroupPromotion
	listCalls int
}

func (r *groupPromotionResolverRepoStub) Create(context.Context, *GroupPromotion) error { return nil }
func (r *groupPromotionResolverRepoStub) GetByID(context.Context, int64) (*GroupPromotion, error) {
	return nil, ErrGroupPromotionNotFound
}
func (r *groupPromotionResolverRepoStub) Update(context.Context, *GroupPromotion) error { return nil }
func (r *groupPromotionResolverRepoStub) Delete(context.Context, int64) error           { return nil }
func (r *groupPromotionResolverRepoStub) List(context.Context, pagination.PaginationParams, GroupPromotionListFilters) ([]GroupPromotion, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *groupPromotionResolverRepoStub) ListEnabled(context.Context) ([]GroupPromotion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	return append([]GroupPromotion(nil), r.items...), nil
}
func (r *groupPromotionResolverRepoStub) HasEnabledOverlap(context.Context, int64, int64, time.Time, time.Time) (bool, error) {
	return false, nil
}

func TestGroupPromotionResolverAppliesDiscountAndFixedRate(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &groupPromotionResolverRepoStub{items: []GroupPromotion{{
		ID:       1,
		Name:     "95 percent",
		GroupID:  12,
		Mode:     GroupPromotionModeDiscountFactor,
		Value:    0.95,
		StartsAt: now.Add(-time.Hour),
		EndsAt:   now.Add(time.Hour),
		Enabled:  true,
	}}}
	resolver := NewGroupPromotionResolver(repo)

	rate, applied := resolver.Apply(context.Background(), 12, 0.20, now)
	require.InEpsilon(t, 0.19, rate, 1e-12)
	require.NotNil(t, applied)
	require.Equal(t, "95 percent", applied.Name)
	require.InEpsilon(t, 0.20, applied.BaseRateMultiplier, 1e-12)

	repo.items = []GroupPromotion{{
		ID:       2,
		Name:     "0.18 cap",
		GroupID:  12,
		Mode:     GroupPromotionModeFixedMultiplier,
		Value:    0.18,
		StartsAt: now.Add(-time.Hour),
		EndsAt:   now.Add(time.Hour),
		Enabled:  true,
	}}
	resolver.Invalidate()

	rate, applied = resolver.Apply(context.Background(), 12, 0.20, now)
	require.InEpsilon(t, 0.18, rate, 1e-12)
	require.NotNil(t, applied)
	require.InEpsilon(t, 0.18, applied.RateMultiplier, 1e-12)
}

func TestGroupPromotionResolverNeverRaisesPersonalRate(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	resolver := NewGroupPromotionResolver(&groupPromotionResolverRepoStub{items: []GroupPromotion{{
		ID:       3,
		Name:     "0.18 cap",
		GroupID:  12,
		Mode:     GroupPromotionModeFixedMultiplier,
		Value:    0.18,
		StartsAt: now.Add(-time.Minute),
		EndsAt:   now.Add(time.Minute),
		Enabled:  true,
	}}})

	rate, applied := resolver.Apply(context.Background(), 12, 0.15, now)
	require.InEpsilon(t, 0.15, rate, 1e-12)
	require.Nil(t, applied)
}

func TestGroupPromotionResolverUsesLeftClosedRightOpenTimeRange(t *testing.T) {
	start := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	resolver := NewGroupPromotionResolver(&groupPromotionResolverRepoStub{items: []GroupPromotion{{
		ID:       4,
		Name:     "daytime",
		GroupID:  12,
		Mode:     GroupPromotionModeDiscountFactor,
		Value:    0.90,
		StartsAt: start,
		EndsAt:   end,
		Enabled:  true,
	}}})

	rate, applied := resolver.Apply(context.Background(), 12, 0.20, start)
	require.InEpsilon(t, 0.18, rate, 1e-12)
	require.NotNil(t, applied)

	rate, applied = resolver.Apply(context.Background(), 12, 0.20, end)
	require.InEpsilon(t, 0.20, rate, 1e-12)
	require.Nil(t, applied)
}

func TestGroupPromotionResolverInvalidatesCachedDefinitions(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo := &groupPromotionResolverRepoStub{items: []GroupPromotion{{
		ID:       5,
		Name:     "first",
		GroupID:  12,
		Mode:     GroupPromotionModeDiscountFactor,
		Value:    0.95,
		StartsAt: now.Add(-time.Hour),
		EndsAt:   now.Add(time.Hour),
		Enabled:  true,
	}}}
	resolver := NewGroupPromotionResolver(repo)

	_, first := resolver.Apply(context.Background(), 12, 0.20, now)
	require.Equal(t, "first", first.Name)
	repo.items[0].Name = "updated"
	_, cached := resolver.Apply(context.Background(), 12, 0.20, now)
	require.Equal(t, "first", cached.Name)

	resolver.Invalidate()
	_, refreshed := resolver.Apply(context.Background(), 12, 0.20, now)
	require.Equal(t, "updated", refreshed.Name)
	require.Equal(t, 2, repo.listCalls)
}
