package repository_test

import (
	"context"
	schema "github.com/977ADAM/marketing-agents/internal/core/repository/mariadb"
	"strings"
	"testing"
)

// CheckSchema: понятные отказы на неподготовленной БД и версия на готовой.
func TestCheckSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("таблицы учёта нет", func(t *testing.T) {
		st := newEmptyStore(t)
		if _, err := schema.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "не применены") {
			t.Errorf("текст ошибки = %q, ожидали подсказку про неприменённые миграции", err)
		}
	})

	t.Run("таблица учёта старого формата", func(t *testing.T) {
		st := newEmptyStore(t)
		if err := st.db.WithContext(ctx).
			Exec(`CREATE TABLE schema_migrations (name VARCHAR(128) PRIMARY KEY, applied_at DATETIME(3))`).Error; err != nil {
			t.Fatalf("старая таблица учёта: %v", err)
		}
		if _, err := schema.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: старая таблица учёта")
		} else if !strings.Contains(err.Error(), "старого формата") {
			t.Errorf("текст ошибки = %q, ожидали упоминание старого формата", err)
		}
	})

	t.Run("таблица учёта пуста", func(t *testing.T) {
		st := newEmptyStore(t)
		if err := st.db.WithContext(ctx).
			Exec(`CREATE TABLE schema_migrations (version varchar(128) PRIMARY KEY)`).Error; err != nil {
			t.Fatalf("таблица учёта: %v", err)
		}
		if _, err := schema.CheckSchema(ctx, st.db); err == nil {
			t.Fatal("ожидали отказ: миграции не применены")
		} else if !strings.Contains(err.Error(), "пуста") {
			t.Errorf("текст ошибки = %q, ожидали упоминание пустой таблицы", err)
		}
	})

	t.Run("готово", func(t *testing.T) {
		st := newTestStore(t)
		version, err := schema.CheckSchema(ctx, st.db)
		if err != nil {
			t.Fatalf("CheckSchema: %v", err)
		}
		if version != "0005" {
			t.Errorf("версия схемы = %q, want 0005", version)
		}
	})
}

func TestSchemaRequiresEveryMigration(t *testing.T) {
	st := newTestStore(t)
	if err := st.db.Exec("DELETE FROM schema_migrations WHERE version='0002'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := schema.CheckSchema(context.Background(), st.db); err == nil {
		t.Fatal("partial migration set accepted")
	}
}
