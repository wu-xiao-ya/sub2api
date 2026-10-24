package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeUpstreamRatioSnapshotRepo struct {
	rows       []UpstreamModelRatio
	observedAt time.Time
	err        error

	replaced      int
	deletedBefore int
	lookups       int
}

func (f *fakeUpstreamRatioSnapshotRepo) ReplaceUpstreamModelRatioSnapshot(ctx context.Context, accountID int64, observedAt time.Time, rows []UpstreamModelRatio) error {
	f.replaced++
	return f.err
}

func (f *fakeUpstreamRatioSnapshotRepo) LatestUpstreamModelRatioRows(ctx context.Context, accountID int64, limit int) ([]UpstreamModelRatio, time.Time, error) {
	f.lookups++
	return f.rows, f.observedAt, f.err
}

func (f *fakeUpstreamRatioSnapshotRepo) ListUpstreamRatioSyncHistory(ctx context.Context, accountID int64, limit int) ([]UpstreamRatioHistoryEntry, error) {
	return nil, nil
}

func (f *fakeUpstreamRatioSnapshotRepo) DeleteUpstreamModelRatioSnapshotsBefore(ctx context.Context, before time.Time) (int64, error) {
	f.deletedBefore++
	return 0, nil
}

func newRatioSyncTestService(repo UpstreamRatioSnapshotRepo, now time.Time) *UpstreamRatioSyncService {
	svc := NewUpstreamRatioSyncService(nil, nil, nil, repo)
	svc.now = func() time.Time { return now }
	return svc
}

