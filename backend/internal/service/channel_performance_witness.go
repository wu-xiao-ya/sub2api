package service

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Only terminal protocol facts are retained. Content bytes never leave this
// bounded in-memory parser; milestones and usage remain separate observations.
type PerformanceStreamWitness struct {
	buffer       []byte
	data         []byte
	event        string
	overflow     bool
	discard      bool
	success      bool
	failure      bool
	unobserved   bool
	jsonBody     []byte
	jsonOverflow bool
}

const performanceWitnessLimit = 64 * 1024

func (w *PerformanceStreamWitness) Write(p []byte) {
	for _, b := range p {
		if b == '\n' {
			if !w.overflow {
				w.line(bytes.TrimSuffix(w.buffer, []byte{'\r'}))
			} else {
				w.discard = true
				w.unobserved = true
			}
			w.buffer = w.buffer[:0]
			w.overflow = false
		} else if !w.overflow {
			if len(w.buffer) >= performanceWitnessLimit {
				w.buffer = nil
				w.overflow = true
			} else {
				w.buffer = append(w.buffer, b)
			}
		}
	}
}

func (w *PerformanceStreamWitness) line(line []byte) {
	if len(line) == 0 {
		if !w.discard {
			w.observe(w.data, w.event)
		}
		w.data = w.data[:0]
		w.event = ""
		w.discard = false
		return
	}
	if bytes.HasPrefix(line, []byte("event:")) {
		w.event = strings.TrimSpace(string(line[6:]))
	}
	if bytes.HasPrefix(line, []byte("data:")) && !w.discard {
		value := bytes.TrimPrefix(line[5:], []byte{' '})
		if len(w.data)+len(value)+1 > performanceWitnessLimit {
			w.discard = true
			w.unobserved = true
			w.data = nil
			return
		}
		if len(w.data) > 0 {
			w.data = append(w.data, '\n')
		}
		w.data = append(w.data, value...)
	}
}

func (w *PerformanceStreamWitness) observe(data []byte, event string) {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("[DONE]")) {
		w.success = true
		return
	}
	var v struct {
		Type    string          `json:"type"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if len(data) > 0 && json.Unmarshal(data, &v) != nil {
		w.unobserved = true
		return
	}
	if v.Type != "" {
		event = v.Type
	}
	if len(v.Error) > 0 && string(v.Error) != "null" {
		w.failure = true
	}
	switch event {
	case "error", "response.failed", "response.incomplete":
		w.failure = true
	case "response.completed", "message_stop", "image_generation.completed":
		w.success = true
	}
	for _, c := range v.Choices {
		if c.FinishReason != nil && *c.FinishReason != "" {
			w.success = true
		}
	}
	for _, c := range v.Candidates {
		if c.FinishReason != "" {
			w.success = true
		}
	}
}

func (w *PerformanceStreamWitness) Outcome() ChannelPerformanceOutcome {
	// Do not invent an event boundary when the connection ended mid-frame.
	if w.failure {
		return PerformanceFailure
	}
	if w.unobserved {
		return PerformanceUnknown
	}
	if w.success {
		return PerformanceSuccess
	}
	return PerformanceUnknown
}

func (w *PerformanceStreamWitness) Incomplete() bool { return w.unobserved }

func (w *PerformanceStreamWitness) WriteJSON(p []byte) {
	if w.jsonOverflow {
		return
	}
	if len(w.jsonBody)+len(p) > performanceWitnessLimit {
		w.jsonBody = nil
		w.jsonOverflow = true
		return
	}
	w.jsonBody = append(w.jsonBody, p...)
}

func (w *PerformanceStreamWitness) NonStreamOutcome(status int, contentType string) ChannelPerformanceOutcome {
	if status < 200 || status >= 300 || !strings.Contains(strings.ToLower(contentType), "application/json") || w.jsonOverflow {
		return PerformanceUnknown
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(w.jsonBody, &body) != nil {
		return PerformanceUnknown
	}
	if err, ok := body["error"]; ok && string(err) != "null" {
		return PerformanceFailure
	}
	if raw, ok := body["status"]; ok && (string(raw) == "\"failed\"" || string(raw) == "\"incomplete\"") {
		return PerformanceFailure
	}
	return PerformanceSuccess
}
