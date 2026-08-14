package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	accountProbeModeStatic  = "static"
	accountProbeModeSticky  = "sticky"
	accountProbeModeConfirm = "confirm"
	accountProbeModeFull    = "full"

	defaultAdaptiveAccountProbeMaxCandidates = 5
	defaultAdaptiveAccountProbeParallelism   = 5
)

// AccountMonitorProbeResult is the read-only result returned by the generic
// account test adapter. The adapter owns platform-specific credentials,
// proxies, TLS fingerprints, and response parsing.
type AccountMonitorProbeResult struct {
	Success          bool
	LatencyMs        int
	Message          string
	Usage            monitorUsage
	RequestAttempted bool
}

type channelMonitorAccountProbeExecutor interface {
	ProbeForChannelMonitor(ctx context.Context, accountID int64, model string) (*AccountMonitorProbeResult, error)
}

type channelMonitorAccountProbeSettingsProvider interface {
	GetChannelMonitorAccountProbeSettings(ctx context.Context) ChannelMonitorAccountProbeSettings
}

// channelMonitorAccountProbeStateRepository is optional so older unit-test
// doubles and integrations that only implement the original monitor
// repository contract remain valid. Production repositories implement it.
type channelMonitorAccountProbeStateRepository interface {
	GetAccountProbeState(ctx context.Context, monitorID int64, model string) (*ChannelMonitorAccountProbeState, error)
	UpsertAccountProbeState(ctx context.Context, state *ChannelMonitorAccountProbeState) error
	ClearAccountProbeStates(ctx context.Context, monitorID int64) error
}

// ChannelMonitorAccountProbeSettings controls the adaptive account strategy.
// It is stored as one JSON system setting to keep updates atomic.
type ChannelMonitorAccountProbeSettings struct {
	Enabled             bool `json:"enabled"`
	ConfirmAttempts     int  `json:"confirm_attempts"`
	DegradedThresholdMs int  `json:"degraded_threshold_ms"`
	MaxCandidates       int  `json:"max_candidates"`
	Parallelism         int  `json:"parallelism"`
	AllowImageFanout    bool `json:"allow_image_fanout"`
}

func DefaultChannelMonitorAccountProbeSettings() ChannelMonitorAccountProbeSettings {
	return ChannelMonitorAccountProbeSettings{
		Enabled:             true,
		ConfirmAttempts:     1,
		DegradedThresholdMs: int(monitorDegradedThreshold / time.Millisecond),
		MaxCandidates:       defaultAdaptiveAccountProbeMaxCandidates,
		Parallelism:         defaultAdaptiveAccountProbeParallelism,
		AllowImageFanout:    false,
	}
}

func normalizeChannelMonitorAccountProbeSettings(settings ChannelMonitorAccountProbeSettings) ChannelMonitorAccountProbeSettings {
	defaults := DefaultChannelMonitorAccountProbeSettings()
	if settings.ConfirmAttempts < 0 || settings.ConfirmAttempts > 2 {
		settings.ConfirmAttempts = defaults.ConfirmAttempts
	}
	if settings.DegradedThresholdMs < 100 || settings.DegradedThresholdMs > 120000 {
		settings.DegradedThresholdMs = defaults.DegradedThresholdMs
	}
	if settings.MaxCandidates < 1 || settings.MaxCandidates > 20 {
		settings.MaxCandidates = defaults.MaxCandidates
	}
	if settings.Parallelism < 1 || settings.Parallelism > 20 {
		settings.Parallelism = defaults.Parallelism
	}
	return settings
}

func (s *ChannelMonitorService) SetAccountProbeExecutor(executor channelMonitorAccountProbeExecutor) {
	s.accountProbeExecutor = executor
}

func (s *ChannelMonitorService) SetAccountProbeSettingsProvider(provider channelMonitorAccountProbeSettingsProvider) {
	s.accountProbeSettingsProvider = provider
}

func (s *ChannelMonitorService) adaptiveAccountProbeSettings(ctx context.Context) ChannelMonitorAccountProbeSettings {
	if s.accountProbeSettingsProvider == nil {
		return DefaultChannelMonitorAccountProbeSettings()
	}
	return normalizeChannelMonitorAccountProbeSettings(
		s.accountProbeSettingsProvider.GetChannelMonitorAccountProbeSettings(ctx),
	)
}

