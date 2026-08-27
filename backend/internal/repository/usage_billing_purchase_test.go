package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestIncrementUsageBillingSubscriptionPurchaseUsesExplicitPurchaseID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectExec("UPDATE subscription_purchases").
		WithArgs(1.25, int64(101), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, incrementUsageBillingSubscriptionPurchase(context.Background(), tx, 101, 1.25))
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}
