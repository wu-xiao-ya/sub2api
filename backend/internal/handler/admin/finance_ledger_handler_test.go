package admin

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFinanceCSVCellNeutralizesFormulas(t *testing.T) {
	for _, value := range []string{"=SUM(A1)", " +cmd", "\t@value", "-formula"} {
		require.Equal(t, "'"+value, financeCSVCell(value))
	}
	require.Equal(t, "ordinary", financeCSVCell("ordinary"))
	require.Equal(t, "", financeCSVCell(""))
}

func TestFinanceLedgerQueryRejectsMalformedFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{
		"start_date=not-a-date",
		"start_date=2026-10-26&end_date=2026-10-01",
		"source=contribution", "direction=unexpected",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		_, err := financeLedgerQuery(c)
		require.Error(t, err, query)
	}
}

func TestFinanceLedgerDatesUseCalendarDaysAcrossDST(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET",
		"/?start_date=2026-03-08&end_date=2026-03-08&timezone=America%2FNew_York&exclude_users=42,43", nil)
	query, err := financeLedgerQuery(c)
	require.NoError(t, err)
	require.Equal(t, float64(23), query.EndTime.Sub(query.StartTime).Hours())
	require.Equal(t, []string{"42", "43"}, query.ExcludeUsers)
}
