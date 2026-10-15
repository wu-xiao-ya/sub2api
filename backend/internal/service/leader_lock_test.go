package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeLeaderLockCache is an in-memory LeaderLockCache for unit tests. It models the
// compare-and-delete release semantics of the real Redis-backed implementation.
type fakeLeaderLockCache struct {
	mu         sync.Mutex
	owners     map[string]string
	acquireErr error
}

func (f *fakeLeaderLockCache) TryAcquireLeaderLock(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	if f.acquireErr != nil {
		return false, f.acquireErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owners == nil {
		f.owners = map[string]string{}
	}
	if _, held := f.owners[key]; held {
		return false, nil
	}
	f.owners[key] = owner
	return true, nil
}

func (f *fakeLeaderLockCache) ReleaseLeaderLock(_ context.Context, key, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.owners[key] == owner {
		delete(f.owners, key)
	}
	return nil
}

func (f *fakeLeaderLockCache) heldBy(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owners[key]
}

func TestTryAcquireSingletonLeaderLock_NoBackendRunsUngated(t *testing.T) {
	release, ok := tryAcquireSingletonLeaderLock(context.Background(), nil, nil, "k", "inst", time.Minute)
	require.True(t, ok)
	require.NotNil(t, release)
	require.NotPanics(t, release)
}

func TestTryAcquireSingletonLeaderLock_ContendedThenReleased(t *testing.T) {
	cache := &fakeLeaderLockCache{}
	ctx := context.Background()
	const key = "leader:test:contended"

	releaseA, ok := tryAcquireSingletonLeaderLock(ctx, cache, nil, key, "A", time.Minute)
	require.True(t, ok, "first instance should acquire")
	require.Equal(t, "A", cache.heldBy(key))

	_, okB := tryAcquireSingletonLeaderLock(ctx, cache, nil, key, "B", time.Minute)
	require.False(t, okB, "peer must be locked out while the lock is held")

	releaseA()
	require.Empty(t, cache.heldBy(key), "release must free the lock")

	releaseB, okB := tryAcquireSingletonLeaderLock(ctx, cache, nil, key, "B", time.Minute)
	require.True(t, okB, "peer should acquire after the holder releases")
	releaseB()
}

// When the cache errors, the helper must fall through rather than acquire via the
// cache. With no DB configured it runs ungated so the job is never starved by a
// flaky Redis.
func TestTryAcquireSingletonLeaderLock_CacheErrorFallsThrough(t *testing.T) {
	cache := &fakeLeaderLockCache{acquireErr: context.DeadlineExceeded}
	release, ok := tryAcquireSingletonLeaderLock(context.Background(), cache, nil, "k", "inst", time.Minute)
	require.True(t, ok, "cache error with no DB must run ungated, not skip")
	require.NotNil(t, release)
	require.NotPanics(t, release)
}

// The subscription-expiry reminder worker is retired and inert: sendExpiryReminders
// never scans or mutates user_subscriptions and never sends native reminder emails,
// regardless of leader-lock state. These subtests pin that the reminder path stays
// decoupled from both the repository and the lock.
func TestSubscriptionExpiryService_ReminderInertRegardlessOfLeadership(t *testing.T) {
	setup := func(cache LeaderLockCache) (*subscriptionExpiryRepoStub, *SubscriptionExpiryService) {
		repo := &subscriptionExpiryRepoStub{}
		svc := NewSubscriptionExpiryService(repo, time.Minute)
		svc.SetLeaderLock(cache, nil)
		return repo, svc
	}

	t.Run("leader", func(t *testing.T) {
		repo, svc := setup(&fakeLeaderLockCache{})
		svc.sendExpiryReminders(context.Background())
		require.Zero(t, repo.listCalls, "inert reminder path must not scan active subscriptions")
		require.Zero(t, repo.batchUpdateCalls)
	})
	t.Run("non_leader", func(t *testing.T) {
		cache := &fakeLeaderLockCache{}
		_, _ = cache.TryAcquireLeaderLock(context.Background(), subscriptionExpiryReminderLeaderLockKey, "peer", time.Minute)
		repo, svc := setup(cache)
		svc.sendExpiryReminders(context.Background())
		require.Zero(t, repo.listCalls, "inert reminder path must not scan active subscriptions")
		require.Zero(t, repo.batchUpdateCalls)
	})
	t.Run("no_backend", func(t *testing.T) {
		repo, svc := setup(nil)
		svc.sendExpiryReminders(context.Background())
		require.Zero(t, repo.listCalls, "inert reminder path must not scan active subscriptions")
		require.Zero(t, repo.batchUpdateCalls)
	})
}
