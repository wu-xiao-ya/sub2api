package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupUpstreamProviderValidation(t *testing.T) {
	for _, provider := range []string{"", "qwen", "hunyuan", "mimo"} {
		got, err := NormalizeGroupUpstreamProvider(PlatformOpenAI, provider)
		require.NoError(t, err)
		require.Equal(t, provider, got)
	}
	for _, tc := range [][2]string{{PlatformAnthropic, "qwen"}, {PlatformKimi, "mimo"}, {PlatformOpenAI, "other"}, {PlatformOpenAI, " Qwen "}} {
		_, err := NormalizeGroupUpstreamProvider(tc[0], tc[1])
		require.Error(t, err)
	}
}
func TestUpdateGroupBrandPreservesRoutingAndInvalidatesDisplay(t *testing.T) {
	repo := &groupPlatformRepoStub{group: &Group{ID: 7, Name: "domestic", Platform: PlatformOpenAI, UpstreamProvider: "qwen"}}
	spy := &channelCacheInvalidatorSpy{}
	svc := &adminServiceImpl{groupRepo: repo, channelCacheInvalidator: spy}
	g, err := svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "qwen", g.UpstreamProvider)
	require.Equal(t, PlatformOpenAI, g.Platform)
	provider := "mimo"
	g, err = svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{UpstreamProvider: &provider})
	require.NoError(t, err)
	require.Equal(t, "mimo", g.UpstreamProvider)
	require.Equal(t, PlatformOpenAI, g.Platform)
	require.Equal(t, 1, spy.calls)
	g, err = svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{Platform: PlatformAnthropic})
	require.NoError(t, err)
	require.Empty(t, g.UpstreamProvider)
}
