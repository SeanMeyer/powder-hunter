package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps the underlying sql.DB and is the entry point for all storage operations.
type DB struct {
	db *sql.DB
}

// Open connects to the SQLite database at path, enables WAL mode and foreign keys,
// and runs schema.sql to create tables if they don't exist.
func Open(path string) (*DB, error) {
	// Pragmas in the connection string apply to every connection in the pool.
	// busy_timeout prevents SQLITE_BUSY during concurrent storm persistence.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Limit to one writer connection — SQLite only supports one writer at a
	// time, so a single connection avoids pool-level contention entirely.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &DB{db: db}, nil
}

// runMigrations applies ALTER TABLE statements for columns added after the
// initial schema. Each migration is idempotent — "duplicate column name"
// errors are silently ignored.
func runMigrations(db *sql.DB) error {
	migrations := []string{
		`ALTER TABLE evaluations ADD COLUMN summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE evaluations ADD COLUMN top_resort_picks TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE regions ADD COLUMN macro_region TEXT NOT NULL DEFAULT ''`,
	}
	for _, m := range migrations {
		if _, err := db.ExecContext(context.Background(), m); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

// ResetStormData deletes all evaluations and storms, preserving regions,
// resorts, profiles, and prompt templates. Returns the number of storms deleted.
func (d *DB) ResetStormData(ctx context.Context) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM eval_costs`); err != nil {
		return 0, fmt.Errorf("delete eval_costs: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM evaluations`); err != nil {
		return 0, fmt.Errorf("delete evaluations: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM storms`)
	if err != nil {
		return 0, fmt.Errorf("delete storms: %w", err)
	}

	count, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit reset: %w", err)
	}
	return count, nil
}

// Close releases the database connection.
func (d *DB) Close() error { return d.db.Close() }
