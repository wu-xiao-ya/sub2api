package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamBillingManualRateAcceptsZeroAndRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  float64
		ok    bool
	}{
		{name: "missing", extra: map[string]any{}, ok: false},
		{name: "zero", extra: map[string]any{UpstreamBillingManualRateExtraKey: 0}, want: 0, ok: true},
		{name: "decimal", extra: map[string]any{UpstreamBillingManualRateExtraKey: 0.07}, want: 0.07, ok: true},
		{name: "negative", extra: map[string]any{UpstreamBillingManualRateExtraKey: -0.1}, ok: false},
		{name: "string", extra: map[string]any{UpstreamBillingManualRateExtraKey: "0.07"}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := UpstreamBillingManualRate(tt.extra)
			require.Equal(t, tt.ok, ok)
			if ok {
				require.InDelta(t, tt.want, got, 1e-12)
			}
		})
	}
}

func TestCurrentUpstreamBillingRatePrefersManualOverride(t *testing.T) {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	account := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.2, now.Add(-time.Minute), time.Hour)
	account.Extra[UpstreamBillingManualRateExtraKey] = 0.07

	rate, ok := CurrentUpstreamBillingRate(account, now)

	require.True(t, ok)
	require.InDelta(t, 0.07, rate, 1e-12)
}

func TestValidateUpstreamBillingManualRateExtraNormalizesNumericInput(t *testing.T) {
	extra := map[string]any{UpstreamBillingManualRateExtraKey: int64(7)}

	require.NoError(t, ValidateUpstreamBillingManualRateExtra(extra))
	require.Equal(t, 7.0, extra[UpstreamBillingManualRateExtraKey])

	err := ValidateUpstreamBillingManualRateExtra(map[string]any{
		UpstreamBillingManualRateExtraKey: "0.07",
	})
	require.Error(t, err)

	clearExtra := map[string]any{UpstreamBillingManualRateExtraKey: nil}
	require.NoError(t, ValidateUpstreamBillingManualRateExtra(clearExtra))
	require.NotContains(t, clearExtra, UpstreamBillingManualRateExtraKey)
}
