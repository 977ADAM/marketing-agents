package config_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	config "github.com/977ADAM/marketing-agents/internal/core/config"
)

// setEnv сбрасывает окружение и задаёт минимально валидную конфигурацию: ключ
// модели и учётные данные MariaDB (адрес БД по умолчанию собирается из MARIADB_*).
// Пары «переменная, значение» из kv накладываются сверху.
func setEnv(t *testing.T, kv ...string) {
	t.Helper()
	os.Clearenv()
	os.Setenv("DEEPSEEK_API_KEY", "sk-test")
	os.Setenv("MARIADB_USER", "marketing")
	os.Setenv("MARIADB_PASSWORD", "secret")
	for i := 0; i+1 < len(kv); i += 2 {
		os.Setenv(kv[i], kv[i+1])
	}
}

// load загружает конфиг и падает, если он не собрался.
func load(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	setEnv(t, "MARIADB_HOST", "db.internal", "CRITIC_MAX_ITER", "5")

	cfg := load(t)
	if want := "mysql://marketing:secret@db.internal:3306/marketing"; cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want)
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

// Адрес БД собирается из MARIADB_*: те же переменные понимает официальный образ
// MariaDB, поэтому один .env обслуживает и контейнер БД, и приложение.
func TestLoadDatabaseURLFromMariaDBVars(t *testing.T) {
	setEnv(t,
		"MARIADB_HOST", "127.0.0.1",
		"MARIADB_PORT", "3307",
		"MARIADB_DATABASE", "marketing_dev")

	cfg := load(t)
	if want := "mysql://marketing:secret@127.0.0.1:3307/marketing_dev"; cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want)
	}
}

// Спецсимволы в пароле не должны ломать URL.
func TestLoadDatabaseURLEscapesPassword(t *testing.T) {
	setEnv(t, "MARIADB_PASSWORD", "p@ss:word/1")

	cfg := load(t)
	if want := "mysql://marketing:p%40ss%3Aword%2F1@127.0.0.1:3306/marketing"; cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, want)
	}
}

// Явный DATABASE_URL имеет приоритет над сборкой из MARIADB_*.
func TestLoadDatabaseURLExplicitWins(t *testing.T) {
	setEnv(t, "DATABASE_URL", "mysql://app:app@db:3306/marketing")

	cfg := load(t)
	if cfg.DatabaseURL != "mysql://app:app@db:3306/marketing" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadRequiresAPIKey(t *testing.T) {
	setEnv(t)
	os.Unsetenv("DEEPSEEK_API_KEY")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DEEPSEEK_API_KEY missing")
	}
}

// Адрес БД обязателен: без DATABASE_URL и без пары MARIADB_* подсказываем, что задать.
func TestLoadRequiresDatabaseURL(t *testing.T) {
	setEnv(t)
	os.Unsetenv("MARIADB_USER")
	os.Unsetenv("MARIADB_PASSWORD")

	_, err := config.Load()
	if err == nil {
		t.Fatal("ожидалась ошибка про незаданный адрес БД")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "MARIADB_") {
		t.Errorf("err = %v, want подсказку про DATABASE_URL и MARIADB_*", err)
	}
}

// Прежние адреса — явная ошибка с подсказкой, а не молчаливый запуск не туда.
func TestLoadRejectsNonMariaDBURLs(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"sqlite-файл", "/var/lib/marketing/marketing.db", "SQLite"},
		{"sqlite-схема", "sqlite:marketing.db", "SQLite"},
		{"postgres", "postgres://app:app@db:5432/marketing?sslmode=disable", "MariaDB"},
		{"aws-стиль", "app:secret@tcp(db:3306)/marketing", "mysql://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, "DATABASE_URL", tc.url)

			_, err := config.Load()
			if err == nil {
				t.Fatal("ожидалась ошибка про неподходящий адрес БД")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want упоминание %s", err, tc.want)
			}
		})
	}
}

// Хост и имя базы обязательны: без них mysql://-адрес некуда подключать.
func TestLoadValidatesDatabaseURLParts(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"без хоста", "mysql://user:pass@/marketing", "хост"},
		{"без базы", "mysql://user:pass@db:3306/", "имя базы"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, "DATABASE_URL", tc.url)

			_, err := config.Load()
			if err == nil {
				t.Fatal("ожидалась ошибка валидации адреса БД")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want упоминание %s", err, tc.want)
			}
		})
	}
}

