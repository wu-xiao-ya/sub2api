package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateSharedPurchaseQuotaBoundary(t *testing.T) {
	svc := &SubscriptionService{}
	for _, dimension := range []string{"lifetime", "daily", "weekly", "monthly"} {
		for _, tc := range []struct {
			name                    string
			quota, used, additional float64
			blocked                 bool
		}{
			{"remaining", 100, 99, 0, false},
			{"exactly exhausted", 100, 100, 0, true},
			{"already exceeded", 100, 101, 0, true},
			{"fills remaining quota", 100, 99, 1, false},
			{"exceeds remaining quota", 100, 99, 2, true},
			{"unlimited", 0, 101, 2, false},
		} {
			t.Run(dimension+"/"+tc.name, func(t *testing.T) {
				purchase := &SharedSubscriptionEntitlement{ExpiresAt: time.Now().Add(time.Hour)}
				wantErr := ErrMonthlyLimitExceeded
				switch dimension {
				case "lifetime":
					purchase.LifetimeQuotaUSD, purchase.LifetimeUsageUSD = tc.quota, tc.used
				case "daily":
					purchase.DailyQuotaUSD, purchase.DailyUsageUSD = tc.quota, tc.used
					wantErr = ErrDailyLimitExceeded
				case "weekly":
					purchase.WeeklyQuotaUSD, purchase.WeeklyUsageUSD = tc.quota, tc.used
					wantErr = ErrWeeklyLimitExceeded
				case "monthly":
					purchase.MonthlyQuotaUSD, purchase.MonthlyUsageUSD = tc.quota, tc.used
				}
				err := svc.ValidateSharedPurchase(purchase, tc.additional)
				if tc.blocked {
					require.ErrorIs(t, err, wantErr)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}
