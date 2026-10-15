package service

import "testing"

func TestContributionSchedulability(t *testing.T) {
	ownerID := int64(7)
	base := Account{Status: StatusActive, Schedulable: true}

	tests := []struct {
		name string
		mut  func(*Account)
		want bool
	}{
		{name: "admin owned account remains schedulable", want: true},
		{name: "pending contribution is not schedulable", mut: func(a *Account) {
			a.OwnerUserID = &ownerID
			a.ContributionStatus = ContributionStatusPending
		}, want: false},
		{name: "approved contribution is schedulable", mut: func(a *Account) {
			a.OwnerUserID = &ownerID
			a.ContributionStatus = ContributionStatusApproved
		}, want: true},
		{name: "revoked contribution is not schedulable", mut: func(a *Account) {
			a.OwnerUserID = &ownerID
			a.ContributionStatus = ContributionStatusRevoked
		}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := base
			if tt.mut != nil {
				tt.mut(&account)
			}
			if got := account.IsSchedulable(); got != tt.want {
				t.Fatalf("IsSchedulable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildUsageBillingCommandContributorReward(t *testing.T) {
	ownerID := int64(42)
	groupID := int64(9)

	cmd := buildUsageBillingCommand("req-1", nil, &postUsageBillingParams{
		Cost:    &CostBreakdown{TotalCost: 10, ActualCost: 6},
		User:    &User{ID: 100},
		APIKey:  &APIKey{ID: 5, GroupID: &groupID, Group: &Group{ID: groupID, ContributorRewardMultiplier: 1.2}},
		Account: &Account{ID: 3, OwnerUserID: &ownerID, ContributionStatus: ContributionStatusApproved},
	})
	if cmd == nil {
		t.Fatal("expected billing command")
	}
	if cmd.ContributorOwnerUserID != ownerID {
		t.Fatalf("owner id = %d, want %d", cmd.ContributorOwnerUserID, ownerID)
	}
	if cmd.ContributorRewardAmount != 6 {
		t.Fatalf("reward amount = %v, want capped actual cost 6", cmd.ContributorRewardAmount)
	}
	if cmd.ContributorRewardTotalCost != 10 || cmd.ContributorRewardActualCost != 6 || cmd.ContributorRewardMultiplier != 1.2 {
		t.Fatalf("unexpected reward metadata: %#v", cmd)
	}
}

func TestBuildUsageBillingCommandContributorRewardSkippedWhenMultiplierZero(t *testing.T) {
	ownerID := int64(42)
	groupID := int64(9)

	cmd := buildUsageBillingCommand("req-1", nil, &postUsageBillingParams{
		Cost:    &CostBreakdown{TotalCost: 10, ActualCost: 10},
		User:    &User{ID: 100},
		APIKey:  &APIKey{ID: 5, GroupID: &groupID, Group: &Group{ID: groupID, ContributorRewardMultiplier: 0}},
		Account: &Account{ID: 3, OwnerUserID: &ownerID, ContributionStatus: ContributionStatusApproved},
	})
	if cmd == nil {
		t.Fatal("expected billing command")
	}
	if cmd.ContributorRewardAmount != 0 || cmd.ContributorOwnerUserID != 0 {
		t.Fatalf("expected no contributor reward, got %#v", cmd)
	}
}
