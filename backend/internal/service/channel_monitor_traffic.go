package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	accountProbeModeTraffic = "traffic"

	defaultTrafficFallbackIdleSeconds      = 1800
	defaultTrafficAggregationWindowSeconds = 300
	defaultTrafficMinimumSamples           = 1
	trafficHistoryWriteInterval            = time.Minute
	trafficObservationDegradedThresholdMs  = 10_000
)

// ChannelMonitorTrafficObservationSettings controls whether account-group text
// monitors can use successful end-user requests as their health signal before
// spending money on an active probe.
type ChannelMonitorTrafficObservationSettings struct {
	Enabled                  bool `json:"enabled"`
	FallbackIdleSeconds      int  `json:"fallback_idle_seconds"`
	AggregationWindowSeconds int  `json:"aggregation_window_seconds"`
	MinimumSamples           int  `json:"minimum_samples"`
}

func DefaultChannelMonitorTrafficObservationSettings() ChannelMonitorTrafficObservationSettings {
	return ChannelMonitorTrafficObservationSettings{
		Enabled:                  false,
		FallbackIdleSeconds:      defaultTrafficFallbackIdleSeconds,
		AggregationWindowSeconds: defaultTrafficAggregationWindowSeconds,
		MinimumSamples:           defaultTrafficMinimumSamples,
	}
}

func normalizeChannelMonitorTrafficObservationSettings(
	settings ChannelMonitorTrafficObservationSettings,
) ChannelMonitorTrafficObservationSettings {
	defaults := DefaultChannelMonitorTrafficObservationSettings()
	if settings.FallbackIdleSeconds < 60 || settings.FallbackIdleSeconds > 86400 {
		settings.FallbackIdleSeconds = defaults.FallbackIdleSeconds
	}
	if settings.AggregationWindowSeconds < 30 || settings.AggregationWindowSeconds > 3600 {
		settings.AggregationWindowSeconds = defaults.AggregationWindowSeconds
	}
	if settings.MinimumSamples < 1 || settings.MinimumSamples > 20 {
		settings.MinimumSamples = defaults.MinimumSamples
	}
	return settings
}

type channelMonitorTrafficSettingsProvider interface {
	GetChannelMonitorTrafficObservationSettings(context.Context) ChannelMonitorTrafficObservationSettings
}

// ChannelMonitorTrafficSample is intentionally compact. The monitor scheduler
// only needs successful request timing and selected-account attribution.
type ChannelMonitorTrafficSample struct {
	AccountID    int64
	Model        string
	DurationMs   int
	FirstTokenMs int
	CreatedAt    time.Time
}

// channelMonitorTrafficUsageRepository stays optional so the monitoring service
// remains compatible with existing narrow repository test doubles.
type channelMonitorTrafficUsageRepository interface {
	ListRecentChannelMonitorTraffic(
		ctx context.Context,
		groupID int64,
		accountIDs []int64,
		models []string,
		since time.Time,
		limit int,
	) ([]ChannelMonitorTrafficSample, error)
}

func (s *ChannelMonitorService) SetTrafficObservationDependencies(
	usageRepo any,
	settingsProvider channelMonitorTrafficSettingsProvider,
) {
	if repo, ok := usageRepo.(channelMonitorTrafficUsageRepository); ok {
		s.trafficUsageRepo = repo
	}
	s.trafficSettingsProvider = settingsProvider
}

func (s *ChannelMonitorService) trafficObservationSettings(
	ctx context.Context,
) ChannelMonitorTrafficObservationSettings {
	if s == nil || s.trafficSettingsProvider == nil {
		return DefaultChannelMonitorTrafficObservationSettings()
	}
	return normalizeChannelMonitorTrafficObservationSettings(
		s.trafficSettingsProvider.GetChannelMonitorTrafficObservationSettings(ctx),
	)
}

func isTrafficObservationEligible(m *ChannelMonitor) bool {
	if m == nil || m.AccountGroupID == nil || *m.AccountGroupID <= 0 {
		return false
	}
	return defaultAPIMode(m.APIMode) != MonitorAPIModeImages
}

