package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const performanceTierKey = "performance_service_tier"
const performanceEffortKey = "performance_reasoning_effort"

func (h *AvailableChannelHandler) PerformanceRecorder() *service.ChannelPerformanceService {
	if h == nil {
		return nil
	}
	return h.performanceService
}

func setPerformanceDimensions(c *gin.Context, body []byte) {
	// Extract only scalar dimensions; never retain the request or its contents.
	for key, path := range map[string][]string{
		performanceTierKey:   {"service_tier"},
		performanceEffortKey: {"reasoning.effort", "reasoning_effort", "generationConfig.thinkingConfig.thinkingLevel", "output_config.effort"},
	} {
		for _, p := range path {
			value := gjson.GetBytes(body, p)
			if value.Type == gjson.String && len(value.Str) <= 64 && strings.TrimSpace(value.Str) != "" {
				c.Set(key, strings.TrimSpace(value.Str))
				break
			}
		}
	}
}

func collectPerformanceFact(c *gin.Context, recorder *service.ChannelPerformanceService, w *opsCaptureWriter, started time.Time) {
	if recorder == nil || c.Request == nil || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") || isCountTokensRequest(c) || opsUsageSourceFromRequest(c) == usagesource.ChannelMonitor {
		return
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.GroupID == nil || *key.GroupID <= 0 {
		return
	}
	model := c.GetString(opsModelKey)
	if model == "" {
		return
	}
	ctx := c.Request.Context()
	requestID := ""
	if id, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(id) != "" {
		requestID = "client:" + strings.TrimSpace(id)
	}
	if requestID == "" {
		if id, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(id) != "" {
			requestID = "local:" + strings.TrimSpace(id)
		}
	}
	if requestID == "" {
		return
	}
	stream := c.GetBool(opsStreamKey)
	status := w.Status()
	outcome := service.PerformanceUnknown
	if stream || strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		outcome = w.performanceWitness.Outcome()
		if outcome == service.PerformanceUnknown && !w.performanceWitness.Incomplete() {
			outcome = service.PerformanceFailure
		}
	} else {
		outcome = w.performanceWitness.NonStreamOutcome(status, w.Header().Get("Content-Type"))
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		outcome = service.PerformanceExcluded
	case w.performanceWriteFailed:
		outcome = service.PerformanceFailure
	case status >= 400:
		switch {
		case service.HasOpsClientBusinessLimited(c):
			outcome = service.PerformanceExcluded
		case isOpsRoutingCapacityLimited(c), hasOpsUpstreamErrorContext(c):
			outcome = service.PerformanceFailure
		case status >= 500:
			outcome = service.PerformanceFailure
		case c.GetInt64(opsAccountIDKey) == 0 && (status == 400 || status == 401 || status == 403 || status == 404 || status == 422):
			outcome = service.PerformanceExcluded
		default:
			outcome = service.PerformanceUnknown
		}
	}
	recorder.RecordFact(service.ChannelPerformanceFact{
		APIKeyID: key.ID, RequestID: requestID, GroupID: *key.GroupID, Model: model,
		ServiceTier: c.GetString(performanceTierKey), ReasoningEffort: c.GetString(performanceEffortKey), Stream: &stream,
		Outcome: outcome, StartedAt: started, CompletedAt: time.Now(), Latency: service.FinalRequestLatencySnapshot(ctx),
	})
}
