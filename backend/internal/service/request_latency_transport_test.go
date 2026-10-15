package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLatencyWebSocketDoesNotInventHandshakeMilestone(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	tracker := ctx.Value(requestLatencyKey{}).(*requestLatency)
	tracker.start = time.Now().Add(-time.Second)
	attempt := BeginRequestLatencyAttempt(ctx)
	attempt.ObserveProtocolEvent(`{"type":"response.created"}`, "")
	attempt.ObserveProtocolEvent(`{"type":"response.reasoning_text.delta","delta":"reasoning"}`, "")
	first := RequestLatencySnapshot(ctx, nil)
	require.Nil(t, first.FirstResponseMs)
	require.NotNil(t, first.FirstEventMs)
	require.NotNil(t, first.FirstOutputMs)
	require.Nil(t, first.FirstCharacterMs)
	require.GreaterOrEqual(t, *first.FirstOutputMs, 1000)
	attempt.ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"visible"}`, "")
	require.NotNil(t, FinalRequestLatencySnapshot(ctx).FirstCharacterMs)
	retry := BeginRequestLatencyAttempt(ctx)
	// A late callback from the first transport cannot overwrite the retry.
	attempt.ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"late"}`, "")
	latest := RequestLatencySnapshot(ctx, nil)
	require.Equal(t, 2, latest.AttemptCount)
	require.Nil(t, latest.FirstCharacterMs)
	retry.ObserveProtocolEvent(`{"type":"response.output_text.delta","delta":"retry"}`, "")
	require.GreaterOrEqual(t, *FinalRequestLatencySnapshot(ctx).FirstCharacterMs, 1000)
}

func TestLatencyDecodedBedrockEventUsesExistingHTTPAttempt(t *testing.T) {
	ctx := WithRequestLatency(context.Background())
	httpAttempt := BeginRequestLatencyAttempt(ctx)
	httpAttempt.ResponseHeadersReceived()
	decoded := CurrentRequestLatencyAttempt(ctx)
	decoded.ObserveProtocolEvent(`{"type":"message_start","message":{"role":"assistant"}}`, "")
	decoded.ObserveProtocolEvent(`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"thinking"}}`, "")
	snapshot := FinalRequestLatencySnapshot(ctx)
	require.Equal(t, 1, snapshot.AttemptCount)
	require.NotNil(t, snapshot.FirstResponseMs)
	require.NotNil(t, snapshot.FirstEventMs)
	require.NotNil(t, snapshot.FirstOutputMs)
	require.Nil(t, snapshot.FirstCharacterMs)
}
