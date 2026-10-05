package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CheckSchema проверяет, что схема БД готова к работе приложения: таблица учёта
// миграций существует, создана golang-migrate и не помечена «грязной».
// Возвращает применённую версию схемы.
//
// Миграции применяет отдельный сервис (в compose — migrate, локально — make
// migrate), поэтому сервер их не выполняет: он отказывается стартовать на
// неподготовленной БД и объясняет, что делать.
func CheckSchema(ctx context.Context, db *sql.DB) (int64, error) {
	columns, err := tableColumns(ctx, db, "schema_migrations")
	if err != nil {
		return 0, err
	}
	switch {
	case columns == nil:
		return 0, errors.New("миграции не применены: запустите сервис migrate (docker compose up migrate) или make migrate")
	case !columns["version"]:
		return 0, fmt.Errorf(
			"таблица учёта миграций старого формата (колонки: %s): переименуйте её и примените миграции сервисом migrate",
			strings.Join(sortedKeys(columns), ", "))
	}

	var version int64
	var dirty bool
	if err := db.QueryRowContext(ctx,
		`SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &dirty); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("миграции не применены: таблица учёта пуста, запустите сервис migrate")
		}
		return 0, fmt.Errorf("store: чтение версии схемы: %w", err)
	}
	if dirty {
		return version, fmt.Errorf("схема БД в «грязном» состоянии: миграция %d не завершилась", version)
	}
	return version, nil
}

// tableColumns возвращает набор колонок таблицы; nil — таблицы нет.
// Имя таблицы подставляется из константы в коде, а не из ввода пользователя.
func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
		return nil, fmt.Errorf("store: проверка таблицы %s: %w", table, err)
	}
	if exists == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("store: колонки таблицы %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, typ        string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("store: колонки таблицы %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: колонки таблицы %s: %w", table, err)
	}
	return columns, nil
}

// sortedKeys — колонки в стабильном порядке: для сообщения об ошибке.
func sortedKeys(columns map[string]bool) []string {
	keys := make([]string, 0, len(columns))
	for k := range columns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