func (s *ChannelMonitorService) runAdaptiveAccountGroupProbeIfConfigured(
	ctx context.Context,
	m *ChannelMonitor,
	forceFull bool,
) ([]*CheckResult, bool) {
	if s == nil || m == nil || m.AccountGroupID == nil || s.accountProbeRepo == nil {
		return nil, false
	}
	settings := s.adaptiveAccountProbeSettings(ctx)
	if !settings.Enabled {
		return nil, false
	}
	groupPlatform, err := s.accountProbeRepo.GetGroupPlatform(ctx, *m.AccountGroupID)
	if err != nil {
		result := newAdaptiveProbeResult(m.PrimaryModel, accountProbeModeFull, fmt.Sprintf("account group platform lookup failed: %v", err))
		return []*CheckResult{result}, true
	}
	if !monitorProviderSupportsAccountPlatform(m.Provider, groupPlatform) {
		result := newAdaptiveProbeResult(
			m.PrimaryModel,
			accountProbeModeFull,
			fmt.Sprintf("account group platform %q is incompatible with monitor provider %q", groupPlatform, m.Provider),
		)
		return []*CheckResult{result}, true
	}
	accounts, err := s.accountProbeRepo.ListSchedulableByGroupIDAndPlatform(ctx, *m.AccountGroupID, groupPlatform)
	if err != nil {
		result := newAdaptiveProbeResult(m.PrimaryModel, accountProbeModeFull, fmt.Sprintf("account group lookup failed: %v", err))
		return []*CheckResult{result}, true
	}
	accounts = sortAdaptiveProbeAccounts(accounts)
	if len(accounts) == 0 {
		result := newAdaptiveProbeResult(m.PrimaryModel, accountProbeModeFull, "account group has no eligible model-capable accounts")
		return []*CheckResult{result}, true
	}

	models := uniqueMonitorModels(m)
	results := make([]*CheckResult, 0, len(models))
	for _, model := range models {
		var result *CheckResult
		if defaultAPIMode(m.APIMode) == MonitorAPIModeImages && !settings.AllowImageFanout {
			result = s.runAdaptiveImageSingleProbe(ctx, m, model, accounts, settings)
		} else {
			result = s.runAdaptiveModelProbe(ctx, m, model, accounts, settings, forceFull)
		}
		results = append(results, result)
	}
	return results, true
}

func monitorProviderSupportsAccountPlatform(provider, platform string) bool {
	provider = strings.TrimSpace(provider)
	platform = strings.TrimSpace(platform)
	if provider == MonitorProviderOpenAI {
		return IsOpenAICompatiblePlatform(platform)
	}
	if provider == MonitorProviderDeepSeek || provider == MonitorProviderKimi || provider == MonitorProviderGLM {
		return provider == platform
	}
	return provider != "" && provider == platform
}

func sortAdaptiveProbeAccounts(accounts []Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.ID <= 0 {
			continue
		}
		out = append(out, account)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func filterAdaptiveProbeAccounts(accounts []Account, model string, maxCandidates int) []Account {
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.IsModelSupported(model) {
			out = append(out, account)
		}
	}
	if maxCandidates > 0 && len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}

