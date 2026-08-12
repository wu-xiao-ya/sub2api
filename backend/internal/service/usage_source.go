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
