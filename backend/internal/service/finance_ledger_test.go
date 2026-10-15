package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildFinanceLedgerWhereUsesExactUserIDAndFuzzyIdentitySearch(t *testing.T) {
	start := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	where, args := buildFinanceLedgerWhere(FinanceLedgerQuery{
		StartTime:   start,
		EndTime:     end,
		Source:      "payment",
		Direction:   "income",
		PaymentType: "alipay",
		User:        "12345",
		Keyword:     "ORDER-42",
	})

	require.Contains(t, where, "user_id::text = $6")
	require.Contains(t, where, "user_email ILIKE $7")
	require.Contains(t, where, "user_name ILIKE $7")
	require.Contains(t, where, "reference ILIKE $8")
	require.Contains(t, where, "notes ILIKE $8")
	require.Equal(t, []any{
		start,
		end,
		"payment",
		"income",
		"alipay",
		"12345",
		"%12345%",
		"%ORDER-42%",
	}, args)
}

func TestBuildFinanceLedgerWhereExcludesExactUsers(t *testing.T) {
	start := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	where, args := buildFinanceLedgerWhere(FinanceLedgerQuery{
		StartTime:    start,
		EndTime:      end,
		ExcludeUsers: []string{"42", "admin@example.com", "42"},
	})

	require.Contains(t, where, "NOT (user_id::text = $3")
	require.Contains(t, where, "LOWER(COALESCE(user_email, '')) = LOWER($3)")
	require.Contains(t, where, "NOT (user_id::text = $4")
	require.Equal(t, []any{start, end, "42", "admin@example.com", "42"}, args)
}

func TestFinanceLedgerRefundsOnlyUseRecordedBalanceDeduction(t *testing.T) {
	require.Contains(t, financeLedgerCTE, "FROM payment_audit_logs pal")
	require.Contains(t, financeLedgerCTE, "pal.action = 'REFUND_SUCCESS'")
	require.Contains(t, financeLedgerCTE, "balanceDeducted")
	require.Contains(t, financeLedgerCTE, "-refund.balance_deducted AS amount")
	require.NotContains(t, financeLedgerCTE, "-po.refund_amount::double precision AS amount")
}

func TestNormalizeFinanceLedgerQueryBoundsPagination(t *testing.T) {
	got := normalizeFinanceLedgerQuery(FinanceLedgerQuery{
		Page:         0,
		PageSize:     maxPageSize + 1,
		User:         "  user@example.com  ",
		ExcludeUsers: []string{" Admin@example.com ", "admin@example.com", ""},
	})

	require.Equal(t, 1, got.Page)
	require.Equal(t, maxPageSize, got.PageSize)
	require.Equal(t, "user@example.com", got.User)
	require.Equal(t, []string{"Admin@example.com"}, got.ExcludeUsers)
	require.True(t, strings.HasPrefix(got.User, "user"))
}
