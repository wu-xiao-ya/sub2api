//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
)

type openAIAccountRuntimeEventCapture struct {
	mu     sync.Mutex
	events []*logger.LogEvent
}

func (s *openAIAccountRuntimeEventCapture) WriteLogEvent(event *logger.LogEvent) {
	if event == nil {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *openAIAccountRuntimeEventCapture) snapshot() []*logger.LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*logger.LogEvent(nil), s.events...)
}

func captureOpenAIAccountRuntimeEvents(t *testing.T) *openAIAccountRuntimeEventCapture {
	t.Helper()
	capture := &openAIAccountRuntimeEventCapture{}
	logger.SetSink(capture)
	t.Cleanup(func() {
		logger.SetSink(nil)
	})
	return capture
}

func TestWriteOpenAIAccountRuntimeEventIncludesCorrelationAndAccountFields(t *testing.T) {
	capture := captureOpenAIAccountRuntimeEvents(t)
	groupID := int64(77)
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "req-local")
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, "client-req")

	writeOpenAIAccountRuntimeEvent(ctx, "account_model_cooldown_entered", &Account{
		ID:          81,
		Name:        "plus-gateway-2",
		Platform:    PlatformOpenAI,
		PoolGroupID: &groupID,
	}, "gpt-5.6-terra", map[string]any{
		"reason":               "upstream_transient",
		"upstream_status_code": 503,
	})

	events := capture.snapshot()
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, openAIAccountRuntimeLogComponent, event.Component)
	require.Equal(t, "account_model_cooldown_entered", event.Message)
	require.Equal(t, int64(81), event.Fields["account_id"])
	require.Equal(t, "plus-gateway-2", event.Fields["account_name"])
	require.Equal(t, "gpt-5.6-terra", event.Fields["model"])
	require.Equal(t, groupID, event.Fields["group_id"])
	require.Equal(t, "req-local", event.Fields["request_id"])
	require.Equal(t, "client-req", event.Fields["client_request_id"])
	require.Equal(t, 503, event.Fields["upstream_status_code"])
}

func TestOpenAIAccountRuntimeBlockLogsOnlyStateChanges(t *testing.T) {
	capture := captureOpenAIAccountRuntimeEvents(t)
	svc := &OpenAIGatewayService{}
	account := &Account{
		ID:       80,
		Name:     "plus-gateway-1",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}
	firstUntil := time.Now().Add(time.Minute)
	extendedUntil := firstUntil.Add(time.Minute)

	svc.BlockAccountScheduling(account, firstUntil, "transport_error")
	svc.BlockAccountScheduling(account, firstUntil.Add(-time.Second), "shorter_noop")
	svc.BlockAccountScheduling(account, extendedUntil, "transport_error")
	svc.ClearAccountSchedulingBlock(account.ID)

	events := capture.snapshot()
	require.Len(t, events, 3)
	require.Equal(t, "account_runtime_block_entered", events[0].Message)
	require.Equal(t, "account_runtime_block_extended", events[1].Message)
	require.Equal(t, "account_runtime_block_cleared", events[2].Message)
	require.Equal(t, "explicit_clear", events[2].Fields["reason"])
}

func TestOpenAIAccountNoCandidatesEventIsThrottled(t *testing.T) {
	capture := captureOpenAIAccountRuntimeEvents(t)
	svc := &OpenAIGatewayService{}
	groupID := int64(9)
	reasons := map[string]int{"account_runtime_cooldown": 2}

	svc.logOpenAIAccountSelectionNoCandidates(context.Background(), &groupID, PlatformOpenAI, "gpt-5.6-terra", map[int64]struct{}{80: {}, 81: {}}, reasons)
	svc.logOpenAIAccountSelectionNoCandidates(context.Background(), &groupID, PlatformOpenAI, "gpt-5.6-terra", map[int64]struct{}{80: {}, 81: {}}, reasons)

	events := capture.snapshot()
	require.Len(t, events, 1)
	require.Equal(t, "account_selection_no_candidates", events[0].Message)
	require.Equal(t, []int64{80, 81}, events[0].Fields["excluded_account_ids"])
	require.Equal(t, reasons, events[0].Fields["exclude_reasons"])
}
