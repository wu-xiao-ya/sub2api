package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FinanceLedgerQuery controls the administrator-facing balance ledger query.
// The date range is a half-open interval: [StartTime, EndTime).
type FinanceLedgerQuery struct {
	StartTime    time.Time
	EndTime      time.Time
	Page         int
	PageSize     int
	User         string
	ExcludeUsers []string
	Source       string
	Direction    string
	PaymentType  string
	Keyword      string
}

// FinanceLedgerEntry is one historical balance movement from an existing
// payment, redeem, administrator adjustment, or affiliate transfer record.
type FinanceLedgerEntry struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	OccurredAt  time.Time `json:"occurred_at"`
	UserID      int64     `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	UserName    string    `json:"user_name"`
	Amount      float64   `json:"amount"`
	Direction   string    `json:"direction"`
	Reference   string    `json:"reference"`
	PaymentType string    `json:"payment_type,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	Status      string    `json:"status"`
}

// FinanceLedgerSummary is calculated for the whole filtered set.
type FinanceLedgerSummary struct {
	Income    float64 `json:"income"`
	Deduction float64 `json:"deduction"`
	Net       float64 `json:"net"`
	Count     int64   `json:"count"`
}

type FinanceLedgerResult struct {
	Items    []FinanceLedgerEntry `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Summary  FinanceLedgerSummary `json:"summary"`
}

const financeLedgerCTE = `
WITH ledger AS (
    SELECT
        'payment'::text AS source,
        'payment:' || po.id::text AS id,
        po.completed_at AS occurred_at,
        po.user_id,
        po.user_email,
        po.user_name,
        po.amount::double precision AS amount,
        'income'::text AS direction,
        po.out_trade_no AS reference,
        po.payment_type,
        ''::text AS notes,
        po.status
    FROM payment_orders po
    WHERE po.order_type = 'balance'
      AND po.completed_at IS NOT NULL
      AND po.status IN ('COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED')

    UNION ALL

    SELECT
        'refund'::text AS source,
        'refund:' || pal.id::text AS id,
        pal.created_at AS occurred_at,
        po.user_id,
        po.user_email,
        po.user_name,
        -refund.balance_deducted AS amount,
        'deduction'::text AS direction,
        po.out_trade_no AS reference,
        po.payment_type,
        COALESCE(po.refund_reason, '') AS notes,
        po.status
    FROM payment_audit_logs pal
    JOIN payment_orders po ON po.id::text = pal.order_id
    CROSS JOIN LATERAL (
        SELECT (
            regexp_match(
                pal.detail,
                '"balanceDeducted"[[:space:]]*:[[:space:]]*(-?[0-9]+(\.[0-9]+)?)'
            )
        )[1]::double precision AS balance_deducted
    ) AS refund
    WHERE pal.action = 'REFUND_SUCCESS'
      AND po.order_type = 'balance'
      AND refund.balance_deducted > 0

    UNION ALL

    SELECT
        CASE WHEN rc.type = 'admin_balance' THEN 'admin_adjustment' ELSE 'redeem' END AS source,
        'redeem:' || rc.id::text AS id,
        rc.used_at AS occurred_at,
        u.id AS user_id,
        u.email AS user_email,
        u.username AS user_name,
        rc.value::double precision AS amount,
        CASE WHEN rc.value >= 0 THEN 'income' ELSE 'deduction' END AS direction,
        rc.code AS reference,
        ''::text AS payment_type,
        COALESCE(rc.notes, '') AS notes,
        rc.status
    FROM redeem_codes rc
    JOIN users u ON u.id = rc.used_by
    WHERE rc.status = 'used'
      AND rc.used_at IS NOT NULL
      AND rc.type IN ('balance', 'admin_balance')
      AND rc.value <> 0

    UNION ALL

    SELECT
        'affiliate_transfer'::text AS source,
        'affiliate:' || ual.id::text AS id,
        ual.created_at AS occurred_at,
        u.id AS user_id,
        u.email AS user_email,
        u.username AS user_name,
        ual.amount::double precision AS amount,
        CASE WHEN ual.amount >= 0 THEN 'income' ELSE 'deduction' END AS direction,
        'AFF-' || ual.id::text AS reference,
        ''::text AS payment_type,
        'affiliate rebate transfer'::text AS notes,
        ual.action AS status
    FROM user_affiliate_ledger ual
    JOIN users u ON u.id = ual.user_id
    WHERE ual.action = 'transfer'
      AND ual.amount <> 0
)
`

func (s *PaymentService) ListFinanceLedger(ctx context.Context, query FinanceLedgerQuery) (*FinanceLedgerResult, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("finance ledger service is unavailable")
	}
	query = normalizeFinanceLedgerQuery(query)
	where, args := buildFinanceLedgerWhere(query)

	summarySQL := financeLedgerCTE + `
SELECT
    COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0)::double precision AS income,
    COALESCE(SUM(CASE WHEN amount < 0 THEN -amount ELSE 0 END), 0)::double precision AS deduction,
    COALESCE(SUM(amount), 0)::double precision AS net,
    COUNT(*) AS count
