package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type intelligenceProbeRepository struct {
	db *sql.DB
}

func NewIntelligenceProbeRepository(db *sql.DB) service.IntelligenceProbeRepository {
	return &intelligenceProbeRepository{db: db}
}

const intelligenceProbeTargetColumns = `
	t.id, t.group_id, COALESCE(g.name, ''), COALESCE(g.platform, ''), t.model, t.enabled,
	t.last_run_at, t.next_run_at, t.created_at, t.updated_at
`

func scanIntelligenceProbeTarget(row scannable) (*service.IntelligenceProbeTarget, error) {
	t := &service.IntelligenceProbeTarget{}
	if err := row.Scan(
		&t.ID, &t.GroupID, &t.GroupName, &t.GroupPlatform, &t.Model, &t.Enabled,
		&t.LastRunAt, &t.NextRunAt, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *intelligenceProbeRepository) ListTargets(ctx context.Context, enabledOnly bool) ([]*service.IntelligenceProbeTarget, error) {
	query := `
		SELECT ` + intelligenceProbeTargetColumns + `
		FROM intelligence_probe_targets t
		LEFT JOIN groups g ON g.id = t.group_id
	`
	if enabledOnly {
		query += ` WHERE t.enabled = TRUE`
	}
	query += ` ORDER BY t.id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	targets := make([]*service.IntelligenceProbeTarget, 0)
	for rows.Next() {
		target, err := scanIntelligenceProbeTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (r *intelligenceProbeRepository) GetTarget(ctx context.Context, id int64) (*service.IntelligenceProbeTarget, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+intelligenceProbeTargetColumns+`
		FROM intelligence_probe_targets t
		LEFT JOIN groups g ON g.id = t.group_id
		WHERE t.id = $1
	`, id)
	target, err := scanIntelligenceProbeTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("intelligence probe target %d not found", id)
	}
	return target, err
}

func (r *intelligenceProbeRepository) TargetGroupPlatform(ctx context.Context, groupID int64) (string, error) {
	var platform string
	err := r.db.QueryRowContext(ctx, `SELECT platform FROM groups WHERE id = $1`, groupID).Scan(&platform)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("group %d does not exist", groupID)
	}
	return platform, err
}

// SyncTargets upserts the given list and removes targets absent from it, in a
// single transaction so history (results) survives untouched edits.
func (r *intelligenceProbeRepository) SyncTargets(ctx context.Context, targets []*service.IntelligenceProbeTarget) ([]*service.IntelligenceProbeTarget, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	keptIDs := make([]int64, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.GroupID <= 0 {
			return nil, fmt.Errorf("intelligence probe target is invalid")
		}
		if target.ID > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE intelligence_probe_targets
				SET group_id = $2, model = $3, enabled = $4, updated_at = NOW()
				WHERE id = $1
			`, target.ID, target.GroupID, target.Model, target.Enabled); err != nil {
				return nil, err
			}
			keptIDs = append(keptIDs, target.ID)
			continue
		}
		var newID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO intelligence_probe_targets (group_id, model, enabled)
			VALUES ($1, $2, $3)
			RETURNING id
		`, target.GroupID, target.Model, target.Enabled).Scan(&newID); err != nil {
			return nil, err
		}
		target.ID = newID
		keptIDs = append(keptIDs, newID)
	}

	if len(keptIDs) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM intelligence_probe_targets`); err != nil {
			return nil, err
		}
	} else {
		// database/sql's postgres driver does not bind Go slices to array
		// parameters, so build numbered placeholders for NOT IN instead.
		placeholders := make([]string, len(keptIDs))
		args := make([]any, len(keptIDs))
		for i, id := range keptIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}
		query := `DELETE FROM intelligence_probe_targets WHERE id NOT IN (` +
			strings.Join(placeholders, ",") + `)`
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.ListTargets(ctx, false)
}

func (r *intelligenceProbeRepository) UpdateTargetAfterRun(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE intelligence_probe_targets
		SET last_run_at = $2, next_run_at = $3, updated_at = NOW()
		WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

func (r *intelligenceProbeRepository) InsertResult(ctx context.Context, result *service.IntelligenceProbeResult) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO intelligence_probe_results
			(target_id, status, response_svg, response_excerpt, error_message, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, result.TargetID, result.Status, result.ResponseSVG, result.ResponseExcerpt, result.ErrorMessage, result.LatencyMs)
	return err
}

func (r *intelligenceProbeRepository) ListResultsByTarget(ctx context.Context, targetID int64, limit int) ([]*service.IntelligenceProbeResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, target_id, status, response_svg, response_excerpt, error_message, latency_ms, created_at
		FROM intelligence_probe_results
		WHERE target_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	results := make([]*service.IntelligenceProbeResult, 0)
	for rows.Next() {
		result := &service.IntelligenceProbeResult{}
		if err := rows.Scan(
			&result.ID, &result.TargetID, &result.Status, &result.ResponseSVG,
			&result.ResponseExcerpt, &result.ErrorMessage, &result.LatencyMs, &result.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

// ListRecentResultsForUser returns, for every enabled target, the newest
// perTargetLimit results with group name and model joined in.
func (r *intelligenceProbeRepository) ListRecentResultsForUser(ctx context.Context, perTargetLimit int) ([]*service.IntelligenceProbeUserResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ranked.id, ranked.target_id, t.group_id, COALESCE(g.name, ''), t.model,
		       ranked.status, ranked.response_svg, ranked.response_excerpt,
		       ranked.error_message, ranked.latency_ms, ranked.created_at
		FROM (
			SELECT id, target_id, status, response_svg, response_excerpt, error_message, latency_ms, created_at,
			       ROW_NUMBER() OVER (PARTITION BY target_id ORDER BY created_at DESC, id DESC) AS rn
			FROM intelligence_probe_results
		) ranked
		JOIN intelligence_probe_targets t ON t.id = ranked.target_id AND t.enabled = TRUE
		LEFT JOIN groups g ON g.id = t.group_id
		WHERE ranked.rn <= $1
		ORDER BY t.id ASC, ranked.created_at DESC, ranked.id DESC
	`, perTargetLimit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	results := make([]*service.IntelligenceProbeUserResult, 0)
	for rows.Next() {
		result := &service.IntelligenceProbeUserResult{}
		if err := rows.Scan(
			&result.ID, &result.TargetID, &result.GroupID, &result.GroupName, &result.Model,
			&result.Status, &result.ResponseSVG, &result.ResponseExcerpt,
			&result.ErrorMessage, &result.LatencyMs, &result.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (r *intelligenceProbeRepository) DeleteResultsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.ExecContext(ctx, `
		DELETE FROM intelligence_probe_results WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	affected, err := tag.RowsAffected()
	return affected, err
}

func (r *intelligenceProbeRepository) PruneResultsPerTarget(ctx context.Context, keep int) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM intelligence_probe_results
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY target_id ORDER BY created_at DESC, id DESC) AS rn
				FROM intelligence_probe_results
			) ranked
			WHERE rn > $1
		)
	`, keep)
	return err
}
