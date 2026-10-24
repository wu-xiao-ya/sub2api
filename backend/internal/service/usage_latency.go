package service

// UsageLatencyBreakdown version 2 starts at logical request ingress. Legacy
// records (absent version or version 1) start at forwarding. Never merge these
// origins or infer unknown milestones from the old first-token field.
type UsageLatencyBreakdown struct {
	Version          int  `json:"version,omitempty"`
	AttemptCount     int  `json:"attempt_count,omitempty"`
	ForwardStartMs   *int `json:"forward_start_ms,omitempty"`
	FirstResponseMs  *int `json:"first_response_ms,omitempty"`
	FirstEventMs     *int `json:"first_event_ms,omitempty"`
	FirstOutputMs    *int `json:"first_output_ms,omitempty"`
	FirstCharacterMs *int `json:"first_character_ms,omitempty"`
	TotalDurationMs  *int `json:"total_duration_ms,omitempty"`
}

func (b *UsageLatencyBreakdown) Clone() *UsageLatencyBreakdown {
	if b == nil {
		return nil
	}
	cloneInt := func(value *int) *int {
		if value == nil {
			return nil
		}
		copied := *value
		return &copied
	}
	return &UsageLatencyBreakdown{
		Version:          b.Version,
		AttemptCount:     b.AttemptCount,
		ForwardStartMs:   cloneInt(b.ForwardStartMs),
		FirstResponseMs:  cloneInt(b.FirstResponseMs),
		FirstEventMs:     cloneInt(b.FirstEventMs),
		FirstOutputMs:    cloneInt(b.FirstOutputMs),
		FirstCharacterMs: cloneInt(b.FirstCharacterMs),
		TotalDurationMs:  cloneInt(b.TotalDurationMs),
	}
}

func (b *UsageLatencyBreakdown) Empty() bool {
	return b == nil ||
		(b.FirstResponseMs == nil &&
			b.FirstEventMs == nil &&
			b.FirstOutputMs == nil &&
			b.FirstCharacterMs == nil &&
			b.TotalDurationMs == nil)
}

func (b *UsageLatencyBreakdown) Map() map[string]int {
	if b.Empty() {
		return nil
	}
	out := make(map[string]int, 5)
	if b.Version > 0 {
		out["version"] = b.Version
	}
	if b.AttemptCount > 0 {
		out["attempt_count"] = b.AttemptCount
	}
	if b.ForwardStartMs != nil {
		out["forward_start_ms"] = *b.ForwardStartMs
	}
	if b.FirstResponseMs != nil {
		out["first_response_ms"] = *b.FirstResponseMs
	}
	if b.FirstEventMs != nil {
		out["first_event_ms"] = *b.FirstEventMs
	}
	if b.FirstOutputMs != nil {
		out["first_output_ms"] = *b.FirstOutputMs
	}
	if b.FirstCharacterMs != nil {
		out["first_character_ms"] = *b.FirstCharacterMs
	}
	if b.TotalDurationMs != nil {
		out["total_duration_ms"] = *b.TotalDurationMs
	}
	return out
}

func UsageLatencyBreakdownFromMap(values map[string]int) *UsageLatencyBreakdown {
	if len(values) == 0 {
		return nil
	}
	ptr := func(key string) *int {
		value, ok := values[key]
		if !ok {
			return nil
		}
		return &value
	}
	out := &UsageLatencyBreakdown{
		Version:          values["version"],
		AttemptCount:     values["attempt_count"],
		ForwardStartMs:   ptr("forward_start_ms"),
		FirstResponseMs:  ptr("first_response_ms"),
		FirstEventMs:     ptr("first_event_ms"),
		FirstOutputMs:    ptr("first_output_ms"),
		FirstCharacterMs: ptr("first_character_ms"),
		TotalDurationMs:  ptr("total_duration_ms"),
	}
	if out.Empty() {
		return nil
	}
	return out
}
