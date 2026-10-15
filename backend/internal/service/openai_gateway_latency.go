package service

import (
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type openAIStreamLatencyTracker struct {
	startedAt time.Time
	values    UsageLatencyBreakdown
}

func newOpenAIStreamLatencyTracker(startedAt time.Time) *openAIStreamLatencyTracker {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	tracker := &openAIStreamLatencyTracker{startedAt: startedAt}
	tracker.mark(&tracker.values.FirstResponseMs)
	return tracker
}

func (t *openAIStreamLatencyTracker) mark(target **int) {
	if t == nil || target == nil || *target != nil {
		return
	}
	value := int(time.Since(t.startedAt).Milliseconds())
	if value < 0 {
		value = 0
	}
	*target = &value
}

func (t *openAIStreamLatencyTracker) observeData(data, eventType string) {
	if t == nil {
		return
	}
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "[DONE]" || !gjson.Valid(trimmed) {
		return
	}
	t.mark(&t.values.FirstEventMs)
	if openAIStreamDataStartsClientOutput(trimmed, eventType) {
		t.mark(&t.values.FirstOutputMs)
	}
	if openAIStreamDataContainsVisibleCharacter(trimmed, eventType) {
		t.mark(&t.values.FirstCharacterMs)
	}
}

func (t *openAIStreamLatencyTracker) result() *UsageLatencyBreakdown {
	if t == nil || t.values.Empty() {
		return nil
	}
	result := t.values.Clone()
	value := int(time.Since(t.startedAt).Milliseconds())
	if value < 0 {
		value = 0
	}
	result.TotalDurationMs = &value
	return result
}

func openAIStreamDataContainsVisibleCharacter(data, eventType string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "[DONE]" || !gjson.Valid(trimmed) {
		return false
	}

	switch strings.TrimSpace(eventType) {
	case "response.output_text.delta", "response.refusal.delta":
		return gjsonStringHasVisibleCharacter(gjson.Get(trimmed, "delta"))
	case "response.output_text.done":
		return gjsonStringHasVisibleCharacter(gjson.Get(trimmed, "text"))
	case "response.refusal.done":
		return gjsonStringHasVisibleCharacter(gjson.Get(trimmed, "refusal"))
	case "response.content_part.added", "response.content_part.done":
		return gjsonStringHasVisibleCharacter(gjson.Get(trimmed, "part.text")) ||
			gjsonStringHasVisibleCharacter(gjson.Get(trimmed, "part.refusal"))
	case "response.output_item.added", "response.output_item.done":
		return openAIResponseItemContainsVisibleCharacter(gjson.Get(trimmed, "item"))
	case "response.completed", "response.done":
		for _, item := range gjson.Get(trimmed, "response.output").Array() {
			if openAIResponseItemContainsVisibleCharacter(item) {
				return true
			}
		}
		return false
	}

	// Raw Chat Completions streams usually omit a Responses event type.
	choices := gjson.Get(trimmed, "choices")
	if !choices.Exists() || !choices.IsArray() {
		return false
	}
	for _, choice := range choices.Array() {
		if gjsonStringHasVisibleCharacter(choice.Get("delta.content")) ||
			gjsonStringHasVisibleCharacter(choice.Get("delta.refusal")) ||
			gjsonStringHasVisibleCharacter(choice.Get("message.content")) {
			return true
		}
		content := choice.Get("delta.content")
		if content.IsArray() {
			for _, part := range content.Array() {
				if gjsonStringHasVisibleCharacter(part.Get("text")) {
					return true
				}
			}
		}
	}
	return false
}

func openAIResponseItemContainsVisibleCharacter(item gjson.Result) bool {
	if !item.Exists() {
		return false
	}
	if gjsonStringHasVisibleCharacter(item.Get("text")) ||
		gjsonStringHasVisibleCharacter(item.Get("refusal")) {
		return true
	}
	for _, part := range item.Get("content").Array() {
		if gjsonStringHasVisibleCharacter(part.Get("text")) ||
			gjsonStringHasVisibleCharacter(part.Get("refusal")) {
			return true
		}
	}
	return false
}

func gjsonStringHasVisibleCharacter(value gjson.Result) bool {
	if !value.Exists() || value.Type != gjson.String {
		return false
	}
	return strings.TrimSpace(value.String()) != ""
}
