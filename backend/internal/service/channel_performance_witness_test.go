package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPerformanceProtocolTerminalWitness(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       ChannelPerformanceOutcome
	}{
		{"responses", "data: {\"type\":\"response.completed\"}\n\n", PerformanceSuccess},
		{"anthropic", "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", PerformanceSuccess},
		{"gemini", "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n", PerformanceSuccess},
		{"chat", "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n", PerformanceSuccess},
		{"done", "data: [DONE]\r\n\r\n", PerformanceSuccess},
		{"heartbeat", ": ping\n\ndata: {\"type\":\"response.created\"}\n\n", PerformanceUnknown},
		{"cut frame", "data: {\"type\":\"response.completed\"}", PerformanceUnknown},
		{"error then done", "data: {\"error\":{\"code\":\"quota_exhausted\"}}\n\ndata: [DONE]\n\n", PerformanceFailure},
		{"done then error", "data: [DONE]\n\nevent: error\ndata: {}\n\n", PerformanceFailure},
		{"incomplete", "data: {\"type\":\"response.incomplete\"}\n\n", PerformanceFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{1, 7, 4096} {
				w := &PerformanceStreamWitness{}
				for start := 0; start < len(tc.body); start += size {
					w.Write([]byte(tc.body[start:min(start+size, len(tc.body))]))
				}
				require.Equal(t, tc.want, w.Outcome())
			}
		})
	}
	w := &PerformanceStreamWitness{}
	w.Write([]byte("data: " + strings.Repeat("x", performanceWitnessLimit+50) + "\n\ndata: [DONE]\n\n"))
	require.Equal(t, PerformanceUnknown, w.Outcome())
	require.LessOrEqual(t, cap(w.buffer), 2*performanceWitnessLimit)
}
