package mariadb_test

import (
	"context"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/repository/mariadb"
)

// CheckSchema: понятные отказы на неподготовленной БД и версия на готовой.
func TestCheckSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("таблицы учёта нет", func(t *testing.T) {
		st := newEmptyStore(t)
		if _, err := mariadb.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "не применены") {
			t.Errorf("текст ошибки = %q, ожидали подсказку про неприменённые миграции", err)
		}
	})

	t.Run("таблица учёта старого формата", func(t *testing.T) {
		st := newEmptyStore(t)
		if _, err := st.db.ExecContext(ctx,
			`CREATE TABLE schema_migrations (name VARCHAR(128) PRIMARY KEY, applied_at DATETIME(3))`); err != nil {
			t.Fatalf("старая таблица учёта: %v", err)
		}
		if _, err := mariadb.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: старая таблица учёта")
		} else if !strings.Contains(err.Error(), "старого формата") {
			t.Errorf("текст ошибки = %q, ожидали упоминание старого формата", err)
		}
	})

	t.Run("таблица учёта пуста", func(t *testing.T) {
		st := newEmptyStore(t)
		if _, err := st.db.ExecContext(ctx,
			`CREATE TABLE schema_migrations (version varchar(128) PRIMARY KEY)`); err != nil {
			t.Fatalf("таблица учёта: %v", err)
		}
		if _, err := mariadb.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "пуста") {
			t.Errorf("текст ошибки = %q, ожидали упоминание пустой таблицы", err)
		}
	})

	t.Run("готово", func(t *testing.T) {
		st := newTestStore(t)
		version, err := mariadb.CheckSchema(ctx, st.db)
		if err != nil {
			t.Fatalf("CheckSchema: %v", err)
		}
		if version != "0002" {
			t.Errorf("версия схемы = %q, want 0002", version)
		}
	})
}
