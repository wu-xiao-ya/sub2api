package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const internalMonitorAPIKeyMarker = "__sub2api_internal_monitor_key__"

type InternalMonitorKey struct {
	ID        int64
	Name      string
	UserID    int64
	GroupID   int64
	GroupName string
	Provider  string
	Status    string
	ExpiresAt *time.Time
}

type InternalMonitorKeyEnsureItem struct {
	InternalMonitorKey
	PlainKey string `json:"plain_key,omitempty"`
	Created  bool   `json:"created"`
}

type InternalMonitorKeyEnsureResult struct {
	UserID    int64                          `json:"user_id"`
	UserEmail string                         `json:"user_email"`
	Items     []InternalMonitorKeyEnsureItem `json:"items"`
}

type channelMonitorInternalKeyCatalog interface {
	ListMonitoringMonitorKeys(ctx context.Context) ([]InternalMonitorKey, error)
	EnsureMonitoringMonitorKeys(ctx context.Context, groupIDs []int64) (*InternalMonitorKeyEnsureResult, error)
}

func normalizeMonitorSourceMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), MonitorSourceInternalGateway) {
		return MonitorSourceInternalGateway
	}
	return MonitorSourceDirectUpstream
}

func NormalizeMonitorSourceModeForAPI(mode string) string {
	return normalizeMonitorSourceMode(mode)
}

func validateMonitorSourceParams(mode, apiKey string, keyID, groupID *int64) error {
	mode = normalizeMonitorSourceMode(mode)
	if mode == MonitorSourceInternalGateway {
		if keyID == nil || *keyID <= 0 {
			return fmt.Errorf("internal gateway monitor requires an internal API key")
		}
		if groupID == nil || *groupID <= 0 {
			return fmt.Errorf("internal gateway monitor requires an internal group")
		}
		return nil
	}
	if strings.TrimSpace(apiKey) == "" {
		return ErrChannelMonitorMissingAPIKey
	}
	return nil
}

func internalMonitorGatewayURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", fmt.Errorf("internal gateway URL is not configured")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("internal gateway URL must be a local http origin")
	}
	host := strings.TrimSpace(u.Hostname())
	if host != "localhost" && !isLoopbackHost(host) {
		return "", fmt.Errorf("internal gateway URL must use localhost or a loopback address")
	}
	return raw, nil
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *ChannelMonitorService) resolveInternalMonitorKey(ctx context.Context, m *ChannelMonitor) (*APIKey, error) {
	if m == nil || m.InternalAPIKeyID == nil || *m.InternalAPIKeyID <= 0 {
		return nil, fmt.Errorf("internal monitor API key is missing")
	}
	if s == nil || s.internalAPIKeyResolver == nil {
		return nil, fmt.Errorf("internal monitor API key resolver is not configured")
	}
	key, err := s.internalAPIKeyResolver.GetByID(ctx, *m.InternalAPIKeyID)
	if err != nil {
		return nil, fmt.Errorf("load internal monitor API key: %w", err)
	}
	if key == nil || key.User == nil || !key.User.IsMonitoringUser {
		return nil, fmt.Errorf("API key does not belong to a monitoring user")
	}
	if key.Status != StatusAPIKeyActive || key.IsExpired() {
		return nil, fmt.Errorf("internal monitor API key is inactive or expired")
	}
	if key.GroupID == nil || m.InternalGroupID == nil || *key.GroupID != *m.InternalGroupID {
		return nil, fmt.Errorf("internal monitor API key group no longer matches monitor")
	}
	if key.Group == nil || !key.Group.IsActive() {
		return nil, fmt.Errorf("internal monitor API key group is unavailable")
	}
	if key.Group.Platform != m.Provider && !(m.Provider == MonitorProviderOpenAI && key.Group.Platform == PlatformOpenAI) {
		return nil, fmt.Errorf("internal monitor API key group platform does not match monitor")
	}
	return key, nil
}

func (s *ChannelMonitorService) ListInternalMonitorKeys(ctx context.Context) ([]InternalMonitorKey, error) {
	if s == nil || s.internalAPIKeyResolver == nil {
		return nil, fmt.Errorf("internal monitor API key resolver is not configured")
	}
	if catalog, ok := s.internalAPIKeyResolver.(channelMonitorInternalKeyCatalog); ok {
		return catalog.ListMonitoringMonitorKeys(ctx)
	}
	monitors, _, err := s.repo.List(ctx, ChannelMonitorListParams{Page: 1, PageSize: 100})
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]struct{})
	out := make([]InternalMonitorKey, 0)
	for _, monitor := range monitors {
		if monitor == nil || normalizeMonitorSourceMode(monitor.SourceMode) != MonitorSourceInternalGateway ||
			monitor.InternalAPIKeyID == nil {
			continue
		}
		id := *monitor.InternalAPIKeyID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		key, keyErr := s.internalAPIKeyResolver.GetByID(ctx, id)
		if keyErr != nil || key == nil || key.User == nil || !key.User.IsMonitoringUser ||
			key.GroupID == nil || key.Group == nil {
			continue
		}
		out = append(out, InternalMonitorKey{
			ID: id, Name: key.Name, UserID: key.UserID, GroupID: *key.GroupID,
			GroupName: key.Group.Name, Provider: key.Group.Platform, Status: key.Status,
			ExpiresAt: key.ExpiresAt,
		})
	}
	return out, nil
}

func (s *ChannelMonitorService) EnsureInternalMonitorKeys(ctx context.Context, groupIDs []int64) (*InternalMonitorKeyEnsureResult, error) {
	if s == nil || s.internalAPIKeyResolver == nil {
		return nil, fmt.Errorf("internal monitor API key resolver is not configured")
	}
	catalog, ok := s.internalAPIKeyResolver.(channelMonitorInternalKeyCatalog)
	if !ok {
		return nil, fmt.Errorf("internal monitor API key initializer is not configured")
	}
	return catalog.EnsureMonitoringMonitorKeys(ctx, groupIDs)
}
