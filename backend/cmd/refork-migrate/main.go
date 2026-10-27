// refork-migrate invokes the official migration runner only on an archived candidate.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	_ "github.com/lib/pq"
)

func run() error {
	name := os.Getenv("REFORK_CANDIDATE_DATABASE")
	digest := os.Getenv("REFORK_ARCHIVE_SHA256")
	initialize := os.Getenv("REFORK_INIT_EMPTY") == "true"
	if !strings.Contains(strings.ToLower(name), "candidate") || (!initialize && len(digest) != 64) {
		return fmt.Errorf("candidate database and archive digest are required")
	}
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("database connection failed")
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		return fmt.Errorf("candidate database identity mismatch")
	}
	if initialize {
		var tables int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_class c
			JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname NOT IN ('pg_catalog','information_schema')
			  AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','f')`).Scan(&tables); err != nil || tables != 0 {
			return fmt.Errorf("empty initialization requires a database with no application objects")
		}
	} else {
		var stored string
		if err := db.QueryRowContext(ctx, `SELECT archive_sha256 FROM refork_audit.checkpoint WHERE baseline=$1`,
			"ac1b8f1271be0d674fa6f074403c1410f30a3a81").Scan(&stored); err != nil || stored != digest {
			return fmt.Errorf("candidate archive checkpoint mismatch")
		}
	}
	if err := repository.ApplyMigrations(ctx, db); err != nil {
		// Do not print database errors, which may contain production row values.
		return fmt.Errorf("official migrations failed; inspect private database diagnostics")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
