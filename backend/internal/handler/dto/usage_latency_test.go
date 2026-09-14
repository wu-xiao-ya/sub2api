package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUsageLatencyDTOUserAndAdmin(t *testing.T) {
	first, total := 123, 456
	record := &service.UsageLog{Model: "model-a", FirstTokenMs: &first, DurationMs: &total,
		LatencyBreakdown: &service.UsageLatencyBreakdown{Version: 2, AttemptCount: 2, FirstResponseMs: &first, TotalDurationMs: &total}}
	for name, value := range map[string]any{"user": UsageLogFromService(record), "admin": UsageLogFromServiceAdmin(record)} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(value)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(data, &decoded))
			b := decoded["latency_breakdown"].(map[string]any)
			require.Equal(t, float64(2), b["version"])
			require.Equal(t, float64(2), b["attempt_count"])
			require.Equal(t, float64(123), b["first_response_ms"])
			require.NotContains(t, b, "first_character_ms")
			require.Equal(t, float64(123), decoded["first_token_ms"])
			require.Equal(t, float64(456), decoded["duration_ms"])
		})
	}
	detached := UsageLogFromService(record)
	detached.LatencyBreakdown["first_response_ms"] = 999
	require.Equal(t, 123, *record.LatencyBreakdown.FirstResponseMs)
	legacy := UsageLogFromService(&service.UsageLog{FirstTokenMs: &first, DurationMs: &total})
	require.Nil(t, legacy.LatencyBreakdown)
}