func uniqueMonitorModels(m *ChannelMonitor) []string {
	if m == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(m.ExtraModels)+1)
	models := make([]string, 0, len(m.ExtraModels)+1)
	for _, model := range append([]string{m.PrimaryModel}, m.ExtraModels...) {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func (s *ChannelMonitorService) runAdaptiveModelProbe(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	accounts []Account,
	settings ChannelMonitorAccountProbeSettings,
	forceFull bool,
) *CheckResult {
	allModelAccounts := filterAdaptiveProbeAccounts(accounts, model, 0)
	if len(allModelAccounts) == 0 {
		return s.persistAdaptiveState(ctx, monitor, model, &CheckResult{
			Model:     model,
			Status:    MonitorStatusError,
			ProbeMode: accountProbeModeFull,
			Message:   "no eligible account supports this model",
			CheckedAt: time.Now(),
		}, true)
	}

	stateRepo, hasStateRepo := s.repo.(channelMonitorAccountProbeStateRepository)
	var state *ChannelMonitorAccountProbeState
	var err error
	if hasStateRepo {
		state, err = stateRepo.GetAccountProbeState(ctx, monitor.ID, model)
	}
	if err != nil {
		slog.Warn("channel_monitor: read adaptive probe state failed", "monitor_id", monitor.ID, "model", model, "error", err)
	}
	modelAccounts := limitAdaptiveProbeAccounts(allModelAccounts, state, settings.MaxCandidates)
	candidateByID := make(map[int64]Account, len(modelAccounts))
	for _, account := range modelAccounts {
		candidateByID[account.ID] = account
	}

	if !forceFull && state != nil && state.FinalStatus == MonitorStatusOperational && state.AccountID != nil {
		if account, ok := candidateByID[*state.AccountID]; ok {
			first := s.probeAdaptiveAccount(ctx, monitor, model, account, accountProbeModeSticky, settings)
			if first.Status == MonitorStatusOperational {
				return s.persistAdaptiveState(ctx, monitor, model, first, false)
			}
			for attempt := 0; attempt < settings.ConfirmAttempts; attempt++ {
				confirmed := s.probeAdaptiveAccount(ctx, monitor, model, account, accountProbeModeConfirm, settings)
				if confirmed.Status == MonitorStatusOperational {
					return s.persistAdaptiveState(ctx, monitor, model, confirmed, false)
				}
			}
			// Sticky account remained abnormal. Fall through to a full sweep
			// for this model only.
		}
	}

	probes := s.probeAdaptiveAccounts(ctx, monitor, model, modelAccounts, settings)
	best := selectBestAdaptiveProbe(probes)
	if best == nil {
		best = newAdaptiveProbeResult(model, accountProbeModeFull, "adaptive account probe returned no result")
	}
	best.CandidateCount = len(probes)
	best.HealthyCount = countOperationalProbes(probes)
	best.Message = decorateAdaptiveProbeMessage(best.Message, best.HealthyCount, len(probes))
	return s.persistAdaptiveState(ctx, monitor, model, best, true)
}

func limitAdaptiveProbeAccounts(
	accounts []Account,
	state *ChannelMonitorAccountProbeState,
	maxCandidates int,
) []Account {
	if maxCandidates <= 0 || len(accounts) <= maxCandidates {
		return accounts
	}
	limited := append([]Account(nil), accounts[:maxCandidates]...)
	if state == nil || state.AccountID == nil {
		return limited
	}
	for _, account := range limited {
		if account.ID == *state.AccountID {
			return limited
		}
	}
	for _, account := range accounts[maxCandidates:] {
		if account.ID == *state.AccountID {
			limited[len(limited)-1] = account
			break
		}
	}
	return limited
}

// runAdaptiveImageSingleProbe keeps real image monitoring billable but bounded:
// when image fan-out is disabled it only probes the sticky account (or the
// highest-priority viable account on first run) and never expands after a
// failure. Administrators can explicitly enable fan-out when they want the
// text-monitor recovery behavior for images too.
func (s *ChannelMonitorService) runAdaptiveImageSingleProbe(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	accounts []Account,
	settings ChannelMonitorAccountProbeSettings,
) *CheckResult {
	allModelAccounts := filterAdaptiveProbeAccounts(accounts, model, 0)
	var state *ChannelMonitorAccountProbeState
	if stateRepo, ok := s.repo.(channelMonitorAccountProbeStateRepository); ok {
		loaded, err := stateRepo.GetAccountProbeState(ctx, monitor.ID, model)
		if err != nil {
			slog.Warn("channel_monitor: read adaptive image probe state failed",
				"monitor_id", monitor.ID, "model", model, "error", err)
		} else {
			state = loaded
		}
	}
	modelAccounts := limitAdaptiveProbeAccounts(allModelAccounts, state, settings.MaxCandidates)
	if len(modelAccounts) == 0 {
		return s.persistAdaptiveState(ctx, monitor, model, &CheckResult{
			Model:     model,
			Status:    MonitorStatusError,
			ProbeMode: accountProbeModeFull,
			Message:   "no eligible account supports this image model",
			CheckedAt: time.Now(),
		}, false)
	}

	selected := modelAccounts[0]
	if state != nil && state.AccountID != nil {
		for _, account := range modelAccounts {
			if account.ID == *state.AccountID {
				selected = account
				break
			}
		}
	}

	result := s.probeAdaptiveAccount(ctx, monitor, model, selected, accountProbeModeSticky, settings)
	result.CandidateCount = 1
	if result.Status == MonitorStatusOperational || settings.ConfirmAttempts == 0 {
		return s.persistAdaptiveState(ctx, monitor, model, result, false)
	}
	for attempt := 0; attempt < settings.ConfirmAttempts; attempt++ {
		confirmed := s.probeAdaptiveAccount(ctx, monitor, model, selected, accountProbeModeConfirm, settings)
		confirmed.CandidateCount = 1
		if confirmed.Status == MonitorStatusOperational {
			return s.persistAdaptiveState(ctx, monitor, model, confirmed, false)
		}
		result = confirmed
	}
	return s.persistAdaptiveState(ctx, monitor, model, result, false)
}

func (s *ChannelMonitorService) probeAdaptiveAccounts(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	accounts []Account,
	settings ChannelMonitorAccountProbeSettings,
) []*CheckResult {
	results := make([]*CheckResult, len(accounts))
	eg, egCtx := errgroup.WithContext(ctx)
	parallelism := settings.Parallelism
	if parallelism > len(accounts) {
		parallelism = len(accounts)
	}
	eg.SetLimit(parallelism)
	for i := range accounts {
		i := i
		account := accounts[i]
		eg.Go(func() error {
			results[i] = s.probeAdaptiveAccount(egCtx, monitor, model, account, accountProbeModeFull, settings)
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

func (s *ChannelMonitorService) probeAdaptiveAccount(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	account Account,
	mode string,
	settings ChannelMonitorAccountProbeSettings,
) *CheckResult {
	result := &CheckResult{
		Model:       model,
		ProbeMode:   mode,
		CheckedAt:   time.Now(),
		AccountName: account.Name,
	}
	accountID := account.ID
	result.AccountID = &accountID
	// Adaptive selection still uses the account-management group, but the
	// outbound request must stay on the low-cost monitor challenge path.
	probed := s.runLowCostAdaptiveAccountProbe(ctx, monitor, model, &account)
	if probed != nil {
		probed.ProbeMode = mode
		probed.AccountID = &accountID
		probed.AccountName = account.Name
		probed.Model = model
		if probed.monitorCostModel == "" {
			probed.monitorCostModel = account.GetMappedModel(model)
		}
		if probed.Status == MonitorStatusOperational && probed.LatencyMs != nil && *probed.LatencyMs >= settings.DegradedThresholdMs {
			probed.Status = MonitorStatusDegraded
			if strings.TrimSpace(probed.Message) == "" {
				probed.Message = fmt.Sprintf("slow response: %dms", *probed.LatencyMs)
			}
		}
		return probed
	}
	result.monitorRequestAttempted = true
	result.monitorCostModel = account.GetMappedModel(model)
	s.recordMonitorCost(ctx, monitor, result, &account)
	result.Status = MonitorStatusError
	result.Message = truncateMessage(fmt.Sprintf("account %d: low-cost adaptive probe returned no result", account.ID))
	return result
}

func selectBestAdaptiveProbe(probes []*CheckResult) *CheckResult {
	var best *CheckResult
	for _, candidate := range probes {
		if candidate == nil {
			continue
		}
		if best == nil || isBetterAdaptiveProbe(candidate, best) {
			best = candidate
		}
	}
	return best
}

func isBetterAdaptiveProbe(candidate, incumbent *CheckResult) bool {
	candidateRank := monitorStatusRank(candidate.Status)
	incumbentRank := monitorStatusRank(incumbent.Status)
	if candidateRank != incumbentRank {
		return candidateRank > incumbentRank
	}
	return monitorLatencySortValue(candidate.LatencyMs) < monitorLatencySortValue(incumbent.LatencyMs)
}

func countOperationalProbes(probes []*CheckResult) int {
	count := 0
	for _, probe := range probes {
		if probe != nil && probe.Status == MonitorStatusOperational {
			count++
		}
	}
	return count
}

func newAdaptiveProbeResult(model, mode, message string) *CheckResult {
	return &CheckResult{
		Model:     model,
		Status:    MonitorStatusError,
		ProbeMode: mode,
		Message:   truncateMessage(message),
		CheckedAt: time.Now(),
	}
}

func decorateAdaptiveProbeMessage(message string, healthy, candidates int) string {
	summary := fmt.Sprintf("adaptive probe: %d/%d operational", healthy, candidates)
	if strings.TrimSpace(message) == "" {
		return summary
	}
	return truncateMessage(message + "; " + summary)
}

func (s *ChannelMonitorService) persistAdaptiveState(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	result *CheckResult,
	fullSweep bool,
) *CheckResult {
	if result == nil {
		result = newAdaptiveProbeResult(model, accountProbeModeFull, "adaptive probe result is empty")
	}
	now := time.Now().UTC()
	state := &ChannelMonitorAccountProbeState{
		MonitorID:     monitor.ID,
		Model:         model,
		AccountID:     result.AccountID,
		AccountName:   result.AccountName,
		FinalStatus:   result.Status,
		LastLatencyMs: result.LatencyMs,
		LastProbeMode: result.ProbeMode,
		LastCheckedAt: result.CheckedAt,
		UpdatedAt:     now,
	}
	if fullSweep {
		state.LastFullSweepAt = &now
	}
	if stateRepo, ok := s.repo.(channelMonitorAccountProbeStateRepository); ok {
		persistCtx, cancel := monitorPersistenceContext(ctx)
		err := stateRepo.UpsertAccountProbeState(persistCtx, state)
		cancel()
		if err != nil {
			slog.Error("channel_monitor: persist adaptive probe state failed",
				"monitor_id", monitor.ID, "model", model, "error", err)
		}
	}
	return result
}

func (s *ChannelMonitorService) runLowCostAdaptiveAccountProbe(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	account *Account,
) *CheckResult {
	if s == nil || monitor == nil || account == nil {
		return nil
	}
	if s.httpUpstream == nil {
		return s.runLegacyAdaptiveAccountTestProbe(ctx, monitor, model, account)
	}
	if account.IsOpenAICompatible() && account.Type == AccountTypeAPIKey {
		scoped := *monitor
		scoped.PrimaryModel = model
		scoped.ExtraModels = nil
		result := s.runOpenAIAPIKeyAccountProbe(ctx, &scoped, account)
		s.recordMonitorCost(ctx, monitor, result, account)
		return result
	}
	result := &CheckResult{
		Model:                   model,
		Status:                  MonitorStatusError,
		CheckedAt:               time.Now(),
		AccountName:             account.Name,
		monitorRequestAttempted: false,
		monitorCostModel:        account.GetMappedModel(model),
	}
	accountID := account.ID
	result.AccountID = &accountID
	result.Message = truncateMessage(fmt.Sprintf("account %d does not support low-cost adaptive probing", account.ID))
	return result
}

func (s *ChannelMonitorService) runLegacyAdaptiveAccountTestProbe(
	ctx context.Context,
	monitor *ChannelMonitor,
	model string,
	account *Account,
) *CheckResult {
	if s.accountProbeExecutor == nil || account == nil {
		return nil
	}
	probe, err := s.accountProbeExecutor.ProbeForChannelMonitor(ctx, account.ID, model)
	result := &CheckResult{
		Model:            model,
		CheckedAt:        time.Now(),
		AccountName:      account.Name,
		monitorCostModel: account.GetMappedModel(model),
	}
	accountID := account.ID
	result.AccountID = &accountID
	if err != nil {
		result.monitorRequestAttempted = true
		s.recordMonitorCost(ctx, monitor, result, account)
		result.Status = MonitorStatusError
		result.Message = truncateMessage(fmt.Sprintf("account %d: %v", account.ID, err))
		return result
	}
	if probe == nil {
		return nil
	}
	result.monitorUsage = probe.Usage
	result.monitorRequestAttempted = probe.RequestAttempted
	s.recordMonitorCost(ctx, monitor, result, account)
	latency := probe.LatencyMs
	result.LatencyMs = &latency
	result.Message = truncateMessage(probe.Message)
	if !probe.Success {
		result.Status = MonitorStatusFailed
		if result.Message == "" {
			result.Message = "account probe returned an invalid response"
		}
		return result
	}
	result.Status = MonitorStatusOperational
	return result
}
