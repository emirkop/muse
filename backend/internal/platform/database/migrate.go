package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (p *Pool) ApplyMigrations(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("database: create schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("database: list embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if err := p.applyMigrationIfNeeded(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) applyMigrationIfNeeded(ctx context.Context, filename string) error {
	var alreadyApplied bool
	if err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, filename,
	).Scan(&alreadyApplied); err != nil {
		return fmt.Errorf("database: check migration status for %s: %w", filename, err)
	}
	if alreadyApplied {
		return nil
	}

	sqlBytes, err := migrationFiles.ReadFile("migrations/" + filename)
	if err != nil {
		return fmt.Errorf("database: read migration %s: %w", filename, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin transaction for migration %s: %w", filename, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("database: apply migration %s: %w", filename, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, filename); err != nil {
		return fmt.Errorf("database: record migration %s: %w", filename, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit migration %s: %w", filename, err)
	}
	return nil
}
