// Package migrate — миграции схемы БД на библиотеке golang-migrate.
//
// Файлы миграций лежат рядом с пакетом (migrations/NNNN_name.up.sql и .down.sql)
// и вшиты в бинарь: сервису миграций не нужен смонтированный каталог с SQL, а
// приложению — доступ к файловой системе. Драйвер БД — database/sqlite
// (modernc.org/sqlite, чистый Go), тот же, что использует приложение, поэтому
// миграции идут по тому же соединению и с теми же pragma.
//
// Приложение миграции не применяет: это отдельный шаг — сервис migrate в
// docker-compose (см. docker-compose.yml) или `make migrate` локально. Сервер на
// старте только проверяет готовность схемы (CheckReady) и падает с понятной
// ошибкой, если схема не готова.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var files embed.FS

// Table — таблица учёта применённых миграций (формат golang-migrate: version, dirty).
const Table = "schema_migrations"

// LegacyTable — имя, под которое уезжает таблица учёта прежнего самописного
// раннера (schema_migrations с колонками name, applied_at).
const LegacyTable = "schema_migrations_legacy"

// FS отдаёт вшитые файлы миграций.
func FS() fs.FS {
	sub, err := fs.Sub(files, "migrations")
	if err != nil {
		// Каталог вшит в бинарь, поэтому ошибка возможна только при сборке.
		panic("migrate: " + err.Error())
	}
	return sub
}

// Up применяет все неприменённые миграции. Повторный запуск ничего не делает.
func Up(ctx context.Context, db *sql.DB) error {
	if _, _, err := AdoptLegacy(ctx, db); err != nil {
		return err
	}
	m, err := newMigrator(db)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Down откатывает одну миграцию — для локальной отладки; в compose не вызывается.
func Down(ctx context.Context, db *sql.DB) error {
	m, err := newMigrator(db)
	if err != nil {
		return err
	}
	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// Force помечает версию применённой, ничего не выполняя: нужно, чтобы вывести
// БД из «грязного» состояния после упавшей миграции.
func Force(ctx context.Context, db *sql.DB, version int) error {
	m, err := newMigrator(db)
	if err != nil {
		return err
	}
	if err := m.Force(version); err != nil {
		return fmt.Errorf("migrate force %d: %w", version, err)
	}
	return nil
}

// Version возвращает текущую версию схемы и признак «грязного» состояния.
// Ноль без ошибки означает, что не применено ни одной миграции.
func Version(ctx context.Context, db *sql.DB) (uint, bool, error) {
	m, err := newMigrator(db)
	if err != nil {
		return 0, false, err
	}
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("migrate version: %w", err)
	}
	return version, dirty, nil
}

// Latest — последняя версия среди вшитых миграций.
func Latest() (uint, error) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		return 0, fmt.Errorf("migrate: чтение вшитых миграций: %w", err)
	}
	var latest uint
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, ok := versionOf(e.Name())
		if !ok {
			return 0, fmt.Errorf("migrate: имя миграции %q не начинается с номера", e.Name())
		}
		if v > latest {
			latest = v
		}
	}
	if latest == 0 {
		return 0, errors.New("migrate: вшитых миграций не найдено")
	}
	return latest, nil
}

// CheckReady проверяет, что схема готова к работе приложения: миграции применены
// полностью и БД не в «грязном» состоянии. Возвращает применённую версию. Ничего
// не применяет — только объясняет, что делать, потому что применением занимается
// отдельный сервис.
func CheckReady(ctx context.Context, db *sql.DB) (uint, error) {
	version, dirty, err := Version(ctx, db)
	if err != nil {
		return 0, err
	}
	latest, err := Latest()
	if err != nil {
		return 0, err
	}
	switch {
	case dirty:
		return version, fmt.Errorf(
			"схема БД в «грязном» состоянии: миграция %d не завершилась; проверьте данные и выполните migrate force %d",
			version, version)
	case version == 0:
		return 0, errors.New("миграции не применены: запустите сервис migrate (docker compose up migrate) или make migrate")
	case version < latest:
		return version, fmt.Errorf("схема БД устарела: применено %d из %d — запустите сервис migrate (или make migrate)", version, latest)
	case version > latest:
		return version, fmt.Errorf("версия схемы %d новее вшитых миграций %d: приложение старее БД", version, latest)
	}
	return version, nil
}

