package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

type websocketPerformanceKey struct{}

// One bounded witness per connection, not a transcript or a billing object.
// The sink must be nonblocking, as ChannelPerformanceService.RecordFact is.
type WebSocketPerformanceSession struct {
	mu              sync.Mutex
	sink            func(ChannelPerformanceFact)
	connectionID    string
	turn            uint64
	current         ChannelPerformanceFact
	active          bool
	cancelRequested bool
	localExclusion  bool
	latencyCtx      context.Context
}

func NewWebSocketPerformanceSession(keyID, groupID int64, payload []byte, at time.Time, sink func(ChannelPerformanceFact)) *WebSocketPerformanceSession {
	if sink == nil || keyID <= 0 || groupID <= 0 {
		return nil
	}
	s := &WebSocketPerformanceSession{sink: sink, connectionID: uuid.NewString(), current: ChannelPerformanceFact{APIKeyID: keyID, GroupID: groupID}}
	s.Next(payload, "", at)
	return s
}

func WithWebSocketPerformance(ctx context.Context, s *WebSocketPerformanceSession) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, websocketPerformanceKey{}, s)
}

// Only a new client create frame starts a turn. Internal retries never call Next.
func (s *WebSocketPerformanceSession) Next(payload []byte, fallbackModel string, at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.commitLocked()
	}
	model := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	if model == "" {
		model = strings.TrimSpace(fallbackModel)
	}
	if model == "" {
		model = s.current.Model
	}
	s.turn++
	stream := true
	s.current = ChannelPerformanceFact{
		APIKeyID: s.current.APIKeyID, GroupID: s.current.GroupID,
		RequestID: "ws:" + s.connectionID + ":" + strconv.FormatUint(s.turn, 10),
		Model:     model, Stream: &stream, StartedAt: at,
		ServiceTier:     performanceFrameDimension(payload, "service_tier"),
		ReasoningEffort: performanceFrameDimension(payload, "reasoning.effort"),
		Outcome:         PerformanceExcluded,
	}
	s.latencyCtx = nil
	s.active = true
	s.cancelRequested = false
	s.localExclusion = false
}

func performanceFrameDimension(payload []byte, path string) string {
	v := gjson.GetBytes(payload, path)
	if v.Type == gjson.String && len(v.Str) <= 64 && strings.TrimSpace(v.Str) != "" {
		return strings.TrimSpace(v.Str)
	}
	return "unknown"
}

// Marks structurally classified routing failures or local client restrictions.
func (s *WebSocketPerformanceSession) SetOutcome(outcome ChannelPerformanceOutcome) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.current.Outcome = outcome
		s.localExclusion = outcome == PerformanceExcluded
	}
}

func (s *WebSocketPerformanceSession) beforeAttempt(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.latencyCtx = ctx
		s.current.Outcome = PerformanceFailure
		s.localExclusion = false
	}
}

func (s *WebSocketPerformanceSession) ClientCancelled() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.cancelRequested = true
	}
}

func (s *WebSocketPerformanceSession) ObserveResult(result *OpenAIForwardResult, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	s.current.CompletedAt = time.Now()
	if result != nil {
		s.current.UsageRequestID = strings.TrimSpace(result.RequestID)
		s.current.Latency = result.LatencyBreakdown.Clone()
		s.current.ServiceTier = "unknown"
		s.current.ReasoningEffort = "unknown"
		if result.ServiceTier != nil {
			s.current.ServiceTier = *result.ServiceTier
		}
		if result.ReasoningEffort != nil {
			s.current.ReasoningEffort = *result.ReasoningEffort
		}
	}
	if err != nil {
		s.observeErrorLocked(err)
		// The handler may still switch accounts. Only Close or a subsequent
		// completed result can commit this logical request's terminal outcome.
		return
	}
	s.current.Outcome = PerformanceUnknown
	if result != nil {
		switch strings.TrimSpace(result.UpstreamTerminalEvent) {
		case "response.completed", "response.done":
			s.current.Outcome = PerformanceSuccess
		case "error", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			s.current.Outcome = PerformanceFailure
		}
		if result.performanceDeliveryFailed {
			s.current.Outcome = PerformanceFailure
		}
		if result.performanceClientCancelled || s.cancelRequested {
			s.current.Outcome = PerformanceExcluded
		}
	}
	s.commitLocked()
}

