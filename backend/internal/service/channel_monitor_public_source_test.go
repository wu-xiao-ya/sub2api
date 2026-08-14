//go:build unit

package service

import "testing"

func TestPublicChannelMonitorSource(t *testing.T) {
	tests := []struct {
		name   string
		latest *ChannelMonitorLatest
		want   string
	}{
		{name: "missing history", latest: nil, want: ""},
		{
			name:   "real traffic",
			latest: &ChannelMonitorLatest{ProbeMode: accountProbeModeTraffic},
			want:   accountProbeModeTraffic,
		},
		{
			name:   "legacy active probe",
			latest: &ChannelMonitorLatest{},
			want:   "probe",
		},
		{
			name:   "adaptive sticky probe",
			latest: &ChannelMonitorLatest{ProbeMode: accountProbeModeSticky},
			want:   "probe",
		},
		{
			name:   "adaptive full probe",
			latest: &ChannelMonitorLatest{ProbeMode: accountProbeModeFull},
			want:   "probe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publicChannelMonitorSource(tt.latest); got != tt.want {
				t.Fatalf("publicChannelMonitorSource() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildUserViewIncludesPerModelSources(t *testing.T) {
	trafficLatency := 120
	probeLatency := 95
	monitor := &ChannelMonitor{
		ID:           1,
		Name:         "Plus",
		Provider:     MonitorProviderOpenAI,
		APIMode:      MonitorAPIModeResponses,
		PrimaryModel: "gpt-5.6-sol",
		ExtraModels:  []string{"gpt-5.5"},
	}
	latestByModel := map[string]*ChannelMonitorLatest{
		"gpt-5.6-sol": {
			Model:     "gpt-5.6-sol",
			Status:    MonitorStatusOperational,
			LatencyMs: &trafficLatency,
			ProbeMode: accountProbeModeTraffic,
		},
		"gpt-5.5": {
			Model:     "gpt-5.5",
			Status:    MonitorStatusOperational,
			LatencyMs: &probeLatency,
			ProbeMode: accountProbeModeSticky,
		},
	}
	summary := buildStatusSummary(
		latestByModel,
		map[string]*ChannelMonitorAvailability{},
		monitor.PrimaryModel,
		monitor.ExtraModels,
	)
	view := buildUserViewFromSummary(
		monitor,
		summary,
		latestByModel[monitor.PrimaryModel],
		nil,
	)

	if view.PrimarySource != accountProbeModeTraffic {
		t.Fatalf("primary source = %q, want traffic", view.PrimarySource)
	}
	if len(view.ExtraModels) != 1 || view.ExtraModels[0].Source != "probe" {
		t.Fatalf("extra model sources = %#v, want one probe source", view.ExtraModels)
	}
}
