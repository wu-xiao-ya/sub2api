package service

import (
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	// UpstreamBillingManualRateExtraKey is an operator-provided upstream cost
	// multiplier. It overrides a probed value for scheduling and profit reports.
	UpstreamBillingManualRateExtraKey = "upstream_billing_manual_rate"

	UpstreamBillingRateSnapshotSourceManual              = "manual"
	UpstreamBillingRateSnapshotSourceManualCleared       = "manual_cleared"
	UpstreamBillingRateSnapshotSourceManualClearFallback = "manual_clear_fallback"
)

// UpstreamBillingManualRate returns the validated manual rate from account extra.
// A zero rate is intentional and remains a valid override.
func UpstreamBillingManualRate(extra map[string]any) (float64, bool) {
	if len(extra) == 0 {
		return 0, false
	}
	raw, exists := extra[UpstreamBillingManualRateExtraKey]
	if !exists {
		return 0, false
	}
	rate, ok := upstreamBillingManualRateNumber(raw)
	if !ok || rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	return rate, true
}

// ValidateUpstreamBillingManualRateExtra validates and normalizes the JSON value
// before an account edit is persisted.
func ValidateUpstreamBillingManualRateExtra(extra map[string]any) error {
	if len(extra) == 0 {
		return nil
	}
	raw, exists := extra[UpstreamBillingManualRateExtraKey]
	if !exists {
		return nil
	}
	if raw == nil {
		delete(extra, UpstreamBillingManualRateExtraKey)
		return nil
	}
	rate, ok := upstreamBillingManualRateNumber(raw)
	if !ok || rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return infraerrors.BadRequest(
			"INVALID_UPSTREAM_BILLING_MANUAL_RATE",
			"upstream_billing_manual_rate must be a finite number greater than or equal to 0",
		)
	}
	extra[UpstreamBillingManualRateExtraKey] = rate
	return nil
}

func upstreamBillingManualRateNumber(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	default:
		return 0, false
	}
}
