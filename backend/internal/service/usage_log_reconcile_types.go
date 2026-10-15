package service

import "time"

// UsageLogReconcileCandidate represents a usage_logs row eligible for cost reconciliation.
type UsageLogReconcileCandidate struct {
	ID                    int64
	AccountID             int64
	CreatedAt             time.Time
	Model                 string
	RequestedModel        string
	InputTokens           int
	OutputTokens          int
	ImageInputTokens      int
	ImageOutputTokens     int
	AccountStatsCost      *float64
	AccountRateMultiplier *float64
}
