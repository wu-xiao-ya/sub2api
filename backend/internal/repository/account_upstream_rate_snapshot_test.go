package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamBillingRateSnapshotValuesUsesObservedAt(t *testing.T) {
	receivedAt := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
	snapshot := &service.UpstreamBillingProbeSnapshot{
		Status:     service.UpstreamBillingProbeStatusOK,
		ReceivedAt: &receivedAt,
		Data: map[string]any{
			"effective_rate_multiplier": 0.07,
			"observed_at":               "2026-08-01T01:02:00Z",
		},
	}

	rate, observedAt, capturedAt, ok := upstreamBillingRateSnapshotValues(snapshot)

	require.True(t, ok)
	require.InDelta(t, 0.07, rate, 1e-12)
	require.Equal(t, "2026-08-01T01:02:00Z", observedAt.Format(time.RFC3339))
	require.Equal(t, receivedAt, capturedAt)
}

func TestUpstreamBillingRateSnapshotValuesRejectsInvalidRate(t *testing.T) {
	snapshot := &service.UpstreamBillingProbeSnapshot{
		Status: service.UpstreamBillingProbeStatusOK,
		Data: map[string]any{
			"effective_rate_multiplier": -0.1,
			"observed_at":               "2026-08-01T01:02:00Z",
		},
	}

	_, _, _, ok := upstreamBillingRateSnapshotValues(snapshot)

	require.False(t, ok)
}
