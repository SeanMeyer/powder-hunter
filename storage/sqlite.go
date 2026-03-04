package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL mode allows concurrent readers while a writer is active, which matters
	// when the pipeline and the Discord bot access the DB simultaneously.
	if _, err := db.ExecContext(context.Background(), `PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &DB{db: db}, nil
}

// Close releases the database connection.
func (d *DB) Close() error { return d.db.Close() }
