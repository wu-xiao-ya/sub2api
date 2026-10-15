package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsCleanupPlan(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		days         int
		wantOK       bool
		wantTruncate bool
		wantCutoff   time.Time
	}{
		{name: "negative skips", days: -1, wantOK: false},
		{name: "zero truncates", days: 0, wantOK: true, wantTruncate: true},
		{name: "positive yields past cutoff", days: 7, wantOK: true, wantCutoff: now.AddDate(0, 0, -7)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cutoff, truncate, ok := opsCleanupPlan(now, tc.days)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if truncate != tc.wantTruncate {
				t.Fatalf("truncate = %v, want %v", truncate, tc.wantTruncate)
			}
			if !tc.wantTruncate && !cutoff.Equal(tc.wantCutoff) {
				t.Fatalf("cutoff = %v, want %v", cutoff, tc.wantCutoff)
			}
		})
	}
}

func TestIsMissingRelationError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil is not missing", err: nil, want: false},
		{name: "match relation does not exist", err: fakeErr(`pq: relation "ops_error_logs" does not exist`), want: true},
		{name: "match case-insensitive", err: fakeErr(`ERROR: Relation "x" Does Not Exist`), want: true},
		{name: "non-matching error", err: fakeErr("connection refused"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingRelationError(tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeleteOldSystemLogsByComponentUsesScopedBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cutoff := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	query := `(?s)WITH batch AS \(\s*SELECT id FROM ops_system_logs\s*WHERE created_at < \$1 AND component = \$2\s*ORDER BY id\s*LIMIT \$3\s*\)\s*DELETE FROM ops_system_logs`
	mock.ExpectExec(query).
		WithArgs(cutoff, openAIAccountRuntimeLogComponent, opsCleanupBatchSize).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(query).
		WithArgs(cutoff, openAIAccountRuntimeLogComponent, opsCleanupBatchSize).
		WillReturnResult(sqlmock.NewResult(0, 0))

	deleted, err := deleteOldSystemLogsByComponent(
		context.Background(),
		db,
		cutoff,
		openAIAccountRuntimeLogComponent,
		opsCleanupBatchSize,
	)

	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsCleanupDeletedCountsIncludesAccountRuntimeLogs(t *testing.T) {
	counts := opsCleanupDeletedCounts{accountRuntimeLogs: 7}
	require.Contains(t, counts.String(), "account_runtime_logs=7")
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
