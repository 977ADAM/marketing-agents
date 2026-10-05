package migrate_test

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/migrate"

	_ "modernc.org/sqlite"
)

// openDB открывает отдельный файл БД под тест (тот же драйвер, что у приложения).
func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite",
		"file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

// execSQL из вшитых файлов: нужно, чтобы собрать состояние «до golang-migrate».
func execSQL(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	body, err := fs.ReadFile(migrate.FS(), name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("exec %s: %v", name, err)
	}
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// Свежая БД: до миграций CheckReady объясняет, что делать; после Up схема готова.
func TestUpPreparesFreshDatabase(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/fresh.db")

	if _, err := migrate.CheckReady(ctx, db); err == nil {
		t.Fatal("CheckReady на пустой БД должен ругаться, что миграции не применены")
	} else if !strings.Contains(err.Error(), "не применены") {
		t.Errorf("текст ошибки = %q, ожидали подсказку про неприменённые миграции", err)
	}

	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	version, dirty, err := migrate.Version(ctx, db)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	latest, err := migrate.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if version != latest || dirty {
		t.Errorf("после Up версия %d dirty=%v, want %d dirty=false", version, dirty, latest)
	}
	if got, err := migrate.CheckReady(ctx, db); err != nil || got != latest {
		t.Errorf("CheckReady = %d, %v; want %d, nil", got, err, latest)
	}

	for _, table := range []string{"clients", "campaigns", "deliverables", "reviews", "run_events"} {
		if n := count(t, db, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='`+table+`'`); n != 1 {
			t.Errorf("таблица %s не создана", table)
		}
	}
}

// Повторный Up безопасен и не переписывает учёт.
func TestUpIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/twice.db")

	for i := 1; i <= 3; i++ {
		if err := migrate.Up(ctx, db); err != nil {
			t.Fatalf("Up #%d: %v", i, err)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM schema_migrations`); n != 1 {
		t.Errorf("строк в schema_migrations = %d, want 1", n)
	}
	latest, _ := migrate.Latest()
	if version, _, _ := migrate.Version(ctx, db); version != latest {
		t.Errorf("версия %d, want %d", version, latest)
	}
}

// БД, созданная прежним самописным раннером: учёт переносится, миграции повторно
// не выполняются, данные остаются на месте.
func TestAdoptLegacyRunnerTable(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/legacy.db")

	// Состояние «до»: схема применена старым раннером, учёт — в его таблице.
	execSQL(t, db, "0001_init.up.sql")
	execSQL(t, db, "0002_run_events.up.sql")
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now'))
	)`); err != nil {
		t.Fatalf("create legacy schema_migrations: %v", err)
	}
	for _, name := range []string{"0001_init.sql", "0002_run_events.sql"} {
		if _, err := db.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO campaigns (id, client_id, status, brief) VALUES ('c-1', ?, 'done', '{}')`,
		"00000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatalf("insert campaign: %v", err)
	}

	version, adopted, err := migrate.AdoptLegacy(ctx, db)
	if err != nil {
		t.Fatalf("AdoptLegacy: %v", err)
	}
	if !adopted || version != 2 {
		t.Fatalf("AdoptLegacy = (%d, %v), want (2, true)", version, adopted)
	}
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up после переноса: %v", err)
	}

	if n := count(t, db, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations_legacy'`); n != 1 {
		t.Error("старая таблица учёта не сохранена под именем schema_migrations_legacy")
	}
	if n := count(t, db, `SELECT count(*) FROM schema_migrations`); n != 1 {
		t.Errorf("строк в schema_migrations = %d, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM campaigns WHERE id='c-1'`); n != 1 {
		t.Error("данные не пережили перенос учёта")
	}
	if _, err := migrate.CheckReady(ctx, db); err != nil {
		t.Errorf("CheckReady после переноса: %v", err)
	}
	// Второй запуск ничего не переносит и не падает.
	if _, adopted, err := migrate.AdoptLegacy(ctx, db); err != nil || adopted {
		t.Errorf("повторный AdoptLegacy = (_, %v, %v), want (_, false, nil)", adopted, err)
	}
}

// Старая таблица учёта без записей: переносить нечего, схема создаётся с нуля.
func TestAdoptLegacyEmptyTable(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/legacy-empty.db")

	if _, err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at DATETIME)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	version, adopted, err := migrate.AdoptLegacy(ctx, db)
	if err != nil {
		t.Fatalf("AdoptLegacy: %v", err)
	}
	if !adopted || version != 0 {
		t.Fatalf("AdoptLegacy = (%d, %v), want (0, true)", version, adopted)
	}
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up: %v", err)
	}
	latest, _ := migrate.Latest()
	if v, _, _ := migrate.Version(ctx, db); v != latest {
		t.Errorf("версия %d, want %d", v, latest)
	}
}

// Устаревшая схема и «грязное» состояние — понятные ошибки, а не молчаливый старт.
func TestCheckReadyExplainsProblems(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/problems.db")
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if _, err := db.Exec(`UPDATE schema_migrations SET version = 1`); err != nil {
		t.Fatalf("set version: %v", err)
	}
	if _, err := migrate.CheckReady(ctx, db); err == nil || !strings.Contains(err.Error(), "устарела") {
		t.Errorf("CheckReady при отстающей схеме = %v, ожидали «устарела»", err)
	}

	if _, err := db.Exec(`UPDATE schema_migrations SET version = 99`); err != nil {
		t.Fatalf("set version: %v", err)
	}
	if _, err := migrate.CheckReady(ctx, db); err == nil || !strings.Contains(err.Error(), "новее") {
		t.Errorf("CheckReady при версии новее бинаря = %v, ожидали «новее»", err)
	}

	if _, err := db.Exec(`UPDATE schema_migrations SET version = 2, dirty = 1`); err != nil {
		t.Fatalf("set dirty: %v", err)
	}
	if _, err := migrate.CheckReady(ctx, db); err == nil || !strings.Contains(err.Error(), "грязном") {
		t.Errorf("CheckReady при dirty = %v, ожидали «грязном»", err)
	}
}

// Down откатывает последнюю миграцию, повторный Up возвращает схему.
func TestDownRevertsLastMigration(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir()+"/down.db")
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if err := migrate.Down(ctx, db); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='run_events'`); n != 0 {
		t.Error("run_events не удалена откатом")
	}
	if version, _, _ := migrate.Version(ctx, db); version != 1 {
		t.Errorf("версия после отката %d, want 1", version)
	}

	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up после отката: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='run_events'`); n != 1 {
		t.Error("run_events не вернулась повторным Up")
	}
}