func TestUpstreamRatioSyncAccountHelpers(t *testing.T) {
	apikeyWithBase := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://new.x5m5x.com"}}
	require.True(t, isUpstreamRatioSyncAccount(apikeyWithBase))

	require.False(t, isUpstreamRatioSyncAccount(&Account{Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://x"}}))
	require.False(t, isUpstreamRatioSyncAccount(&Account{Type: AccountTypeAPIKey}))
	require.False(t, isUpstreamRatioSyncAccount(nil))

	enabledAccount := &Account{Extra: map[string]any{UpstreamRatioSyncEnabledExtraKey: true}}
	require.True(t, upstreamRatioSyncEnabled(enabledAccount))
	require.False(t, upstreamRatioSyncEnabled(&Account{}))

	groupAccount := &Account{Extra: map[string]any{UpstreamRatioSyncGroupExtraKey: "国模自营（1号主力池）"}}
	require.Equal(t, "国模自营（1号主力池）", upstreamRatioSyncGroup(groupAccount))
	require.Equal(t, "default", upstreamRatioSyncGroup(&Account{}))
}

func TestUpstreamRatioSyncSnapshotDecode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := &UpstreamRatioSyncSnapshot{
		Status:     UpstreamRatioSyncStatusOK,
		ModelCount: 40,
		Group:      "国模自营（1号主力池）",
		GroupRatio: ptrUpstreamRatio(0.03),
		ReceivedAt: &now,
		NextSyncAt: now.Add(30 * time.Minute),
	}
	extra := map[string]any{UpstreamRatioSyncExtraKey: snapshot}
	decoded := decodeUpstreamRatioSyncSnapshot(extra)
	require.NotNil(t, decoded)
	require.Equal(t, UpstreamRatioSyncStatusOK, decoded.Status)
	require.Equal(t, 40, decoded.ModelCount)
	require.NotNil(t, decoded.GroupRatio)
	require.InDelta(t, 0.03, *decoded.GroupRatio, 1e-9)

	require.Nil(t, decodeUpstreamRatioSyncSnapshot(map[string]any{UpstreamRatioSyncExtraKey: "bogus"}))
	require.Nil(t, decodeUpstreamRatioSyncSnapshot(nil))
}

func TestUpstreamRatioSyncSettingsDefaults(t *testing.T) {
	settings, err := (&SettingService{}).GetUpstreamRatioSyncSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, 30, settings.IntervalMinutes)
	require.Equal(t, int64(500000), settings.QuotaPerUnit)
	require.Equal(t, 90, settings.RetentionDays)
}

func TestUpstreamRatioCostLookup(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-5 * time.Minute)
	rows := []UpstreamModelRatio{
		{ModelName: "deepseek-v4.1-flash", QuotaType: 0, ModelRatio: 0.5, CompletionRatio: 2, GroupRatio: 0.03},
		{ModelName: "per-call-model", QuotaType: 1, ModelPrice: 0.02, GroupRatio: 0.5},
	}

	t.Run("fresh table yields cost", func(t *testing.T) {
		svc := newRatioSyncTestService(&fakeUpstreamRatioSnapshotRepo{rows: rows, observedAt: fresh}, now)
		cost := svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash",
			UsageTokens{InputTokens: 1_000_000, OutputTokens: 500_000}, 1)
		require.NotNil(t, cost)
		require.InDelta(t, 0.06, *cost, 1e-9)

		perCall := svc.UpstreamRatioCost(context.Background(), 222, "per-call-model", UsageTokens{}, 2)
		require.NotNil(t, perCall)
		require.InDelta(t, 0.02, *perCall, 1e-9)
	})

	t.Run("recent stale table remains usable during outage", func(t *testing.T) {
		svc := newRatioSyncTestService(&fakeUpstreamRatioSnapshotRepo{rows: rows, observedAt: now.Add(-48 * time.Hour)}, now)
		cost := svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash",
			UsageTokens{InputTokens: 1000}, 1)
		require.NotNil(t, cost)
	})

	t.Run("very old stale table falls through", func(t *testing.T) {
		svc := newRatioSyncTestService(&fakeUpstreamRatioSnapshotRepo{rows: rows, observedAt: now.Add(-8 * 24 * time.Hour)}, now)
		cost := svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash",
			UsageTokens{InputTokens: 1000}, 1)
		require.Nil(t, cost)
	})

	t.Run("no table negative-caches", func(t *testing.T) {
		repo := &fakeUpstreamRatioSnapshotRepo{}
		svc := newRatioSyncTestService(repo, now)
		cost := svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash", UsageTokens{InputTokens: 1000}, 1)
		require.Nil(t, cost)
		cost = svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash", UsageTokens{InputTokens: 1000}, 1)
		require.Nil(t, cost)
		require.Equal(t, 1, repo.callCount(), "second lookup must be served from the negative cache")
	})

	t.Run("unknown model returns nil", func(t *testing.T) {
		svc := newRatioSyncTestService(&fakeUpstreamRatioSnapshotRepo{rows: rows, observedAt: fresh}, now)
		require.Nil(t, svc.UpstreamRatioCost(context.Background(), 222, "unknown-model", UsageTokens{InputTokens: 1000}, 1))
	})

	t.Run("sync refresh installs cache", func(t *testing.T) {
		repo := &fakeUpstreamRatioSnapshotRepo{rows: rows, observedAt: now.Add(-48 * time.Hour)}
		svc := newRatioSyncTestService(repo, now)
		svc.refreshCache(222, rows, now.Add(-1*time.Minute), 30)
		cost := svc.UpstreamRatioCost(context.Background(), 222, "deepseek-v4.1-flash",
			UsageTokens{InputTokens: 1_000_000, OutputTokens: 500_000}, 1)
		require.NotNil(t, cost)
		require.InDelta(t, 0.06, *cost, 1e-9)
	})
}

func (f *fakeUpstreamRatioSnapshotRepo) callCount() int {
	return f.lookups
}

func TestUpstreamRatioSyncNilServiceSafety(t *testing.T) {
	var svc *UpstreamRatioSyncService
	require.Nil(t, svc.UpstreamRatioCost(context.Background(), 1, "m", UsageTokens{}, 1))
	var billing *BillingService
	require.Nil(t, billing.upstreamRatioSyncCost(1, "m", UsageTokens{}, 1))
}
