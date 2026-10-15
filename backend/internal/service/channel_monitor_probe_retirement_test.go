//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeChannelMonitorMode(t *testing.T) {
	require.Equal(t, ChannelMonitorModeV2, normalizeChannelMonitorMode(""))
	require.Equal(t, ChannelMonitorModeV1, normalizeChannelMonitorMode("v1"))
	require.Equal(t, ChannelMonitorModeV2, normalizeChannelMonitorMode("v2"))
	require.Equal(t, ChannelMonitorModeV2, normalizeChannelMonitorMode("invalid"))
	require.Equal(t, ChannelMonitorModeV1, normalizeChannelMonitorMode(" V1 "))
}

func TestChannelMonitorRuntimeAllowsBothMonitorLayers(t *testing.T) {
	require.False(t, (ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeV1}).ActiveProbesAllowed())
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}).ActiveProbesAllowed())
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}).ActiveProbesAllowed())
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}).PassiveAggregationAllowed())
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}).PassiveAggregationAllowed())
}
