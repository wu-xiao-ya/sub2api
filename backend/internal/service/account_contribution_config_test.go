package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestAccountContributionService_UpdateMineConfig_AllowsOnlySafeFields(t *testing.T) {
	ownerID := int64(7)
	expiresAt := time.Now().Add(24 * time.Hour)
	loadFactor := 4
	tempEnabled := true
	autoPause := false
	clearExpiresAt := int64(0)
	threshold5h := 90.0
	threshold7d := 0.75
	disabled5h := false
	disabled7d := true
	notes := "   "
	concurrency := 3

	repo := &contributionConfigRepoStub{account: &Account{
		ID:                 42,
		Name:               "old",
		Notes:              contributionConfigStringPtr("keep"),
		Platform:           PlatformOpenAI,
		Type:               AccountTypeOAuth,
		Credentials:        map[string]any{"access_token": "secret-token"},
		Extra:              map[string]any{"contribution_identity_key": "identity", "codex_usage_updated_at": "runtime"},
		Concurrency:        1,
		LoadFactor:         &loadFactor,
		Status:             StatusActive,
		Schedulable:        true,
		ExpiresAt:          &expiresAt,
		AutoPauseOnExpired: true,
		OwnerUserID:        &ownerID,
		ContributionStatus: ContributionStatusApproved,
		GroupIDs:           []int64{1, 2},
	}}
	svc := &AccountContributionService{accountRepo: repo}

	updated, err := svc.UpdateMineConfig(context.Background(), ownerID, 42, ContributionAccountConfigInput{
		Name:                     contributionConfigStringPtr("  new name  "),
		Notes:                    &notes,
		Concurrency:              &concurrency,
		LoadFactor:               contributionConfigIntPtr(8),
		ExpiresAt:                &clearExpiresAt,
		AutoPauseOnExpired:       &autoPause,
		TempUnschedulableEnabled: &tempEnabled,
		TempUnschedulableRules: &[]TempUnschedulableRule{{
			ErrorCode:       429,
			Keywords:        []string{" rate limit ", "rate limit", "quota"},
			DurationMinutes: 30,
			Description:     "  cool down  ",
		}},
		AutoPause5hThreshold: &threshold5h,
		AutoPause7dThreshold: &threshold7d,
		AutoPause5hDisabled:  &disabled5h,
		AutoPause7dDisabled:  &disabled7d,
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCount)
	require.Equal(t, "new name", updated.Name)
	require.Nil(t, updated.Notes)
	require.Equal(t, 3, updated.Concurrency)
	require.NotNil(t, updated.LoadFactor)
	require.Equal(t, 8, *updated.LoadFactor)
	require.Nil(t, updated.ExpiresAt)
	require.False(t, updated.AutoPauseOnExpired)
	require.Equal(t, StatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.Equal(t, []int64{1, 2}, updated.GroupIDs)

	require.Equal(t, "secret-token", updated.Credentials["access_token"])
	require.Equal(t, true, updated.Credentials["temp_unschedulable_enabled"])
	rules, ok := updated.Credentials["temp_unschedulable_rules"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, rules, 1)
	require.Equal(t, 429, rules[0]["error_code"])
	require.Equal(t, []string{"rate limit", "quota"}, rules[0]["keywords"])
	require.Equal(t, 30, rules[0]["duration_minutes"])
	require.Equal(t, "cool down", rules[0]["description"])

	require.Equal(t, "identity", updated.Extra["contribution_identity_key"])
	require.Equal(t, "runtime", updated.Extra["codex_usage_updated_at"])
	require.InDelta(t, 0.9, updated.Extra["auto_pause_5h_threshold"], 0.0001)
	require.InDelta(t, 0.75, updated.Extra["auto_pause_7d_threshold"], 0.0001)
	require.NotContains(t, updated.Extra, "auto_pause_5h_disabled")
	require.Equal(t, true, updated.Extra["auto_pause_7d_disabled"])
}

func TestAccountContributionService_UpdateMineConfig_RejectsNonOwner(t *testing.T) {
	ownerID := int64(7)
	repo := &contributionConfigRepoStub{account: &Account{
		ID:                 42,
		OwnerUserID:        &ownerID,
		ContributionStatus: ContributionStatusApproved,
	}}
	svc := &AccountContributionService{accountRepo: repo}

	_, err := svc.UpdateMineConfig(context.Background(), 8, 42, ContributionAccountConfigInput{Name: contributionConfigStringPtr("new")})

	require.ErrorIs(t, err, ErrContributionOwnership)
	require.Zero(t, repo.updateCount)
}

func TestAccountContributionService_UpdateMineConfig_RejectsClosedContribution(t *testing.T) {
	ownerID := int64(7)
	repo := &contributionConfigRepoStub{account: &Account{
		ID:                 42,
		OwnerUserID:        &ownerID,
		ContributionStatus: ContributionStatusRevoked,
	}}
	svc := &AccountContributionService{accountRepo: repo}

	_, err := svc.UpdateMineConfig(context.Background(), ownerID, 42, ContributionAccountConfigInput{Name: contributionConfigStringPtr("new")})

	require.ErrorIs(t, err, ErrContributionInvalidStatus)
	require.Zero(t, repo.updateCount)
}

func TestAccountContributionService_Republish_RevokedContribution(t *testing.T) {
	ownerID := int64(7)
	submittedAt := time.Now().Add(-48 * time.Hour)
	approvedAt := time.Now().Add(-24 * time.Hour)
	revokedAt := time.Now().Add(-time.Hour)
	refresher := &contributionConfigSchedulerRefresher{}
	repo := &contributionConfigRepoStub{account: &Account{
		ID:                      42,
		OwnerUserID:             &ownerID,
		ContributionStatus:      ContributionStatusRevoked,
		ContributionSubmittedAt: &submittedAt,
		ContributionApprovedAt:  &approvedAt,
		ContributionRevokedAt:   &revokedAt,
		Status:                  StatusActive,
		Schedulable:             true,
		GroupIDs:                []int64{1, 2},
	}}
	svc := &AccountContributionService{accountRepo: repo, schedulerRefresher: refresher}

	updated, err := svc.Republish(context.Background(), ownerID, 42)

	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCount)
	require.Equal(t, ContributionStatusPending, updated.ContributionStatus)
	require.Equal(t, StatusDisabled, updated.Status)
	require.False(t, updated.Schedulable)
	require.NotNil(t, updated.ContributionSubmittedAt)
	require.WithinDuration(t, time.Now(), *updated.ContributionSubmittedAt, time.Second)
	require.Nil(t, updated.ContributionApprovedAt)
	require.Nil(t, updated.ContributionRevokedAt)
	require.Equal(t, []int64{1, 2}, updated.GroupIDs)
	require.Equal(t, 1, refresher.callCount)
	require.Equal(t, "contribution_republish", refresher.reason)
	require.Equal(t, []int64{1, 2}, refresher.groupIDs)
}

func TestAccountContributionService_Republish_RejectsNonOwner(t *testing.T) {
	ownerID := int64(7)
	repo := &contributionConfigRepoStub{account: &Account{
		ID:                 42,
		OwnerUserID:        &ownerID,
		ContributionStatus: ContributionStatusRevoked,
	}}
	svc := &AccountContributionService{accountRepo: repo}

	_, err := svc.Republish(context.Background(), 8, 42)

	require.ErrorIs(t, err, ErrContributionOwnership)
	require.Zero(t, repo.updateCount)
}

func TestAccountContributionService_Republish_RejectsOpenContribution(t *testing.T) {
	ownerID := int64(7)
	repo := &contributionConfigRepoStub{account: &Account{
		ID:                 42,
		OwnerUserID:        &ownerID,
		ContributionStatus: ContributionStatusApproved,
	}}
	svc := &AccountContributionService{accountRepo: repo}

	_, err := svc.Republish(context.Background(), ownerID, 42)

	require.ErrorIs(t, err, ErrContributionInvalidStatus)
	require.Zero(t, repo.updateCount)
}

type contributionConfigRepoStub struct {
	account     *Account
	updated     *Account
	updateCount int
}

func (r *contributionConfigRepoStub) Create(context.Context, *Account) error { return nil }

func (r *contributionConfigRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	if r.updated != nil {
		return cloneContributionConfigAccount(r.updated), nil
	}
	return cloneContributionConfigAccount(r.account), nil
}

func (r *contributionConfigRepoStub) Update(_ context.Context, account *Account) error {
	r.updateCount++
	r.updated = cloneContributionConfigAccount(account)
	return nil
}

func (r *contributionConfigRepoStub) BindGroups(context.Context, int64, []int64) error { return nil }

func (r *contributionConfigRepoStub) FindByExtraField(context.Context, string, any) ([]Account, error) {
	return nil, nil
}

func (r *contributionConfigRepoStub) ListContributionsByOwner(context.Context, int64, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

func (r *contributionConfigRepoStub) ListContributionsByStatus(context.Context, string, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

func cloneContributionConfigAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Credentials = cloneContributionConfigMap(account.Credentials)
	clone.Extra = cloneContributionConfigMap(account.Extra)
	if account.LoadFactor != nil {
		value := *account.LoadFactor
		clone.LoadFactor = &value
	}
	if account.ExpiresAt != nil {
		value := *account.ExpiresAt
		clone.ExpiresAt = &value
	}
	if account.OwnerUserID != nil {
		value := *account.OwnerUserID
		clone.OwnerUserID = &value
	}
	if account.ContributionSubmittedAt != nil {
		value := *account.ContributionSubmittedAt
		clone.ContributionSubmittedAt = &value
	}
	if account.ContributionApprovedAt != nil {
		value := *account.ContributionApprovedAt
		clone.ContributionApprovedAt = &value
	}
	if account.ContributionRevokedAt != nil {
		value := *account.ContributionRevokedAt
		clone.ContributionRevokedAt = &value
	}
	clone.GroupIDs = append([]int64(nil), account.GroupIDs...)
	return &clone
}

type contributionConfigSchedulerRefresher struct {
	callCount int
	groupIDs  []int64
	reason    string
}

func (r *contributionConfigSchedulerRefresher) RefreshAccount(_ context.Context, _ *Account, groupIDs []int64, reason string) error {
	r.callCount++
	r.groupIDs = append([]int64(nil), groupIDs...)
	r.reason = reason
	return nil
}

func cloneContributionConfigMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func contributionConfigStringPtr(value string) *string { return &value }

func contributionConfigIntPtr(value int) *int { return &value }
