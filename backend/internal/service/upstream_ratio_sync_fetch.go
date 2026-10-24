package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// newAPIPricingURL normalizes a custom base_url into the new-api panel
// pricing endpoint. Bases may carry a "/v1" suffix (gateway-style base URLs)
// which must be stripped because /api/pricing is a panel route.
func newAPIPricingURL(baseURL string) (string, error) {
	trimmed := strings.TrimSpace(baseURL)
	for _, suffix := range []string{"/", "/v1/", "/v1"} {
		for strings.HasSuffix(trimmed, suffix) {
			trimmed = strings.TrimSuffix(trimmed, suffix)
		}
	}
	if trimmed == "" {
		return "", fmt.Errorf("invalid_base_url")
	}
	return trimmed + "/api/pricing", nil
}

// persistSyncFailure records a failed/unsupported attempt while keeping the
// previous successful snapshot data visible (same contract as the billing probe).
func (s *UpstreamRatioSyncService) persistSyncFailure(
	ctx context.Context,
	account *Account,
	intervalMinutes int,
	now time.Time,
	statusCode int,
	reason string,
	retryAfterDuration time.Duration,
) (*UpstreamRatioSyncSnapshot, error) {
	previous := decodeUpstreamRatioSyncSnapshot(account.Extra)
	failureCount := 1
	if previous != nil {
		failureCount = previous.FailureCount + 1
	}
	status := UpstreamRatioSyncStatusFailed
	if reason == "unsupported" {
		status = UpstreamRatioSyncStatusUnsupported
	}
	snapshot := &UpstreamRatioSyncSnapshot{
		Status:        status,
		Group:         upstreamRatioSyncGroup(account),
		LastAttemptAt: now,
		NextSyncAt:    now.Add(nextProbeDelay(intervalMinutes, retryAfterDuration)),
		FailureCount:  failureCount,
		HTTPStatus:    statusCode,
		LastError:     reason,
	}
	if previous != nil {
		snapshot.ModelCount = previous.ModelCount
		snapshot.GroupRatio = previous.GroupRatio
		snapshot.ReceivedAt = previous.ReceivedAt
		snapshot.FreshUntil = previous.FreshUntil
		if snapshot.FreshUntil == nil && previous.Status == UpstreamRatioSyncStatusOK && previous.ReceivedAt != nil {
			snapshot.FreshUntil = probeTimePtr(previous.ReceivedAt.Add(2 * time.Duration(intervalMinutes) * time.Minute))
		}
	}
	if err := s.updateSyncSnapshot(ctx, account, snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *UpstreamRatioSyncService) updateSyncSnapshot(ctx context.Context, account *Account, snapshot *UpstreamRatioSyncSnapshot) error {
	writer, ok := s.accountRepo.(upstreamRatioSyncSnapshotWriter)
	if !ok {
		return ErrUpstreamRatioSyncUnavailable
	}
	// Observational extra merge: the ratio snapshot only ever touches its own
	// JSONB key, so a plain atomic merge is enough (no scheduling CAS needed).
	return writer.UpdateExtra(ctx, account.ID, map[string]any{
		UpstreamRatioSyncExtraKey: snapshot,
	})
}

// syncLoadedAccount fetches and stores one account's ratio table.
func (s *UpstreamRatioSyncService) syncLoadedAccount(ctx context.Context, account *Account, intervalMinutes int) (*UpstreamRatioSyncSnapshot, error) {
	now := s.currentTime().UTC()
	if s.accountTestService == nil || s.accountTestService.httpUpstream == nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, "transport_unavailable", 0)
	}
	rawBaseURL := strings.TrimSpace(account.GetCredential("base_url"))
	normalizedBaseURL, err := s.accountTestService.validateUpstreamBaseURL(rawBaseURL)
	if err != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, "invalid_base_url", 0)
	}
	pricingURL, err := newAPIPricingURL(normalizedBaseURL)
	if err != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, "invalid_base_url", 0)
	}
	req, tlsProfile, err := s.buildUpstreamRatioSyncRequest(ctx, account, pricingURL)
	if err != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, err.Error(), 0)
	}
	syncCtx, cancel := context.WithTimeout(ctx, upstreamRatioSyncRequestTimeout)
	defer cancel()
	req = req.WithContext(syncCtx)
	reqCtx := req.Context()
	if account.Platform == PlatformOpenAI {
		reqCtx = WithHTTPUpstreamProfile(reqCtx, HTTPUpstreamProfileOpenAI)
	}
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(reqCtx))
	proxyURL := ResolveAccountProxyURL(account)
	resp, err := s.accountTestService.httpUpstream.DoWithTLS(WithAccountOutbound(req, account, proxyURL), proxyURL, account.ID, account.Concurrency, tlsProfile)
	if err != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, "request_failed", 0)
	}
	if resp == nil || resp.Body == nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, 0, "empty_response", 0)
	}
	defer func() { _ = resp.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, upstreamRatioSyncMaxBodyBytes+1))
	if readErr != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, "response_read_failed", retryAfter(resp.Header, now))
	}
	if len(body) > upstreamRatioSyncMaxBodyBytes {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, "response_too_large", retryAfter(resp.Header, now))
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, "unsupported", retryAfter(resp.Header, now))
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, UpstreamRatioSyncReasonUnauthorized, retryAfter(resp.Header, now))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, "http_error", retryAfter(resp.Header, now))
	}
	group := upstreamRatioSyncGroup(account)
	rows, groupRatio, parseErr := ParseUpstreamRatioPricing(body, group)
	if parseErr != nil {
		return s.persistSyncFailure(ctx, account, intervalMinutes, now, resp.StatusCode, "invalid_response", retryAfter(resp.Header, now))
	}
	if len(rows) > upstreamRatioSyncMaxModels {
		rows = rows[:upstreamRatioSyncMaxModels]
	}
	observedAt := now
	if s.snapshotRepo != nil {
		if err := s.snapshotRepo.ReplaceUpstreamModelRatioSnapshot(ctx, account.ID, observedAt, rows); err != nil {
			return nil, err
		}
	}
	snapshot := &UpstreamRatioSyncSnapshot{
		Status:        UpstreamRatioSyncStatusOK,
		ModelCount:    len(rows),
		Group:         group,
		GroupRatio:    probeFloatPtr(groupRatio),
		ReceivedAt:    probeTimePtr(now),
		FreshUntil:    probeTimePtr(now.Add(2 * time.Duration(intervalMinutes) * time.Minute)),
		LastAttemptAt: now,
		NextSyncAt:    now.Add(nextProbeDelay(intervalMinutes, 0)),
		HTTPStatus:    resp.StatusCode,
	}
	if err := s.updateSyncSnapshot(ctx, account, snapshot); err != nil {
		return nil, err
	}
	s.refreshCache(account.ID, rows, observedAt, intervalMinutes)
	return snapshot, nil
}

func (s *UpstreamRatioSyncService) buildUpstreamRatioSyncRequest(ctx context.Context, account *Account, pricingURL string) (*http.Request, *tlsfingerprint.Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pricingURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	// Prefer the panel access token (new-api system token) for upstreams whose
	// pricing endpoint requires authentication; the model API key is sent as a
	// harmless fallback header that public endpoints ignore.
	if panelToken := strings.TrimSpace(account.GetCredential(UpstreamRatioSyncPanelTokenCredentialKey)); panelToken != "" {
		req.Header.Set("Authorization", panelToken)
	} else if apiKey := strings.TrimSpace(account.GetCredential("api_key")); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	account.ApplyHeaderOverrides(req.Header)
	var tlsProfile *tlsfingerprint.Profile
	if s.accountTestService.tlsFPProfileService != nil {
		tlsProfile = s.accountTestService.tlsFPProfileService.ResolveTLSProfile(account)
	}
	return req, tlsProfile, nil
}

func probeFloatPtr(value float64) *float64 {
	return &value
}
