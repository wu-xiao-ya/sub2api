package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestPerformanceWebSocketRetryAndTurnIdentity(t *testing.T) {
	var facts []ChannelPerformanceFact
	at := time.Now().Add(-time.Second)
	s := NewWebSocketPerformanceSession(7, 8, []byte(`{"model":"public-model","service_tier":"priority","reasoning":{"effort":"high"}}`), at,
		func(f ChannelPerformanceFact) { facts = append(facts, f) })
	ctx := WithWebSocketPerformance(withRequestLatencyAt(context.Background(), at), s)
	BeginRequestLatencyAttempt(ctx)
	s.ObserveError(&UpstreamFailoverError{StatusCode: 503})
	require.Empty(t, facts, "an internal retry is not a terminal failure")
	BeginRequestLatencyAttempt(ctx).ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"ok"}`, "")
	tier := "priority"
	s.ObserveResult(&OpenAIForwardResult{RequestID: "resp_native", Model: "mapped-model", ServiceTier: &tier,
		UpstreamTerminalEvent: "response.completed", LatencyBreakdown: FinalRequestLatencySnapshot(ctx)}, nil)
	require.Len(t, facts, 1)
	require.Equal(t, PerformanceSuccess, facts[0].Outcome)
	require.Equal(t, "public-model", facts[0].Model)
	require.Equal(t, "resp_native", facts[0].UsageRequestID)
	require.NotEqual(t, "resp_native", facts[0].RequestID)
	require.Equal(t, 2, facts[0].Latency.AttemptCount)
	require.Equal(t, "unknown", facts[0].ReasoningEffort, "missing effective effort must not inherit the request value")
	s.Close(nil)
	require.Len(t, facts, 1, "idle disconnect must not add a request")
	s.Next([]byte(`{"model":"second-model"}`), "", time.Now())
	s.SetOutcome(PerformanceFailure)
	s.ObserveError(errors.New("no upstream response"))
	s.Close(nil)
	s.Close(nil)
	require.Len(t, facts, 2)
	require.NotEqual(t, facts[0].RequestID, facts[1].RequestID)
	require.Empty(t, facts[1].UsageRequestID)
	require.Equal(t, PerformanceFailure, facts[1].Outcome)
	require.Equal(t, "second-model", facts[1].Model)
	require.Equal(t, "unknown", facts[1].ServiceTier)
}

func TestPerformanceWebSocketStructuredOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *OpenAIForwardResult
		err    error
		cancel bool
		want   ChannelPerformanceOutcome
	}{
		{name: "done alias", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.done"}, want: PerformanceSuccess},
		{name: "upstream failed", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.failed"}, want: PerformanceFailure},
		{name: "upstream cancelled without client intent", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.cancelled"}, want: PerformanceFailure},
		{name: "explicit client cancel", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.cancelled"}, cancel: true, want: PerformanceExcluded},
		{name: "provider quota", err: &UpstreamFailoverError{StatusCode: 402}, want: PerformanceFailure},
		{name: "user policy", err: NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "quota", nil), want: PerformanceExcluded},
		{name: "lease loss is infrastructure", err: ErrOpenAIWSIngressLeaseLost, want: PerformanceFailure},
		{name: "network interruption", err: io.ErrUnexpectedEOF, want: PerformanceFailure},
		{name: "delivery failed after upstream success", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.completed", performanceDeliveryFailed: true}, want: PerformanceFailure},
		{name: "drained after voluntary close", result: &OpenAIForwardResult{UpstreamTerminalEvent: "response.completed", performanceDeliveryFailed: true, performanceClientCancelled: true}, want: PerformanceExcluded},
		{name: "unobserved terminal", result: &OpenAIForwardResult{}, want: PerformanceUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var facts []ChannelPerformanceFact
			s := NewWebSocketPerformanceSession(1, 2, []byte(`{"model":"m"}`), time.Now(), func(f ChannelPerformanceFact) { facts = append(facts, f) })
			if tc.cancel {
				s.ClientCancelled()
			}
			s.ObserveResult(tc.result, tc.err)
			s.Close(nil)
			require.Len(t, facts, 1)
			require.Equal(t, tc.want, facts[0].Outcome)
		})
	}
}

func TestPerformanceWebSocketPayloadWitness(t *testing.T) {
	for _, payload := range []string{`{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"body"}`, `broken`} {
		require.Nil(t, websocketPerformanceResult([]byte(payload), nil, nil, nil))
	}
	result := websocketPerformanceResult([]byte(`{"type":"error","id":"event_not_response","error":{"code":"rate_limit_exceeded"}}`), nil, nil, nil)
	require.NotNil(t, result)
	require.Empty(t, result.RequestID, "event IDs are not billing IDs")
	result = websocketPerformanceResult([]byte(`{"type":"response.completed","response_id":"resp_real","id":"event_other"}`), nil, nil, nil)
	require.Equal(t, "resp_real", result.RequestID)
}

func TestPerformanceWebSocketConcurrencyOwnership(t *testing.T) {
	for _, userLimited := range []bool{false, true} {
		var facts []ChannelPerformanceFact
		s := NewWebSocketPerformanceSession(1, 2, []byte(`{"model":"m"}`), time.Now(), func(f ChannelPerformanceFact) { facts = append(facts, f) })
		s.SetOutcome(PerformanceFailure)
		want := PerformanceFailure
		if userLimited {
			s.SetOutcome(PerformanceExcluded)
			want = PerformanceExcluded
		}
		err := NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "busy", nil)
		s.ObserveResult(nil, err)
		s.ObserveError(err)
		s.Close(err)
		require.Len(t, facts, 1)
		require.Equal(t, want, facts[0].Outcome)
		s.Next([]byte(`{"model":"m"}`), "", time.Now())
		s.ObserveError(err)
		s.Close(nil)
		require.Equal(t, PerformanceFailure, facts[1].Outcome, "a later turn must not inherit client ownership")
	}
}

func TestPerformanceWebSocketInheritsOnlyModel(t *testing.T) {
	var facts []ChannelPerformanceFact
	s := NewWebSocketPerformanceSession(1, 2, []byte(`{"model":"first","service_tier":"priority"}`), time.Now(), func(f ChannelPerformanceFact) { facts = append(facts, f) })
	s.ObserveResult(&OpenAIForwardResult{UpstreamTerminalEvent: "response.completed"}, nil)
	s.Next([]byte(`{}`), "session-update-model", time.Now())
	s.Close(nil)
	require.Len(t, facts, 2)
	require.Equal(t, "session-update-model", facts[1].Model)
	require.Equal(t, "unknown", facts[1].ServiceTier)
	var disabled *WebSocketPerformanceSession
	disabled.Next(nil, "", time.Now())
	disabled.ObserveResult(nil, nil)
	disabled.Close(nil)
}

func attachWebSocketPerformanceTestHooks(ctx context.Context, payload []byte, hooks *OpenAIWSIngressHooks, facts chan ChannelPerformanceFact) (context.Context, *WebSocketPerformanceSession) {
	at := time.Now().Add(-time.Second)
	s := NewWebSocketPerformanceSession(7, 8, payload, at, func(f ChannelPerformanceFact) { facts <- f })
	hooks.RequestReceived = s.Next
	hooks.ClientCancelled = s.ClientCancelled
	hooks.PerformanceResult = s.ObserveResult
	return WithWebSocketPerformance(WithWebSocketFirstTurnReceived(ctx, at), s), s
}
