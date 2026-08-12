package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestApplyContributorRewardCreditsOwnerOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cmd := &service.UsageBillingCommand{
		RequestID:                   "req-contribution-1",
		APIKeyID:                    9,
		UserID:                      31,
		ContributorOwnerUserID:      17,
		ContributorRewardAccountID:  44,
		ContributorRewardGroupID:    5,
		ContributorRewardTotalCost:  0.12,
		ContributorRewardActualCost: 0.09,
		ContributorRewardMultiplier: 0.5,
		ContributorRewardAmount:     0.06,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO contributor_reward_logs`).
		WithArgs(
			cmd.RequestID,
			cmd.APIKeyID,
			cmd.ContributorOwnerUserID,
			cmd.UserID,
			cmd.ContributorRewardAccountID,
			cmd.ContributorRewardGroupID,
			cmd.ContributorRewardTotalCost,
			cmd.ContributorRewardActualCost,
			cmd.ContributorRewardMultiplier,
			cmd.ContributorRewardAmount,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectExec(`UPDATE users`).
		WithArgs(cmd.ContributorRewardAmount, cmd.ContributorOwnerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	applied, err := applyContributorReward(context.Background(), tx, cmd)
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyContributorRewardSkipsDuplicateRequest(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cmd := &service.UsageBillingCommand{
		RequestID:                   "req-contribution-duplicate",
		APIKeyID:                    9,
		UserID:                      31,
		ContributorOwnerUserID:      17,
		ContributorRewardAccountID:  44,
		ContributorRewardGroupID:    5,
		ContributorRewardTotalCost:  0.12,
		ContributorRewardActualCost: 0.09,
		ContributorRewardMultiplier: 0.5,
		ContributorRewardAmount:     0.06,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO contributor_reward_logs`).
		WithArgs(
			cmd.RequestID,
			cmd.APIKeyID,
			cmd.ContributorOwnerUserID,
			cmd.UserID,
			cmd.ContributorRewardAccountID,
			cmd.ContributorRewardGroupID,
			cmd.ContributorRewardTotalCost,
			cmd.ContributorRewardActualCost,
			cmd.ContributorRewardMultiplier,
			cmd.ContributorRewardAmount,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	applied, err := applyContributorReward(context.Background(), tx, cmd)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}
