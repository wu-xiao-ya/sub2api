package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
)

func trustedChannelMonitorContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.UsageSource, usagesource.ChannelMonitor)
}

func TestMonitorOnlyAccountStaysHiddenFromNormalScheduling(t *testing.T) {
	account := &Account{
		Status:      StatusActive,
		Schedulable: false,
		Extra:       map[string]any{"monitor_only": true},
	}

	if account.IsSchedulable() {
		t.Fatal("monitor-only account must remain unavailable to normal scheduling")
	}
	if account.IsSchedulableForModelWithContext(context.Background(), "test-model") {
		t.Fatal("normal model scheduling must not allow monitor-only account")
	}
}

func TestMonitorOnlyAccountIsAvailableToTrustedChannelMonitor(t *testing.T) {
	account := &Account{
		Status:      StatusActive,
		Schedulable: false,
		Extra:       map[string]any{"monitor_only": true},
	}

	if !account.IsSchedulableForContext(trustedChannelMonitorContext()) {
		t.Fatal("trusted channel-monitor context should allow monitor-only account")
	}
	if !account.IsSchedulableForModelWithContext(trustedChannelMonitorContext(), "test-model") {
		t.Fatal("trusted channel-monitor model scheduling should allow monitor-only account")
	}
}
