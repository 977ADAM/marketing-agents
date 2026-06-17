package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://localhost/x")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")
	os.Setenv("CRITIC_MAX_ITER", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost/x" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
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

func TestLoadRequiresAPIKey(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://localhost/x")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DEEPSEEK_API_KEY missing")
	}
}
