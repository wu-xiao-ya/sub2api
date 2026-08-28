package service

import (
	"context"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"golang.org/x/sync/errgroup"
)

func (s *ChannelMonitorService) runInternalGatewayChecks(ctx context.Context, m *ChannelMonitor) []*CheckResult {
	key, err := s.resolveInternalMonitorKey(ctx, m)
	if err != nil {
		return []*CheckResult{newMonitorAccountProbeErrorResult(m, err.Error())}
	}
	endpoint := strings.TrimRight(strings.TrimSpace(s.internalGatewayURL), "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(strings.TrimSpace(m.Endpoint), "/")
	}
	if _, err := internalMonitorGatewayURL(endpoint); err != nil {
		return []*CheckResult{newMonitorAccountProbeErrorResult(m, err.Error())}
	}
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	results := make([]*CheckResult, len(models))
	opts := &CheckOptions{
		APIMode:          m.APIMode,
		LowCost:          true,
		RequestTimeout:   monitorRequestTimeoutFor(m),
		ExtraHeaders:     channelMonitorInternalHeaders(m.ExtraHeaders),
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
	}
	var group errgroup.Group
	var mu sync.Mutex
	for i, model := range models {
		i, model := i, model
		group.Go(func() error {
			result := runCheckForModel(ctx, m.Provider, endpoint, key.Key, model, opts)
			result.ProbeMode = "internal"
			result.CandidateCount = 1
			if result.Status == MonitorStatusOperational || result.Status == MonitorStatusDegraded {
				result.HealthyCount = 1
			}
			mu.Lock()
			results[i] = result
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()
	return results
}

func channelMonitorInternalHeaders(source map[string]string) map[string]string {
	headers := make(map[string]string, len(source)+2)
	for key, value := range source {
		headers[key] = value
	}
	return usagesource.MarkChannelMonitor(headers)
}
