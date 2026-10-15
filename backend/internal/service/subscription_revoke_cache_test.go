//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRevokeSubscriptionFailClosedWithoutRepositoryCalls asserts the retired
// native revoke endpoint fails closed without touching any repository.
func TestRevokeSubscriptionFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	err := svc.RevokeSubscription(context.Background(), 1)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// TestRestoreSubscriptionFailClosedWithoutRepositoryCalls asserts the retired
// native restore endpoint fails closed without restoring any row.
func TestRestoreSubscriptionFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	restored, err := svc.RestoreSubscription(context.Background(), 1)
	require.Nil(t, restored)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}

// TestGetActiveSubscriptionFailClosedWithoutRepositoryCalls asserts the retired
// active-subscription lookup fails closed without repository reads.
func TestGetActiveSubscriptionFailClosedWithoutRepositoryCalls(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	sub, err := svc.GetActiveSubscription(context.Background(), 10, 20)
	require.Nil(t, sub)
	require.ErrorIs(t, err, ErrNativeSubscriptionRetired)
}
