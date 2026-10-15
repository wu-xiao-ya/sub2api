package service

import (
	"context"
	"math"
	"sync"
	"time"
)

const groupPromotionResolverMaxTTL = time.Minute

// GroupPromotionResolver only caches promotion definitions. It never caches
// the final multiplier because user-specific, peak, and media rates differ
// for every request.
type GroupPromotionResolver struct {
	repo GroupPromotionRepository

	mu      sync.RWMutex
	items   []GroupPromotion
	expires time.Time
	loaded  bool
}

var defaultGroupPromotionResolver struct {
	mu       sync.RWMutex
	resolver *GroupPromotionResolver
}

func setDefaultGroupPromotionResolver(resolver *GroupPromotionResolver) {
	defaultGroupPromotionResolver.mu.Lock()
	defaultGroupPromotionResolver.resolver = resolver
	defaultGroupPromotionResolver.mu.Unlock()
}

func currentGroupPromotionResolver() *GroupPromotionResolver {
	defaultGroupPromotionResolver.mu.RLock()
	resolver := defaultGroupPromotionResolver.resolver
	defaultGroupPromotionResolver.mu.RUnlock()
	return resolver
}

func NewGroupPromotionResolver(repo GroupPromotionRepository) *GroupPromotionResolver {
	return &GroupPromotionResolver{repo: repo}
}

func (r *GroupPromotionResolver) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.expires = time.Time{}
	r.loaded = false
	r.mu.Unlock()
}

// Apply returns the lowered rate and its immutable audit snapshot. It
// deliberately returns nil when an activity would not lower the charge, so a
// fixed-rate activity can never raise a user's existing lower custom rate.
func (r *GroupPromotionResolver) Apply(ctx context.Context, groupID int64, baseRate float64, now time.Time) (float64, *AppliedGroupPromotion) {
	if groupID <= 0 {
		return baseRate, nil
	}
	if baseRate < 0 {
		baseRate = 0
	}
	if math.IsNaN(baseRate) || math.IsInf(baseRate, 0) {
		return 0, nil
	}
	items := r.resolve(ctx, now)
	for i := range items {
		promotion := items[i]
		if promotion.GroupID != groupID || !promotion.IsActiveAt(now) {
			continue
		}
		finalRate := baseRate
		switch promotion.Mode {
		case GroupPromotionModeDiscountFactor:
			finalRate = baseRate * promotion.Value
		case GroupPromotionModeFixedMultiplier:
			finalRate = math.Min(baseRate, promotion.Value)
		default:
			continue
		}
		if math.IsNaN(finalRate) || math.IsInf(finalRate, 0) || finalRate < 0 {
			continue
		}
		if finalRate >= baseRate {
			return baseRate, nil
		}
		return finalRate, &AppliedGroupPromotion{
			ID:                 promotion.ID,
			Name:               promotion.Name,
			Mode:               promotion.Mode,
			Value:              promotion.Value,
			BaseRateMultiplier: baseRate,
			RateMultiplier:     finalRate,
		}
	}
	return baseRate, nil
}

func applyCurrentGroupPromotion(ctx context.Context, groupID int64, baseRate float64, now time.Time) (float64, *AppliedGroupPromotion) {
	resolver := currentGroupPromotionResolver()
	if resolver == nil {
		return baseRate, nil
	}
	return resolver.Apply(ctx, groupID, baseRate, now)
}

func applyUsageLogPromotionSnapshot(log *UsageLog, promotion *AppliedGroupPromotion) {
	if log == nil || promotion == nil {
		return
	}
	id := promotion.ID
	name := promotion.Name
	base := promotion.BaseRateMultiplier
	log.PromotionID = &id
	log.PromotionName = &name
	log.BaseRateMultiplier = &base
}

func (r *GroupPromotionResolver) resolve(ctx context.Context, now time.Time) []GroupPromotion {
	if r == nil || r.repo == nil {
		return nil
	}
	r.mu.RLock()
	if r.loaded && now.Before(r.expires) {
		items := r.items
		r.mu.RUnlock()
		return items
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded && now.Before(r.expires) {
		return r.items
	}
	items, err := r.repo.ListEnabled(ctx)
	if err != nil {
		// Keep a previously known snapshot if the database has a transient
		// problem. The short retry avoids turning transient DB trouble into a
		// request-wide billing outage.
		r.expires = now.Add(5 * time.Second)
		return r.items
	}
	r.items = items
	r.loaded = true
	r.expires = nextPromotionResolverExpiry(items, now)
	return r.items
}

func nextPromotionResolverExpiry(items []GroupPromotion, now time.Time) time.Time {
	expiry := now.Add(groupPromotionResolverMaxTTL)
	for i := range items {
		for _, boundary := range []time.Time{items[i].StartsAt, items[i].EndsAt} {
			if boundary.After(now) && boundary.Before(expiry) {
				expiry = boundary
			}
		}
	}
	return expiry
}
