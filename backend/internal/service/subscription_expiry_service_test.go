package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// subscriptionExpiryRepoStub implements UserSubscriptionRepository so
// NewSubscriptionExpiryService keeps compiling, and counts calls to the two
// methods the retired worker used to touch: List (scan) and
// BatchUpdateExpiredStatus (status mutation).
type subscriptionExpiryRepoStub struct {
	listCalls        int
	batchUpdateCalls int
}

func (r *subscriptionExpiryRepoStub) Create(context.Context, *UserSubscription) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) GetByID(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) Update(context.Context, *UserSubscription) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) Restore(context.Context, int64, string) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionExpiryRepoStub) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (r *subscriptionExpiryRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func (r *subscriptionExpiryRepoStub) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *subscriptionExpiryRepoStub) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	r.listCalls++
	return nil, &pagination.PaginationResult{Page: 1, Pages: 1}, nil
}

func (r *subscriptionExpiryRepoStub) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *subscriptionExpiryRepoStub) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (r *subscriptionExpiryRepoStub) ExtendExpiry(context.Context, int64, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) UpdateStatus(context.Context, int64, string) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) UpdateNotes(context.Context, int64, string) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ActivateWindows(context.Context, int64, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetWeeklyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) ResetMonthlyUsage(context.Context, int64, *time.Time, time.Time) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) IncrementUsage(context.Context, int64, float64) error {
	return nil
}

func (r *subscriptionExpiryRepoStub) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	r.batchUpdateCalls++
	return 0, nil
}

// TestSubscriptionExpiryService_RunPathDoesNotCallRepo proves the per-cycle
// run path (runOnce) never reads or writes user_subscriptions.
func TestSubscriptionExpiryService_RunPathDoesNotCallRepo(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{}
	svc := NewSubscriptionExpiryService(repo, time.Minute)

	svc.runOnce()

	require.Zero(t, repo.listCalls, "runOnce must not list user_subscriptions")
	require.Zero(t, repo.batchUpdateCalls, "runOnce must not batch-update expired subscriptions")
}

// TestSubscriptionExpiryService_CheckExpiryDoesNotCallRepo proves the retired
// expiry-status check path never reads or writes user_subscriptions.
func TestSubscriptionExpiryService_CheckExpiryDoesNotCallRepo(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{}
	svc := NewSubscriptionExpiryService(repo, time.Minute)

	require.NoError(t, svc.CheckExpiry(context.Background()))

	require.Zero(t, repo.listCalls, "CheckExpiry must not list user_subscriptions")
	require.Zero(t, repo.batchUpdateCalls, "CheckExpiry must not batch-update expired subscriptions")
}

// TestSubscriptionExpiryService_StartLoopDoesNotCallRepo proves the background
// Start loop (the "Run" worker) never touches user_subscriptions across several
// timer cycles.
func TestSubscriptionExpiryService_StartLoopDoesNotCallRepo(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{}
	svc := NewSubscriptionExpiryService(repo, time.Millisecond)

	svc.Start()
	time.Sleep(15 * time.Millisecond) // several ticks
	svc.Stop()

	require.Zero(t, repo.listCalls, "Start loop must not list user_subscriptions")
	require.Zero(t, repo.batchUpdateCalls, "Start loop must not batch-update expired subscriptions")
}

// TestSubscriptionExpiryService_ReminderPathDoesNotCallRepo proves the retired
// reminder path never scans user_subscriptions (the scan that used to feed
// native reminder emails) and never mutates expired subscription status.
func TestSubscriptionExpiryService_ReminderPathDoesNotCallRepo(t *testing.T) {
	repo := &subscriptionExpiryRepoStub{}
	svc := NewSubscriptionExpiryService(repo, time.Minute)

	svc.sendExpiryReminders(context.Background())
	svc.sendExpiryReminderIfDue(context.Background(), &UserSubscription{ID: 1})

	require.Zero(t, repo.listCalls, "reminder path must not list active subscriptions")
	require.Zero(t, repo.batchUpdateCalls, "reminder path must not mutate subscriptions")
}
