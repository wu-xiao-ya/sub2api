package service

import "context"

// AggregateGroupHasCapacity distinguishes an empty/unavailable group from an
// unmapped account. The ordinary model catalog uses nil for both, and its static
// fallback must never make an unavailable group win aggregate priority.
func (s *GatewayService) AggregateGroupHasCapacity(ctx context.Context, group *Group) bool {
	if s == nil || s.accountRepo == nil || group == nil || !group.IsActive() {
		return false
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, group.ID)
	if err != nil {
		return false
	}
	for i := range accounts {
		account := &accounts[i]
		if !account.IsSchedulable() {
			continue
		}
		if group.Platform == PlatformComposite || account.Platform == group.Platform || mixedListingAccountAllowed(group.Platform, account) {
			return true
		}
	}
	return false
}
