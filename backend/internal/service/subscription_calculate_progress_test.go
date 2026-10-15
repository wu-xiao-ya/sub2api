package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ptrFloat64 / ptrTime are shared test helpers (ptrTime is also used by other
// package test files), so they stay here as small no-op utilities.
func ptrFloat64(v float64) *float64  { return &v }
func ptrTime(t time.Time) *time.Time { return &t }

// TestGetSubscriptionProgressFailClosedWithoutRepositoryCalls asserts the retired
// progress endpoint fails closed without reaching any repository.
func TestGetSubscriptionProgressFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	progress, err := svc.GetSubscriptionProgress(context.Background(), 42)
	require.Nil(t, progress)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// TestGetUserSubscriptionsWithProgressFailClosedWithoutRepositoryCalls asserts
// the retired per-user progress list endpoint fails closed without repository I/O.
func TestGetUserSubscriptionsWithProgressFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	progress, err := svc.GetUserSubscriptionsWithProgress(context.Background(), 42)
	require.Nil(t, progress)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}
