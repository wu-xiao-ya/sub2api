package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

const (
	channelMonitorCostSourceUnavailable          = "unavailable"
	channelMonitorCostSourceModelPricing         = "model_pricing"
	channelMonitorCostSourceAccountPricing       = "account_pricing"
	channelMonitorCostSourceImageDefault         = "image_default"
	channelMonitorCostSourceImageAccountOverride = "image_account_override"
)

// monitorUsage carries only the metered units reported by an upstream probe.
// It remains internal so monitor result/history APIs keep their existing shape.
type monitorUsage struct {
	Tokens     UsageTokens
	ImageCount int
	Observed   bool
}

func (u monitorUsage) hasMeteredUnits() bool {
	return u.Observed || u.ImageCount > 0 ||
		u.Tokens.InputTokens > 0 ||
		u.Tokens.OutputTokens > 0 ||
		u.Tokens.CacheCreationTokens > 0 ||
		u.Tokens.CacheReadTokens > 0 ||
		u.Tokens.ImageInputTokens > 0 ||
		u.Tokens.ImageOutputTokens > 0
}

// ChannelMonitorCostEvent is an append-only internal ledger entry for one
// outbound monitoring request. It never represents end-user usage.
type ChannelMonitorCostEvent struct {
	MonitorID           int64
	AccountID           *int64
	Provider            string
	APIMode             string
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	EstimatedCost       float64
	AccountCost         float64
	CostSource          string
	CreatedAt           time.Time
}

// channelMonitorCostEventRepository is optional so existing monitor repository
// test doubles do not need to grow a new method.
type channelMonitorCostEventRepository interface {
	InsertCostEvents(ctx context.Context, events []*ChannelMonitorCostEvent) error
}

func (s *ChannelMonitorService) SetCostDependencies(
	settingService *SettingService,
	channelService *ChannelService,
	billingService *BillingService,
) {
	s.settingService = settingService
	s.channelService = channelService
	s.billingService = billingService
}

func (s *ChannelMonitorService) recordMonitorCost(
	ctx context.Context,
	monitor *ChannelMonitor,
	result *CheckResult,
	account *Account,
) {
	if s == nil || monitor == nil || result == nil || result.monitorCostRecorded || !result.monitorRequestAttempted {
		return
	}

	// Mark first: a failed best-effort write must never later be retried through
	// the selected-result history path with an incomplete account attribution.
	result.monitorCostRecorded = true

	repo, ok := s.repo.(channelMonitorCostEventRepository)
	if !ok {
		return
	}

	persistCtx, cancel := monitorPersistenceContext(ctx)
	defer cancel()
	event := s.buildMonitorCostEvent(persistCtx, monitor, result, account)
	if err := repo.InsertCostEvents(persistCtx, []*ChannelMonitorCostEvent{event}); err != nil {
		slog.Error("channel_monitor: insert cost event failed",
			"monitor_id", monitor.ID,
			"model", event.Model,
			"account_id", event.AccountID,
			"error", err,
		)
	}
}

