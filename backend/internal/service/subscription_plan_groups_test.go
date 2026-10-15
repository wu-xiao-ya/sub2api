package service

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

const subscriptionPlanGroupIDsQuery = "SELECT group_id FROM subscription_plan_groups WHERE plan_id = $1 ORDER BY group_id"

func newSubscriptionPlanGroupQueryClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
	})
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	return dbent.NewClient(dbent.Driver(driver)), mock
}

func TestListPlanGroupIDsWithClientReadsCompositeMapping(t *testing.T) {
	client, mock := newSubscriptionPlanGroupQueryClient(t)
	mock.ExpectQuery(regexp.QuoteMeta(subscriptionPlanGroupIDsQuery)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(int64(14)).AddRow(int64(20)))

	svc := &SubscriptionService{}
	got, err := svc.listPlanGroupIDsWithClient(context.Background(), client, 1, 13)

	require.NoError(t, err)
	require.Equal(t, []int64{13, 14, 20}, got)
}

func TestListPlanGroupIDsWithClientFallsBackToLegacyGroup(t *testing.T) {
	client, mock := newSubscriptionPlanGroupQueryClient(t)
	mock.ExpectQuery(regexp.QuoteMeta(subscriptionPlanGroupIDsQuery)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}))

	svc := &SubscriptionService{}
	got, err := svc.listPlanGroupIDsWithClient(context.Background(), client, 1, 13)

	require.NoError(t, err)
	require.Equal(t, []int64{13}, got)
}

func TestAdminListSubscriptionPlanGroupIDsReadsCompositeMapping(t *testing.T) {
	client, mock := newSubscriptionPlanGroupQueryClient(t)
	mock.ExpectQuery(regexp.QuoteMeta(subscriptionPlanGroupIDsQuery)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(int64(14)).AddRow(int64(20)))

	svc := &adminServiceImpl{entClient: client}
	plan := &dbent.SubscriptionPlan{ID: 1, GroupID: 13}
	got, err := svc.listSubscriptionPlanGroupIDs(context.Background(), plan)

	require.NoError(t, err)
	require.Equal(t, []int64{13, 14, 20}, got)
}
