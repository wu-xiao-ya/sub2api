package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *SettingService) validateTrafficRelay(ctx context.Context, settings *SystemSettings) error {
	if settings.TrafficRelayUnavailableTTLSeconds == 0 {
		settings.TrafficRelayUnavailableTTLSeconds = 45
	}
	if settings.TrafficRelayUnavailableTTLSeconds < 5 || settings.TrafficRelayUnavailableTTLSeconds > 600 {
		return infraerrors.BadRequest("INVALID_TRAFFIC_RELAY_TTL", "relay cooldown must be between 5 and 600 seconds")
	}
	if settings.TrafficRelayProxyID < 0 {
		return infraerrors.BadRequest("INVALID_TRAFFIC_RELAY_PROXY", "relay proxy ID cannot be negative")
	}
	// Disabling must remain possible even after the referenced proxy was deleted.
	if !settings.TrafficRelayEnabled {
		return nil
	}
	if settings.TrafficRelayProxyID == 0 || s.proxyRepo == nil {
		return infraerrors.BadRequest("INVALID_TRAFFIC_RELAY_PROXY", "an active HTTP proxy is required")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, settings.TrafficRelayProxyID)
	if err != nil || proxy == nil || !proxy.IsActive() || strings.ToLower(strings.TrimSpace(proxy.Protocol)) != "http" {
		return infraerrors.BadRequest("INVALID_TRAFFIC_RELAY_PROXY", "relay proxy must exist, be active, and use HTTP")
	}
	return nil
}
