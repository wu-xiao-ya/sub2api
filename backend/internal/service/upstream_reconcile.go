package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	upstreamReconcileDefaultIntervalMinutes = 60
	upstreamReconcileMinIntervalMinutes     = 10
	upstreamReconcileMaxIntervalMinutes     = 24 * 60
	upstreamReconcileRequestTimeout         = 30 * time.Second
	upstreamReconcileMaxBodyBytes           = 10 * 1024 * 1024
	upstreamReconcileDefaultLookbackHours   = 24
)

// UpstreamReconcileSettings controls the periodic reconciliation runner.
type UpstreamReconcileSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
}

// GetUpstreamReconcileSettings returns defaults when the setting is absent.
func (s *SettingService) GetUpstreamReconcileSettings(ctx context.Context) (*UpstreamReconcileSettings, error) {
	defaults := defaultUpstreamReconcileSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyUpstreamReconcileSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get upstream reconcile settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return defaults, nil
	}
	settings := *defaults
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return nil, fmt.Errorf("parse upstream reconcile settings: %w", err)
	}
	if settings.IntervalMinutes == 0 {
		settings.IntervalMinutes = defaults.IntervalMinutes
	}
	normalizeUpstreamReconcileSettings(&settings)
	return &settings, nil
}

// SetUpstreamReconcileSettings validates and persists the runner settings.
func (s *SettingService) SetUpstreamReconcileSettings(ctx context.Context, settings *UpstreamReconcileSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting repository is unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if settings.IntervalMinutes < upstreamReconcileMinIntervalMinutes || settings.IntervalMinutes > upstreamReconcileMaxIntervalMinutes {
		return fmt.Errorf(
			"interval_minutes must be between %d and %d", upstreamReconcileMinIntervalMinutes, upstreamReconcileMaxIntervalMinutes,
		)
	}
	normalizeUpstreamReconcileSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal upstream reconcile settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyUpstreamReconcileSettings, string(data))
}

func defaultUpstreamReconcileSettings() *UpstreamReconcileSettings {
	return &UpstreamReconcileSettings{Enabled: false, IntervalMinutes: upstreamReconcileDefaultIntervalMinutes}
}

func normalizeUpstreamReconcileSettings(settings *UpstreamReconcileSettings) {
	if settings.IntervalMinutes < upstreamReconcileMinIntervalMinutes {
		settings.IntervalMinutes = upstreamReconcileMinIntervalMinutes
	}
	if settings.IntervalMinutes > upstreamReconcileMaxIntervalMinutes {
		settings.IntervalMinutes = upstreamReconcileMaxIntervalMinutes
	}
}

// UpstreamReconcileService reconciles usage_logs.account_stats_cost against upstream panel logs.
type UpstreamReconcileService struct {
	accountRepo    AccountRepository
	usageLogRepo   UsageLogRepository
	settingService *SettingService
	httpUpstream   HTTPUpstream
}

func NewUpstreamReconcileService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	settingService *SettingService,
	httpUpstream HTTPUpstream,
) *UpstreamReconcileService {
	return &UpstreamReconcileService{
		accountRepo:    accountRepo,
		usageLogRepo:   usageLogRepo,
		settingService: settingService,
		httpUpstream:   httpUpstream,
	}
}

// newAPILogEntry represents one consumption record from /api/log/self.
type newAPILogEntry struct {
	ID               int64   `json:"id"`
	UserID           int     `json:"user_id"`
	CreatedAt        int64   `json:"created_at"`        // Unix timestamp (seconds)
	Type             int     `json:"type"`              // 1=prompt tokens, 2=completion
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Quota            int     `json:"quota"`             // Actual charged quota
	Content          string  `json:"content"`
	Username         string  `json:"username"`
	TokenName        string  `json:"token_name"`
	ModelRatio       float64 `json:"model_ratio"`       // May be present in newer new-api
	Group            string  `json:"group"`             // May be present in newer new-api
	Channel          string  `json:"channel"`
}

// newAPILogResponse wraps the paginated log list from /api/log/self.
type newAPILogResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    []newAPILogEntry `json:"data"`
}

