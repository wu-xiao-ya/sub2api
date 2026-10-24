package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLatencyProtocolMilestones(t *testing.T) {
	for _, tc := range []struct {
		name, data, event      string
		valid, output, visible bool
	}{
		{"heartbeat", "{}", "ping", false, false, false},
		{"role", `{"choices":[{"delta":{"role":"assistant"}}]}`, "", true, false, false},
		{"whitespace", `{"choices":[{"delta":{"content":" "}}]}`, "", true, false, false},
		{"reasoning", `{"choices":[{"delta":{"reasoning_content":"think"}}]}`, "", true, true, false},
		{"tool", `{"choices":[{"delta":{"tool_calls":[{"function":{"name":"search"}}]}}]}`, "", true, true, false},
		{"refusal", `{"delta":"cannot"}`, "response.refusal.delta", true, true, true},
		{"claude think", `{"type":"content_block_delta","delta":{"thinking":"think"}}`, "", true, true, false},
		{"claude text", `{"type":"content_block_delta","delta":{"text":"hello"}}`, "", true, true, true},
		{"claude signature", `{"type":"content_block_delta","delta":{"signature":"abc"}}`, "", true, false, false},
		{"gemini thought", `{"candidates":[{"content":{"parts":[{"text":"think","thought":true}]}}]}`, "", true, true, false},
		{"antigravity text", `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}}`, "", true, true, true},
		{"gemini image", `{"candidates":[{"content":{"parts":[{"inlineData":{"data":"base64"}}]}}]}`, "", true, true, false},
		{"status", `{"type":"response.in_progress"}`, "", true, false, false},
		{"invalid", "not json", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, o, c := classifyLatencyPayload(tc.data, tc.event)
			require.Equal(t, tc.valid, v)
			require.Equal(t, tc.output, o)
			require.Equal(t, tc.visible, c)
		})
	}
}

