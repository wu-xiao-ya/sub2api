package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAccountReplacesAndClearsManualUpstreamBillingRate(t *testing.T) {
	const accountID int64 = 701
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformAnthropic,
			Type:     AccountTypeAPIKey,
			Status:   StatusActive,
			Extra: map[string]any{
				UpstreamBillingManualRateExtraKey: 0.07,
			},
		},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Extra: map[string]any{UpstreamBillingManualRateExtraKey: 0.12},
	})
	require.NoError(t, err)
	rate, ok := UpstreamBillingManualRate(updated.Extra)
	require.True(t, ok)
	require.InDelta(t, 0.12, rate, 1e-12)

	updated, err = svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Extra: map[string]any{"custom": "value"},
	})
	require.NoError(t, err)
	require.NotContains(t, updated.Extra, UpstreamBillingManualRateExtraKey)
	require.Equal(t, "value", updated.Extra["custom"])
}
