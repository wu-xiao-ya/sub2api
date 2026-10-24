package service

import (
	"context"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ReserveFixedCharge 为扣费前即可确定金额的费用（倍率后 USD，如显式缓存存储费）做准入并登记在途预留。
//
// 余额模式要求缓存余额覆盖 amount 及用户的其他在途预留；订阅模式要求日 / 周 / 月剩余额度覆盖 amount；
// API Key 总额度与限速窗口同样叠加 amount。订阅与 API Key 的用量取库中权威值，并叠加同一订阅 / Key 上其他
// 在途预留：先登记本次预留再读用量，并发的两笔费用中后读用量的一方必然看到另一方（预留或已落库的扣费）。
// 返回的句柄可能为 nil（未登记任何预留）；调用方须在 handler 结束时调用 HandlerDone，预留在计费任务落库后释放。
// 须在 CheckBillingEligibility 通过后调用；简易模式不校验。
func (s *BillingCacheService) ReserveFixedCharge(ctx context.Context, user *User, apiKey *APIKey, group *Group, subscription *UserSubscription, amount float64) (*InflightReservation, error) {
	if user == nil || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, nil
	}
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, nil
	}
	subscriptionBill := group != nil && group.IsSubscriptionType() && subscription != nil
	if !subscriptionBill {
		balance, err := s.GetUserBalance(ctx, user.ID)
		if err != nil {
			return nil, ErrBillingServiceUnavailable.WithCause(err)
		}
		if balance < amount {
			return nil, ErrInsufficientBalance
		}
	}
	res, err := s.reserveInflight(ctx, user, group, subscription, amount, false, false)
	if err != nil {
		return nil, err
	}
	var subscriptionPending, apiKeyPending float64
	if subscriptionBill && (group.HasDailyLimit() || group.HasWeeklyLimit() || group.HasMonthlyLimit()) {
		res, subscriptionPending = s.reserveInflightScope(ctx, res, user.ID, inflightSubscriptionScope(subscription.ID), amount)
	}
	if apiKey != nil && (apiKey.Quota > 0 || apiKey.HasRateLimits()) {
		res, apiKeyPending = s.reserveInflightScope(ctx, res, user.ID, inflightAPIKeyScope(apiKey.ID), amount)
	}
	if subscriptionBill {
		err = s.checkFixedChargeSubscription(ctx, user.ID, group, amount+subscriptionPending)
	}
	if err == nil && apiKey != nil {
		err = s.checkFixedChargeAPIKey(ctx, apiKey, amount+apiKeyPending)
	}
	if err != nil {
		res.Release()
		return nil, err
	}
	return res, nil
}

// checkFixedChargeSubscription 按库中订阅用量校验日 / 周 / 月剩余额度覆盖 pending。
func (s *BillingCacheService) checkFixedChargeSubscription(ctx context.Context, userID int64, group *Group, pending float64) error {
	subData, err := s.readFixedChargeSubscription(ctx, userID, group)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if group.HasDailyLimit() && subData.DailyUsage+pending > *group.DailyLimitUSD {
		return ErrDailyLimitExceeded
	}
	if group.HasWeeklyLimit() && subData.WeeklyUsage+pending > *group.WeeklyLimitUSD {
		return ErrWeeklyLimitExceeded
	}
	if group.HasMonthlyLimit() && subData.MonthlyUsage+pending > *group.MonthlyLimitUSD {
		return ErrMonthlyLimitExceeded
	}
	return nil
}

// readFixedChargeSubscription 读取订阅的权威用量快照。
// 本 fork 的原生单分组订阅已退役（GetSubscriptionStatus / getSubscriptionFromDB 均返回
// ErrNativeSubscriptionRetired），因此优先走仓储，其次才回退到订阅缓存。
func (s *BillingCacheService) readFixedChargeSubscription(ctx context.Context, userID int64, group *Group) (*subscriptionCacheData, error) {
	if s.subRepo != nil {
		sub, err := s.subRepo.GetActiveByUserIDAndGroupID(ctx, userID, group.ID)
		if err != nil {
			return nil, err
		}
		return &subscriptionCacheData{
			Status:       sub.Status,
			ExpiresAt:    sub.ExpiresAt,
			DailyUsage:   sub.DailyUsageUSD,
			WeeklyUsage:  sub.WeeklyUsageUSD,
			MonthlyUsage: sub.MonthlyUsageUSD,
		}, nil
	}
	if s.cache == nil {
		return nil, ErrBillingServiceUnavailable
	}
	data, err := s.cache.GetSubscriptionCache(ctx, userID, group.ID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return &subscriptionCacheData{}, nil
	}
	return s.convertFromPortsData(data), nil
}

// checkFixedChargeAPIKey 按库中 Key 用量校验总额度与限速窗口覆盖 pending；未接入仓储时使用请求上下文与限速缓存中的用量。
func (s *BillingCacheService) checkFixedChargeAPIKey(ctx context.Context, apiKey *APIKey, pending float64) error {
	if apiKey.Quota <= 0 && !apiKey.HasRateLimits() {
		return nil
	}
	if s.apiKeyRateLimitLoader == nil {
		if apiKey.Quota > 0 && apiKey.QuotaUsed+pending > apiKey.Quota {
			return ErrAPIKeyQuotaExhausted
		}
		if apiKey.HasRateLimits() {
			return s.checkAPIKeyRateLimitsWithPending(ctx, apiKey, pending)
		}
		return nil
	}
	data, err := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	exceeds := func(usage, limit float64) bool {
		return limit > 0 && usage+pending > limit
	}
	switch {
	case exceeds(data.QuotaUsed, apiKey.Quota):
		return ErrAPIKeyQuotaExhausted
	case exceeds(data.EffectiveUsage5h(), apiKey.RateLimit5h):
		return ErrAPIKeyRateLimit5hExceeded
	case exceeds(data.EffectiveUsage1d(), apiKey.RateLimit1d):
		return ErrAPIKeyRateLimit1dExceeded
	case exceeds(data.EffectiveUsage7d(), apiKey.RateLimit7d):
		return ErrAPIKeyRateLimit7dExceeded
	}
	return nil
}
