package service

import (
	"context"
	"strconv"
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

func parseTrafficRelayProxyID(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func parseTrafficRelayUnavailableTTLSeconds(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 45
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 45
	}
	if seconds < 5 {
		return 5
	}
	if seconds > 600 {
		return 600
	}
	return seconds
}
