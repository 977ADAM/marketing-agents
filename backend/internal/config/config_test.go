package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("SQLITE_PATH", "/tmp/marketing-test.db")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")
	os.Setenv("CRITIC_MAX_ITER", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SQLitePath != "/tmp/marketing-test.db" {
		t.Errorf("SQLitePath = %q", cfg.SQLitePath)
	}
	if cfg.ModelDefault != "deepseek-v4-pro" {
		t.Errorf("ModelDefault = %q, want default", cfg.ModelDefault)
	}
	if cfg.CriticMaxIter != 5 {
		t.Errorf("CriticMaxIter = %d, want 5", cfg.CriticMaxIter)
	}
	if cfg.RunTimeout != 10*time.Minute {
		t.Errorf("RunTimeout = %v, want default 10m", cfg.RunTimeout)
	}
}

func TestLoadDefaultSQLitePath(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SQLitePath != "data/marketing.db" {
		t.Errorf("SQLitePath = %q, want default data/marketing.db", cfg.SQLitePath)
	}
}

func TestLoadRequiresAPIKey(t *testing.T) {
	os.Clearenv()
	os.Setenv("SQLITE_PATH", "/tmp/x.db")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DEEPSEEK_API_KEY missing")
	}
}

// Прежняя переменная DATABASE_URL продолжает работать, если это путь к файлу.
func TestLoadAcceptsLegacyDatabaseURLPath(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "/var/lib/marketing/marketing.db")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SQLitePath != "/var/lib/marketing/marketing.db" {
		t.Errorf("SQLitePath = %q", cfg.SQLitePath)
	}
}

// Строка подключения к Postgres — явная ошибка с подсказкой, а не тихий sqlite-файл.
func TestLoadRejectsPostgresDSN(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://app:app@db:5432/marketing?sslmode=disable")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for postgres DSN")
	}
	if !strings.Contains(err.Error(), "SQLITE_PATH") {
		t.Errorf("err = %v, want hint about SQLITE_PATH", err)
	}
}

func TestBasicAuthDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BasicAuthUser != "" || cfg.BasicAuthPass != "" {
		t.Errorf("auth defaults should be empty, got %q/%q", cfg.BasicAuthUser, cfg.BasicAuthPass)
	}
}

// По умолчанию API слушает только loopback: локальный запуск не должен быть
// доступен из сети (basic-auth по умолчанию выключен).
func TestLoadDefaultHTTPAddrIsLoopback(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want loopback 127.0.0.1:8080", cfg.HTTPAddr)
	}
}

// docker-compose переопределяет адрес на ":8080" — внутри контейнера нужно
// слушать все интерфейсы, иначе фронт не достучится до API.
func TestLoadHTTPAddrOverride(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")
	os.Setenv("HTTP_ADDR", ":8080")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}
