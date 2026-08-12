package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/gin-gonic/gin"
)

var _ channelMonitorAccountProbeExecutor = (*AccountTestService)(nil)

// ProbeForChannelMonitor runs the existing account test request path without
// allowing the probe to persist account state, quota, rate-limit, or extra
// metadata. It translates the captured account-test SSE into the narrow
// result consumed by adaptive channel monitoring.
func (s *AccountTestService) ProbeForChannelMonitor(
	ctx context.Context,
	accountID int64,
	model string,
) (*AccountMonitorProbeResult, error) {
	if s == nil {
		return nil, errors.New("account test service is nil")
	}
	if s.accountRepo == nil {
		return nil, errors.New("account test repository is not configured")
	}
	if s.httpUpstream == nil {
		return nil, errors.New("account test HTTP upstream is not configured")
	}

	startedAt := time.Now()
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx = context.WithValue(ctx, ctxkey.UsageSource, usagesource.ChannelMonitor)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/internal/channel-monitor/account-probe", nil).
		WithContext(withAccountTestProbeOnly(ctx))

	testErr := s.TestAccountConnection(ginCtx, accountID, model, "", AccountTestModeDefault)
	success, message, usage := parseChannelMonitorAccountProbeOutput(recorder.Body.String(), testErr)

	return &AccountMonitorProbeResult{
		Success:          success,
		LatencyMs:        int(time.Since(startedAt).Milliseconds()),
		Message:          message,
		Usage:            usage,
		RequestAttempted: true,
	}, nil
}

func parseChannelMonitorAccountProbeOutput(body string, testErr error) (bool, string, monitorUsage) {
	var (
		completed bool
		success   bool
		errMsg    string
		usage     monitorUsage
	)

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !sseDataPrefix.MatchString(line) {
			continue
		}
		var event TestEvent
		if err := json.Unmarshal(sseDataPrefix.ReplaceAll([]byte(line), nil), &event); err != nil {
			continue
		}
		switch event.Type {
		case "error":
			if message := strings.TrimSpace(event.Error); message != "" {
				errMsg = message
			}
		case "usage":
			if observed := monitorUsageFromTestEventData(event.Data); observed.Observed {
				usage = observed
			}
		case "test_complete":
			completed = true
			success = event.Success
		}
	}

	if errMsg != "" {
		return false, errMsg, usage
	}
	if testErr != nil {
		return false, testErr.Error(), usage
	}
	if !completed {
		return false, "account test ended without a completion event", usage
	}
	if !success {
		return false, "account test completed without success", usage
	}
	return true, "account test completed", usage
}
