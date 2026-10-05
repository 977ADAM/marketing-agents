package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/config"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("SQLITE_PATH", "/tmp/marketing-test.db")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")
	os.Setenv("CRITIC_MAX_ITER", "5")

	cfg, err := config.Load()
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

	cfg, err := config.Load()
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
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DEEPSEEK_API_KEY missing")
	}
}

// Прежняя переменная DATABASE_URL продолжает работать, если это путь к файлу.
func TestLoadAcceptsLegacyDatabaseURLPath(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "/var/lib/marketing/marketing.db")
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")

	cfg, err := config.Load()
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

	_, err := config.Load()
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

	cfg, err := config.Load()
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

	cfg, err := config.Load()
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

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

// Wordstat не настроен — сервис всё равно поднимается, подбор тем выключен,
// а дефолты порогов и множителя тем уже проставлены.
func TestWordstatDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WordstatMCPURL != "" {
		t.Errorf("WordstatMCPURL = %q, want пусто", cfg.WordstatMCPURL)
	}
	if cfg.WordstatRegionDefault != "225" {
		t.Errorf("WordstatRegionDefault = %q, want 225", cfg.WordstatRegionDefault)
	}
	if cfg.WordstatMaxCallsPerRun != 60 {
		t.Errorf("WordstatMaxCallsPerRun = %d, want 60", cfg.WordstatMaxCallsPerRun)
	}
}

func TestWordstatOverrides(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")
	os.Setenv("WORDSTAT_MCP_URL", "https://example.test/wordstat-mcp/mcp")
	os.Setenv("WORDSTAT_MCP_USER", "admin")
	os.Setenv("WORDSTAT_MCP_PASS", "secret")
	os.Setenv("WORDSTAT_REGION_DEFAULT", "213")
	os.Setenv("WORDSTAT_MAX_CALLS_PER_RUN", "20")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WordstatMCPURL != "https://example.test/wordstat-mcp/mcp" {
		t.Errorf("WordstatMCPURL = %q", cfg.WordstatMCPURL)
	}
	if cfg.WordstatMCPUser != "admin" || cfg.WordstatMCPPass != "secret" {
		t.Errorf("креды = %q/%q", cfg.WordstatMCPUser, cfg.WordstatMCPPass)
	}
	if cfg.WordstatRegionDefault != "213" {
		t.Errorf("WordstatRegionDefault = %q, want 213", cfg.WordstatRegionDefault)
	}
	if cfg.WordstatMaxCallsPerRun != 20 {
		t.Errorf("WordstatMaxCallsPerRun = %d, want 20", cfg.WordstatMaxCallsPerRun)
	}
}

// Креды Wordstat задаются парой: половина пары — почти наверняка опечатка.
func TestWordstatAuthPairing(t *testing.T) {
	for _, tc := range []struct {
		name string
		user string
		pass string
		want string
	}{
		{"только логин", "admin", "", "WORDSTAT_MCP_PASS"},
		{"только пароль", "", "secret", "WORDSTAT_MCP_USER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			os.Clearenv()
			os.Setenv("DEEPSEEK_API_KEY", "k")
			os.Setenv("WORDSTAT_MCP_USER", tc.user)
			os.Setenv("WORDSTAT_MCP_PASS", tc.pass)

			_, err := config.Load()
			if err == nil {
				t.Fatal("ожидалась ошибка про неполную пару кред")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want упоминание %s", err, tc.want)
			}
		})
	}

	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")
	os.Setenv("WORDSTAT_MCP_USER", "admin")
	os.Setenv("WORDSTAT_MCP_PASS", "secret")
	if _, err := config.Load(); err != nil {
		t.Errorf("полная пара кред должна приниматься: %v", err)
	}
}

func TestWordstatValidation(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"url без схемы", map[string]string{"WORDSTAT_MCP_URL": "example.test/mcp"}, "WORDSTAT_MCP_URL"},
		{"регион не число", map[string]string{"WORDSTAT_REGION_DEFAULT": "Москва"}, "WORDSTAT_REGION_DEFAULT"},
		{"пустой регион", map[string]string{"WORDSTAT_REGION_DEFAULT": ""}, "WORDSTAT_REGION_DEFAULT"},
		{"нулевой лимит вызовов", map[string]string{"WORDSTAT_MAX_CALLS_PER_RUN": "0"}, "WORDSTAT_MAX_CALLS_PER_RUN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Clearenv()
			os.Setenv("DEEPSEEK_API_KEY", "k")
			for k, v := range tc.env {
				os.Setenv(k, v)
			}

			_, err := config.Load()
			if err == nil {
				t.Fatal("ожидалась ошибка валидации")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want упоминание %s", err, tc.want)
			}
		})
	}
}

// Трасса: дефолты и проверка режима.
func TestTraceDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TraceMode != "summary" {
		t.Errorf("TraceMode = %q, want summary", cfg.TraceMode)
	}
	if cfg.TraceRetentionDays != 30 {
		t.Errorf("TraceRetentionDays = %d, want 30", cfg.TraceRetentionDays)
	}
	if cfg.TraceMaxPayloadBytes != 32768 {
		t.Errorf("TraceMaxPayloadBytes = %d, want 32768", cfg.TraceMaxPayloadBytes)
	}
}

func TestTraceOverridesAndValidation(t *testing.T) {
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "k")
	os.Setenv("TRACE_MODE", "off")
	os.Setenv("TRACE_RETENTION_DAYS", "7")
	os.Setenv("TRACE_MAX_PAYLOAD_BYTES", "1024")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TraceMode != "off" || cfg.TraceRetentionDays != 7 || cfg.TraceMaxPayloadBytes != 1024 {
		t.Errorf("переопределения не применились: %+v", cfg)
	}

	// Опечатка в режиме — ошибка конфига, а не молчаливое «выключено».
	os.Setenv("TRACE_MODE", "verbose")
	if _, err := config.Load(); err == nil {
		t.Fatal("ожидалась ошибка на неизвестном режиме трассы")
	}

	os.Setenv("TRACE_MODE", "summary")
	os.Setenv("TRACE_MAX_PAYLOAD_BYTES", "0")
	if _, err := config.Load(); err == nil {
		t.Fatal("ожидалась ошибка на нулевом лимите payload")
	}
}
