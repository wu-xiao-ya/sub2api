package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountPoolGroupRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewAccountPoolGroupRepository(client *dbent.Client, sqlDB *sql.DB) service.AccountPoolGroupRepository {
	return &accountPoolGroupRepository{client: client, sql: sqlDB}
}

func (r *accountPoolGroupRepository) exec(ctx context.Context) sqlExecutor {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.sql
}

func (r *accountPoolGroupRepository) List(ctx context.Context) ([]service.AccountPoolGroup, error) {
	rows, err := r.exec(ctx).QueryContext(ctx, `
		SELECT id, name, upstream_key, description, sort_order, status, created_at, updated_at
		FROM account_pool_groups
		WHERE deleted_at IS NULL
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]service.AccountPoolGroup, 0)
	for rows.Next() {
		group, scanErr := scanAccountPoolGroup(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountPoolGroupRepository) GetByID(ctx context.Context, id int64) (*service.AccountPoolGroup, error) {
	rows, err := r.exec(ctx).QueryContext(ctx, `
		SELECT id, name, upstream_key, description, sort_order, status, created_at, updated_at
		FROM account_pool_groups
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAccountPoolGroupNotFound
	}
	group, err := scanAccountPoolGroup(rows)
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *accountPoolGroupRepository) Create(ctx context.Context, group *service.AccountPoolGroup) error {
	if group == nil {
		return service.ErrAccountPoolGroupNameRequired
	}
	rows, err := r.exec(ctx).QueryContext(ctx, `
		INSERT INTO account_pool_groups (name, upstream_key, description, sort_order, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`, group.Name, group.UpstreamKey, group.Description, group.SortOrder, group.Status)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrAccountPoolGroupExists)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrAccountPoolGroupNotFound
	}
	if scanErr := rows.Scan(&group.ID, &group.CreatedAt, &group.UpdatedAt); scanErr != nil {
		return scanErr
	}
	return rows.Err()
}

func (r *accountPoolGroupRepository) Update(ctx context.Context, group *service.AccountPoolGroup) error {
	if group == nil {
		return service.ErrAccountPoolGroupNameRequired
	}
	result, err := r.exec(ctx).ExecContext(ctx, `
		UPDATE account_pool_groups
		SET name = $2,
		    upstream_key = $3,
		    description = $4,
		    sort_order = $5,
		    status = $6,
		    updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, group.ID, group.Name, group.UpstreamKey, group.Description, group.SortOrder, group.Status)
	if err != nil {
		return translatePersistenceError(err, service.ErrAccountPoolGroupNotFound, service.ErrAccountPoolGroupExists)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return service.ErrAccountPoolGroupNotFound
	}
	return nil
}

func (r *accountPoolGroupRepository) Delete(ctx context.Context, id int64) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.deleteWithExec(ctx, tx.Client(), id)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}
	if tx == nil {
		return r.deleteWithExec(ctx, r.sql, id)
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.deleteWithExec(dbent.NewTxContext(ctx, tx), tx.Client(), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *accountPoolGroupRepository) deleteWithExec(ctx context.Context, exec sqlExecutor, id int64) error {
	result, err := exec.ExecContext(ctx, `
		UPDATE account_pool_groups
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return service.ErrAccountPoolGroupNotFound
	}
	if _, err := exec.ExecContext(ctx, `
		UPDATE accounts
		SET pool_group_id = NULL, updated_at = NOW()
		WHERE pool_group_id = $1 AND deleted_at IS NULL
	`, id); err != nil {
		return err
	}
	return nil
}

type accountPoolGroupScanner interface {
	Scan(dest ...any) error
}

func scanAccountPoolGroup(scanner accountPoolGroupScanner) (service.AccountPoolGroup, error) {
	var group service.AccountPoolGroup
	err := scanner.Scan(
		&group.ID,
		&group.Name,
		&group.UpstreamKey,
		&group.Description,
		&group.SortOrder,
		&group.Status,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	return group, err
}
