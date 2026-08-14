package service

import "time"

// OpsUpstreamAttributionItem is a compact, account-level view of upstream
// failures. It is intentionally derived from ops_error_logs so it does not
// create another high-write metrics table.
type OpsUpstreamAttributionItem struct {
	GroupID       *int64 `json:"group_id"`
	GroupName     string `json:"group_name"`
	AccountID     *int64 `json:"account_id"`
	AccountName   string `json:"account_name"`
	Total         int64  `json:"total"`
	Overload      int64  `json:"overload"`
	RateLimit     int64  `json:"rate_limit"`
	ServerError   int64  `json:"server_error"`
	Transport     int64  `json:"transport"`
	StreamFailure int64  `json:"stream_failure"`

	AverageUpstreamLatencyMs *int64    `json:"average_upstream_latency_ms,omitempty"`
	LastErrorAt               time.Time `json:"last_error_at"`
	LastStatusCode            int        `json:"last_status_code"`
	LastMessage               string     `json:"last_message"`
	LastEndpoint              string     `json:"last_endpoint"`
}

type OpsUpstreamAttributionResponse struct {
	StartTime time.Time                       `json:"start_time"`
	EndTime   time.Time                       `json:"end_time"`
	Platform  string                          `json:"platform"`
	GroupID   *int64                          `json:"group_id"`
	Total     int64                           `json:"total"`
	Items     []*OpsUpstreamAttributionItem   `json:"items"`
}
