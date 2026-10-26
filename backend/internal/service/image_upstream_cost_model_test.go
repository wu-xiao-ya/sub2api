package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newImageCostService(t *testing.T) (*SettingService, *imageUpstreamCostSettingRepoStub) {
	t.Helper()
	repo := &imageUpstreamCostSettingRepoStub{}
	return NewSettingService(repo, nil), repo
}

func TestResolveImageUpstreamCost_PriorityOrder(t *testing.T) {
	svc, repo := newImageCostService(t)

	require.NoError(t, repo.Set(context.Background(), SettingKeyImageUpstreamCostPerImage, "0.5"))

	// Global default only.
	require.InDelta(t, 0.5, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "2K"), 1e-9)

	// Account level outranks the default.
	accountOverrides := []ImageUpstreamCostAccountOverride{{AccountID: 228, CostPerImage: 0.4}}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, &accountOverrides, nil, nil, nil))
	require.InDelta(t, 0.4, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "2K"), 1e-9)

	// Model level outranks the account level.
	modelOverrides := []ImageUpstreamCostModelOverride{
		{Model: "gpt-image-2", Tiers: map[string]float64{"1K": 0.1, "2K": 0.2, "4K": 0.4}},
	}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, nil, &modelOverrides, nil, nil))
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "2K"), 1e-9)

	// Account+model outranks everything.
	accountModelOverrides := []ImageUpstreamCostAccountModelOverride{
		{AccountID: 228, Model: "gpt-image-2", Tiers: map[string]float64{"2K": 0.15}},
	}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, nil, nil, &accountModelOverrides, nil))
	require.InDelta(t, 0.15, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "2K"), 1e-9)

	// A different account still falls back through model to the default.
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(context.Background(), 999, "gpt-image-2", "2K"), 1e-9)
	require.InDelta(t, 0.5, svc.ResolveImageUpstreamCost(context.Background(), 999, "other", "2K"), 1e-9)
}

func TestResolveImageUpstreamCost_TierFallthrough(t *testing.T) {
	svc, repo := newImageCostService(t)
	require.NoError(t, repo.Set(context.Background(), SettingKeyImageUpstreamCostPerImage, "0.9"))

	// Only 4K configured for this model: other tiers must keep falling through
	// to the lower-priority sources rather than borrowing the 4K price.
	modelOverrides := []ImageUpstreamCostModelOverride{
		{Model: "gpt-image-2", Tiers: map[string]float64{"4K": 0.8}},
	}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, nil, &modelOverrides, nil, nil))

	require.InDelta(t, 0.8, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "4K"), 1e-9)
	require.InDelta(t, 0.9, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "2K"), 1e-9)
	require.InDelta(t, 0.9, svc.ResolveImageUpstreamCost(context.Background(), 228, "gpt-image-2", "1K"), 1e-9)
}

func TestResolveImageUpstreamCost_SizeNormalization(t *testing.T) {
	svc, _ := newImageCostService(t)
	modelOverrides := []ImageUpstreamCostModelOverride{
		{Model: "gpt-image-2", Tiers: map[string]float64{"1K": 0.1, "2K": 0.2, "4K": 0.4}},
	}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, nil, &modelOverrides, nil, nil))
	ctx := context.Background()

	// Explicit tiers and their pixel equivalents must agree.
	require.InDelta(t, 0.1, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "1K"), 1e-9)
	require.InDelta(t, 0.1, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "1024x1024"), 1e-9)
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "2048x2048"), 1e-9)
	require.InDelta(t, 0.4, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "3840x2160"), 1e-9)
	// Unknown or empty sizes fall back to the 2K tier, matching revenue side.
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", ""), 1e-9)
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "auto"), 1e-9)
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(ctx, 228, "gpt-image-2", "bogus"), 1e-9)
}

func TestResolveImageUpstreamCost_ModelKeyIsCaseInsensitive(t *testing.T) {
	svc, _ := newImageCostService(t)
	modelOverrides := []ImageUpstreamCostModelOverride{
		{Model: "GPT-Image-2", Tiers: map[string]float64{"2K": 0.2}},
	}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, nil, &modelOverrides, nil, nil))

	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(context.Background(), 1, "gpt-image-2", "2K"), 1e-9)
	require.InDelta(t, 0.2, svc.ResolveImageUpstreamCost(context.Background(), 1, "  Gpt-IMAGE-2 ", "2K"), 1e-9)
}

