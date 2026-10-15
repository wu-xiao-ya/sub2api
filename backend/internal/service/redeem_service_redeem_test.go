package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type redeemRejectRepo struct {
	code      RedeemCode
	useCalled bool
}

func (r *redeemRejectRepo) Create(ctx context.Context, code *RedeemCode) error {
	panic("unexpected Create call")
}

func (r *redeemRejectRepo) CreateBatch(ctx context.Context, codes []RedeemCode) error {
	panic("unexpected CreateBatch call")
}

func (r *redeemRejectRepo) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	if r.code.ID != id {
		return nil, ErrRedeemCodeNotFound
	}
	clone := r.code
	return &clone, nil
}

func (r *redeemRejectRepo) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	if r.code.Code != code {
		return nil, ErrRedeemCodeNotFound
	}
	clone := r.code
	return &clone, nil
}

func (r *redeemRejectRepo) Update(ctx context.Context, code *RedeemCode) error {
	panic("unexpected Update call")
}

func (r *redeemRejectRepo) BatchUpdate(ctx context.Context, ids []int64, fields RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected BatchUpdate call")
}

func (r *redeemRejectRepo) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}

func (r *redeemRejectRepo) Use(ctx context.Context, id, userID int64) error {
	r.useCalled = true
	r.code.Status = StatusUsed
	r.code.UsedBy = &userID
	return nil
}

func (r *redeemRejectRepo) List(ctx context.Context, params pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *redeemRejectRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, codeType, status, search string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (r *redeemRejectRepo) ListByUser(ctx context.Context, userID int64, limit int) ([]RedeemCode, error) {
	panic("unexpected ListByUser call")
}

func (r *redeemRejectRepo) ListByUserPaginated(ctx context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserPaginated call")
}

func (r *redeemRejectRepo) SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error) {
	panic("unexpected SumPositiveBalanceByUser call")
}

func TestRedeemRejectsInvitationCodeBeforeTransaction(t *testing.T) {
	ctx := context.Background()
	redeemRepo := &redeemRejectRepo{
		code: RedeemCode{
			ID:     1,
			Code:   "INVITE-001",
			Type:   RedeemTypeInvitation,
			Status: StatusUnused,
		},
	}
	redeemService := NewRedeemService(redeemRepo, nil, nil, nil, nil, nil, nil, nil)

	got, err := redeemService.Redeem(ctx, 2, redeemRepo.code.Code)

	require.Nil(t, got)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "REDEEM_CODE_UNSUPPORTED_TYPE", infraerrors.Reason(err))
	require.Equal(t, "invitation codes can only be used during registration", infraerrors.Message(err))
	require.False(t, redeemRepo.useCalled)
	require.Equal(t, StatusUnused, redeemRepo.code.Status)
	require.Nil(t, redeemRepo.code.UsedBy)
}

// TestGrantRedeemCodePurchaseRejectsNegativeValidityFailsClosed verifies that a
// subscription redeem code with negative validity is rejected before any
// database statement runs. Entitlement is granted through the immutable
// subscription_purchase snapshot (GrantRedeemCodePurchase) — the legacy
// reduceOrCancelSubscription path that mutated native user_subscriptions rows
// has been removed, and negative validity must fail closed here instead.
func TestGrantRedeemCodePurchaseRejectsNegativeValidityFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	drv := entsql.OpenDB(dialect.SQLite, db)
	entClient := dbent.NewClient(dbent.Driver(drv))
	svc := &SubscriptionService{entClient: entClient}

	negative := RedeemCode{ID: 7, Code: "SUB-NEG", Type: RedeemTypeSubscription, ValidityDays: -3}
	got, err := svc.GrantRedeemCodePurchase(context.Background(), 42, &negative)

	require.Nil(t, got)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "REDEEM_CODE_INVALID", infraerrors.Reason(err))
	require.Equal(t, "subscription redeem codes cannot reduce validity", infraerrors.Message(err))

	// Fail closed: no query or exec statement was issued, so neither a purchase
	// snapshot nor a native user_subscriptions row can have been written.
	require.NoError(t, mock.ExpectationsWereMet())
}
