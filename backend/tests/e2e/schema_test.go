package e2e_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// applyMigrations готовит схему так же, как сервис migrate (апстрим-CLI
// golang-migrate): выполняет backend/migrations/*.up.sql по порядку и заводит
// таблицу учёта. Библиотеку миграций тесты не тянут — её нет в зависимостях.
func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("поиск миграций: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("миграции не найдены: ожидались backend/migrations/*.up.sql")
	}
	sort.Strings(files)

	var latest int64
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("чтение %s: %v", file, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("применение %s: %v", file, err)
		}
		if v, ok := versionOf(file); ok {
			latest = v
		}
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		dirty   BOOLEAN NOT NULL
	)`); err != nil {
		t.Fatalf("таблица учёта миграций: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO schema_migrations (version, dirty) VALUES (?, 0)`, latest); err != nil {
		t.Fatalf("версия схемы: %v", err)
	}
}

// versionOf достаёт номер из имени файла миграции: 0001_init.up.sql → 1.
func versionOf(name string) (int64, bool) {
	base := filepath.Base(name)
	i := strings.IndexByte(base, '_')
	if i <= 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(base[:i], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