// ReconcileAccount pulls upstream logs and backfills account_stats_cost for one account.
func (s *UpstreamReconcileService) ReconcileAccount(ctx context.Context, accountID int64, lookbackHours int) error {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return fmt.Errorf("service unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load account: %w", err)
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return fmt.Errorf("unsupported account platform or type")
	}
	token := strings.TrimSpace(account.GetCredential(UpstreamRatioSyncPanelTokenCredentialKey))
	if token == "" {
		return fmt.Errorf("upstream_panel_token not configured")
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	if lookbackHours <= 0 {
		lookbackHours = upstreamReconcileDefaultLookbackHours
	}
	startTimestamp := time.Now().Add(-time.Duration(lookbackHours) * time.Hour).Unix()

	// Build request to /api/log/self?p=0&order=&start_timestamp=X
	logURL := strings.TrimRight(baseURL, "/") + fmt.Sprintf("/api/log/self?p=0&start_timestamp=%d", startTimestamp)
	reqCtx, cancel := context.WithTimeout(ctx, upstreamReconcileRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, logURL, bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ResolveAccountProxyURL(account)
	req = req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	resp, err := s.httpUpstream.Do(WithAccountOutbound(req, account, proxyURL), proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("empty response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("unauthorized: check upstream_panel_token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http error: status %d", resp.StatusCode)
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, upstreamReconcileMaxBodyBytes+1))
	if readErr != nil {
		return fmt.Errorf("read response: %w", readErr)
	}
	if len(body) > upstreamReconcileMaxBodyBytes {
		return fmt.Errorf("response too large")
	}

	var logResp newAPILogResponse
	if err := json.Unmarshal(body, &logResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if !logResp.Success {
		return fmt.Errorf("upstream error: %s", logResp.Message)
	}

	if len(logResp.Data) == 0 {
		logger.LegacyPrintf("service.upstream_reconcile", "no upstream logs for account %d in lookback window", accountID)
		return nil
	}

	logger.LegacyPrintf("service.upstream_reconcile", "pulled %d log entries for account %d", len(logResp.Data), accountID)

	// Pull our usage_logs candidates for the same time window
	startTime := time.Unix(startTimestamp, 0)
	endTime := time.Now()
	candidates, err := s.usageLogRepo.FindReconcileCandidates(ctx, accountID, startTime, endTime, 10000)
	if err != nil {
		return fmt.Errorf("find reconcile candidates: %w", err)
	}

	logger.LegacyPrintf("service.upstream_reconcile", "found %d local usage_logs candidates for account %d", len(candidates), accountID)

	// Match upstream logs to our usage_logs by model + tokens + time proximity
	// new-api quota is in "points", QuotaPerUnit=500000 means $1 = 500000 quota
	quotaPerUnit := int64(500000) // TODO: make configurable via settings
	matches := matchUpstreamLogsToUsageLogs(logResp.Data, candidates, quotaPerUnit)

	if len(matches) == 0 {
		logger.LegacyPrintf("service.upstream_reconcile", "no matches found for account %d", accountID)
		return nil
	}

	// Batch update account_stats_cost
	updated, err := s.usageLogRepo.BatchUpdateAccountStatsCost(ctx, matches)
	if err != nil {
		return fmt.Errorf("batch update account_stats_cost: %w", err)
	}

	logger.LegacyPrintf("service.upstream_reconcile", "reconciled %d rows for account %d", updated, accountID)
	return nil
}

// matchUpstreamLogsToUsageLogs matches upstream panel logs to our usage_logs by model + tokens + time.
// Returns map[usageLogID]upstreamCost.
func matchUpstreamLogsToUsageLogs(
	upstreamLogs []newAPILogEntry,
	candidates []UsageLogReconcileCandidate,
	quotaPerUnit int64,
) map[int64]float64 {
	matches := make(map[int64]float64)
	if quotaPerUnit <= 0 {
		quotaPerUnit = 500000
	}

	// Build index: upstream logs by (model, prompt_tokens, completion_tokens, created_at_second)
	type logKey struct {
		model            string
		promptTokens     int
		completionTokens int
		createdAtSecond  int64
	}
	upstreamByKey := make(map[logKey][]newAPILogEntry)
	for _, entry := range upstreamLogs {
		key := logKey{
			model:            strings.ToLower(entry.Model),
			promptTokens:     entry.PromptTokens,
			completionTokens: entry.CompletionTokens,
			createdAtSecond:  entry.CreatedAt,
		}
		upstreamByKey[key] = append(upstreamByKey[key], entry)
	}

	// Match each candidate
	for _, c := range candidates {
		// Try exact match first (same second)
		key := logKey{
			model:            strings.ToLower(c.Model),
			promptTokens:     c.InputTokens,
			completionTokens: c.OutputTokens,
			createdAtSecond:  c.CreatedAt.Unix(),
		}
		upstreamMatches := upstreamByKey[key]
		if len(upstreamMatches) == 0 {
			// Try ±5 second window
			for offset := int64(-5); offset <= 5; offset++ {
				if offset == 0 {
					continue
				}
				key.createdAtSecond = c.CreatedAt.Unix() + offset
				upstreamMatches = upstreamByKey[key]
				if len(upstreamMatches) > 0 {
					break
				}
			}
		}

		if len(upstreamMatches) == 0 {
			continue
		}

		// Take the first match and compute cost from quota
		entry := upstreamMatches[0]
		upstreamCost := float64(entry.Quota) / float64(quotaPerUnit)
		matches[c.ID] = upstreamCost

		// Remove matched entry so it won't match again
		if len(upstreamMatches) > 1 {
			upstreamByKey[key] = upstreamMatches[1:]
		} else {
			delete(upstreamByKey, key)
		}
	}

	return matches
}

