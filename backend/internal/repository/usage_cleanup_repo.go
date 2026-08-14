package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbusagecleanuptask "github.com/Wei-Shaw/sub2api/ent/usagecleanuptask"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageCleanupRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewUsageCleanupRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageCleanupRepository {
	return newUsageCleanupRepositoryWithSQL(client, sqlDB)
}

func newUsageCleanupRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageCleanupRepository {
	return &usageCleanupRepository{client: client, sql: sqlq}
}

func (r *usageCleanupRepository) CreateTask(ctx context.Context, task *service.UsageCleanupTask) error {
	if task == nil {
		return nil
	}
	if r.client != nil {
		return r.createTaskWithEnt(ctx, task)
	}
	return r.createTaskWithSQL(ctx, task)
}

func (r *usageCleanupRepository) ListTasks(ctx context.Context, params pagination.PaginationParams) ([]service.UsageCleanupTask, *pagination.PaginationResult, error) {
	if r.client != nil {
		return r.listTasksWithEnt(ctx, params)
	}
	var total int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM usage_cleanup_tasks", nil, &total); err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.UsageCleanupTask{}, paginationResultFromTotal(0, params), nil
	}

	query := `
		SELECT id, status, filters, created_by, deleted_rows, error_message,
			canceled_by, canceled_at,
			started_at, finished_at, created_at, updated_at
		FROM usage_cleanup_tasks
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.sql.QueryContext(ctx, query, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]service.UsageCleanupTask, 0)
	for rows.Next() {
		var task service.UsageCleanupTask
		var filtersJSON []byte
		var errMsg sql.NullString
		var canceledBy sql.NullInt64
		var canceledAt sql.NullTime
		var startedAt sql.NullTime
		var finishedAt sql.NullTime
		if err := rows.Scan(
			&task.ID,
			&task.Status,
			&filtersJSON,
			&task.CreatedBy,
			&task.DeletedRows,
			&errMsg,
			&canceledBy,
			&canceledAt,
			&startedAt,
			&finishedAt,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(filtersJSON, &task.Filters); err != nil {
			return nil, nil, fmt.Errorf("parse cleanup filters: %w", err)
		}
		if errMsg.Valid {
			task.ErrorMsg = &errMsg.String
		}
		if canceledBy.Valid {
			v := canceledBy.Int64
			task.CanceledBy = &v
		}
		if canceledAt.Valid {
			task.CanceledAt = &canceledAt.Time
		}
		if startedAt.Valid {
			task.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			task.FinishedAt = &finishedAt.Time
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return tasks, paginationResultFromTotal(total, params), nil
}

func (r *usageCleanupRepository) ClaimNextPendingTask(ctx context.Context, staleRunningAfterSeconds int64) (*service.UsageCleanupTask, error) {
	if staleRunningAfterSeconds <= 0 {
		staleRunningAfterSeconds = 1800
	}
	query := `
		WITH next AS (
			SELECT id
			FROM usage_cleanup_tasks
			WHERE status = $1
				OR (
					status = $2
					AND started_at IS NOT NULL
					AND started_at < NOW() - ($3 * interval '1 second')
				)
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE usage_cleanup_tasks AS tasks
		SET status = $4,
			started_at = NOW(),
			finished_at = NULL,
			error_message = NULL,
			updated_at = NOW()
		FROM next
		WHERE tasks.id = next.id
		RETURNING tasks.id, tasks.status, tasks.filters, tasks.created_by, tasks.deleted_rows, tasks.error_message,
			tasks.started_at, tasks.finished_at, tasks.created_at, tasks.updated_at
	`
	var task service.UsageCleanupTask
	var filtersJSON []byte
	var errMsg sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{
			service.UsageCleanupStatusPending,
			service.UsageCleanupStatusRunning,
			staleRunningAfterSeconds,
			service.UsageCleanupStatusRunning,
		},
		&task.ID,
		&task.Status,
		&filtersJSON,
		&task.CreatedBy,
		&task.DeletedRows,
		&errMsg,
		&startedAt,
		&finishedAt,
		&task.CreatedAt,
		&task.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(filtersJSON, &task.Filters); err != nil {
		return nil, fmt.Errorf("parse cleanup filters: %w", err)
	}
	if errMsg.Valid {
		task.ErrorMsg = &errMsg.String
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		task.FinishedAt = &finishedAt.Time
	}
	return &task, nil
}

func (r *usageCleanupRepository) GetTaskStatus(ctx context.Context, taskID int64) (string, error) {
	if r.client != nil {
		return r.getTaskStatusWithEnt(ctx, taskID)
	}
	var status string
	if err := scanSingleRow(ctx, r.sql, "SELECT status FROM usage_cleanup_tasks WHERE id = $1", []any{taskID}, &status); err != nil {
		return "", err
	}
	return status, nil
}

func (r *usageCleanupRepository) UpdateTaskProgress(ctx context.Context, taskID int64, deletedRows int64) error {
	if r.client != nil {
		return r.updateTaskProgressWithEnt(ctx, taskID, deletedRows)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET deleted_rows = $1,
			updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.sql.ExecContext(ctx, query, deletedRows, taskID)
	return err
}

func (r *usageCleanupRepository) CancelTask(ctx context.Context, taskID int64, canceledBy int64) (bool, error) {
	if r.client != nil {
		return r.cancelTaskWithEnt(ctx, taskID, canceledBy)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = $1,
			canceled_by = $3,
			canceled_at = NOW(),
			finished_at = NOW(),
			error_message = NULL,
			updated_at = NOW()
		WHERE id = $2
			AND status IN ($4, $5)
		RETURNING id
	`
	var id int64
	err := scanSingleRow(ctx, r.sql, query, []any{
		service.UsageCleanupStatusCanceled,
		taskID,
		canceledBy,
		service.UsageCleanupStatusPending,
		service.UsageCleanupStatusRunning,
	}, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *usageCleanupRepository) MarkTaskSucceeded(ctx context.Context, taskID int64, deletedRows int64) error {
	if r.client != nil {
		return r.markTaskSucceededWithEnt(ctx, taskID, deletedRows)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = $1,
			deleted_rows = $2,
			finished_at = NOW(),
			updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.sql.ExecContext(ctx, query, service.UsageCleanupStatusSucceeded, deletedRows, taskID)
	return err
}

func (r *usageCleanupRepository) MarkTaskFailed(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error {
	if r.client != nil {
		return r.markTaskFailedWithEnt(ctx, taskID, deletedRows, errorMsg)
	}
	query := `
		UPDATE usage_cleanup_tasks
		SET status = $1,
			deleted_rows = $2,
			error_message = $3,
			finished_at = NOW(),
			updated_at = NOW()
		WHERE id = $4
	`
	_, err := r.sql.ExecContext(ctx, query, service.UsageCleanupStatusFailed, deletedRows, errorMsg, taskID)
	return err
}

func (r *usageCleanupRepository) DeleteUsageLogsBatch(ctx context.Context, filters service.UsageCleanupFilters, limit int) (int64, error) {
	if filters.StartTime.IsZero() || filters.EndTime.IsZero() {
		return 0, fmt.Errorf("cleanup filters missing time range")
	}
	whereClause, args := buildUsageCleanupWhere(filters)
	if whereClause == "" {
		return 0, fmt.Errorf("cleanup filters missing time range")
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		WITH target AS (
			SELECT id
			FROM usage_logs
			WHERE %s
			ORDER BY created_at ASC, id ASC
			LIMIT $%d
		)
		DELETE FROM usage_logs
		WHERE id IN (SELECT id FROM target)
		RETURNING id
	`, whereClause, len(args))

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	var deleted int64
	for rows.Next() {
		deleted++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *usageCleanupRepository) FindNextUsageLogArchiveWindow(ctx context.Context, cutoff time.Time, window time.Duration) (*service.UsageLogArchiveWindow, error) {
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("usage cleanup repository not ready")
	}
	if window <= 0 {
		window = time.Hour
	}
	cutoff = cutoff.UTC()
	var bucketStart sql.NullTime
	if err := scanSingleRow(ctx, r.sql, `
		SELECT date_trunc('hour', MIN(created_at))
		FROM usage_logs
		WHERE created_at < $1
	`, []any{cutoff}, &bucketStart); err != nil {
		return nil, err
	}
	if !bucketStart.Valid {
		return nil, nil
	}
	start := bucketStart.Time.UTC().Truncate(time.Hour)
	end := start.Add(window)
	return &service.UsageLogArchiveWindow{StartTime: start, EndTime: end}, nil
}

func (r *usageCleanupRepository) ArchiveUsageLogsWindow(ctx context.Context, start, end time.Time) (*service.UsageLogArchiveResult, error) {
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("usage cleanup repository not ready")
	}
	start = start.UTC()
	end = end.UTC()
	if !end.After(start) {
		return nil, fmt.Errorf("archive window end must be after start")
	}
	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		result, err := archiveUsageLogsWindowWithExecutor(ctx, tx, start, end)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	return archiveUsageLogsWindowWithExecutor(ctx, r.sql, start, end)
}

func archiveUsageLogsWindowWithExecutor(ctx context.Context, exec sqlExecutor, start, end time.Time) (*service.UsageLogArchiveResult, error) {
	query := `
		WITH aggregated AS (
			SELECT
				date_trunc('hour', created_at) AS bucket_start,
				user_id,
				api_key_id,
				account_id,
				COALESCE(group_id, 0)::bigint AS group_id,
				COALESCE(channel_id, 0)::bigint AS channel_id,
				COALESCE(NULLIF(TRIM(model), ''), '') AS model,
				COALESCE(NULLIF(TRIM(requested_model), ''), '') AS requested_model,
				COALESCE(NULLIF(TRIM(upstream_model), ''), '') AS upstream_model,
				COALESCE(NULLIF(TRIM(billing_tier), ''), '') AS billing_tier,
				COALESCE(NULLIF(TRIM(billing_mode), ''), '') AS billing_mode,
				COALESCE(request_type, 0)::smallint AS request_type,
				COALESCE(stream, false) AS stream,
				COALESCE(billing_type, 0)::smallint AS billing_type,
				COUNT(*)::bigint AS request_count,
				COUNT(*) FILTER (WHERE COALESCE(actual_cost, 0) > 0)::bigint AS success_count,
				COUNT(*) FILTER (WHERE COALESCE(actual_cost, 0) <= 0)::bigint AS zero_cost_count,
				COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
				COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0)::bigint AS cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0)::bigint AS cache_read_tokens,
				COALESCE(SUM(cache_creation_5m_tokens), 0)::bigint AS cache_creation_5m_tokens,
				COALESCE(SUM(cache_creation_1h_tokens), 0)::bigint AS cache_creation_1h_tokens,
				COALESCE(SUM(image_count), 0)::bigint AS image_count,
				COALESCE(SUM(image_input_tokens), 0)::bigint AS image_input_tokens,
				COALESCE(SUM(image_output_tokens), 0)::bigint AS image_output_tokens,
				COALESCE(SUM(video_count), 0)::bigint AS video_count,
				COALESCE(SUM(video_duration_seconds), 0)::bigint AS video_duration_seconds,
				COALESCE(SUM(input_cost), 0) AS input_cost,
				COALESCE(SUM(output_cost), 0) AS output_cost,
				COALESCE(SUM(cache_creation_cost), 0) AS cache_creation_cost,
				COALESCE(SUM(cache_read_cost), 0) AS cache_read_cost,
				COALESCE(SUM(image_input_cost), 0) AS image_input_cost,
				COALESCE(SUM(image_output_cost), 0) AS image_output_cost,
				COALESCE(SUM(total_cost), 0) AS total_cost,
				COALESCE(SUM(actual_cost), 0) AS actual_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS account_cost,
				COALESCE(SUM(duration_ms), 0)::bigint AS total_duration_ms,
				COALESCE(SUM(first_token_ms), 0)::bigint AS total_first_token_ms
			FROM usage_logs
			WHERE created_at >= $1 AND created_at < $2
			GROUP BY
				bucket_start,
				user_id,
				api_key_id,
				account_id,
				COALESCE(group_id, 0),
				COALESCE(channel_id, 0),
				COALESCE(NULLIF(TRIM(model), ''), ''),
				COALESCE(NULLIF(TRIM(requested_model), ''), ''),
				COALESCE(NULLIF(TRIM(upstream_model), ''), ''),
				COALESCE(NULLIF(TRIM(billing_tier), ''), ''),
				COALESCE(NULLIF(TRIM(billing_mode), ''), ''),
				COALESCE(request_type, 0),
				COALESCE(stream, false),
				COALESCE(billing_type, 0)
		),
		upserted AS (
			INSERT INTO usage_log_hourly_archives (
				bucket_start,
				user_id,
				api_key_id,
				account_id,
				group_id,
				channel_id,
				model,
				requested_model,
				upstream_model,
				billing_tier,
				billing_mode,
				request_type,
				stream,
				billing_type,
				request_count,
				success_count,
				zero_cost_count,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				cache_creation_5m_tokens,
				cache_creation_1h_tokens,
				image_count,
				image_input_tokens,
				image_output_tokens,
				video_count,
				video_duration_seconds,
				input_cost,
				output_cost,
				cache_creation_cost,
				cache_read_cost,
				image_input_cost,
				image_output_cost,
				total_cost,
				actual_cost,
				account_cost,
				total_duration_ms,
				total_first_token_ms
			)
			SELECT
				bucket_start,
				user_id,
				api_key_id,
				account_id,
				group_id,
				channel_id,
				model,
				requested_model,
				upstream_model,
				billing_tier,
				billing_mode,
				request_type,
				stream,
				billing_type,
				request_count,
				success_count,
				zero_cost_count,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				cache_creation_5m_tokens,
				cache_creation_1h_tokens,
				image_count,
				image_input_tokens,
				image_output_tokens,
				video_count,
				video_duration_seconds,
				input_cost,
				output_cost,
				cache_creation_cost,
				cache_read_cost,
				image_input_cost,
				image_output_cost,
				total_cost,
				actual_cost,
				account_cost,
				total_duration_ms,
				total_first_token_ms
			FROM aggregated
			ON CONFLICT (
				bucket_start,
				user_id,
				api_key_id,
				account_id,
				group_id,
				channel_id,
				model,
				requested_model,
				upstream_model,
				billing_tier,
				billing_mode,
				request_type,
				stream,
				billing_type
			)
			DO UPDATE SET
				request_count = EXCLUDED.request_count,
				success_count = EXCLUDED.success_count,
				zero_cost_count = EXCLUDED.zero_cost_count,
				input_tokens = EXCLUDED.input_tokens,
				output_tokens = EXCLUDED.output_tokens,
				cache_creation_tokens = EXCLUDED.cache_creation_tokens,
				cache_read_tokens = EXCLUDED.cache_read_tokens,
				cache_creation_5m_tokens = EXCLUDED.cache_creation_5m_tokens,
				cache_creation_1h_tokens = EXCLUDED.cache_creation_1h_tokens,
				image_count = EXCLUDED.image_count,
				image_input_tokens = EXCLUDED.image_input_tokens,
				image_output_tokens = EXCLUDED.image_output_tokens,
				video_count = EXCLUDED.video_count,
				video_duration_seconds = EXCLUDED.video_duration_seconds,
				input_cost = EXCLUDED.input_cost,
				output_cost = EXCLUDED.output_cost,
				cache_creation_cost = EXCLUDED.cache_creation_cost,
				cache_read_cost = EXCLUDED.cache_read_cost,
				image_input_cost = EXCLUDED.image_input_cost,
				image_output_cost = EXCLUDED.image_output_cost,
				total_cost = EXCLUDED.total_cost,
				actual_cost = EXCLUDED.actual_cost,
				account_cost = EXCLUDED.account_cost,
				total_duration_ms = EXCLUDED.total_duration_ms,
				total_first_token_ms = EXCLUDED.total_first_token_ms,
				updated_at = NOW()
			RETURNING 1
		),
		deleted AS (
			DELETE FROM usage_logs
			WHERE created_at >= $1 AND created_at < $2
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM upserted)::bigint AS summary_rows,
			(SELECT COUNT(*) FROM deleted)::bigint AS deleted_rows
	`
	result := &service.UsageLogArchiveResult{}
	if err := scanSingleRow(ctx, exec, query, []any{start, end}, &result.SummaryRows, &result.DeletedRows); err != nil {
		return nil, err
	}
	return result, nil
}

func buildUsageCleanupWhere(filters service.UsageCleanupFilters) (string, []any) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	idx := 1
	if !filters.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, filters.StartTime)
		idx++
	}
	if !filters.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, filters.EndTime)
		idx++
	}
	if filters.UserID != nil {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, *filters.UserID)
		idx++
	}
	if filters.APIKeyID != nil {
		conditions = append(conditions, fmt.Sprintf("api_key_id = $%d", idx))
		args = append(args, *filters.APIKeyID)
		idx++
	}
	if filters.AccountID != nil {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", idx))
		args = append(args, *filters.AccountID)
		idx++
	}
	if filters.GroupID != nil {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", idx))
		args = append(args, *filters.GroupID)
		idx++
	}
	if filters.Model != nil {
		model := strings.TrimSpace(*filters.Model)
		if model != "" {
			conditions = append(conditions, fmt.Sprintf("model = $%d", idx))
			args = append(args, model)
			idx++
		}
	}
	if filters.RequestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(idx, *filters.RequestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		idx += len(conditionArgs)
	} else if filters.Stream != nil {
		conditions = append(conditions, fmt.Sprintf("stream = $%d", idx))
		args = append(args, *filters.Stream)
		idx++
	}
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", idx))
		args = append(args, *filters.BillingType)
	}
	return strings.Join(conditions, " AND "), args
}

func (r *usageCleanupRepository) createTaskWithEnt(ctx context.Context, task *service.UsageCleanupTask) error {
	client := clientFromContext(ctx, r.client)
	filtersJSON, err := json.Marshal(task.Filters)
	if err != nil {
		return fmt.Errorf("marshal cleanup filters: %w", err)
	}
	created, err := client.UsageCleanupTask.
		Create().
		SetStatus(task.Status).
		SetFilters(json.RawMessage(filtersJSON)).
		SetCreatedBy(task.CreatedBy).
		SetDeletedRows(task.DeletedRows).
		Save(ctx)
	if err != nil {
		return err
	}
	task.ID = created.ID
	task.CreatedAt = created.CreatedAt
	task.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *usageCleanupRepository) createTaskWithSQL(ctx context.Context, task *service.UsageCleanupTask) error {
	filtersJSON, err := json.Marshal(task.Filters)
	if err != nil {
		return fmt.Errorf("marshal cleanup filters: %w", err)
	}
	query := `
		INSERT INTO usage_cleanup_tasks (
			status,
			filters,
			created_by,
			deleted_rows
		) VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	if err := scanSingleRow(ctx, r.sql, query, []any{task.Status, filtersJSON, task.CreatedBy, task.DeletedRows}, &task.ID, &task.CreatedAt, &task.UpdatedAt); err != nil {
		return err
	}
	return nil
}

func (r *usageCleanupRepository) listTasksWithEnt(ctx context.Context, params pagination.PaginationParams) ([]service.UsageCleanupTask, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	query := client.UsageCleanupTask.Query()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.UsageCleanupTask{}, paginationResultFromTotal(0, params), nil
	}
	rows, err := query.
		Order(dbent.Desc(dbusagecleanuptask.FieldCreatedAt), dbent.Desc(dbusagecleanuptask.FieldID)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	tasks := make([]service.UsageCleanupTask, 0, len(rows))
	for _, row := range rows {
		task, err := usageCleanupTaskFromEnt(row)
		if err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, paginationResultFromTotal(int64(total), params), nil
}

func (r *usageCleanupRepository) getTaskStatusWithEnt(ctx context.Context, taskID int64) (string, error) {
	client := clientFromContext(ctx, r.client)
	task, err := client.UsageCleanupTask.Query().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return "", sql.ErrNoRows
		}
		return "", err
	}
	return task.Status, nil
}

func (r *usageCleanupRepository) updateTaskProgressWithEnt(ctx context.Context, taskID int64, deletedRows int64) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetDeletedRows(deletedRows).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func (r *usageCleanupRepository) cancelTaskWithEnt(ctx context.Context, taskID int64, canceledBy int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	affected, err := client.UsageCleanupTask.Update().
		Where(
			dbusagecleanuptask.IDEQ(taskID),
			dbusagecleanuptask.StatusIn(service.UsageCleanupStatusPending, service.UsageCleanupStatusRunning),
		).
		SetStatus(service.UsageCleanupStatusCanceled).
		SetCanceledBy(canceledBy).
		SetCanceledAt(now).
		SetFinishedAt(now).
		ClearErrorMessage().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *usageCleanupRepository) markTaskSucceededWithEnt(ctx context.Context, taskID int64, deletedRows int64) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetStatus(service.UsageCleanupStatusSucceeded).
		SetDeletedRows(deletedRows).
		SetFinishedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func (r *usageCleanupRepository) markTaskFailedWithEnt(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error {
	client := clientFromContext(ctx, r.client)
	now := time.Now()
	_, err := client.UsageCleanupTask.Update().
		Where(dbusagecleanuptask.IDEQ(taskID)).
		SetStatus(service.UsageCleanupStatusFailed).
		SetDeletedRows(deletedRows).
		SetErrorMessage(errorMsg).
		SetFinishedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	return err
}

func usageCleanupTaskFromEnt(row *dbent.UsageCleanupTask) (service.UsageCleanupTask, error) {
	task := service.UsageCleanupTask{
		ID:          row.ID,
		Status:      row.Status,
		CreatedBy:   row.CreatedBy,
		DeletedRows: row.DeletedRows,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if len(row.Filters) > 0 {
		if err := json.Unmarshal(row.Filters, &task.Filters); err != nil {
			return service.UsageCleanupTask{}, fmt.Errorf("parse cleanup filters: %w", err)
		}
	}
	if row.ErrorMessage != nil {
		task.ErrorMsg = row.ErrorMessage
	}
	if row.CanceledBy != nil {
		task.CanceledBy = row.CanceledBy
	}
	if row.CanceledAt != nil {
		task.CanceledAt = row.CanceledAt
	}
	if row.StartedAt != nil {
		task.StartedAt = row.StartedAt
	}
	if row.FinishedAt != nil {
		task.FinishedAt = row.FinishedAt
	}
	return task, nil
}
