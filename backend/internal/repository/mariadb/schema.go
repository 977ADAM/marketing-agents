package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CheckSchema проверяет, что схема БД готова к работе приложения: таблица учёта
// миграций dbmate существует и не пуста. Возвращает применённую версию
// (например «0002»).
//
// Миграции применяет отдельный сервис (в compose — migrate, локально — make
// migrate), поэтому сервер их не выполняет: он отказывается стартовать на
// неподготовленной БД и объясняет, что делать.
func CheckSchema(ctx context.Context, db *sql.DB) (string, error) {
	columns, err := tableColumns(ctx, db, "schema_migrations")
	if err != nil {
		return "", err
	}
	switch {
	case columns == nil:
		return "", errors.New("миграции не применены: запустите сервис migrate (docker compose run --rm migrate) или make migrate")
	case !columns["version"]:
		return "", fmt.Errorf(
			"таблица учёта миграций старого формата (колонки: %s): переименуйте её и примените миграции сервисом migrate",
			strings.Join(sortedKeys(columns), ", "))
	}

	var version string
	if err := db.QueryRowContext(ctx,
		`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("миграции не применены: таблица учёта пуста, запустите сервис migrate")
		}
		return "", fmt.Errorf("mariadb: чтение версии схемы: %w", err)
	}
	return version, nil
}

// tableColumns возвращает набор колонок таблицы; nil — таблицы нет.
// Имя таблицы подставляется из константы в коде, а не из ввода пользователя.
func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&exists); err != nil {
		return nil, fmt.Errorf("mariadb: проверка таблицы %s: %w", table, err)
	}
	if exists == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = ?`, table)
	if err != nil {
		return nil, fmt.Errorf("mariadb: колонки таблицы %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("mariadb: колонки таблицы %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mariadb: колонки таблицы %s: %w", table, err)
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
