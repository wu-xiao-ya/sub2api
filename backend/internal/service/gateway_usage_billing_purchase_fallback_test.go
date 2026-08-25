package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type purchaseUsageFallbackRepoStub struct {
	userSubRepoNoop
	purchaseID int64
	costUSD    float64
	calls      int
}

func (r *purchaseUsageFallbackRepoStub) IncrementPurchaseUsage(_ context.Context, purchaseID int64, costUSD float64) error {
	r.calls++
	r.purchaseID = purchaseID
	r.costUSD = costUSD
	return nil
}

func TestPostUsageBillingPurchaseFallbackUsesExplicitPurchaseID(t *testing.T) {
	purchaseID := int64(42)
	repo := &purchaseUsageFallbackRepoStub{}
	postUsageBilling(context.Background(), &postUsageBillingParams{
		Cost:               &CostBreakdown{ActualCost: 1.25},
		User:               &User{ID: 7},
		APIKey:             &APIKey{},
		Account:            &Account{},
		Subscription:       &UserSubscription{UserID: 7, GroupID: 9, SubscriptionPurchaseID: &purchaseID},
		IsSubscriptionBill: true,
	}, &billingDeps{userSubRepo: repo})

	require.Equal(t, 1, repo.calls)
	require.Equal(t, purchaseID, repo.purchaseID)
	require.Equal(t, 1.25, repo.costUSD)
}

func TestPostUsageBillingPurchaseFallbackNeverWritesLegacySubscription(t *testing.T) {
	repo := &purchaseUsageFallbackRepoStub{}
	postUsageBilling(context.Background(), &postUsageBillingParams{
		Cost:               &CostBreakdown{ActualCost: 1.25},
		User:               &User{ID: 7},
		APIKey:             &APIKey{},
		Account:            &Account{},
		Subscription:       &UserSubscription{ID: 88, UserID: 7, GroupID: 9},
		IsSubscriptionBill: true,
	}, &billingDeps{userSubRepo: repo})

	require.Zero(t, repo.calls)
}
