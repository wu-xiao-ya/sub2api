package repository

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRecordUpstreamBillingManualRateSnapshotWritesManualAndClearTimelineMarkers(t *testing.T) {
	now := time.Now().UTC()
	freshUntil := now.Add(time.Hour)
	tests := []struct {
		name       string
		account    *service.Account
		manualRate *float64
		wantRate   float64
		wantSource string
		wantGroups string
	}{
		{
			name: "manual override expands every group",
			account: &service.Account{
				ID:       42,
				Platform: service.PlatformAnthropic,
				Extra:    map[string]any{},
				GroupIDs: []int64{7, 3},
			},
			manualRate: float64Ptr(0.07),
			wantRate:   0.07,
			wantSource: service.UpstreamBillingRateSnapshotSourceManual,
			wantGroups: "{7,3}",
		},
		{
			name: "clearing with fresh probe falls back to probe rate",
			account: &service.Account{
				ID:       43,
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeAPIKey,
				Extra: map[string]any{
					service.UpstreamBillingProbeExtraKey: &service.UpstreamBillingProbeSnapshot{
						Status:     service.UpstreamBillingProbeStatusOK,
						ReceivedAt: &now,
						FreshUntil: &freshUntil,
						Data: map[string]any{
							"billing_scope":            "token",
							"resolved_rate_multiplier": 0.12,
							"peak_rate_enabled":        false,
						},
					},
				},
			},
			wantRate:   0.12,
			wantSource: service.UpstreamBillingRateSnapshotSourceManualClearFallback,
			wantGroups: "{0}",
		},
		{
			name: "clearing without a usable probe leaves explicit legacy marker",
			account: &service.Account{
				ID:       44,
				Platform: service.PlatformAnthropic,
				Extra:    map[string]any{},
			},
			wantRate:   0,
			wantSource: service.UpstreamBillingRateSnapshotSourceManualCleared,
			wantGroups: "{0}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &recordingSQLExecutor{result: rowsAffectedResult(1)}
			repo := newAccountRepositoryWithSQL(nil, executor, nil)

			err := repo.RecordUpstreamBillingManualRateSnapshot(context.Background(), tt.account, tt.manualRate)

			require.NoError(t, err)
			require.Len(t, executor.execQueries, 1)
			require.Contains(t, executor.execQueries[0], "INSERT INTO account_upstream_rate_snapshots")
			require.Contains(t, executor.execQueries[0], "FROM unnest($5::bigint[])")
			require.Len(t, executor.execArgs[0], 5)
			require.Equal(t, tt.account.ID, executor.execArgs[0][0])
			require.InDelta(t, tt.wantRate, executor.execArgs[0][1], 1e-12)
			require.Equal(t, tt.wantSource, executor.execArgs[0][3])
			groups, ok := executor.execArgs[0][4].(driver.Valuer)
			require.True(t, ok)
			value, err := groups.Value()
			require.NoError(t, err)
			require.Equal(t, tt.wantGroups, strings.TrimSpace(value.(string)))
		})
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}
