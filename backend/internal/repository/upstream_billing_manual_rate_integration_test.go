//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestManualUpstreamRateClearRemovesOnlyExplicitKey(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	fallback := 0.2
	account := mustCreateAccount(t, tx.Client(), &service.Account{
		Name: "manual-rate-clear", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, RateMultiplier: &fallback,
		Credentials: map[string]any{"api_key": "synthetic-only"},
		Extra: map[string]any{
			service.UpstreamBillingManualRateExtraKey:      0.07,
			service.UpstreamBillingProbeEnabledExtraKey:    true,
			service.UpstreamBillingRateSyncEnabledExtraKey: true,
			"unrelated_nullable_field":                     nil,
		},
	})
	require.NoError(t, tx.Client().Account.UpdateOneID(account.ID).SetRateMultiplier(fallback).Exec(ctx))
	loaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	automatic := 0.4
	snapshot := &service.UpstreamBillingProbeSnapshot{
		Status: service.UpstreamBillingProbeStatusOK, LastAttemptAt: time.Now().UTC(),
	}
	require.NoError(t, repo.UpdateUpstreamBillingProbeSnapshot(ctx, loaded, snapshot, &automatic))
	loaded, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, fallback, *loaded.RateMultiplier)
	require.InDelta(t, 0.07, loaded.BillingRateMultiplier(), 1e-12)
	require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{
		service.UpstreamBillingManualRateExtraKey: nil,
	}))
	loaded, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NotContains(t, loaded.Extra, service.UpstreamBillingManualRateExtraKey)
	require.Contains(t, loaded.Extra, "unrelated_nullable_field")
	require.Equal(t, fallback, loaded.BillingRateMultiplier())
	require.NoError(t, repo.UpdateUpstreamBillingProbeSnapshot(ctx, loaded, snapshot, &automatic))
	loaded, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, automatic, *loaded.RateMultiplier)

	require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{
		service.UpstreamBillingManualRateExtraKey: 0,
	}))
	affected, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{
		Extra: map[string]any{service.UpstreamBillingManualRateExtraKey: nil},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
	loaded, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NotContains(t, loaded.Extra, service.UpstreamBillingManualRateExtraKey)
	require.Contains(t, loaded.Extra, "unrelated_nullable_field")
	require.Equal(t, automatic, loaded.BillingRateMultiplier())
}
