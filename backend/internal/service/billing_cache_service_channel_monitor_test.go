package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/stretchr/testify/require"
)

func TestCheckBillingEligibilitySkipsTrustedChannelMonitorRequests(t *testing.T) {
	svc := &BillingCacheService{
		cfg: &config.Config{},
	}
	ctx := context.WithValue(context.Background(), ctxkey.UsageSource, usagesource.ChannelMonitor)

	err := svc.CheckBillingEligibility(ctx, &User{ID: 79}, nil, nil, nil, "")
	require.NoError(t, err)
}
