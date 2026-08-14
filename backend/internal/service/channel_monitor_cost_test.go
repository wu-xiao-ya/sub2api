//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type monitorCostEventRepoStub struct {
	ChannelMonitorRepository
	mu     sync.Mutex
	events []*ChannelMonitorCostEvent
}

func (r *monitorCostEventRepoStub) InsertCostEvents(_ context.Context, events []*ChannelMonitorCostEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		copy := *event
		if event.AccountID != nil {
			accountID := *event.AccountID
			copy.AccountID = &accountID
		}
		r.events = append(r.events, &copy)
	}
	return nil
}

func (r *monitorCostEventRepoStub) snapshot() []*ChannelMonitorCostEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*ChannelMonitorCostEvent, len(r.events))
	for i, event := range r.events {
		copy := *event
		out[i] = &copy
	}
	return out
}

type monitorCostProbeExecutor struct {
	usage monitorUsage
}

func (e monitorCostProbeExecutor) ProbeForChannelMonitor(
	context.Context,
	int64,
	string,
) (*AccountMonitorProbeResult, error) {
	return &AccountMonitorProbeResult{
		Success:          true,
		LatencyMs:        5,
		Usage:            e.usage,
		RequestAttempted: true,
	}, nil
}

func TestMonitorUsageFromOpenAIResponseSeparatesCachedInputTokens(t *testing.T) {
	usage := monitorUsageFromResponse(MonitorProviderOpenAI, MonitorAPIModeResponses, []byte(`{
		"usage": {
			"input_tokens": 120,
			"output_tokens": 7,
			"input_tokens_details": {"cached_tokens": 80}
		}
	}`))

	require.True(t, usage.Observed)
	require.Equal(t, 40, usage.Tokens.InputTokens)
	require.Equal(t, 80, usage.Tokens.CacheReadTokens)
	require.Equal(t, 7, usage.Tokens.OutputTokens)
}

func TestParseChannelMonitorAccountProbeOutputCarriesUsageEvent(t *testing.T) {
	success, message, usage := parseChannelMonitorAccountProbeOutput(`
data: {"type":"usage","data":{"input_tokens":12,"output_tokens":3,"cache_read_tokens":8,"observed":true}}

data: {"type":"test_complete","success":true}
`, nil)

	require.True(t, success)
	require.Equal(t, "account test completed", message)
	require.True(t, usage.Observed)
	require.Equal(t, 12, usage.Tokens.InputTokens)
	require.Equal(t, 3, usage.Tokens.OutputTokens)
	require.Equal(t, 8, usage.Tokens.CacheReadTokens)
}

func TestAdaptiveFullProbeRecordsEveryAccountCostEvent(t *testing.T) {
	repo := &monitorCostEventRepoStub{}
	billing := &BillingService{
		fallbackPrices: map[string]*ModelPricing{
			"claude-sonnet-4": {
				InputPricePerToken:  0.01,
				OutputPricePerToken: 0.02,
			},
		},
	}
	service := NewChannelMonitorService(repo, nil)
	service.SetAccountProbeExecutor(monitorCostProbeExecutor{
		usage: monitorUsage{
			Tokens:   UsageTokens{InputTokens: 2, OutputTokens: 1},
			Observed: true,
		},
	})
	service.SetCostDependencies(nil, nil, billing)

	rate := 0.2
	accounts := []Account{
		{ID: 1, Name: "one", RateMultiplier: &rate},
		{ID: 2, Name: "two", RateMultiplier: &rate},
		{ID: 3, Name: "three", RateMultiplier: &rate},
		{ID: 4, Name: "four", RateMultiplier: &rate},
		{ID: 5, Name: "five", RateMultiplier: &rate},
	}
	monitor := &ChannelMonitor{
		ID:           99,
		Provider:     MonitorProviderOpenAI,
		APIMode:      MonitorAPIModeResponses,
		PrimaryModel: "claude-sonnet-4",
	}

	results := service.probeAdaptiveAccounts(
		context.Background(),
		monitor,
		"claude-sonnet-4",
		accounts,
		ChannelMonitorAccountProbeSettings{Parallelism: len(accounts)},
	)

	require.Len(t, results, len(accounts))
	events := repo.snapshot()
	require.Len(t, events, len(accounts))
	for _, event := range events {
		require.Equal(t, int64(99), event.MonitorID)
		require.NotNil(t, event.AccountID)
		require.Equal(t, "claude-sonnet-4", event.Model)
		require.Equal(t, 2, event.InputTokens)
		require.Equal(t, 1, event.OutputTokens)
		require.InDelta(t, 0.04, event.EstimatedCost, 1e-9)
		require.InDelta(t, 0.008, event.AccountCost, 1e-9)
		require.Equal(t, channelMonitorCostSourceModelPricing, event.CostSource)
		require.WithinDuration(t, time.Now(), event.CreatedAt, time.Minute)
	}
}

func TestBuildMonitorCostEventUsesConfiguredImageDefault(t *testing.T) {
	service := NewChannelMonitorService(nil, nil)
	event := service.buildMonitorCostEvent(context.Background(), &ChannelMonitor{
		ID:       3,
		Provider: MonitorProviderOpenAI,
		APIMode:  MonitorAPIModeImages,
	}, &CheckResult{
		Model:                   "gpt-image-2",
		CheckedAt:               time.Now().UTC(),
		monitorRequestAttempted: true,
		monitorUsage:            monitorUsage{ImageCount: 2, Observed: true},
	}, nil)

	require.Equal(t, 2, event.ImageCount)
	require.InDelta(t, 0.002, event.EstimatedCost, 1e-12)
	require.InDelta(t, 0.002, event.AccountCost, 1e-12)
	require.Equal(t, channelMonitorCostSourceImageDefault, event.CostSource)
}