func (s *WebSocketPerformanceSession) ObserveError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.observeErrorLocked(err)
	}
}

func (s *WebSocketPerformanceSession) observeErrorLocked(err error) {
	s.current.CompletedAt = time.Now()
	s.current.Outcome = PerformanceFailure
	if s.latencyCtx != nil {
		s.current.Latency = FinalRequestLatencySnapshot(s.latencyCtx)
	}
	var failover *UpstreamFailoverError
	var closeErr *OpenAIWSClientCloseError
	switch {
	case errors.Is(err, ErrOpenAIWSIngressLeaseLost):
	case errors.As(err, &failover):
	case errors.Is(err, context.Canceled):
		s.current.Outcome = PerformanceExcluded
	case isPerformanceVoluntaryDisconnect(err):
		s.current.Outcome = PerformanceExcluded
	case errors.As(err, &closeErr):
		if closeErr.StatusCode() == coderws.StatusPolicyViolation || closeErr.StatusCode() == coderws.StatusNormalClosure {
			s.current.Outcome = PerformanceExcluded
		}
	}
	if s.cancelRequested && !errors.Is(err, ErrOpenAIWSIngressLeaseLost) {
		s.current.Outcome = PerformanceExcluded
	}
	// The same close code can represent a user limit or upstream saturation.
	// Preserve the handler's explicit ownership, not the human error message.
	if s.localExclusion {
		s.current.Outcome = PerformanceExcluded
	}
}

// Closing an idle connection produces no additional request. A pending failure
// without an upstream response ID still has its independent logical identity.
func (s *WebSocketPerformanceSession) Close(cause error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	if cause != nil {
		s.observeErrorLocked(cause)
	}
	if s.cancelRequested && !errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
		s.current.Outcome = PerformanceExcluded
	}
	s.current.CompletedAt = time.Now()
	if s.latencyCtx != nil {
		s.current.Latency = FinalRequestLatencySnapshot(s.latencyCtx)
	}
	s.commitLocked()
}

func (s *WebSocketPerformanceSession) commitLocked() {
	if s.current.CompletedAt.IsZero() {
		s.current.CompletedAt = time.Now()
	}
	fact := s.current
	fact.Latency = fact.Latency.Clone()
	s.active = false
	s.latencyCtx = nil
	if fact.Validate() == nil {
		s.sink(fact)
	}
}

// A telemetry-only result, independent of the earlier usage/billing callback.
// Keep the native ID lookup identical to the relay's terminal usage lookup.
func websocketPerformanceResult(payload []byte, latency *UsageLatencyBreakdown, tier, effort *string) *OpenAIForwardResult {
	if !gjson.ValidBytes(payload) {
		return nil
	}
	event := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if event != "error" && !openAIWSPassthroughIsTerminalOutput(payload) {
		return nil
	}
	id := ""
	for _, path := range []string{"response.id", "response_id"} {
		if value := gjson.GetBytes(payload, path); value.Type == gjson.String && strings.TrimSpace(value.Str) != "" {
			id = strings.TrimSpace(value.Str)
			break
		}
	}
	if id == "" && event != "error" {
		id = strings.TrimSpace(gjson.GetBytes(payload, "id").String())
	}
	return &OpenAIForwardResult{RequestID: id, OpenAIWSMode: true, Stream: true,
		UpstreamTerminalEvent: event, LatencyBreakdown: latency.Clone(), ServiceTier: tier, ReasoningEffort: effort}
}

func isPerformanceVoluntaryDisconnect(err error) bool {
	status := coderws.CloseStatus(err)
	return status == coderws.StatusNormalClosure || status == coderws.StatusGoingAway
}
