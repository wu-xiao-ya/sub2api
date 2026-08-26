package service

// UsageLatencyBreakdown records request milestones measured from the gateway
// forwarding start. Nil fields mean the protocol did not expose an exact
// milestone; callers must not infer missing stages from another metric.
type UsageLatencyBreakdown struct {
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
