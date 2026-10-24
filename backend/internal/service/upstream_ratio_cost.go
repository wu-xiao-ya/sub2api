package service

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const upstreamRatioSyncStaleRetention = 7 * 24 * time.Hour

// refreshCache installs a freshly synced ratio table into the in-memory cache.
func (s *UpstreamRatioSyncService) refreshCache(accountID int64, rows []UpstreamModelRatio, observedAt time.Time, intervalMinutes int) {
	if s == nil {
		return
	}
	settings := defaultUpstreamRatioSyncSettings()
	if s.settingService != nil {
		if loaded, err := s.settingService.GetUpstreamRatioSyncSettings(context.Background()); err == nil && loaded != nil {
			settings = loaded
		}
	}
	byModel := make(map[string]UpstreamModelRatio, len(rows))
	for _, row := range rows {
		byModel[row.ModelName] = row
		byModel[strings.ToLower(row.ModelName)] = row
	}
	entry := &upstreamRatioCacheEntry{
		rows:         byModel,
		observedAt:   observedAt,
		freshUntil:   observedAt.Add(2 * time.Duration(intervalMinutes) * time.Minute),
		quotaPerUnit: float64(settings.QuotaPerUnit),
	}
	s.cacheMu.Lock()
	s.cache[accountID] = entry
	s.cacheMu.Unlock()
}

// loadCacheEntry lazily hydrates the cache from the latest stored snapshot.
func (s *UpstreamRatioSyncService) loadCacheEntry(ctx context.Context, accountID int64) *upstreamRatioCacheEntry {
	if s == nil || s.snapshotRepo == nil {
		return nil
	}
	value, err, _ := s.loadGroup.Do(strconv.FormatInt(accountID, 10), func() (any, error) {
		s.cacheMu.RLock()
		existing := s.cache[accountID]
		s.cacheMu.RUnlock()
		if existing != nil {
			return existing, nil
		}
		interval := upstreamRatioSyncDefaultIntervalMinutes
		quotaPerUnit := float64(upstreamRatioSyncDefaultQuotaPerUnit)
		if s.settingService != nil {
			if settings, settingsErr := s.settingService.GetUpstreamRatioSyncSettings(ctx); settingsErr == nil && settings != nil {
				interval = settings.IntervalMinutes
				quotaPerUnit = float64(settings.QuotaPerUnit)
			}
		}
		rows, observedAt, repoErr := s.snapshotRepo.LatestUpstreamModelRatioRows(ctx, accountID, upstreamRatioSyncMaxModels)
		if repoErr != nil || len(rows) == 0 {
			// Negative cache so a missing table does not hit the DB per request.
			return &upstreamRatioCacheEntry{
				rows:         nil,
				observedAt:   s.currentTime(),
				freshUntil:   s.currentTime().Add(upstreamRatioSyncNegativeCacheTTL),
				quotaPerUnit: quotaPerUnit,
			}, nil
		}
		byModel := make(map[string]UpstreamModelRatio, len(rows)*2)
		for _, row := range rows {
			byModel[row.ModelName] = row
			byModel[strings.ToLower(row.ModelName)] = row
		}
		return &upstreamRatioCacheEntry{
			rows:         byModel,
			observedAt:   observedAt,
			freshUntil:   observedAt.Add(2 * time.Duration(interval) * time.Minute),
			quotaPerUnit: quotaPerUnit,
		}, nil
	})
	if err != nil {
		return nil
	}
	entry, ok := value.(*upstreamRatioCacheEntry)
	if !ok {
		return nil
	}
	s.cacheMu.Lock()
	if current := s.cache[accountID]; current == nil {
		s.cache[accountID] = entry
	} else {
		entry = current
	}
	s.cacheMu.Unlock()
	return entry
}

// upstreamRatioSyncCost is the BillingService-facing hook used by
// resolveAccountStatsCost priority 3. Safe on a nil receiver.
func (s *BillingService) upstreamRatioSyncCost(accountID int64, upstreamModel string, tokens UsageTokens, requestCount int) *float64 {
	if s == nil || s.upstreamRatioSync == nil {
		return nil
	}
	return s.upstreamRatioSync.UpstreamRatioCost(context.Background(), accountID, upstreamModel, tokens, requestCount)
}

// UpstreamRatioCost resolves the upstream cost of one request from the synced
// new-api ratio table. A nil return means "no fresh synced entry for this
// account+model" and lets the caller fall back to the next pricing source.
func (s *UpstreamRatioSyncService) UpstreamRatioCost(ctx context.Context, accountID int64, upstreamModel string, tokens UsageTokens, requestCount int) *float64 {
	if s == nil || s.snapshotRepo == nil || accountID <= 0 || strings.TrimSpace(upstreamModel) == "" {
		return nil
	}
	s.cacheMu.RLock()
	entry := s.cache[accountID]
	s.cacheMu.RUnlock()
	if entry == nil {
		entry = s.loadCacheEntry(ctx, accountID)
		if entry == nil {
			return nil
		}
	}
	now := s.currentTime()
	if entry.rows == nil {
		// Negative cache: no synced table for this account.
		return nil
	}
	if now.After(entry.freshUntil) {
		// Keep the last known-good upstream price for a bounded period. This
		// preserves cost observability during a temporary panel outage while
		// preventing an indefinitely stale snapshot from becoming authoritative.
		if entry.observedAt.IsZero() || now.After(entry.observedAt.Add(upstreamRatioSyncStaleRetention)) {
			return nil
		}
	}
	row, ok := entry.rows[upstreamModel]
	if !ok {
		return nil
	}
	cost := row.CostUSD(tokens, requestCount, entry.quotaPerUnit)
	return &cost
}
