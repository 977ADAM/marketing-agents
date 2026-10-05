package e2e_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// applyMigrations готовит схему так же, как сервис migrate (образ dbmate):
// применяет секции `-- migrate:up` из backend/migrations/*.sql по порядку и
// заводит таблицу учёта версий.
func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("поиск миграций: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("миграции не найдены: ожидались backend/migrations/*.sql")
	}
	sort.Strings(files)

	versions := make([]string, 0, len(files))
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("чтение %s: %v", file, err)
		}
		up, ok := upSection(string(body))
		if !ok {
			t.Fatalf("%s: нет секции -- migrate:up", file)
		}
		if _, err := db.Exec(up); err != nil {
			t.Fatalf("применение %s: %v", file, err)
		}
		versions = append(versions, versionOf(file))
	}

	// Таблица учёта — та же, что создаёт dbmate (version varchar primary key).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version varchar(128) PRIMARY KEY
	)`); err != nil {
		t.Fatalf("таблица учёта миграций: %v", err)
	}
	for _, v := range versions {
		if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)`, v); err != nil {
			t.Fatalf("версия схемы %s: %v", v, err)
		}
	}
}

// upSection вырезает SQL между `-- migrate:up` и `-- migrate:down`.
func upSection(body string) (string, bool) {
	const upMarker, downMarker = "-- migrate:up", "-- migrate:down"
	i := strings.Index(body, upMarker)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(upMarker):]
	if j := strings.Index(rest, downMarker); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

// versionOf — версия миграции: ведущие цифры имени файла (0001_init.sql → 0001).
func versionOf(name string) string {
	base := filepath.Base(name)
	i := 0
	for i < len(base) && base[i] >= '0' && base[i] <= '9' {
		i++
	}
	return base[:i]
}
