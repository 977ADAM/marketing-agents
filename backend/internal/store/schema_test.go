package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/store"
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

	t.Run("готово", func(t *testing.T) {
		st := newTestStore(t)
		version, err := store.CheckSchema(ctx, st.DB())
		if err != nil {
			t.Fatalf("CheckSchema: %v", err)
		}
		if version != 2 {
			t.Errorf("версия схемы = %d, want 2", version)
		}
	})

	t.Run("грязное состояние", func(t *testing.T) {
		st := newTestStore(t)
		if _, err := st.DB().ExecContext(ctx, `UPDATE schema_migrations SET dirty = 1`); err != nil {
			t.Fatalf("dirty: %v", err)
		}
		if _, err := store.CheckSchema(ctx, st.DB()); err == nil {
			t.Fatal("ожидали отказ: грязное состояние")
		} else if !strings.Contains(err.Error(), "грязном") {
			t.Errorf("текст ошибки = %q, ожидали упоминание грязного состояния", err)
		}
	})
}

// openStore открывает БД без применения схемы: нужна для негативных проверок.
func openStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	return store.Open(context.Background(), filepath.Join(t.TempDir(), "schema.db"))
}
