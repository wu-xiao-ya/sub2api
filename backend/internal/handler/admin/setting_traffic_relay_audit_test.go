package admin

import (
	"slices"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestTrafficRelaySettingsAreAudited(t *testing.T) {
	before := &service.SystemSettings{}
	after := &service.SystemSettings{TrafficRelayEnabled: true, TrafficRelayProxyID: 7, TrafficRelayUnavailableTTLSeconds: 45}
	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{})
	for _, key := range []string{"traffic_relay_enabled", "traffic_relay_proxy_id", "traffic_relay_unavailable_ttl_seconds"} {
		if !slices.Contains(changed, key) {
			t.Fatalf("missing audit field %q: %v", key, changed)
		}
	}
}
