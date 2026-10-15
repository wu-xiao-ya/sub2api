//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/stretchr/testify/require"
)

func TestFillDashboardMonitorUsageStatsReadsInternalMonitorCostEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	mock.ExpectQuery("channel_monitor_cost_events").
		WithArgs(start, end, usagesource.ChannelMonitor).
		WillReturnRows(sqlmock.NewRows([]string{
			"monitor_requests", "monitor_actual_cost", "monitor_account_cost",
		}).AddRow(int64(6), 0.0456, 0.1234))

	repo := newUsageLogRepositoryWithSQL(nil, db)
	stats := &DashboardStats{}
	err = repo.fillDashboardMonitorUsageStats(context.Background(), stats, start, end)

	require.NoError(t, err)
	require.Equal(t, int64(6), stats.TodayMonitorRequests)
	require.InDelta(t, 0.0456, stats.TodayMonitorActualCost, 1e-12)
	require.InDelta(t, 0.1234, stats.TodayMonitorAccountCost, 1e-12)
	require.NoError(t, mock.ExpectationsWereMet())
}
