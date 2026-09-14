package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type requestLatencyKey struct{}
type frozenLatencyKey struct{}
type webSocketFirstTurnReceivedKey struct{}

// The dedicated key retains only the first logical turn across account retries.
// It is not a session-wide active tracker: subsequent turns get their own tracker
// and asynchronous billing receives an immutable result snapshot.
func WithWebSocketFirstTurnReceived(ctx context.Context, at time.Time) context.Context {
	return context.WithValue(ctx, webSocketFirstTurnReceivedKey{}, &requestLatency{start: at})
}

func firstWebSocketTurnLatencyContext(ctx context.Context) context.Context {
	tracker, ok := ctx.Value(webSocketFirstTurnReceivedKey{}).(*requestLatency)
	if !ok || tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, requestLatencyKey{}, tracker)
}

func WithFrozenRequestLatency(ctx context.Context, snapshot *UsageLatencyBreakdown) context.Context {
	return context.WithValue(ctx, frozenLatencyKey{}, snapshot.Clone())
}

// Called after forwarding returns, before asynchronous billing is queued. Queue
// wait is not request duration; the immutable snapshot cannot change on retry.
func FinalRequestLatencySnapshot(ctx context.Context) *UsageLatencyBreakdown {
	t, _ := ctx.Value(requestLatencyKey{}).(*requestLatency)
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.current.Clone()
	if result != nil {
		elapsed := int(time.Since(t.start).Milliseconds())
		result.TotalDurationMs = &elapsed
	}
	return result
}

// A retry replaces the terminal attempt's milestones, never the request clock.
type requestLatency struct {
	mu       sync.Mutex
	start    time.Time
	attempts int
	current  *UsageLatencyBreakdown
}

func WithRequestLatency(ctx context.Context) context.Context {
	return withRequestLatencyAt(ctx, time.Now())
}

func withRequestLatencyAt(ctx context.Context, at time.Time) context.Context {
	return context.WithValue(ctx, requestLatencyKey{}, &requestLatency{start: at})
}

func BeginRequestLatencyAttempt(ctx context.Context) *UsageLatencyAttempt {
	t, _ := ctx.Value(requestLatencyKey{}).(*requestLatency)
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.attempts++
	elapsed := int(time.Since(t.start).Milliseconds())
	b := &UsageLatencyBreakdown{Version: 2, AttemptCount: t.attempts, ForwardStartMs: &elapsed}
	t.current = b
	return &UsageLatencyAttempt{owner: t, values: b}
}

type UsageLatencyAttempt struct {
	owner  *requestLatency
	values *UsageLatencyBreakdown
}

// Binary and WebSocket transports call this after decoding a protocol event.
// They do not invent HTTP response headers for reused WebSocket connections.
func CurrentRequestLatencyAttempt(ctx context.Context) *UsageLatencyAttempt {
	t, _ := ctx.Value(requestLatencyKey{}).(*requestLatency)
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil {
		return nil
	}
	return &UsageLatencyAttempt{owner: t, values: t.current}
}

func (a *UsageLatencyAttempt) ObserveProtocolEvent(data, event string) {
	a.observeProtocol(data, event, true)
}

func (a *UsageLatencyAttempt) observeProtocol(data, event string, stream bool) bool {
	if a == nil {
		return false
	}
	a.owner.mu.Lock()
	complete := (!stream || a.values.FirstEventMs != nil) && a.values.FirstOutputMs != nil && a.values.FirstCharacterMs != nil
	a.owner.mu.Unlock()
	if complete {
		return true
	}
	valid, output, visible := classifyLatencyPayload(data, event)
	if !valid {
		return false
	}
	a.owner.mu.Lock()
	defer a.owner.mu.Unlock()
	if stream {
		a.mark(&a.values.FirstEventMs)
	}
	if output {
		a.mark(&a.values.FirstOutputMs)
	}
	if visible {
		a.mark(&a.values.FirstCharacterMs)
	}
	return (!stream || a.values.FirstEventMs != nil) && a.values.FirstOutputMs != nil && a.values.FirstCharacterMs != nil
}

func (a *UsageLatencyAttempt) mark(field **int) {
	if *field == nil {
		n := int(time.Since(a.owner.start).Milliseconds())
		*field = &n
	}
}

