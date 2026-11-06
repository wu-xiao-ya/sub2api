//go:build integration

package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAggregateBillingDeduplicatesBothKeysAndChargesWalletOnce(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("aggregate-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash", Balance: 100})
	t.Cleanup(func() { cleanupAggregateFixtures(t, []int64{user.ID}, nil) })
	root := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "aggregate", Quota: 10})
	member := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "member", Quota: 1})
	repo := NewUsageBillingRepository(client, integrationDB)
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: root.ID, UserID: user.ID, BalanceCost: 1.25, APIKeyQuotaCost: 1.25, APIKeyRateLimitCost: 1.25, RoutedMemberKeyID: member.ID, RoutedMemberQuotaCost: 1.25, RoutedMemberRateLimitCost: 1.25}
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.RoutedMemberQuotaExhausted)
	result, err = repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.Equal(t, 98.75, balance)
	for _, id := range []int64{root.ID, member.ID} {
		var quota, h, d, w float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used, usage_5h, usage_1d, usage_7d FROM api_keys WHERE id=$1", id).Scan(&quota, &h, &d, &w))
		require.Equal(t, 1.25, quota)
		require.Equal(t, 1.25, h)
		require.Equal(t, 1.25, d)
		require.Equal(t, 1.25, w)
	}
	// A late member-counter failure must roll back wallet, root and dedup.
	failed := *cmd
	failed.RequestID = uuid.NewString()
	failed.RequestFingerprint = ""
	failed.RoutedMemberQuotaCost = 1e30
	_, err = repo.Apply(ctx, &failed)
	require.Error(t, err)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.Equal(t, 98.75, balance)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", failed.RequestID, root.ID).Scan(&count))
	require.Zero(t, count)
	for _, id := range []int64{root.ID, member.ID} {
		var used float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id=$1", id).Scan(&used))
		require.Equal(t, 1.25, used)
	}
	changed := *cmd
	changed.RequestFingerprint = ""
	changed.RoutedMemberKeyID = root.ID
	_, err = repo.Apply(ctx, &changed)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestAggregateMembershipWritesPreserveOrderAndRejectForeignKeysAtomically(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("aggregate-meta-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash"})
	other := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("aggregate-other-%d@example.com", time.Now().UnixNano()), PasswordHash: "hash"})
	var groupIDs []int64
	t.Cleanup(func() { cleanupAggregateFixtures(t, []int64{user.ID, other.ID}, groupIDs) })
	group := mustCreateGroup(t, client, &service.Group{Name: "aggregate-" + uuid.NewString(), Platform: service.PlatformOpenAI})
	group2 := mustCreateGroup(t, client, &service.Group{Name: "aggregate-" + uuid.NewString(), Platform: service.PlatformAnthropic})
	groupIDs = []int64{group.ID, group2.ID}
	a := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &group.ID, Key: "sk-" + uuid.NewString(), Name: "a"})
	b := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &group2.ID, Key: "sk-" + uuid.NewString(), Name: "b"})
	foreign := mustCreateApiKey(t, client, &service.APIKey{UserID: other.ID, GroupID: &group.ID, Key: "sk-" + uuid.NewString(), Name: "foreign"})
	repo := NewAPIKeyRepository(client, integrationDB).(*apiKeyRepository)
	root := &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "root", Status: service.StatusActive, KeyKind: service.APIKeyKindAggregate}
	require.NoError(t, repo.CreateAggregate(ctx, root, []int64{b.ID, a.ID}))
	got, err := repo.GetByID(ctx, root.ID)
	require.NoError(t, err)
	require.True(t, got.IsAggregate())
	require.Equal(t, []int64{b.ID, a.ID}, got.MemberIDs())
	require.Equal(t, root.Key, got.Key)
	got.Name = "must roll back"
	err = repo.UpdateAggregate(ctx, got, service.APIKeyUpdateFields{Name: true}, []int64{foreign.ID})
	require.ErrorIs(t, err, service.ErrAggregateMemberNotOwned)
	got, err = repo.GetByID(ctx, root.ID)
	require.NoError(t, err)
	require.Equal(t, "root", got.Name)
	require.Equal(t, []int64{b.ID, a.ID}, got.MemberIDs())
	require.NoError(t, repo.UpdateAggregate(ctx, got, service.APIKeyUpdateFields{}, []int64{a.ID, b.ID}))
	got, err = repo.GetByID(ctx, root.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{a.ID, b.ID}, got.MemberIDs())
}

// The integration harness is shared with suites that assert exact platform
// catalogs, so these fixtures must not survive into another repository test.
func cleanupAggregateFixtures(t *testing.T, userIDs, groupIDs []int64) {
	t.Helper()
	_, err := integrationDB.Exec("DELETE FROM usage_billing_dedup WHERE api_key_id IN (SELECT id FROM api_keys WHERE user_id = ANY($1))", pq.Array(userIDs))
	require.NoError(t, err)
	_, err = integrationDB.Exec("DELETE FROM api_keys WHERE user_id = ANY($1)", pq.Array(userIDs))
	require.NoError(t, err)
	if len(groupIDs) > 0 {
		_, err = integrationDB.Exec("DELETE FROM groups WHERE id = ANY($1)", pq.Array(groupIDs))
		require.NoError(t, err)
	}
	_, err = integrationDB.Exec("DELETE FROM users WHERE id = ANY($1)", pq.Array(userIDs))
	require.NoError(t, err)
}
