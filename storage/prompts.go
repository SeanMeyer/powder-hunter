package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PromptVersion holds metadata for a single versioned prompt template.
type PromptVersion struct {
	ID        string
	Version   string
	Template  string
	CreatedAt time.Time
	IsActive  bool
}

// SavePromptTemplate inserts a new version and marks it active, deactivating all
// prior versions of the same prompt ID in a single transaction.
func (d *DB) SavePromptTemplate(ctx context.Context, id, version, template string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`UPDATE prompt_templates SET is_active = 0 WHERE id = ?`, id,
	); err != nil {
		return fmt.Errorf("deactivate prior prompt versions: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO prompt_templates (id, version, template, created_at, is_active)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(id, version) DO UPDATE SET is_active = 1`,
		id, version, template,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert prompt template %s@%s: %w", id, version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit prompt template: %w", err)
	}
	return nil
}

// GetActivePrompt returns the active version and template body for the given prompt ID.
func (d *DB) GetActivePrompt(ctx context.Context, id string) (version, template string, err error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT version, template FROM prompt_templates WHERE id = ? AND is_active = 1`,
		id,
	)
	if err := row.Scan(&version, &template); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("no active prompt for id %s: %w", id, err)
		}
		return "", "", fmt.Errorf("get active prompt %s: %w", id, err)
	}
	return version, template, nil
}

// GetPromptByVersion returns the template body for a specific version of a prompt ID.
func (d *DB) GetPromptByVersion(ctx context.Context, id, version string) (string, string, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT version, template FROM prompt_templates WHERE id = ? AND version = ?`,
		id, version,
	)
	var v, tmpl string
	if err := row.Scan(&v, &tmpl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("prompt %s version %s not found: %w", id, version, err)
		}
		return "", "", fmt.Errorf("get prompt %s version %s: %w", id, version, err)
	}
	return v, tmpl, nil
}

// ListPromptVersions returns all versions of a prompt ID in creation order.
func (d *DB) ListPromptVersions(ctx context.Context, id string) ([]PromptVersion, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, version, template, created_at, is_active
		FROM prompt_templates
		WHERE id = ?
		ORDER BY created_at ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list prompt versions for %s: %w", id, err)
	}
	defer rows.Close()

	var versions []PromptVersion
	for rows.Next() {
		var pv PromptVersion
		var createdAt string
		var isActive int

		if err := rows.Scan(&pv.ID, &pv.Version, &pv.Template, &createdAt, &isActive); err != nil {
			return nil, fmt.Errorf("scan prompt version: %w", err)
		}
		var parseErr error
		if pv.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt); parseErr != nil {
			return nil, fmt.Errorf("scan prompt version: parse created_at: %w", parseErr)
		}
		pv.IsActive = isActive != 0
		versions = append(versions, pv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("prompt version rows: %w", err)
	}
	return versions, nil
}