FROM ledger` + where
	summaryRows, err := s.entClient.QueryContext(ctx, summarySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query finance ledger summary: %w", err)
	}
	defer func() { _ = summaryRows.Close() }()
	var summary FinanceLedgerSummary
	if !summaryRows.Next() {
		return nil, fmt.Errorf("query finance ledger summary: missing result")
	}
	if err := summaryRows.Scan(
		&summary.Income,
		&summary.Deduction,
		&summary.Net,
		&summary.Count,
	); err != nil {
		return nil, fmt.Errorf("query finance ledger summary: %w", err)
	}
	if err := summaryRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finance ledger summary: %w", err)
	}

	listArgs := append(append([]any{}, args...), query.PageSize, (query.Page-1)*query.PageSize)
	listSQL := financeLedgerCTE + `
SELECT id, source, occurred_at, user_id, user_email, user_name, amount,
       direction, reference, payment_type, notes, status
FROM ledger` + where + `
ORDER BY occurred_at DESC, id DESC
LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	rows, err := s.entClient.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("query finance ledger entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items, err := scanFinanceLedgerRows(rows)
	if err != nil {
		return nil, err
	}
	return &FinanceLedgerResult{
		Items:    items,
		Total:    summary.Count,
		Page:     query.Page,
		PageSize: query.PageSize,
		Summary:  summary,
	}, nil
}

// StreamFinanceLedger invokes visit for every result matching the same filter.
func (s *PaymentService) StreamFinanceLedger(ctx context.Context, query FinanceLedgerQuery, visit func(FinanceLedgerEntry) error) error {
	if s == nil || s.entClient == nil {
		return fmt.Errorf("finance ledger service is unavailable")
	}
	if visit == nil {
		return fmt.Errorf("finance ledger visitor is required")
	}
	query = normalizeFinanceLedgerQuery(query)
	where, args := buildFinanceLedgerWhere(query)
	rows, err := s.entClient.QueryContext(ctx, financeLedgerCTE+`
SELECT id, source, occurred_at, user_id, user_email, user_name, amount,
       direction, reference, payment_type, notes, status
FROM ledger`+where+`
ORDER BY occurred_at DESC, id DESC`, args...)
	if err != nil {
		return fmt.Errorf("query finance ledger export: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		entry, err := scanFinanceLedgerRow(rows)
		if err != nil {
			return err
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate finance ledger export: %w", err)
	}
	return nil
}

func normalizeFinanceLedgerQuery(query FinanceLedgerQuery) FinanceLedgerQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = defaultPageSize
	}
	if query.PageSize > maxPageSize {
		query.PageSize = maxPageSize
	}
	query.User = strings.TrimSpace(query.User)
	query.ExcludeUsers = normalizeFinanceLedgerExcludedUsers(query.ExcludeUsers)
	query.Source = strings.TrimSpace(query.Source)
	query.Direction = strings.TrimSpace(query.Direction)
	query.PaymentType = strings.TrimSpace(query.PaymentType)
	query.Keyword = strings.TrimSpace(query.Keyword)
	return query
}

func normalizeFinanceLedgerExcludedUsers(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func buildFinanceLedgerWhere(query FinanceLedgerQuery) (string, []any) {
	args := make([]any, 0, 7+len(query.ExcludeUsers))
	conditions := make([]string, 0, 7)
	add := func(prefix string, value any) {
		args = append(args, value)
		conditions = append(conditions, prefix+fmt.Sprint(len(args)))
	}
	add("occurred_at >= $", query.StartTime)
	add("occurred_at < $", query.EndTime)
	if query.Source != "" {
		add("source = $", query.Source)
	}
	if query.Direction != "" {
		add("direction = $", query.Direction)
	}
	if query.PaymentType != "" {
		add("payment_type = $", query.PaymentType)
	}
	if query.User != "" {
		args = append(args, query.User)
		idArg := len(args)
		args = append(args, "%"+query.User+"%")
		likeArg := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(user_id::text = $%d OR user_email ILIKE $%d OR user_name ILIKE $%d)",
			idArg, likeArg, likeArg,
		))
	}
	for _, excluded := range query.ExcludeUsers {
		args = append(args, excluded)
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"NOT (user_id::text = $%d OR LOWER(COALESCE(user_email, '')) = LOWER($%d) OR LOWER(COALESCE(user_name, '')) = LOWER($%d))",
			n, n, n,
		))
	}
	if query.Keyword != "" {
		args = append(args, "%"+query.Keyword+"%")
		n := len(args)
		conditions = append(conditions, fmt.Sprintf(
			"(reference ILIKE $%d OR notes ILIKE $%d)", n, n,
		))
	}
	return "\nWHERE " + strings.Join(conditions, "\n  AND "), args
}

func scanFinanceLedgerRows(rows *sql.Rows) ([]FinanceLedgerEntry, error) {
	items := make([]FinanceLedgerEntry, 0)
	for rows.Next() {
		entry, err := scanFinanceLedgerRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finance ledger entries: %w", err)
	}
	return items, nil
}

func scanFinanceLedgerRow(rows *sql.Rows) (FinanceLedgerEntry, error) {
	var entry FinanceLedgerEntry
	if err := rows.Scan(
		&entry.ID,
		&entry.Source,
		&entry.OccurredAt,
		&entry.UserID,
		&entry.UserEmail,
		&entry.UserName,
		&entry.Amount,
		&entry.Direction,
		&entry.Reference,
		&entry.PaymentType,
		&entry.Notes,
		&entry.Status,
	); err != nil {
		return FinanceLedgerEntry{}, fmt.Errorf("scan finance ledger entry: %w", err)
	}
	return entry, nil
}