// AdoptLegacy переносит учёт миграций из таблицы прежнего раннера (колонки
// name, applied_at) в формат golang-migrate (version, dirty). Сами миграции при
// этом не выполняются: схема уже создана, поэтому версия помечается через Force.
//
// Возвращает перенесённую версию и признак того, что перенос был сделан.
func AdoptLegacy(ctx context.Context, db *sql.DB) (uint, bool, error) {
	columns, err := tableColumns(ctx, db, Table)
	if err != nil {
		return 0, false, err
	}
	switch {
	case columns == nil:
		return 0, false, nil // таблицы нет: БД создаётся с нуля
	case columns["version"]:
		return 0, false, nil // уже формат golang-migrate
	case !columns["name"]:
		return 0, false, fmt.Errorf("migrate: таблица %s неизвестного формата (колонки: %s)",
			Table, strings.Join(sortedKeys(columns), ", "))
	}

	version, err := legacyVersion(ctx, db)
	if err != nil {
		return 0, false, err
	}
	latest, err := Latest()
	if err != nil {
		return 0, false, err
	}
	if version > latest {
		return 0, false, fmt.Errorf(
			"migrate: старая таблица учёта ссылается на версию %d, а известно максимум %d — обновите бинарь",
			version, latest)
	}
	if version == 0 {
		// Ничего не применялось — состояние переносить нечего.
		if _, err := db.ExecContext(ctx, `DROP TABLE `+Table); err != nil {
			return 0, false, fmt.Errorf("migrate: убрать пустую таблицу учёта: %w", err)
		}
		return 0, true, nil
	}

	if _, err := db.ExecContext(ctx, `ALTER TABLE `+Table+` RENAME TO `+LegacyTable); err != nil {
		return 0, false, fmt.Errorf("migrate: отложить старую таблицу учёта: %w", err)
	}
	m, err := newMigrator(db)
	if err != nil {
		return 0, false, err
	}
	if err := m.Force(int(version)); err != nil {
		return 0, false, fmt.Errorf("migrate: пометить версию %d применённой: %w", version, err)
	}
	return version, true, nil
}

// newMigrator собирает мигратор поверх существующего соединения.
//
// Close() у мигратора не вызываем намеренно: database/sqlite.Close() закрывает
// переданный *sql.DB (см. исходники драйвера), а соединение принадлежит
// приложению и живёт дольше одной операции.
func newMigrator(db *sql.DB) (*migrate.Migrate, error) {
	src, err := iofs.New(FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: источник миграций: %w", err)
	}
	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{MigrationsTable: Table})
	if err != nil {
		return nil, fmt.Errorf("migrate: драйвер sqlite: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return m, nil
}

// legacyVersion — максимальная версия, записанная прежним раннером.
func legacyVersion(ctx context.Context, db *sql.DB) (uint, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM `+Table)
	if err != nil {
		return 0, fmt.Errorf("migrate: чтение старой таблицы учёта: %w", err)
	}
	defer rows.Close()

	var latest uint
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, fmt.Errorf("migrate: чтение старой таблицы учёта: %w", err)
		}
		if v, ok := versionOf(name); ok && v > latest {
			latest = v
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("migrate: чтение старой таблицы учёта: %w", err)
	}
	return latest, nil
}

// versionOf достаёт номер из имени миграции: 0001_init.up.sql и 0001_init.sql → 1.
func versionOf(name string) (uint, bool) {
	base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql"), ".sql")
	i := strings.IndexByte(base, '_')
	if i <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(base[:i])
	if err != nil || n < 0 {
		return 0, false
	}
	return uint(n), true
}

// tableColumns возвращает набор колонок таблицы; nil — таблицы нет.
// Имя таблицы подставляется из констант пакета, а не из ввода пользователя.
func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
		return nil, fmt.Errorf("migrate: проверка таблицы %s: %w", table, err)
	}
	if exists == 0 {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("migrate: колонки таблицы %s: %w", table, err)
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
			return nil, fmt.Errorf("migrate: колонки таблицы %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: колонки таблицы %s: %w", table, err)
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
