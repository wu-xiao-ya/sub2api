package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUsageRecordTaskFreezesLatencyBeforeWorkerStart(t *testing.T) {
	parent := service.WithRequestLatency(context.Background())
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}
	service.BeginRequestLatencyAttempt(parent).ObserveResponse(resp)
	require.NoError(t, resp.Body.Close())
	var observed *service.UsageLatencyBreakdown
	task := wrapUsageRecordTaskContext(parent, func(ctx context.Context) { observed = service.RequestLatencySnapshot(ctx, nil) })
	service.BeginRequestLatencyAttempt(parent)
	task(context.Background())
	require.NotNil(t, observed)
	require.Equal(t, 1, observed.AttemptCount)
	require.NotNil(t, observed.FirstResponseMs)
	require.NotNil(t, observed.TotalDurationMs)
}
