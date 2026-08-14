package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFinanceLedgerQueryIncludesWholeEndDateInRequestedTimezone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/finance/ledger?start_date=2026-03-08&end_date=2026-03-08&timezone=America/New_York&page=2&page_size=50",
		nil,
	)

	query, startDate, endDate := financeLedgerQueryFromRequest(ctx)
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	require.Equal(t, "2026-03-08", startDate)
	require.Equal(t, "2026-03-08", endDate)
	require.Equal(t, time.Date(2026, time.March, 8, 0, 0, 0, 0, location), query.StartTime)
	require.Equal(t, time.Date(2026, time.March, 9, 0, 0, 0, 0, location), query.EndTime)
	require.Equal(t, 23*time.Hour, query.EndTime.Sub(query.StartTime))
	require.Equal(t, 2, query.Page)
	require.Equal(t, 50, query.PageSize)
}

func TestFinanceLedgerQueryNormalizesReverseDates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/finance/ledger?start_date=2026-08-14&end_date=2026-08-12&timezone=Asia/Shanghai",
		nil,
	)

	query, startDate, endDate := financeLedgerQueryFromRequest(ctx)

	require.Equal(t, "2026-08-12", startDate)
	require.Equal(t, "2026-08-14", endDate)
	require.Equal(t, 3*24*time.Hour, query.EndTime.Sub(query.StartTime))
}

func TestFinanceLedgerQueryParsesBlacklistUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/finance/ledger?exclude_users=42%2Cadmin%40example.com%3B42%0Aops",
		nil,
	)

	query, _, _ := financeLedgerQueryFromRequest(ctx)

	require.Equal(t, []string{"42", "admin@example.com", "42", "ops"}, query.ExcludeUsers)
}
