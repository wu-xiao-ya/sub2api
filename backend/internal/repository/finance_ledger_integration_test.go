//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFinanceLedgerPaymentRedemptionRefundAndExactExclusion(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("finance-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive,
		Balance: 10, Concurrency: 5,
	})
	now := time.Now().UTC().Truncate(time.Second)
	code := fmt.Sprintf("FIN-%d", time.Now().UnixNano())
	createOrder := func(reference string, completed time.Time) int64 {
		order, err := client.PaymentOrder.Create().
			SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("Finance").
			SetAmount(100).SetPayAmount(700).SetRechargeCode(reference).
			SetOutTradeNo(reference).SetPaymentType("alipay").SetPaymentTradeNo("provider").
			SetClientIP("127.0.0.1").SetSrcHost("fixture.invalid").
			SetOrderType("balance").SetStatus("PARTIALLY_REFUNDED").
			SetExpiresAt(completed.Add(time.Hour)).SetCompletedAt(completed).Save(ctx)
		require.NoError(t, err)
		return order.ID
	}
	orderID := createOrder(code, now)
	_, err := client.RedeemCode.Create().SetCode(code).SetType("balance").
		SetValue(100).SetStatus("used").SetUsedBy(user.ID).SetUsedAt(now).Save(ctx)
	require.NoError(t, err)
	for index, detail := range []string{`{"balanceDeducted":25}`, `{"balanceDeducted":1e-8}`, `invalid audit detail`} {
		refundOrderID := orderID
		if index > 0 {
			refundOrderID = createOrder(fmt.Sprintf("%s-refund-%d", code, index), now.Add(-48*time.Hour))
		}
		_, err := client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(refundOrderID, 10)).
			SetAction("REFUND_SUCCESS").SetDetail(detail).SetCreatedAt(now).Save(ctx)
		require.NoError(t, err)
	}
	for index, amount := range []float64{2, -3} {
		_, err := client.RedeemCode.Create().SetCode(fmt.Sprintf("%s-%d", code, index)).
			SetType("admin_balance").SetValue(amount).SetStatus("used").
			SetUsedBy(user.ID).SetUsedAt(now).Save(ctx)
		require.NoError(t, err)
	}
	svc := service.NewPaymentService(client, nil, nil, nil, nil, nil, nil, nil, nil)
	query := service.FinanceLedgerQuery{StartTime: now.Add(-time.Minute), EndTime: now.Add(time.Minute),
		User: strconv.FormatInt(user.ID, 10)}
	result, err := svc.ListFinanceLedger(ctx, query)
	require.NoError(t, err)
	require.EqualValues(t, 5, result.Total)
	require.InDelta(t, 102, result.Summary.Income, 1e-10)
	require.InDelta(t, 28.00000001, result.Summary.Deduction, 1e-10)
	count := 0
	require.NoError(t, svc.StreamFinanceLedger(ctx, query, func(entry service.FinanceLedgerEntry) error {
		count++
		require.Equal(t, user.ID, entry.UserID)
		return nil
	}))
	require.Equal(t, len(result.Items), count)
	query.ExcludeUsers = []string{user.Email}
	excluded, err := svc.ListFinanceLedger(ctx, query)
	require.NoError(t, err)
	require.Zero(t, excluded.Total)
	require.Zero(t, excluded.Summary.Net)
}
