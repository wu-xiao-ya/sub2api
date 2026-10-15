package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// FindReconcileCandidates finds usage_logs rows for the given account and time window
// that may need cost reconciliation against upstream panel logs.
func (r *usageLogRepository) FindReconcileCandidates(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
	limit int,
) ([]service.UsageLogReconcileCandidate, error) {
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}

	query := `
		SELECT 
			id, account_id, created_at, model, 
			COALESCE(requested_model, '') AS requested_model,
			input_tokens, output_tokens,
			image_input_tokens, image_output_tokens,
			account_stats_cost, account_rate_multiplier
		FROM usage_logs
		WHERE account_id = $1
			AND created_at >= $2
			AND created_at < $3
		ORDER BY created_at ASC
		LIMIT $4
	`

	rows, err := r.db.QueryContext(ctx, query, accountID, startTime, endTime, limit)
	if err != nil {
		return nil, fmt.Errorf("query reconcile candidates: %w", err)
	}
	defer rows.Close()

	var candidates []service.UsageLogReconcileCandidate
	for rows.Next() {
		var c service.UsageLogReconcileCandidate
		var requestedModel string
		err := rows.Scan(
			&c.ID, &c.AccountID, &c.CreatedAt, &c.Model,
			&requestedModel,
			&c.InputTokens, &c.OutputTokens,
			&c.ImageInputTokens, &c.ImageOutputTokens,
			&c.AccountStatsCost, &c.AccountRateMultiplier,
		)
		if err != nil {
			return nil, fmt.Errorf("scan reconcile candidate: %w", err)
		}
		if requestedModel != "" {
			c.RequestedModel = requestedModel
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconcile candidates: %w", err)
	}

	return candidates, nil
}

// UpdateAccountStatsCost backfills account_stats_cost for a single usage_logs row.
func (r *usageLogRepository) UpdateAccountStatsCost(
	ctx context.Context,
	usageLogID int64,
	accountStatsCost float64,
) error {
	query := `
		UPDATE usage_logs
		SET account_stats_cost = $2, updated_at = now()
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query, usageLogID, accountStatsCost)
	if err != nil {
		return fmt.Errorf("update account_stats_cost: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get affected rows: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// BatchUpdateAccountStatsCost backfills account_stats_cost for multiple rows.
func (r *usageLogRepository) BatchUpdateAccountStatsCost(
	ctx context.Context,
	updates map[int64]float64,
) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE usage_logs
		SET account_stats_cost = $2, updated_at = now()
		WHERE id = $1
	`)
	if err != nil {
		return 0, fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	updated := 0
	for usageLogID, cost := range updates {
		result, err := stmt.ExecContext(ctx, usageLogID, cost)
		if err != nil {
			return updated, fmt.Errorf("update row %d: %w", usageLogID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return updated, fmt.Errorf("get affected rows for %d: %w", usageLogID, err)
		}
		if affected > 0 {
			updated++
		}
	}

	if err := tx.Commit(); err != nil {
		return updated, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}
