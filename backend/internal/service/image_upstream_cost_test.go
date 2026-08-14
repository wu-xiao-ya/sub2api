package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type imageUpstreamCostSettingRepoStub struct {
	values map[string]string
}

func (r *imageUpstreamCostSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *imageUpstreamCostSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *imageUpstreamCostSettingRepoStub) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *imageUpstreamCostSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}

func (r *imageUpstreamCostSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *imageUpstreamCostSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *imageUpstreamCostSettingRepoStub) Delete(context.Context, string) error {
	return nil
}

func TestImageUpstreamCostPerImage_DefaultAndRoundTrip(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)

	require.InDelta(t, ImageUpstreamCostPerImageDefault, svc.GetImageUpstreamCostPerImage(context.Background()), 1e-12)
	require.NoError(t, svc.SetImageUpstreamCostPerImage(context.Background(), 0.00025))
	require.Equal(t, "0.00025", repo.values[SettingKeyImageUpstreamCostPerImage])
	require.InDelta(t, 0.00025, svc.GetImageUpstreamCostPerImage(context.Background()), 1e-12)
	require.Equal(t, &ImageUpstreamCostSettings{
		CostPerImage:     0.00025,
		AccountOverrides: []ImageUpstreamCostAccountOverride{},
		BillingMode:      string(BillingModeImage),
		Unit:             "USD/image",
	}, svc.GetImageUpstreamCostSettings(context.Background()))
}

func TestImageUpstreamCostPerImage_InvalidValuesAreRejected(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)

	for _, value := range []float64{-0.001, 0.00000000001, math.Inf(1)} {
		require.Error(t, svc.SetImageUpstreamCostPerImage(context.Background(), value))
	}
	require.Empty(t, repo.values)
}

func TestImageUpstreamCostAccountOverridesRoundTrip(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)
	overrides := []ImageUpstreamCostAccountOverride{
		{AccountID: 69, CostPerImage: 0.01},
		{AccountID: 68, CostPerImage: 0.1},
	}

	require.NoError(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, &overrides))
	require.JSONEq(t, `{"68":0.1,"69":0.01}`, repo.values[SettingKeyImageUpstreamCostByAccount])
	require.Equal(t, []ImageUpstreamCostAccountOverride{
		{AccountID: 68, CostPerImage: 0.1},
		{AccountID: 69, CostPerImage: 0.01},
	}, svc.GetImageUpstreamCostAccountOverrides(context.Background()))
}

func TestImageUpstreamCostAccountOverridesRejectInvalidValues(t *testing.T) {
	repo := &imageUpstreamCostSettingRepoStub{}
	svc := NewSettingService(repo, nil)

	duplicate := []ImageUpstreamCostAccountOverride{
		{AccountID: 68, CostPerImage: 0.1},
		{AccountID: 68, CostPerImage: 0.01},
	}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, &duplicate))

	invalid := []ImageUpstreamCostAccountOverride{{AccountID: 0, CostPerImage: 0.1}}
	require.Error(t, svc.UpdateImageUpstreamCostSettings(context.Background(), nil, &invalid))
}
