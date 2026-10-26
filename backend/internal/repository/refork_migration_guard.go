package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// Restored custom databases must be archived before startup runs official cleanup.
// Empty official databases retain the upstream initialization path.
func validateReforkArchive(ctx context.Context, db *sql.Conn) error {
	var legacy bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM schema_migrations
		WHERE filename = '233_backfill_and_freeze_native_subscriptions.sql'
	)`).Scan(&legacy)
	if err != nil || !legacy {
		return err
	}
	var checkpoint bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('refork_audit.checkpoint') IS NOT NULL`).Scan(&checkpoint); err != nil {
		return err
	}
	if !checkpoint {
		return fmt.Errorf("legacy database requires the refork preflight and private archive before official migrations")
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM refork_audit.checkpoint
		WHERE baseline = 'ac1b8f1271be0d674fa6f074403c1410f30a3a81'
		  AND archive_sha256 ~ '^[0-9a-f]{64}$'
		  AND (phase IN ('converted', 'audited')
		       OR (phase = 'archived' AND archive_sha256 = $1))
	)`, os.Getenv("REFORK_ARCHIVE_SHA256")).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("legacy database archive checkpoint missing or invalid")
	}
	var active bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM subscription_purchases
		WHERE lower(status) = 'active' AND expires_at > now()
	)`).Scan(&active); err != nil {
		return err
	}
	if active {
		return fmt.Errorf("active legacy subscription purchases block the refork")
	}
	return nil
}