// Пароль не должен попадать в текст ошибки: сообщение уходит в логи.
func TestLoadDoesNotLeakPassword(t *testing.T) {
	setEnv(t, "DATABASE_URL", "postgres://app:supersecret@db:5432/marketing")

	_, err := config.Load()
	if err == nil {
		t.Fatal("ожидалась ошибка про неподходящий адрес БД")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("err = %v: пароль утёк в сообщение", err)
	}
}

func TestBasicAuthDefaults(t *testing.T) {
	setEnv(t)

	cfg := load(t)
	if cfg.BasicAuthUser != "" || cfg.BasicAuthPass != "" {
		t.Errorf("auth defaults should be empty, got %q/%q", cfg.BasicAuthUser, cfg.BasicAuthPass)
	}
}

// По умолчанию API слушает только loopback: локальный запуск не должен быть
// доступен из сети (basic-auth по умолчанию выключен).
func TestLoadDefaultHTTPAddrIsLoopback(t *testing.T) {
	setEnv(t)

	if cfg := load(t); cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want loopback 127.0.0.1:8080", cfg.HTTPAddr)
	}
}

// docker-compose переопределяет адрес на ":8080" — внутри контейнера нужно
// слушать все интерфейсы, иначе фронт не достучится до API.
func TestLoadHTTPAddrOverride(t *testing.T) {
	setEnv(t, "HTTP_ADDR", ":8080")

	if cfg := load(t); cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

// Wordstat не настроен — сервис всё равно поднимается, подбор тем выключен.
func TestWordstatDefaults(t *testing.T) {
	setEnv(t)

	cfg := load(t)
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
	setEnv(t,
		"WORDSTAT_MCP_URL", "https://example.test/wordstat-mcp/mcp",
		"WORDSTAT_MCP_USER", "admin",
		"WORDSTAT_MCP_PASS", "secret",
		"WORDSTAT_REGION_DEFAULT", "213",
		"WORDSTAT_MAX_CALLS_PER_RUN", "20")

	cfg := load(t)
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
			setEnv(t, "WORDSTAT_MCP_USER", tc.user, "WORDSTAT_MCP_PASS", tc.pass)

			_, err := config.Load()
			if err == nil {
				t.Fatal("ожидалась ошибка про неполную пару кред")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want упоминание %s", err, tc.want)
			}
		})
	}

	setEnv(t, "WORDSTAT_MCP_USER", "admin", "WORDSTAT_MCP_PASS", "secret")
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
			kv := make([]string, 0, len(tc.env)*2)
			for k, v := range tc.env {
				kv = append(kv, k, v)
			}
			setEnv(t, kv...)

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
	setEnv(t)

	cfg := load(t)
	if cfg.TraceMode != "full" {
		t.Errorf("TraceMode = %q, want full (по умолчанию агенты должны быть видны)", cfg.TraceMode)
	}
	if cfg.TraceRetentionDays != 30 {
		t.Errorf("TraceRetentionDays = %d, want 30", cfg.TraceRetentionDays)
	}
	if cfg.TraceMaxPayloadBytes != 262144 {
		t.Errorf("TraceMaxPayloadBytes = %d, want 262144", cfg.TraceMaxPayloadBytes)
	}
}

func TestTraceOverridesAndValidation(t *testing.T) {
	setEnv(t, "TRACE_MODE", "off", "TRACE_RETENTION_DAYS", "7", "TRACE_MAX_PAYLOAD_BYTES", "1024")

	cfg := load(t)
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

func TestDatabaseCredentialsRoundTrip(t *testing.T) {
	for _, secret := range []string{"with space", "plus+sign", "p@ss:word/1", "percent%"} {
		t.Run(secret, func(t *testing.T) {
			setEnv(t, "MARIADB_PASSWORD", secret)
			cfg := load(t)
			u, err := url.Parse(cfg.DatabaseURL)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := u.User.Password()
			if p != secret {
				t.Fatalf("roundtrip=%q", p)
			}
		})
	}
}
