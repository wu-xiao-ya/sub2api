package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// imageCostCandidatesUsageRepoStub satisfies only the two candidate reads; the
// remaining UsageLogRepository methods are never reached by these tests.
type imageCostCandidatesUsageRepoStub struct {
	UsageLogRepository
	models      []string
	accounts    []ImageUpstreamCostAccountCandidate
	modelsErr   error
	accountsErr error
}

func (r *imageCostCandidatesUsageRepoStub) ListImageGenerationModels(context.Context) ([]string, error) {
	return r.models, r.modelsErr
}

func (r *imageCostCandidatesUsageRepoStub) ListImageGenerationAccounts(context.Context) ([]ImageUpstreamCostAccountCandidate, error) {
	return r.accounts, r.accountsErr
}

func TestGetImageUpstreamCostCandidates_ReturnsUsageData(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)
	svc.SetUsageLogRepository(&imageCostCandidatesUsageRepoStub{
		models: []string{"gpt-image-2", "gpt-image-2.5-sunburst"},
		accounts: []ImageUpstreamCostAccountCandidate{
			{AccountID: 228, AccountName: "swiftapi 生图"},
		},
	})

	got := svc.GetImageUpstreamCostCandidates(context.Background())
	require.Equal(t, []string{"gpt-image-2", "gpt-image-2.5-sunburst"}, got.Models)
	require.Len(t, got.Accounts, 1)
	require.Equal(t, int64(228), got.Accounts[0].AccountID)
	require.Equal(t, "swiftapi 生图", got.Accounts[0].AccountName)
}

// The settings page must stay usable when usage reporting is unavailable, so a
// failed candidate read degrades to empty lists instead of an error.
func TestGetImageUpstreamCostCandidates_DegradesOnError(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)
	svc.SetUsageLogRepository(&imageCostCandidatesUsageRepoStub{
		modelsErr:   errors.New("usage unavailable"),
		accountsErr: errors.New("usage unavailable"),
	})

	got := svc.GetImageUpstreamCostCandidates(context.Background())
	require.NotNil(t, got)
	require.Empty(t, got.Models)
	require.Empty(t, got.Accounts)
}

// A repository that is missing entirely (or returns nothing) must produce
// usable empty slices, not nil, so the JSON payload always carries arrays.
func TestGetImageUpstreamCostCandidates_EmptyWithoutRepository(t *testing.T) {
	svc := NewSettingService(&imageUpstreamCostSettingRepoStub{}, nil)
	got := svc.GetImageUpstreamCostCandidates(context.Background())
	require.NotNil(t, got)
	require.NotNil(t, got.Models)
	require.NotNil(t, got.Accounts)
	require.Empty(t, got.Models)
	require.Empty(t, got.Accounts)
}

func TestGetImageUpstreamCostCandidates_NilServiceIsSafe(t *testing.T) {
	var svc *SettingService
	got := svc.GetImageUpstreamCostCandidates(context.Background())
	require.NotNil(t, got)
	require.Empty(t, got.Models)
	require.Empty(t, got.Accounts)
}
