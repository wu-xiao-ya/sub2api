package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type internalMonitorResolverStub struct {
	key *APIKey
	err error
}

func (s internalMonitorResolverStub) GetByID(context.Context, int64) (*APIKey, error) {
	return s.key, s.err
}

func TestIsMonitoringKeyPlatform(t *testing.T) {
	require.True(t, isMonitoringKeyPlatform(PlatformDeepSeek))
	require.True(t, isMonitoringKeyPlatform(PlatformMiMo))
	require.True(t, isMonitoringKeyPlatform(PlatformHunyuan))
	require.False(t, isMonitoringKeyPlatform(PlatformOpenAI))
	require.False(t, isMonitoringKeyPlatform("unknown"))
}

func TestInternalMonitorGatewayURLOnlyAllowsLoopbackOrigin(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:8080", mustInternalMonitorGatewayURL(t, "http://127.0.0.1:8080/"))
	require.Equal(t, "http://[::1]:8080", mustInternalMonitorGatewayURL(t, "http://[::1]:8080"))
	require.Equal(t, "http://localhost:8080", mustInternalMonitorGatewayURL(t, "http://localhost:8080"))
	_, err := internalMonitorGatewayURL("http://example.invalid:8080")
	require.Error(t, err)
	_, err = internalMonitorGatewayURL("http://127.0.0.1:8080/admin")
	require.Error(t, err)
}

func mustInternalMonitorGatewayURL(t *testing.T, raw string) string {
	t.Helper()
	value, err := internalMonitorGatewayURL(raw)
	require.NoError(t, err)
	return value
}

func TestValidateInternalMonitorBindingRequiresStationMonitoringUser(t *testing.T) {
	group := &Group{ID: 7, Name: "GLM", Platform: PlatformGLM, Status: StatusActive}
	keyID := int64(11)
	groupID := int64(7)
	monitor := &ChannelMonitor{
		Provider:         MonitorProviderGLM,
		SourceMode:       MonitorSourceInternalGateway,
		InternalAPIKeyID: &keyID,
		InternalGroupID:  &groupID,
	}
	svc := NewChannelMonitorService(nil, nil)
	svc.SetInternalGatewayDependencies(internalMonitorResolverStub{
		key: &APIKey{
			ID: keyID, User: &User{IsMonitoringUser: false},
			GroupID: &groupID, Group: group, Status: StatusAPIKeyActive,
		},
	}, "http://127.0.0.1:8080")

	err := svc.validateInternalMonitorBinding(context.Background(), monitor)
	require.Error(t, err)
	require.Contains(t, err.Error(), "monitoring user")
}

func TestValidateInternalMonitorBindingAcceptsMatchingStationKey(t *testing.T) {
	group := &Group{ID: 7, Name: "GLM", Platform: PlatformGLM, Status: StatusActive}
	keyID := int64(11)
	groupID := int64(7)
	monitor := &ChannelMonitor{
		Provider:         MonitorProviderGLM,
		SourceMode:       MonitorSourceInternalGateway,
		InternalAPIKeyID: &keyID,
		InternalGroupID:  &groupID,
		Endpoint:         "http://example.invalid",
	}
	svc := NewChannelMonitorService(nil, nil)
	svc.SetInternalGatewayDependencies(internalMonitorResolverStub{
		key: &APIKey{
			ID: keyID, User: &User{IsMonitoringUser: true},
			GroupID: &groupID, Group: group, Status: StatusAPIKeyActive,
		},
	}, "http://127.0.0.1:8080")

	require.NoError(t, svc.validateInternalMonitorBinding(context.Background(), monitor))
	require.Equal(t, "http://127.0.0.1:8080", monitor.Endpoint)
}
