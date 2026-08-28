package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
)

func usageSourceFromContext(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	source, ok := ctx.Value(ctxkey.UsageSource).(string)
	if !ok {
		return nil
	}
	normalized, ok := usagesource.Normalize(source)
	if !ok {
		return nil
	}
	return &normalized
}

// IsChannelMonitorRequest reports whether the request carries the trusted
// internal monitoring source marker. This is used to bypass customer billing
// while preserving normal gateway authentication and account routing.
func IsChannelMonitorRequest(ctx context.Context) bool {
	source := usageSourceFromContext(ctx)
	return source != nil && *source == usagesource.ChannelMonitor
}
