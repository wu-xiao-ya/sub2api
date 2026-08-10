package service

import (
	"testing"
)

func TestBuildStatusSummaryIncludesExtraModelAvailability(t *testing.T) {
	primaryLatency := 124
	extraLatency := 83

	summary := buildStatusSummary(
		map[string]*ChannelMonitorLatest{
			"gpt-5.6-sol": {
				Model:     "gpt-5.6-sol",
				Status:    MonitorStatusOperational,
				LatencyMs: &primaryLatency,
			},
			"gpt-5.5": {
				Model:     "gpt-5.5",
				Status:    MonitorStatusDegraded,
				LatencyMs: &extraLatency,
			},
		},
		map[string]*ChannelMonitorAvailability{
			"gpt-5.6-sol": {
				Model:           "gpt-5.6-sol",
				AvailabilityPct: 99.5,
			},
			"gpt-5.5": {
				Model:           "gpt-5.5",
				AvailabilityPct: 96.25,
			},
		},
		"gpt-5.6-sol",
		[]string{"gpt-5.5"},
	)

	if summary.Availability7d != 99.5 {
		t.Fatalf("primary availability = %v, want 99.5", summary.Availability7d)
	}
	if len(summary.ExtraModels) != 1 {
		t.Fatalf("extra model count = %d, want 1", len(summary.ExtraModels))
	}

	extra := summary.ExtraModels[0]
	if extra.Model != "gpt-5.5" {
		t.Fatalf("extra model = %q, want gpt-5.5", extra.Model)
	}
	if extra.Availability7d != 96.25 {
		t.Fatalf("extra availability = %v, want 96.25", extra.Availability7d)
	}
	if extra.LatencyMs == nil || *extra.LatencyMs != extraLatency {
		t.Fatalf("extra latency = %v, want %d", extra.LatencyMs, extraLatency)
	}
}
