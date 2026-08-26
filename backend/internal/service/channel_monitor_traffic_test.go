package service

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTrafficObservationResultUsesMedianRecentSuccesses(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	account := Account{ID: 7, Name: "plus-a"}
	result, ok := buildTrafficObservationResult(
		"gpt-5.6",
		[]ChannelMonitorTrafficSample{
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 1200, CreatedAt: now.Add(-time.Minute)},
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 300, CreatedAt: now.Add(-2 * time.Minute)},
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 800, CreatedAt: now.Add(-3 * time.Minute)},
			{AccountID: 99, Model: "gpt-5.6", DurationMs: 20, CreatedAt: now.Add(-time.Minute)},
		},
		map[int64]Account{7: account},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           2,
		},
	)
	if !ok {
		t.Fatal("expected fresh eligible traffic to be usable")
	}
	if result.ProbeMode != accountProbeModeTraffic {
		t.Fatalf("probe mode = %q, want traffic", result.ProbeMode)
	}
	if result.LatencyMs == nil || *result.LatencyMs != 800 {
		t.Fatalf("median latency = %v, want 800", result.LatencyMs)
	}
	if result.Status != MonitorStatusOperational {
		t.Fatalf("status = %q, want operational", result.Status)
	}
	if result.AccountID == nil || *result.AccountID != account.ID {
		t.Fatalf("account id = %v, want %d", result.AccountID, account.ID)
	}
}

func TestBuildTrafficObservationResultPrefersFirstTokenOverTotalDuration(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	result, ok := buildTrafficObservationResult(
		"gpt-5.6-sol",
		[]ChannelMonitorTrafficSample{
			{AccountID: 7, Model: "gpt-5.6-sol", DurationMs: 11800, FirstTokenMs: 1400, CreatedAt: now.Add(-time.Minute)},
			{AccountID: 7, Model: "gpt-5.6-sol", DurationMs: 10200, FirstTokenMs: 900, CreatedAt: now.Add(-2 * time.Minute)},
			{AccountID: 7, Model: "gpt-5.6-sol", DurationMs: 15100, FirstTokenMs: 1100, CreatedAt: now.Add(-3 * time.Minute)},
		},
		map[int64]Account{7: {ID: 7, Name: "plus-a"}},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           2,
		},
	)
	if !ok {
		t.Fatal("expected first-token traffic to be usable")
	}
	if result.LatencyMs == nil || *result.LatencyMs != 1100 {
		t.Fatalf("first-token median latency = %v, want 1100", result.LatencyMs)
	}
	if result.Status != MonitorStatusOperational {
		t.Fatalf("status = %q, want operational", result.Status)
	}
	if !strings.Contains(result.Message, "first token") {
		t.Fatalf("message = %q, want first token kind", result.Message)
	}
}

func TestBuildTrafficObservationResultAggregatesLatencyBreakdownPerStage(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	ms := func(value int) *int { return &value }
	result, ok := buildTrafficObservationResult(
		"gpt-5.6-sol",
		[]ChannelMonitorTrafficSample{
			{
				AccountID:    7,
				Model:        "gpt-5.6-sol",
				DurationMs:   7000,
				FirstTokenMs: 1500,
				LatencyBreakdown: &UsageLatencyBreakdown{
					FirstResponseMs:  ms(300),
					FirstEventMs:     ms(400),
					FirstOutputMs:    ms(900),
					FirstCharacterMs: ms(1500),
					TotalDurationMs:  ms(7000),
				},
				CreatedAt: now.Add(-time.Minute),
			},
			{
				AccountID:    7,
				Model:        "gpt-5.6-sol",
				DurationMs:   9000,
				FirstTokenMs: 2500,
				LatencyBreakdown: &UsageLatencyBreakdown{
					FirstResponseMs:  ms(500),
					FirstEventMs:     ms(600),
					FirstOutputMs:    ms(1200),
					FirstCharacterMs: ms(2500),
					TotalDurationMs:  ms(9000),
				},
				CreatedAt: now.Add(-2 * time.Minute),
			},
			{
				AccountID:    7,
				Model:        "gpt-5.6-sol",
				DurationMs:   8000,
				FirstTokenMs: 2000,
				LatencyBreakdown: &UsageLatencyBreakdown{
					FirstResponseMs:  ms(400),
					FirstOutputMs:    ms(1000),
					FirstCharacterMs: ms(2000),
					TotalDurationMs:  ms(8000),
				},
				CreatedAt: now.Add(-3 * time.Minute),
			},
		},
		map[int64]Account{7: {ID: 7, Name: "plus-a"}},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           2,
		},
	)
	if !ok {
		t.Fatal("expected latency breakdown traffic to be usable")
	}
	if result.LatencyBreakdown == nil {
		t.Fatal("expected latency breakdown")
	}
	if got := *result.LatencyBreakdown.FirstResponseMs; got != 400 {
		t.Fatalf("first response median = %d, want 400", got)
	}
	if got := *result.LatencyBreakdown.FirstEventMs; got != 600 {
		t.Fatalf("first event median = %d, want 600", got)
	}
	if got := *result.LatencyBreakdown.FirstOutputMs; got != 1000 {
		t.Fatalf("first output median = %d, want 1000", got)
	}
	if got := *result.LatencyBreakdown.FirstCharacterMs; got != 2000 {
		t.Fatalf("first character median = %d, want 2000", got)
	}
	if got := *result.LatencyBreakdown.TotalDurationMs; got != 8000 {
		t.Fatalf("total duration median = %d, want 8000", got)
	}
}

