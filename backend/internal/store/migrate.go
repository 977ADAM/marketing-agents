package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate применяет ещё не применённые встроенные .sql-миграции в
// лексикографическом порядке, каждую — в своей транзакции, и отмечает факт
// применения в schema_migrations. Повторный запуск безопасен и ничего не делает.
//
// Учёт применённых миграций (вместо «идемпотентного SQL») нужен потому, что
// SQLite не поддерживает ALTER TABLE ... ADD COLUMN IF NOT EXISTS.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (`+nowExpr+`)
	)`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, db, name, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

func appliedMigrations(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// applyMigration выполняет один файл и записывает его в schema_migrations.
// SQLite умеет транзакционный DDL, поэтому «половина миграции» невозможна.
func applyMigration(ctx context.Context, db *sql.DB, name, sqlText string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
		return fmt.Errorf("migrate %s: mark applied: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate %s: commit: %w", name, err)
	}
	return nil
}