func (s *ChannelMonitorService) buildMonitorCostEvent(
	ctx context.Context,
	monitor *ChannelMonitor,
	result *CheckResult,
	account *Account,
) *ChannelMonitorCostEvent {
	usage := result.monitorUsage
	model := strings.TrimSpace(result.monitorCostModel)
	if model == "" {
		model = strings.TrimSpace(result.Model)
	}
	if model == "" {
		model = strings.TrimSpace(monitor.PrimaryModel)
	}

	event := &ChannelMonitorCostEvent{
		MonitorID:           monitor.ID,
		Provider:            monitor.Provider,
		APIMode:             defaultAPIMode(monitor.APIMode),
		Model:               model,
		InputTokens:         usage.Tokens.InputTokens,
		OutputTokens:        usage.Tokens.OutputTokens,
		CacheCreationTokens: usage.Tokens.CacheCreationTokens,
		CacheReadTokens:     usage.Tokens.CacheReadTokens,
		ImageCount:          usage.ImageCount,
		CostSource:          channelMonitorCostSourceUnavailable,
		CreatedAt:           result.CheckedAt,
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if account != nil && account.ID > 0 {
		accountID := account.ID
		event.AccountID = &accountID
	}

	if usage.ImageCount > 0 {
		defaultCost := ImageUpstreamCostPerImageDefault
		if s.settingService != nil {
			defaultCost = s.settingService.GetImageUpstreamCostPerImage(ctx)
		}
		event.EstimatedCost = defaultCost * float64(usage.ImageCount)
		event.AccountCost = event.EstimatedCost
		event.CostSource = channelMonitorCostSourceImageDefault

		if account != nil {
			if override, ok := imageUpstreamCostOverrideForAccount(ctx, s.settingService, account.ID); ok {
				event.AccountCost = override * float64(usage.ImageCount)
				event.CostSource = channelMonitorCostSourceImageAccountOverride
			}
		}
		return event
	}

	if !usage.hasMeteredUnits() || s.billingService == nil {
		return event
	}

	breakdown, err := s.billingService.CalculateCost(model, usage.Tokens, 1)
	if err != nil || breakdown == nil {
		return event
	}
	event.EstimatedCost = breakdown.TotalCost
	event.AccountCost = breakdown.TotalCost
	event.CostSource = channelMonitorCostSourceModelPricing

	if account == nil {
		return event
	}

	var groupID int64
	if monitor.AccountGroupID != nil {
		groupID = *monitor.AccountGroupID
	}
	if resolved := resolveAccountStatsCost(
		ctx,
		s.channelService,
		s.billingService,
		account.ID,
		groupID,
		model,
		usage.Tokens,
		1,
		breakdown.TotalCost,
	); resolved != nil {
		event.AccountCost = *resolved
		event.CostSource = channelMonitorCostSourceAccountPricing
	}
	event.AccountCost *= account.BillingRateMultiplier()
	return event
}

func imageUpstreamCostOverrideForAccount(
	ctx context.Context,
	settingService *SettingService,
	accountID int64,
) (float64, bool) {
	if settingService == nil || accountID <= 0 {
		return 0, false
	}
	for _, override := range settingService.GetImageUpstreamCostAccountOverrides(ctx) {
		if override.AccountID == accountID {
			return override.CostPerImage, true
		}
	}
	return 0, false
}

func monitorUsageFromResponse(provider, apiMode string, response []byte) monitorUsage {
	var payload map[string]any
	if err := json.Unmarshal(response, &payload); err != nil {
		return monitorUsage{}
	}
	return monitorUsageFromPayload(provider, apiMode, payload)
}

func monitorUsageFromPayload(provider, apiMode string, payload map[string]any) monitorUsage {
	switch provider {
	case MonitorProviderOpenAI, MonitorProviderGrok, MonitorProviderDeepSeek, MonitorProviderKimi, MonitorProviderGLM,
		MonitorProviderQwen, MonitorProviderMiniMax, MonitorProviderMiMo, MonitorProviderHunyuan:
		if defaultAPIMode(apiMode) == MonitorAPIModeResponses {
			if response, ok := payload["response"].(map[string]any); ok {
				return monitorUsageFromOpenAIPayload(response)
			}
		}
		return monitorUsageFromOpenAIPayload(payload)
	case MonitorProviderAnthropic:
		return monitorUsageFromAnthropicPayload(payload)
	case MonitorProviderGemini:
		return monitorUsageFromGeminiPayload(payload)
	default:
		return monitorUsage{}
	}
}

func monitorUsageFromOpenAIPayload(payload map[string]any) monitorUsage {
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		return monitorUsage{}
	}
	input := intFromPayload(usage["input_tokens"])
	if input == 0 {
		input = intFromPayload(usage["prompt_tokens"])
	}
	output := intFromPayload(usage["output_tokens"])
	if output == 0 {
		output = intFromPayload(usage["completion_tokens"])
	}
	cached := 0
	if details, ok := usage["input_tokens_details"].(map[string]any); ok {
		cached = intFromPayload(details["cached_tokens"])
	}
	if cached == 0 {
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			cached = intFromPayload(details["cached_tokens"])
		}
	}
	if cached > input {
		cached = input
	}
	return monitorUsage{
		Tokens: UsageTokens{
			InputTokens:     input - cached,
			OutputTokens:    output,
			CacheReadTokens: cached,
		},
		Observed: true,
	}
}

func monitorUsageFromAnthropicPayload(payload map[string]any) monitorUsage {
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		return monitorUsage{}
	}
	return monitorUsage{
		Tokens: UsageTokens{
			InputTokens:         intFromPayload(usage["input_tokens"]),
			OutputTokens:        intFromPayload(usage["output_tokens"]),
			CacheCreationTokens: intFromPayload(usage["cache_creation_input_tokens"]),
			CacheReadTokens:     intFromPayload(usage["cache_read_input_tokens"]),
		},
		Observed: true,
	}
}

func monitorUsageFromGeminiPayload(payload map[string]any) monitorUsage {
	usage, ok := payload["usageMetadata"].(map[string]any)
	if !ok {
		return monitorUsage{}
	}
	input := intFromPayload(usage["promptTokenCount"])
	cached := intFromPayload(usage["cachedContentTokenCount"])
	if cached > input {
		cached = input
	}
	return monitorUsage{
		Tokens: UsageTokens{
			InputTokens:     input - cached,
			OutputTokens:    intFromPayload(usage["candidatesTokenCount"]),
			CacheReadTokens: cached,
		},
		Observed: true,
	}
}

func monitorImageCountFromResponse(response []byte) int {
	var payload map[string]any
	if err := json.Unmarshal(response, &payload); err != nil {
		return 0
	}
	items, ok := payload["data"].([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func intFromPayload(value any) int {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case json.Number:
		n, err := v.Int64()
		if err == nil && n > 0 {
			return int(n)
		}
	}
	return 0
}
