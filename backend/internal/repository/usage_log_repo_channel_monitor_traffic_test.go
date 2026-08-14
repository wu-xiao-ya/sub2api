package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListRecentChannelMonitorTrafficUsesAccountGroupMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := &usageLogRepository{sql: db}
	now := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"account_id", "model", "duration_ms", "first_token_ms", "created_at"}).
		AddRow(int64(82), "gpt-5.6-sol", 1200, 480, now)
	mock.ExpectQuery(
		`(?s)SELECT ul\.account_id.*FROM usage_logs ul.*EXISTS \(.*FROM account_groups ag.*ag\.account_id = ul\.account_id.*ag\.group_id = \$1.*ul\.actual_cost > 0`,
	).
		WithArgs(int64(14), sqlmock.AnyArg(), sqlmock.AnyArg(), now.Add(-time.Hour), 100).
		WillReturnRows(rows)

	samples, err := repo.ListRecentChannelMonitorTraffic(
		context.Background(),
		14,
		[]int64{82, 83},
		[]string{"gpt-5.6-sol"},
		now.Add(-time.Hour),
		100,
	)
	require.NoError(t, err)
	require.Len(t, samples, 1)
	require.Equal(t, int64(82), samples[0].AccountID)
	require.Equal(t, 1200, samples[0].DurationMs)
	require.Equal(t, 480, samples[0].FirstTokenMs)
	require.NoError(t, mock.ExpectationsWereMet())
}