// collectTrafficObservations returns one result for every model with enough
// recent successful user traffic. Models without traffic stay in missing so
// the scheduler can actively probe only those models.
func (s *ChannelMonitorService) collectTrafficObservations(
	ctx context.Context,
	m *ChannelMonitor,
) (results []*CheckResult, missing []string, applicable bool) {
	if s == nil || !isTrafficObservationEligible(m) || s.trafficUsageRepo == nil || s.accountProbeRepo == nil {
		return nil, nil, false
	}
	settings := s.trafficObservationSettings(ctx)
	if !settings.Enabled {
		return nil, nil, false
	}

	groupPlatform, err := s.accountProbeRepo.GetGroupPlatform(ctx, *m.AccountGroupID)
	if err != nil || !monitorProviderSupportsAccountPlatform(m.Provider, groupPlatform) {
		return nil, nil, false
	}
	accounts, err := s.accountProbeRepo.ListSchedulableByGroupIDAndPlatform(ctx, *m.AccountGroupID, groupPlatform)
	if err != nil || len(accounts) == 0 {
		return nil, nil, false
	}

	models := uniqueMonitorModels(m)
	if len(models) == 0 {
		return nil, nil, false
	}
	eligible := make(map[string]map[int64]Account, len(models))
	accountIDs := make(map[int64]struct{}, len(accounts))
	for _, model := range models {
		eligible[model] = make(map[int64]Account)
		for _, account := range accounts {
			if account.ID <= 0 || !account.IsModelSupported(model) {
				continue
			}
			eligible[model][account.ID] = account
			accountIDs[account.ID] = struct{}{}
		}
	}
	if len(accountIDs) == 0 {
		return nil, nil, false
	}

	now := time.Now().UTC()
	samples, err := s.trafficUsageRepo.ListRecentChannelMonitorTraffic(
		ctx,
		*m.AccountGroupID,
		int64SetToSortedSlice(accountIDs),
		models,
		now.Add(-time.Duration(settings.FallbackIdleSeconds)*time.Second),
		trafficSampleQueryLimit(len(models)),
	)
	if err != nil || len(samples) == 0 {
		return nil, append([]string(nil), models...), true
	}

	results = make([]*CheckResult, 0, len(models))
	missing = make([]string, 0, len(models))
	for _, model := range models {
		result, ok := buildTrafficObservationResult(
			model,
			samples,
			eligible[model],
			now,
			settings,
		)
		if !ok {
			missing = append(missing, model)
			continue
		}
		results = append(results, result)
	}
	return results, missing, true
}

func trafficSampleQueryLimit(modelCount int) int {
	if modelCount < 1 {
		return 100
	}
	limit := modelCount * 100
	if limit > 500 {
		return 500
	}
	return limit
}

// runTrafficObservationIfConfigured preserves the all-model contract used by
// grouped probes. Single-monitor scheduling uses collectTrafficObservations so
// it can combine real traffic with active probes per model.
func (s *ChannelMonitorService) runTrafficObservationIfConfigured(
	ctx context.Context,
	m *ChannelMonitor,
) (results []*CheckResult, handled bool) {
	results, missing, applicable := s.collectTrafficObservations(ctx, m)
	return results, applicable && len(results) > 0 && len(missing) == 0
}

