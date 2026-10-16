package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
	"github.com/stretchr/testify/require"
)

const leaderboardQueryPattern = `(?s)SUM\(ul\.input_tokens \+ ul\.output_tokens \+ ul\.cache_creation_tokens \+ ul\.cache_read_tokens\).*u\.deleted_at IS NULL.*u\.is_monitoring_user.*ul\.usage_source <> \$4.*ORDER BY total_tokens DESC, u\.id ASC`

func TestLeaderboardServiceResults(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	rows := sqlmock.NewRows([]string{"id", "email", "username", "tokens", "requests", "cost"}).
		AddRow(42, "first@example.test", nil, 1200, 4, 0.25).
		AddRow(43, "other@example.test", "  Public name  ", 900, 3, 0.15)
	mock.ExpectQuery(leaderboardQueryPattern).
		WithArgs(start, end, 20, usagesource.ChannelMonitor).
		WillReturnRows(rows).RowsWillBeClosed()
	entries, err := NewLeaderboardService(db).GetLeaderboard(context.Background(), start, end, 20, 42)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "f***t@example.test", entries[0].DisplayName)
	require.Equal(t, int64(1200), entries[0].TotalTokens)
	require.True(t, entries[0].IsCurrentUser)
	require.False(t, entries[1].IsCurrentUser)
	require.Equal(t, "Public name", entries[1].DisplayName)
	require.Equal(t, 2, entries[1].Rank)
	serialized, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "first@example.test")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLeaderboardServiceEmptyAndRowErrors(t *testing.T) {
	for _, kind := range []string{"empty", "row_error", "scan_error", "query_error"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			start := time.Unix(0, 0)
			end := start.Add(time.Hour)
			rows := sqlmock.NewRows([]string{"id", "email", "username", "tokens", "requests", "cost"})
			expected := mock.ExpectQuery(leaderboardQueryPattern)
			if kind == "query_error" {
				expected.WillReturnError(errors.New("database unavailable"))
			} else {
				if kind != "empty" {
					id := any(int64(42))
					if kind == "scan_error" {
						id = "not-an-id"
					}
					rows.AddRow(id, "a@example.test", nil, 1, 1, 0.1)
				}
				if kind == "row_error" {
					rows.RowError(0, errors.New("interrupted read"))
				}
				expected.WillReturnRows(rows).RowsWillBeClosed()
			}
			entries, err := NewLeaderboardService(db).GetLeaderboard(context.Background(), start, end, 20, 42)
			if kind == "empty" {
				require.NoError(t, err)
				require.NotNil(t, entries)
				require.Empty(t, entries)
			} else {
				require.Error(t, err)
				require.Nil(t, entries)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLeaderboardServiceInvalidArguments(t *testing.T) {
	_, err := (*LeaderboardService)(nil).GetLeaderboard(context.Background(), time.Time{}, time.Time{}, 20, 0)
	require.Error(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	for _, limit := range []int{0, 101} {
		_, err = NewLeaderboardService(db).GetLeaderboard(context.Background(), time.Unix(0, 0), time.Now(), limit, 0)
		require.Error(t, err)
	}
	_, err = NewLeaderboardService(db).GetLeaderboard(context.Background(), time.Now(), time.Unix(0, 0), 20, 0)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLeaderboardEmailMaskUnicode(t *testing.T) {
	for _, email := range []string{"", "invalid", "@example.test", "a@example.test", "\u7528\u6237@example.test"} {
		masked := maskLeaderboardEmail(email)
		require.True(t, utf8.ValidString(masked))
		require.Contains(t, masked, "***")
		require.False(t, strings.Contains(masked, "\ufffd"))
	}
}
