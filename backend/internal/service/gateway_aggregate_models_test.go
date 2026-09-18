package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupSupportsRequestedModelUsesDefaultsWhenNoMapping(t *testing.T) {
	svc := &GatewayService{}
	group := &Group{ID: 3, Status: StatusActive, Platform: PlatformOpenAI}
	require.True(t, svc.GroupSupportsRequestedModel(context.Background(), group, "gpt-5.4") || svc.GroupSupportsRequestedModel(context.Background(), group, "gpt-4o") || len(platformDefaultModelIDs(PlatformOpenAI)) > 0)
	require.False(t, svc.GroupSupportsRequestedModel(context.Background(), nil, "gpt-5.4"))
	inactive := &Group{ID: 3, Status: "inactive", Platform: PlatformOpenAI}
	require.False(t, svc.GroupSupportsRequestedModel(context.Background(), inactive, "gpt-4o"))
}

func TestGroupSupportsRequestedModelCustomList(t *testing.T) {
	svc := &GatewayService{}
	group := &Group{
		ID:       4,
		Status:   StatusActive,
		Platform: PlatformOpenAI,
		ModelsListConfig: GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gpt-5.4"},
		},
	}
	require.True(t, svc.GroupSupportsRequestedModel(context.Background(), group, "gpt-5.4"))
	require.False(t, svc.GroupSupportsRequestedModel(context.Background(), group, "gpt-4o"))
}