func int64SetToSortedSlice(values map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(values))
	for id := range values {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func buildTrafficObservationResult(
	model string,
	allSamples []ChannelMonitorTrafficSample,
	eligibleAccounts map[int64]Account,
	now time.Time,
	settings ChannelMonitorTrafficObservationSettings,
) (*CheckResult, bool) {
	windowStart := now.Add(-time.Duration(settings.AggregationWindowSeconds) * time.Second)
	recent := make([]ChannelMonitorTrafficSample, 0)
	fresh := make([]ChannelMonitorTrafficSample, 0)
	for _, sample := range allSamples {
		if sample.Model != model || sample.latencyMs() <= 0 || sample.CreatedAt.IsZero() {
			continue
		}
		if _, ok := eligibleAccounts[sample.AccountID]; !ok {
			continue
		}
		fresh = append(fresh, sample)
		if !sample.CreatedAt.Before(windowStart) {
			recent = append(recent, sample)
		}
	}
	if len(fresh) == 0 {
		return nil, false
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].CreatedAt.After(fresh[j].CreatedAt) })
	if fresh[0].CreatedAt.Before(now.Add(-time.Duration(settings.FallbackIdleSeconds) * time.Second)) {
		return nil, false
	}
	if len(recent) < settings.MinimumSamples {
		// The aggregation window controls how current the latency median is,
		// while fallback_idle_seconds controls whether a paid active probe is
		// necessary at all. A quiet-but-still-fresh group should not start
		// spending on probes just because its last real request predates the
		// shorter display window.
		recent = fresh
	}
	if len(recent) < settings.MinimumSamples {
		return nil, false
	}

	durations := make([]int, 0, len(recent))
	observedAccounts := make(map[int64]struct{}, len(recent))
	for _, sample := range recent {
		if sample.FirstTokenMs > 0 {
			durations = append(durations, sample.FirstTokenMs)
		}
		observedAccounts[sample.AccountID] = struct{}{}
	}
	if len(durations) < settings.MinimumSamples {
		durations = durations[:0]
		for _, sample := range recent {
			if value := sample.latencyMs(); value > 0 {
				durations = append(durations, value)
			}
		}
	}
	sort.Ints(durations)
	if len(durations) < settings.MinimumSamples {
		return nil, false
	}
	latency := durations[len(durations)/2]
	account := eligibleAccounts[fresh[0].AccountID]
	accountID := account.ID
	status := MonitorStatusOperational
	// Traffic observations use a wider threshold than active probes. A real
	// request includes client/network and upstream variability, so 10 seconds
	// is the green boundary for traffic-derived status.
	if latency > trafficObservationDegradedThresholdMs {
		status = MonitorStatusDegraded
	}
	message := fmt.Sprintf(
		"real traffic observation: %d successful request(s), %s median, latest %s ago",
		len(recent),
		trafficLatencyKind(recent),
		formatTrafficObservationAge(now.Sub(fresh[0].CreatedAt)),
	)
	return &CheckResult{
		Model:          model,
		Status:         status,
		LatencyMs:      &latency,
		AccountID:      &accountID,
		AccountName:    strings.TrimSpace(account.Name),
		ProbeMode:      accountProbeModeTraffic,
		CandidateCount: len(eligibleAccounts),
		HealthyCount:   len(observedAccounts),
		Message:        message,
		CheckedAt:      fresh[0].CreatedAt.UTC(),
	}, true
}

func formatTrafficObservationAge(age time.Duration) string {
	if age < time.Minute {
		return "under 1 minute"
	}
	if age < time.Hour {
		return fmt.Sprintf("%d minute(s)", int(age.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("%d hour(s)", int(age.Round(time.Hour)/time.Hour))
}

func (s ChannelMonitorTrafficSample) latencyMs() int {
	if s.FirstTokenMs > 0 {
		return s.FirstTokenMs
	}
	return s.DurationMs
}

func trafficLatencyKind(samples []ChannelMonitorTrafficSample) string {
	firstToken := 0
	for _, sample := range samples {
		if sample.FirstTokenMs > 0 {
			firstToken++
		}
	}
	if firstToken == 0 {
		return "total duration"
	}
	if firstToken == len(samples) {
		return "first token"
	}
	return "first token preferred"
}

func (s *ChannelMonitorService) shouldPersistTrafficObservation(monitorID int64, results []*CheckResult) bool {
	if s == nil || monitorID <= 0 || len(results) == 0 {
		return false
	}
	key := fmt.Sprintf("%d", monitorID)
	now := time.Now().UTC()
	s.trafficObservationMu.Lock()
	defer s.trafficObservationMu.Unlock()
	if last, ok := s.trafficObservationWrites[key]; ok && now.Sub(last) < trafficHistoryWriteInterval {
		return false
	}
	s.trafficObservationWrites[key] = now
	return true
}