func (a *UsageLatencyAttempt) ObserveResponse(resp *http.Response) {
	if a == nil || resp == nil {
		return
	}
	a.ResponseHeadersReceived()
	if resp.Body == nil {
		return
	}
	// Observe decoded bytes without changing reads, flushes, errors or closure.
	resp.Body = &latencyObservedBody{ReadCloser: resp.Body, attempt: a,
		stream: strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream"),
		valid:  resp.StatusCode >= 200 && resp.StatusCode < 300}
}

func (a *UsageLatencyAttempt) ResponseHeadersReceived() {
	if a == nil {
		return
	}
	a.owner.mu.Lock()
	a.mark(&a.values.FirstResponseMs)
	a.owner.mu.Unlock()
}

func RequestLatencySnapshot(ctx context.Context, fallback *UsageLatencyBreakdown) *UsageLatencyBreakdown {
	if snapshot, ok := ctx.Value(frozenLatencyKey{}).(*UsageLatencyBreakdown); ok && snapshot != nil {
		return snapshot.Clone()
	}
	t, _ := ctx.Value(requestLatencyKey{}).(*requestLatency)
	if t == nil {
		return fallback.Clone()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil {
		return fallback.Clone()
	}
	return t.current.Clone()
}

const latencyEventLimit = 256 * 1024

type latencyObservedBody struct {
	io.ReadCloser
	mu           sync.Mutex
	attempt      *UsageLatencyAttempt
	stream       bool
	valid        bool
	buffer       []byte
	data         []byte
	event        string
	dropping     bool
	discardEvent bool
	captured     bool
	done         bool
}

func (b *latencyObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.valid && !b.done && !b.captured {
		b.observe(p[:n])
	}
	if err != nil {
		if err == io.EOF && b.valid && !b.done {
			if b.stream {
				b.line()
				b.emit()
			} else if !b.dropping {
				b.sample(string(b.buffer), "", false)
			}
		}
		b.finish()
	}
	return n, err
}

func (b *latencyObservedBody) Close() error {
	err := b.ReadCloser.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finish()
	return err
}

func (b *latencyObservedBody) finish() {
	if b.done {
		return
	}
	b.done = true
	b.attempt.owner.mu.Lock()
	b.attempt.mark(&b.attempt.values.TotalDurationMs)
	b.attempt.owner.mu.Unlock()
	b.buffer = nil
	b.data = nil
}

func (b *latencyObservedBody) observe(p []byte) {
	if !b.stream {
		if len(b.buffer)+len(p) > latencyEventLimit {
			b.dropping = true
			b.buffer = nil
		}
		if !b.dropping {
			b.buffer = append(b.buffer, p...)
		}
		return
	}
	for _, c := range p {
		if b.captured {
			b.buffer = nil
			b.data = nil
			return
		}
		if c == '\n' {
			b.line()
			continue
		}
		if len(b.buffer) >= latencyEventLimit {
			b.dropping = true
			b.discardEvent = true
			b.buffer = nil
		}
		if !b.dropping {
			b.buffer = append(b.buffer, c)
		}
	}
}

func (b *latencyObservedBody) line() {
	line := strings.TrimSuffix(string(b.buffer), "\r")
	b.buffer = b.buffer[:0]
	if b.dropping {
		b.dropping = false
		b.data = nil
		b.event = ""
		return
	}
	if line == "" {
		b.emit()
		return
	}
	if b.discardEvent {
		return
	}
	if strings.HasPrefix(line, "event:") {
		b.event = strings.TrimSpace(line[6:])
	}
	if strings.HasPrefix(line, "data:") {
		value := strings.TrimPrefix(line[5:], " ")
		if len(b.data)+len(value)+1 > latencyEventLimit {
			b.data = nil
			b.discardEvent = true
			return
		}
		b.data = append(b.data, value...)
		b.data = append(b.data, '\n')
	}
}

func (b *latencyObservedBody) emit() {
	if len(b.data) > 0 && !b.discardEvent {
		b.sample(string(b.data), b.event, true)
	}
	b.discardEvent = false
	b.data = b.data[:0]
	b.event = ""
}

func (b *latencyObservedBody) sample(data, event string, stream bool) {
	b.captured = b.attempt.observeProtocol(data, event, stream)
}
