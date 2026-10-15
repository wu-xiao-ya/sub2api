package service

import (
	"testing"
	"time"
)

func TestOpenAIStreamLatencyTrackerResponsesMilestones(t *testing.T) {
	tracker := newOpenAIStreamLatencyTracker(time.Now().Add(-time.Second))
	if tracker.values.FirstResponseMs == nil {
		t.Fatal("expected first response to be recorded at tracker creation")
	}

	tracker.observeData(`{"type":"response.created"}`, "response.created")
	if tracker.values.FirstEventMs == nil {
		t.Fatal("expected first valid event")
	}
	if tracker.values.FirstOutputMs != nil || tracker.values.FirstCharacterMs != nil {
		t.Fatal("response.created must not count as output or visible text")
	}

	tracker.observeData(
		`{"type":"response.output_item.added","item":{"type":"message","content":[]}}`,
		"response.output_item.added",
	)
	if tracker.values.FirstOutputMs == nil {
		t.Fatal("expected output item to start client output")
	}
	if tracker.values.FirstCharacterMs != nil {
		t.Fatal("empty output item must not count as visible text")
	}

	tracker.observeData(
		`{"type":"response.reasoning_summary_text.delta","delta":"internal reasoning"}`,
		"response.reasoning_summary_text.delta",
	)
	if tracker.values.FirstCharacterMs != nil {
		t.Fatal("reasoning event must not count as user-visible text")
	}

	tracker.observeData(
		`{"type":"response.output_text.delta","delta":"hello"}`,
		"response.output_text.delta",
	)
	if tracker.values.FirstCharacterMs == nil {
		t.Fatal("output text delta must count as first visible character")
	}

	result := tracker.result()
	if result == nil || result.TotalDurationMs == nil {
		t.Fatal("expected total duration when finalizing tracker")
	}
}

func TestOpenAIStreamLatencyTrackerChatCompletionVisibleCharacter(t *testing.T) {
	tracker := newOpenAIStreamLatencyTracker(time.Now().Add(-time.Second))

	tracker.observeData(`{"choices":[{"delta":{"role":"assistant"}}]}`, "")
	if tracker.values.FirstEventMs == nil || tracker.values.FirstOutputMs == nil {
		t.Fatal("role-only chat chunk should count as an event and output structure")
	}
	if tracker.values.FirstCharacterMs != nil {
		t.Fatal("role-only chat chunk must not count as visible text")
	}

	tracker.observeData(`{"choices":[{"delta":{"content":"hello"}}]}`, "")
	if tracker.values.FirstCharacterMs == nil {
		t.Fatal("chat content delta must count as first visible character")
	}
}

func TestOpenAIStreamDataContainsVisibleCharacterFallbackEvents(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      string
	}{
		{
			name:      "output text done",
			eventType: "response.output_text.done",
			data:      `{"type":"response.output_text.done","text":"hello"}`,
		},
		{
			name:      "output item done",
			eventType: "response.output_item.done",
			data:      `{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"hello"}]}}`,
		},
		{
			name:      "response completed",
			eventType: "response.completed",
			data:      `{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !openAIStreamDataContainsVisibleCharacter(test.data, test.eventType) {
				t.Fatalf("%s should contain visible output", test.eventType)
			}
		})
	}
}

func TestOpenAIStreamLatencyTrackerIgnoresInvalidData(t *testing.T) {
	tracker := newOpenAIStreamLatencyTracker(time.Now().Add(-time.Second))
	tracker.observeData("", "")
	tracker.observeData("[DONE]", "")
	tracker.observeData("not-json", "")

	if tracker.values.FirstEventMs != nil ||
		tracker.values.FirstOutputMs != nil ||
		tracker.values.FirstCharacterMs != nil {
		t.Fatal("invalid or terminal data must not create stream milestones")
	}
}
