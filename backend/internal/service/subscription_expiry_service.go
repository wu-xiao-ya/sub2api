package service

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const (
	// subscriptionExpiryReminderLeaderLockKey is retained for compatibility with
	// the pre-retirement reminder scan. The reminder worker is now inert, so the
	// lock is never acquired; the key is kept only so existing references and
	// tests continue to compile.
	subscriptionExpiryReminderLeaderLockKey = "subscription:expiry:reminder:leader"
)

// SubscriptionExpiryService is the retired native subscription expiry/reminder
// worker.
//
// user_subscriptions is frozen read-only: the application no longer mutates
// expired subscription status through this worker and no longer sends native
// subscription reminder emails. The service is absent from the Wire provider
// set (never constructed/started/stopped) and every worker path below is a
// deliberate no-op kept only so legacy call sites and tests keep compiling.
// Do not re-wire it without a purchase-backed replacement.
type SubscriptionExpiryService struct {
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewSubscriptionExpiryService retains its historical signature for wiring
// compatibility. The userSubRepo argument is deliberately NOT retained: the
// retired worker must never read from or write to user_subscriptions.
func NewSubscriptionExpiryService(userSubRepo UserSubscriptionRepository, interval time.Duration) *SubscriptionExpiryService {
	return &SubscriptionExpiryService{
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// SetLeaderLock is retained for compatibility. The retired worker never
// acquires the reminder leader lock, so the arguments are intentionally
// ignored.
func (s *SubscriptionExpiryService) SetLeaderLock(_ LeaderLockCache, _ *sql.DB) {}

// SetSettingRepository is retained for compatibility. The retired worker never
// reads the reminder switch, so the argument is intentionally ignored.
func (s *SubscriptionExpiryService) SetSettingRepository(_ SettingRepository) {}

// SetNotificationEmailService is retained for compatibility. The retired
// worker never sends native subscription reminders, so the argument is
// intentionally ignored.
func (s *SubscriptionExpiryService) SetNotificationEmailService(_ *NotificationEmailService) {}

// Start runs the retained-but-inert worker loop. Each cycle calls runOnce,
// which is a no-op, so the loop never reads or writes user_subscriptions and
// never sends reminder emails.
func (s *SubscriptionExpiryService) Start() {
	if s == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *SubscriptionExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runOnce is the retired per-cycle run path. It is a deliberate no-op: it never
// calls the repository, so it cannot read or mutate user_subscriptions.
func (s *SubscriptionExpiryService) runOnce() {}

// CheckExpiry is the retired expiry-status check path. It is a deliberate
// no-op: it returns nil and never calls the repository, so it cannot read or
// mutate user_subscriptions.
func (s *SubscriptionExpiryService) CheckExpiry(ctx context.Context) error {
	return nil
}

// sendExpiryReminders is the retired reminder path. It is a deliberate no-op:
// it never scans user_subscriptions and never sends native subscription
// reminder emails.
func (s *SubscriptionExpiryService) sendExpiryReminders(ctx context.Context) {}

// sendExpiryReminderIfDue is the retired per-subscription reminder path. It is
// a deliberate no-op: it never sends a native subscription reminder email.
func (s *SubscriptionExpiryService) sendExpiryReminderIfDue(ctx context.Context, sub *UserSubscription) {
}
