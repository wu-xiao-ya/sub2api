package middleware

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func sharedSubscriptionForRequest(
	ctx context.Context,
	subscriptionService *service.SubscriptionService,
	userID, groupID int64,
) (*service.SharedSubscriptionEntitlement, *service.UserSubscription, error) {
	if subscriptionService == nil || userID <= 0 || groupID <= 0 {
		return nil, nil, service.ErrSharedSubscriptionNotFound
	}
	purchase, err := subscriptionService.GetActiveSharedSubscriptionForGroup(ctx, userID, groupID)
	if err != nil {
		return nil, nil, err
	}
	return purchase, purchase.AsLegacySubscription(nil), nil
}

func isSharedSubscriptionQuotaError(err error) bool {
	return errors.Is(err, service.ErrDailyLimitExceeded) ||
		errors.Is(err, service.ErrWeeklyLimitExceeded) ||
		errors.Is(err, service.ErrMonthlyLimitExceeded)
}

func sharedSubscriptionConcurrency(
	ctx context.Context,
	subscriptionService *service.SubscriptionService,
	userID int64,
) int {
	if subscriptionService == nil {
		return 0
	}
	items, err := subscriptionService.ListActiveSharedSubscriptions(ctx, userID)
	if err != nil {
		return 0
	}
	total := 0
	for _, item := range items {
		if item.ConcurrencyEntitlement > 0 {
			total += item.ConcurrencyEntitlement
		}
	}
	return total
}

func sharedSubscriptionConcurrencyPlan(
	ctx context.Context,
	subscriptionService *service.SubscriptionService,
	userID, groupID int64,
	userLimit int,
) (*service.SubscriptionConcurrencyPlan, error) {
	if subscriptionService == nil || userID <= 0 || groupID <= 0 {
		return nil, service.ErrSharedSubscriptionNotFound
	}
	items, err := subscriptionService.ListActiveSharedSubscriptionsForGroup(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	plan := &service.SubscriptionConcurrencyPlan{
		UserID:                userID,
		GroupID:               groupID,
		UserLimit:             userLimit,
		HasSharedSubscription: len(items) > 0,
	}
	for _, item := range items {
		validateErr := subscriptionService.ValidateSharedPurchase(&item, 0)
		if validateErr != nil {
			if item.BillingPriority == "balance" {
				plan.AllowBalancePriority = true
				if plan.BalancePriorityPurchaseID == 0 {
					plan.BalancePriorityPurchaseID = item.ID
				}
			}
			if item.BalanceTopupEnabled && isSharedSubscriptionQuotaError(validateErr) {
				plan.AllowBalanceTopup = true
				if plan.BalanceTopupPurchaseID == 0 {
					plan.BalanceTopupPurchaseID = item.ID
				}
			}
			continue
		}
		if item.BillingPriority == "balance" {
			plan.AllowBalancePriority = true
			if plan.BalancePriorityPurchaseID == 0 {
				plan.BalancePriorityPurchaseID = item.ID
			}
		}
		if item.BalanceTopupEnabled {
			plan.AllowBalanceTopup = true
			if plan.BalanceTopupPurchaseID == 0 {
				plan.BalanceTopupPurchaseID = item.ID
			}
		}
		itemCopy := item
		plan.Entitlements = append(plan.Entitlements, service.SubscriptionConcurrencyEntitlement{
			PurchaseID:          item.ID,
			Concurrency:         item.ConcurrencyEntitlement,
			BalanceTopupEnabled: item.BalanceTopupEnabled,
			BillingPriority:     item.BillingPriority,
			ExpiresAt:           item.ExpiresAt,
			Subscription:        itemCopy.AsLegacySubscription(nil),
			SharedSubscription:  &itemCopy,
		})
	}
	if !plan.HasSharedSubscription {
		return nil, service.ErrSharedSubscriptionNotFound
	}
	return plan, nil
}