func TestBuildTrafficObservationResultUsesFreshTrafficOutsideDisplayWindow(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	result, ok := buildTrafficObservationResult(
		"gpt-5.6",
		[]ChannelMonitorTrafficSample{
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 600, CreatedAt: now.Add(-time.Minute)},
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 700, CreatedAt: now.Add(-10 * time.Minute)},
		},
		map[int64]Account{7: {ID: 7, Name: "plus-a"}},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           2,
		},
	)
	if !ok {
		t.Fatal("expected fresh traffic inside the idle window to avoid an active probe")
	}
	if result.LatencyMs == nil || *result.LatencyMs != 700 {
		t.Fatalf("fallback median latency = %v, want 700", result.LatencyMs)
	}
}

func TestBuildTrafficObservationResultMarksSlowTrafficDegraded(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	result, ok := buildTrafficObservationResult(
		"gpt-5.6",
		[]ChannelMonitorTrafficSample{
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 10100, CreatedAt: now.Add(-time.Minute)},
		},
		map[int64]Account{7: {ID: 7, Name: "plus-a"}},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           1,
		},
	)
	if !ok || result.Status != MonitorStatusDegraded {
		t.Fatalf("result = %#v, want degraded traffic observation", result)
	}
}

func TestBuildTrafficObservationResultKeepsSixSecondTrafficOperational(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	result, ok := buildTrafficObservationResult(
		"gpt-5.6",
		[]ChannelMonitorTrafficSample{
			{AccountID: 7, Model: "gpt-5.6", DurationMs: 16000, FirstTokenMs: 6100, CreatedAt: now.Add(-time.Minute)},
		},
		map[int64]Account{7: {ID: 7, Name: "plus-a"}},
		now,
		ChannelMonitorTrafficObservationSettings{
			FallbackIdleSeconds:      1800,
			AggregationWindowSeconds: 300,
			MinimumSamples:           1,
		},
	)
	if !ok || result.Status != MonitorStatusOperational {
		t.Fatalf("result = %#v, want operational traffic observation", result)
	}
	if result.LatencyMs == nil || *result.LatencyMs != 6100 {
		t.Fatalf("latency = %v, want 6100ms first token", result.LatencyMs)
	}
}

func TestMonitorWithModelsKeepsAccountBindingAndOnlyRequestedModels(t *testing.T) {
	groupID := int64(34)
	monitor := &ChannelMonitor{
		ID:             16,
		Name:           "plus-gpt",
		AccountGroupID: &groupID,
		PrimaryModel:   "gpt-5.6-sol",
		ExtraModels:    []string{"gpt-5.5", "gpt-5.4"},
	}

	clone := monitorWithModels(monitor, []string{"gpt-5.5", "gpt-5.4"})
	if clone == monitor {
		t.Fatal("expected a cloned monitor")
	}
	if clone.ID != monitor.ID || clone.AccountGroupID == nil || *clone.AccountGroupID != groupID {
		t.Fatalf("clone lost monitor identity or account group: %#v", clone)
	}
	if clone.PrimaryModel != "gpt-5.5" {
		t.Fatalf("clone primary model = %q, want gpt-5.5", clone.PrimaryModel)
	}
	if len(clone.ExtraModels) != 1 || clone.ExtraModels[0] != "gpt-5.4" {
		t.Fatalf("clone extra models = %#v, want [gpt-5.4]", clone.ExtraModels)
	}
	if monitor.PrimaryModel != "gpt-5.6-sol" || len(monitor.ExtraModels) != 2 {
		t.Fatalf("source monitor was mutated: %#v", monitor)
	}
}

func TestOrderCheckResultsMergesTrafficAndProbeByModel(t *testing.T) {
	trafficLatency := 1200
	probeLatency := 900
	results := orderCheckResults(
		[]string{"gpt-5.6-sol", "gpt-5.5"},
		[]*CheckResult{{
			Model:     "gpt-5.6-sol",
			ProbeMode: accountProbeModeTraffic,
			LatencyMs: &trafficLatency,
		}},
		[]*CheckResult{{
			Model:     "gpt-5.5",
			ProbeMode: accountProbeModeSticky,
			LatencyMs: &probeLatency,
		}},
	)
	if len(results) != 2 {
		t.Fatalf("merged result count = %d, want 2", len(results))
	}
	if results[0].Model != "gpt-5.6-sol" || results[0].ProbeMode != accountProbeModeTraffic {
		t.Fatalf("first merged result = %#v", results[0])
	}
	if results[1].Model != "gpt-5.5" || results[1].ProbeMode != accountProbeModeSticky {
		t.Fatalf("second merged result = %#v", results[1])
	}
}

func TestTrafficSampleQueryLimitScalesWithModels(t *testing.T) {
	tests := []struct {
		modelCount int
		want       int
	}{
		{modelCount: 0, want: 100},
		{modelCount: 1, want: 100},
		{modelCount: 2, want: 200},
		{modelCount: 5, want: 500},
		{modelCount: 8, want: 500},
	}
	for _, tt := range tests {
		if got := trafficSampleQueryLimit(tt.modelCount); got != tt.want {
			t.Fatalf("trafficSampleQueryLimit(%d) = %d, want %d", tt.modelCount, got, tt.want)
		}
	}
}