func TestUpdateImageUpstreamCostSettings_Validation(t *testing.T) {
	ctx := context.Background()
	svc, repo := newImageCostService(t)

	// No field at all is rejected.
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, nil, nil, nil))

	// Unsupported tier names are rejected.
	badTier := []ImageUpstreamCostModelOverride{{Model: "gpt-image-2", Tiers: map[string]float64{"8K": 0.2}}}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &badTier, nil, nil))

	// Negative prices are rejected.
	negative := []ImageUpstreamCostModelOverride{{Model: "gpt-image-2", Tiers: map[string]float64{"2K": -1}}}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &negative, nil, nil))

	// A model entry with no tiers is rejected.
	empty := []ImageUpstreamCostModelOverride{{Model: "gpt-image-2"}}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &empty, nil, nil))

	// Duplicate model entries are rejected.
	dup := []ImageUpstreamCostModelOverride{
		{Model: "gpt-image-2", Tiers: map[string]float64{"2K": 0.2}},
		{Model: "gpt-image-2", Tiers: map[string]float64{"1K": 0.1}},
	}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &dup, nil, nil))

	// Account+model requires a positive account id.
	badAccount := []ImageUpstreamCostAccountModelOverride{
		{AccountID: 0, Model: "gpt-image-2", Tiers: map[string]float64{"2K": 0.2}},
	}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, nil, &badAccount, nil))

	// Zero is a deliberate value and must be accepted.
	zero := []ImageUpstreamCostModelOverride{{Model: "gpt-image-2", Tiers: map[string]float64{"2K": 0}}}
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &zero, nil, nil))
	_ = repo
}

func TestImageUpstreamCostSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, repo := newImageCostService(t)

	modelOverrides := []ImageUpstreamCostModelOverride{
		{Model: "gpt-image-2", Tiers: map[string]float64{"1K": 0.1, "2K": 0.2}},
		{Model: "gpt-image-1.5", Tiers: map[string]float64{"2K": 0.05}},
	}
	accountModelOverrides := []ImageUpstreamCostAccountModelOverride{
		{AccountID: 228, Model: "gpt-image-2", Tiers: map[string]float64{"2K": 0.15}},
	}
	ignore := true
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, &modelOverrides, &accountModelOverrides, &ignore))

	settings := svc.GetImageUpstreamCostSettings(ctx)
	require.Len(t, settings.ModelOverrides, 2)
	require.Len(t, settings.AccountModelOverrides, 1)
	require.Equal(t, int64(228), settings.AccountModelOverrides[0].AccountID)
	require.True(t, settings.IgnoreUpstreamRateSnapshot)

	// Values are normalized into the canonical tiers on read.
	byModel := map[string]map[string]float64{}
	for _, override := range settings.ModelOverrides {
		byModel[override.Model] = override.Tiers
	}
	require.InDelta(t, 0.2, byModel["gpt-image-2"]["2K"], 1e-9)
	require.InDelta(t, 0.1, byModel["gpt-image-2"]["1K"], 1e-9)
	require.InDelta(t, 0.05, byModel["gpt-image-1.5"]["2K"], 1e-9)

	// Persisted JSON carries canonical tier keys.
	raw, err := repo.GetValue(ctx, SettingKeyImageUpstreamCostByModel)
	require.NoError(t, err)
	require.Contains(t, raw, `"2K"`)
	require.Contains(t, raw, "gpt-image-2")
}

func TestImageUpstreamCostSettings_IgnoreSnapshotDefaultsOff(t *testing.T) {
	ctx := context.Background()
	svc, repo := newImageCostService(t)

	// Missing setting keeps historical behavior.
	require.False(t, svc.GetImageCostIgnoreUpstreamRateSnapshot(ctx))

	on := true
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, nil, nil, &on))
	require.True(t, svc.GetImageCostIgnoreUpstreamRateSnapshot(ctx))
	require.Equal(t, "true", repo.values[SettingKeyImageCostIgnoreUpstreamRateSnapshot])

	off := false
	require.NoError(t, svc.UpdateImageUpstreamCostSettings(ctx, nil, nil, nil, nil, &off))
	require.False(t, svc.GetImageCostIgnoreUpstreamRateSnapshot(ctx))
}

func TestGetImageUpstreamCostModelOverrides_IgnoresMalformedData(t *testing.T) {
	ctx := context.Background()
	svc, repo := newImageCostService(t)

	require.NoError(t, repo.Set(ctx, SettingKeyImageUpstreamCostByModel, "not json"))
	require.Empty(t, svc.GetImageUpstreamCostModelOverrides(ctx))

	// Entries with unusable tiers are dropped without failing the whole read.
	require.NoError(t, repo.Set(ctx, SettingKeyImageUpstreamCostByModel,
		`{"gpt-image-2":{"2K":0.2},"bad-model":{"8K":0.1},"":{"2K":0.3}}`))
	overrides := svc.GetImageUpstreamCostModelOverrides(ctx)
	require.Len(t, overrides, 1)
	require.Equal(t, "gpt-image-2", overrides[0].Model)
}