func TestLatencyRetriesAndTransparentBody(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	tracker := ctx.Value(requestLatencyKey{}).(*requestLatency)
	tracker.start = time.Now().Add(-time.Second)
	old := BeginRequestLatencyAttempt(ctx)
	old.ObserveResponse(&http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error"))})
	current := BeginRequestLatencyAttempt(ctx)
	body := "event: response.output_text.delta\r\ndata: {\"delta\":\"hello\"}\r\n\r\ndata: [DONE]\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	current.ObserveResponse(resp)
	var received strings.Builder
	buffer := make([]byte, 3)
	for {
		n, e := resp.Body.Read(buffer)
		received.Write(buffer[:n])
		if e != nil {
			require.Equal(t, io.EOF, e)
			break
		}
	}
	require.NoError(t, resp.Body.Close())
	require.Equal(t, body, received.String())
	snapshot := RequestLatencySnapshot(ctx, nil)
	require.Equal(t, 2, snapshot.Version)
	require.Equal(t, 2, snapshot.AttemptCount)
	require.GreaterOrEqual(t, *snapshot.FirstResponseMs, 1000)
	require.NotNil(t, snapshot.FirstEventMs)
	require.NotNil(t, snapshot.FirstOutputMs)
	require.NotNil(t, snapshot.FirstCharacterMs)
	require.NotNil(t, snapshot.TotalDurationMs)
	require.Equal(t, snapshot, UsageLatencyBreakdownFromMap(snapshot.Map()))
	frozen := WithFrozenRequestLatency(context.Background(), snapshot)
	BeginRequestLatencyAttempt(ctx)
	require.Equal(t, snapshot, RequestLatencySnapshot(frozen, nil))
}

func TestLatencyNonStreamingDoesNotInventEvents(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"hello"}}]}`))}
	BeginRequestLatencyAttempt(ctx).ObserveResponse(resp)
	_, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	b := RequestLatencySnapshot(ctx, nil)
	require.Nil(t, b.FirstEventMs)
	require.NotNil(t, b.FirstCharacterMs)
	require.Nil(t, BeginRequestLatencyAttempt(context.Background()))
}

func TestLatencyBoundedObservation(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	payload := strings.Repeat("x", latencyEventLimit*2)
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}
	BeginRequestLatencyAttempt(ctx).ObserveResponse(resp)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
	require.Nil(t, RequestLatencySnapshot(ctx, nil).FirstCharacterMs)
}

func TestLatencyHeaderTimestampPrecedesBodyInspection(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	attempt := BeginRequestLatencyAttempt(ctx)
	attempt.ResponseHeadersReceived()
	header := *RequestLatencySnapshot(ctx, nil).FirstResponseMs
	ctx.Value(requestLatencyKey{}).(*requestLatency).start = time.Now().Add(-time.Second)
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}
	attempt.ObserveResponse(resp)
	require.Equal(t, header, *RequestLatencySnapshot(ctx, nil).FirstResponseMs)
	require.NoError(t, resp.Body.Close())
}

func TestLatencyCohortsNeverMixForwardAndIngressOrigins(t *testing.T) {
	old, newValue := 10, 1000
	result := medianTrafficLatencyBreakdown([]ChannelMonitorTrafficSample{
		{LatencyBreakdown: &UsageLatencyBreakdown{FirstCharacterMs: &old}},
		{LatencyBreakdown: &UsageLatencyBreakdown{FirstCharacterMs: &old}},
		{LatencyBreakdown: &UsageLatencyBreakdown{Version: 2, FirstCharacterMs: &newValue}},
	})
	require.Equal(t, 2, result.Version)
	require.Equal(t, 1000, *result.FirstCharacterMs)
}

func TestLatencyWebSocketTurnsUseIndependentIngressClocks(t *testing.T) {
	received := time.Now().Add(-2 * time.Second)
	session := WithWebSocketFirstTurnReceived(context.Background(), received)
	first := firstWebSocketTurnLatencyContext(session)
	require.Nil(t, CurrentRequestLatencyAttempt(session), "session must not own a mutable turn tracker")
	BeginRequestLatencyAttempt(first).ObserveProtocolEvent(`{"type":"response.in_progress"}`, "")
	retry := BeginRequestLatencyAttempt(first)
	retry.ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"hello"}`, "")
	one := FinalRequestLatencySnapshot(first)
	require.Equal(t, 2, one.AttemptCount)
	require.GreaterOrEqual(t, *one.FirstCharacterMs, 2000)
	require.Nil(t, one.FirstResponseMs, "reused WebSocket must not invent HTTP response headers")
	frozen := WithFrozenRequestLatency(context.Background(), one)
	nextReceived := time.Now().Add(-100 * time.Millisecond)
	second := withRequestLatencyAt(session, nextReceived)
	BeginRequestLatencyAttempt(second).ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"next"}`, "")
	two := FinalRequestLatencySnapshot(second)
	require.Equal(t, nextReceived, second.Value(requestLatencyKey{}).(*requestLatency).start)
	require.Equal(t, 1, two.AttemptCount)
	require.NotSame(t, first.Value(requestLatencyKey{}), second.Value(requestLatencyKey{}))
	require.Equal(t, one, RequestLatencySnapshot(frozen, nil), "later turns cannot mutate queued billing snapshots")
	require.Equal(t, 2, RequestLatencySnapshot(first, nil).AttemptCount)
	// Re-entering the proxy after first-turn account failover is still the same
	// logical turn, while a second client frame remains entirely independent.
	accountRetry := firstWebSocketTurnLatencyContext(session)
	BeginRequestLatencyAttempt(accountRetry)
	require.Equal(t, 3, RequestLatencySnapshot(accountRetry, nil).AttemptCount)
	require.Equal(t, received, accountRetry.Value(requestLatencyKey{}).(*requestLatency).start)
	require.Equal(t, 1, RequestLatencySnapshot(second, nil).AttemptCount)
	require.Equal(t, one, RequestLatencySnapshot(frozen, nil))
}
