package mariadb

import (
	"context"
	"errors"
	"fmt"
	"github.com/977ADAM/marketing-agents/migrations"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// schemaMigrationRow — таблица учёта миграций dbmate: version varchar primary key.
type schemaMigrationRow struct {
	Version string `gorm:"column:version;type:varchar(255);primaryKey"`
}

func (schemaMigrationRow) TableName() string { return "schema_migrations" }

// CheckSchema проверяет, что схема БД готова к работе приложения: таблица учёта
// миграций dbmate существует и не пуста. Возвращает применённую версию
// (например «0002»).
//
// Миграции применяет отдельный сервис (в compose — migrate, локально — make
// migrate), поэтому сервер их не выполняет: он отказывается стартовать на
// неподготовленной БД и объясняет, что делать.
func CheckSchema(ctx context.Context, db *gorm.DB) (string, error) {
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable("schema_migrations") {
		return "", errors.New("миграции не применены: запустите сервис migrate (docker compose run --rm migrate) или make migrate")
	}

	columns, err := tableColumns(ctx, db, "schema_migrations")
	if err != nil {
		return "", err
	}
	if !columns["version"] {
		return "", fmt.Errorf(
			"таблица учёта миграций старого формата (колонки: %s): переименуйте её и примените миграции сервисом migrate",
			strings.Join(sortedKeys(columns), ", "))
	}

	var row schemaMigrationRow
	err = db.WithContext(ctx).Order("version DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", errors.New("миграции не применены: таблица учёта пуста, запустите сервис migrate")
	}
	if err != nil {
		return "", fmt.Errorf("mariadb: чтение версии схемы: %w", err)
	}
	var versions []string
	if err := db.WithContext(ctx).Model(&schemaMigrationRow{}).Pluck("version", &versions).Error; err != nil {
		return "", fmt.Errorf("mariadb: migration manifest: %w", err)
	}
	found := map[string]bool{}
	for _, v := range versions {
		found[v] = true
	}
	for _, v := range migrations.RequiredVersions {
		if !found[v] {
			return "", fmt.Errorf("миграция %s не применена: запустите migrate", v)
		}
	}
	return row.Version, nil
}

// tableColumns возвращает набор колонок таблицы. Имя таблицы подставляется из
// константы в коде, а не из ввода пользователя.
func tableColumns(ctx context.Context, db *gorm.DB, table string) (map[string]bool, error) {
	types, err := db.WithContext(ctx).Migrator().ColumnTypes(table)
	if err != nil {
		return nil, fmt.Errorf("mariadb: колонки таблицы %s: %w", table, err)
	}
	columns := make(map[string]bool, len(types))
	for _, t := range types {
		columns[strings.ToLower(t.Name())] = true
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
