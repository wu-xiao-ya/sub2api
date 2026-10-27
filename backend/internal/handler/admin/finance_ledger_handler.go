package admin

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func financeLedgerQuery(c *gin.Context) (service.FinanceLedgerQuery, error) {
	tz := c.Query("timezone")
	now := timezone.NowInUserLocation(tz)
	parse := func(key string, fallback time.Time) (time.Time, error) {
		raw := c.Query(key)
		if raw == "" {
			return timezone.StartOfDayInUserLocation(fallback, tz), nil
		}
		return timezone.ParseInUserLocation("2006-01-02", raw, tz)
	}
	start, err := parse("start_date", now.AddDate(0, 0, -1))
	if err != nil {
		return service.FinanceLedgerQuery{}, fmt.Errorf("invalid start_date")
	}
	end, err := parse("end_date", now)
	if err != nil || end.Before(start) {
		return service.FinanceLedgerQuery{}, fmt.Errorf("invalid end_date")
	}
	page, pageSize := response.ParsePagination(c)
	query := service.FinanceLedgerQuery{
		StartTime: start, EndTime: end.AddDate(0, 0, 1), Page: page, PageSize: pageSize,
		User: c.Query("user"), Keyword: c.Query("keyword"), PaymentType: c.Query("payment_type"),
		Source: c.Query("source"), Direction: c.Query("direction"),
	}
	if query.Source != "" && query.Source != "payment" && query.Source != "refund" &&
		query.Source != "redeem" && query.Source != "admin_adjustment" && query.Source != "affiliate_transfer" {
		return query, fmt.Errorf("invalid source")
	}
	if query.Direction != "" && query.Direction != "income" && query.Direction != "deduction" {
		return query, fmt.Errorf("invalid direction")
	}
	for _, raw := range c.QueryArray("exclude_users") {
		query.ExcludeUsers = append(query.ExcludeUsers, strings.FieldsFunc(raw, func(r rune) bool {
			return strings.ContainsRune(",;，； \r\n\t", r)
		})...)
	}
	return query, nil
}

func (h *PaymentHandler) ListFinanceLedger(c *gin.Context) {
	query, err := financeLedgerQuery(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.paymentService.ListFinanceLedger(c.Request.Context(), query)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"items": result.Items, "total": result.Total, "page": result.Page, "page_size": result.PageSize,
		"summary": result.Summary, "start_date": query.StartTime.Format("2006-01-02"),
		"end_date": query.EndTime.AddDate(0, 0, -1).Format("2006-01-02"),
	})
}

// Prefix dangerous spreadsheet cells without altering their stored ledger values.
func financeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func (h *PaymentHandler) ExportFinanceLedger(c *gin.Context) {
	query, err := financeLedgerQuery(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// Buffer only the first page/summary before committing response headers.
	// CSV rows themselves are streamed with the exact same filter.
	result, err := h.paymentService.ListFinanceLedger(c.Request.Context(), query)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=finance_ledger.csv")
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(c.Writer)
	write := func(row []string) error { return writer.Write(row) }
	for _, row := range [][]string{
		{"start_date", query.StartTime.Format(time.RFC3339)},
		{"end_exclusive", query.EndTime.Format(time.RFC3339)},
		{"balance_unit", "USD"},
		{"income", strconv.FormatFloat(result.Summary.Income, 'f', 8, 64)},
		{"deduction", strconv.FormatFloat(result.Summary.Deduction, 'f', 8, 64)},
		{"net", strconv.FormatFloat(result.Summary.Net, 'f', 8, 64)},
		{"record_count", strconv.FormatInt(result.Total, 10)},
		{},
		{"time", "user_id", "user_email", "user_name", "source", "amount", "direction", "reference", "payment_type", "notes", "status"},
	} {
		if err := write(row); err != nil {
			_ = c.Error(err)
			return
		}
	}
	err = h.paymentService.StreamFinanceLedger(c.Request.Context(), query, func(entry service.FinanceLedgerEntry) error {
		return write([]string{
			entry.OccurredAt.Format(time.RFC3339), strconv.FormatInt(entry.UserID, 10),
			financeCSVCell(entry.UserEmail), financeCSVCell(entry.UserName), entry.Source,
			strconv.FormatFloat(entry.Amount, 'f', 8, 64), entry.Direction,
			financeCSVCell(entry.Reference), financeCSVCell(entry.PaymentType),
			financeCSVCell(entry.Notes), financeCSVCell(entry.Status),
		})
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = c.Error(err)
	}
}
