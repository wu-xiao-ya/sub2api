package service

import "testing"

// Opus 5.5 makes thinking mandatory: a low-cost probe with a one-token budget
// gets its whole output eaten by hidden reasoning and returns 2xx with no
// visible text. The challenge must treat that as a live channel.
func TestValidateMonitorChallengeResponseAnthropicThinkingEvidence(t *testing.T) {
	lowCost := &CheckOptions{LowCost: true}

	// Gateway shape: usage.output_tokens_details.thinking_tokens (observed on
	// feilunhai-style gateways).
	thinkingUsage := []byte(`{
		"content": [{"type": "text", "text": ""}],
		"stop_reason": "max_tokens",
		"usage": {"output_tokens_details": {"thinking_tokens": 1}}
	}`)
	if !validateMonitorChallengeResponse(MonitorProviderAnthropic, "", thinkingUsage, "1", lowCost) {
		t.Fatal("thinking-token usage with stop_reason=max_tokens should prove liveness")
	}

	// Official API shape: an explicit thinking content block.
	thinkingBlock := []byte(`{
		"content": [{"type": "thinking", "thinking": "", "signature": "sig"}],
		"stop_reason": "max_tokens"
	}`)
	if !validateMonitorChallengeResponse(MonitorProviderAnthropic, "", thinkingBlock, "1", lowCost) {
		t.Fatal("thinking content block with stop_reason=max_tokens should prove liveness")
	}

	// The model ended on its own without answering — not a liveness proof.
	endedWithoutAnswer := []byte(`{
		"content": [{"type": "thinking", "thinking": "", "signature": "sig"}],
		"stop_reason": "end_turn"
	}`)
	if validateMonitorChallengeResponse(MonitorProviderAnthropic, "", endedWithoutAnswer, "1", lowCost) {
		t.Fatal("end_turn without answer text must stay failed")
	}

	// A max_tokens cut with zero thinking evidence is still a failure.
	empty := []byte(`{"content": [{"type": "text", "text": ""}], "stop_reason": "max_tokens"}`)
	if validateMonitorChallengeResponse(MonitorProviderAnthropic, "", empty, "1", lowCost) {
		t.Fatal("empty reply without thinking evidence must stay failed")
	}

	// The fallback is low-cost only; regular checks keep the full challenge.
	if validateMonitorChallengeResponse(MonitorProviderAnthropic, "", thinkingUsage, "1", nil) {
		t.Fatal("non-low-cost checks must keep the full challenge requirement")
	}
}
