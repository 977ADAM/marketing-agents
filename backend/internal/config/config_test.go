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
