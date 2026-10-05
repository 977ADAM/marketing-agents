package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/store"
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

// CheckSchema: понятные отказы на неподготовленной БД и версия на готовой.
func TestCheckSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("таблицы учёта нет", func(t *testing.T) {
		st, err := openStore(t)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := store.CheckSchema(ctx, st.DB()); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "не применены") {
			t.Errorf("текст ошибки = %q, ожидали подсказку про неприменённые миграции", err)
		}
	})

	t.Run("таблица учёта старого формата", func(t *testing.T) {
		st, err := openStore(t)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := st.DB().ExecContext(ctx,
			`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at DATETIME)`); err != nil {
			t.Fatalf("старая таблица учёта: %v", err)
		}
		if _, err := store.CheckSchema(ctx, st.DB()); err == nil {
			t.Fatal("ожидали отказ: старая таблица учёта")
		} else if !strings.Contains(err.Error(), "старого формата") {
			t.Errorf("текст ошибки = %q, ожидали упоминание старого формата", err)
		}
	})

	t.Run("таблица учёта пуста", func(t *testing.T) {
		st, err := openStore(t)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := st.DB().ExecContext(ctx,
			`CREATE TABLE schema_migrations (version varchar(128) PRIMARY KEY)`); err != nil {
			t.Fatalf("таблица учёта: %v", err)
		}
		if _, err := store.CheckSchema(ctx, st.DB()); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "пуста") {
			t.Errorf("текст ошибки = %q, ожидали упоминание пустой таблицы", err)
		}
	})

	t.Run("готово", func(t *testing.T) {
		st := newTestStore(t)
		version, err := store.CheckSchema(ctx, st.DB())
		if err != nil {
			t.Fatalf("CheckSchema: %v", err)
		}
		if version != "0002" {
			t.Errorf("версия схемы = %q, want 0002", version)
		}
	})
}

// openStore открывает БД без применения схемы: нужна для негативных проверок.
func openStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	return store.Open(context.Background(), filepath.Join(t.TempDir(), "schema.db"))
}
