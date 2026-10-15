package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamRatioSnapshotRepository struct {
	db *sql.DB
}

// NewUpstreamRatioSnapshotRepository persists synced new-api per-model ratio
// timelines used by request-time upstream cost accounting.
func NewUpstreamRatioSnapshotRepository(db *sql.DB) service.UpstreamRatioSnapshotRepo {
	return &upstreamRatioSnapshotRepository{db: db}
}

const upstreamModelRatioInsert = `
	INSERT INTO upstream_model_ratio_snapshots
		(account_id, model_name, quota_type, model_ratio, completion_ratio,
		 model_price, cache_ratio, group_ratio, observed_at, captured_at, raw)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), $10)
	ON CONFLICT (account_id, model_name, observed_at) DO NOTHING
`

func (r *upstreamRatioSnapshotRepository) ReplaceUpstreamModelRatioSnapshot(
	ctx context.Context,
	accountID int64,
	observedAt time.Time,
	rows []service.UpstreamModelRatio,
) error {
	if r.db == nil {
		return service.ErrUpstreamRatioSyncUnavailable
	}
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Clear the same observed_at first so idempotent retries do not leave a
	// partially inserted snapshot behind after a failed earlier attempt.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM upstream_model_ratio_snapshots WHERE account_id = $1 AND observed_at = $2",
		accountID, observedAt,
	); err != nil {
		return err
	}
	for _, row := range rows {
		var cacheRatio any
		if row.CacheRatio != nil {
			cacheRatio = *row.CacheRatio
		}
		var raw any
		if row.Raw != nil {
			encoded, err := json.Marshal(row.Raw)
			if err != nil {
				return fmt.Errorf("marshal upstream ratio raw entry: %w", err)
			}
			raw = encoded
		}
		if _, err := tx.ExecContext(ctx, upstreamModelRatioInsert,
			accountID, row.ModelName, row.QuotaType, row.ModelRatio,
			row.EffectiveCompletionRatio(), row.ModelPrice, cacheRatio, row.GroupRatio,
			observedAt, raw,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *upstreamRatioSnapshotRepository) LatestUpstreamModelRatioRows(
	ctx context.Context,
	accountID int64,
	limit int,
) ([]service.UpstreamModelRatio, time.Time, error) {
	if r.db == nil {
		return nil, time.Time{}, service.ErrUpstreamRatioSyncUnavailable
	}
	if limit <= 0 {
		limit = 5000
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT model_name, quota_type, model_ratio, completion_ratio, model_price,
		       cache_ratio, group_ratio, observed_at, raw
		FROM upstream_model_ratio_snapshots
		WHERE account_id = $1
		  AND observed_at = (SELECT MAX(observed_at) FROM upstream_model_ratio_snapshots WHERE account_id = $1)
		ORDER BY model_name
		LIMIT $2
	`, accountID, limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()

	var observedAt time.Time
	out := make([]service.UpstreamModelRatio, 0, 64)
	for rows.Next() {
		var row service.UpstreamModelRatio
		var cacheRatio sql.NullFloat64
		var rawJSON []byte
		if err := rows.Scan(
			&row.ModelName, &row.QuotaType, &row.ModelRatio, &row.CompletionRatio,
			&row.ModelPrice, &cacheRatio, &row.GroupRatio, &observedAt, &rawJSON,
		); err != nil {
			return nil, time.Time{}, err
		}
		if cacheRatio.Valid {
			value := cacheRatio.Float64
			row.CacheRatio = &value
		}
		if len(rawJSON) > 0 {
			var raw map[string]any
			if err := json.Unmarshal(rawJSON, &raw); err == nil {
				row.Raw = raw
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	return out, observedAt, nil
}

func (r *upstreamRatioSnapshotRepository) ListUpstreamRatioSyncHistory(
	ctx context.Context,
	accountID int64,
	limit int,
) ([]service.UpstreamRatioHistoryEntry, error) {
	if r.db == nil {
		return nil, service.ErrUpstreamRatioSyncUnavailable
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT observed_at, COUNT(*) AS model_count,
		       COALESCE(MAX(group_ratio), 1) AS group_ratio
		FROM upstream_model_ratio_snapshots
		WHERE account_id = $1
		GROUP BY observed_at
		ORDER BY observed_at DESC
		LIMIT $2
	`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.UpstreamRatioHistoryEntry, 0, limit)
	for rows.Next() {
		var entry service.UpstreamRatioHistoryEntry
		if err := rows.Scan(&entry.ObservedAt, &entry.ModelCount, &entry.GroupRatio); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *upstreamRatioSnapshotRepository) DeleteUpstreamModelRatioSnapshotsBefore(
	ctx context.Context,
	before time.Time,
) (int64, error) {
	if r.db == nil {
		return 0, service.ErrUpstreamRatioSyncUnavailable
	}
	result, err := r.db.ExecContext(ctx,
		"DELETE FROM upstream_model_ratio_snapshots WHERE observed_at < $1", before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
