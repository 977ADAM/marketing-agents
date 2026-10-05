// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config — конфигурация сервиса, собранная из переменных окружения.
type Config struct {
	HTTPAddr string
	// DatabaseURL — адрес MariaDB: mysql://user:pass@host:port/dbname.
	// Задаётся напрямую или собирается из MARIADB_* (см. databaseURL).
	DatabaseURL  string
	APIKey       string
	BaseURL      string
	ModelDefault string // сильная модель: стратег и критик
	ModelFast    string // быстрая/дешёвая модель: копирайтеры

	LLMMaxRetries        int
	RunTimeout           time.Duration
	CriticMaxIter        int
	CriticScoreThreshold int
	MaxTopics            int // верхний кап на число тем от стратега (контроль стоимости)

	CostPer1KPrompt     float64
	CostPer1KCompletion float64

	RateLimitPerMin int
	LogLevel        string

	BasicAuthUser string
	BasicAuthPass string

	// Wordstat — подбор тем по поисковому спросу через MCP-сервер.
	// Пустой URL означает, что подбор выключен: темы генерирует стратег, как раньше.
	WordstatMCPURL         string
	WordstatMCPUser        string
	WordstatMCPPass        string
	WordstatRegionDefault  string // geo ID Яндекса: 225 Россия, 1 Москва и область, 213 Москва
	WordstatMaxCallsPerRun int    // лимит обращений к Wordstat на прогон

	// Трасса прогона: журнал того, что делали агенты.
	TraceMode            string // off | summary | full
	TraceRetentionDays   int    // срок хранения событий
	TraceMaxPayloadBytes int    // обрезка одного payload
}

// DefaultHTTPAddr — адрес прослушивания по умолчанию: только loopback, чтобы
// локальный запуск не торчал в сеть (у API по умолчанию выключен basic-auth).
// В docker-compose переменная переопределяется на ":8080": внутри контейнера
// слушать нужно все интерфейсы, иначе фронт не достучится до бэкенда по
// внутренней сети, а проброс порта не заработает. Наружу контейнер при этом
// не выставлен — порт публикуется только на 127.0.0.1 (см. docker-compose.yml).
const DefaultHTTPAddr = "127.0.0.1:8080"

// Значения по умолчанию для сборки DATABASE_URL из MARIADB_*: локальный запуск
// без Docker ходит в контейнер MariaDB, опубликованный на loopback.
const (
	DefaultMariaDBHost     = "127.0.0.1"
	DefaultMariaDBPort     = "3306"
	DefaultMariaDBDatabase = "marketing"
)

// DefaultWordstatRegion — регион по умолчанию для подбора тем: 225 — Россия.
const DefaultWordstatRegion = "225"

// Режимы трассы прогона. Конфиг знает свои значения сам и не тянет доменный
// пакет trace: в trace.Mode его переводит composition root (cmd/server).
const (
	TraceModeOff     = "off"
	TraceModeSummary = "summary"
	TraceModeFull    = "full"
)

// DefaultTraceMaxPayloadBytes — лимит одного payload в режиме full.
const DefaultTraceMaxPayloadBytes = 32 << 10

// Load читает env, подставляет дефолты и валидирует обязательные поля.
func Load() (*Config, error) {
	// .env опционален: если файла нет — читаем только реальное окружение.
	_ = godotenv.Load()

	dbURL := databaseURL()

	cfg := &Config{
		HTTPAddr:     getStr("HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL:  dbURL,
		APIKey:       getStr("DEEPSEEK_API_KEY", ""),
		BaseURL:      getStr("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		ModelDefault: getStr("MODEL_DEFAULT", "deepseek-v4-pro"),
		ModelFast:    getStr("MODEL_FAST", "deepseek-v4-flash"),
		LogLevel:     getStr("LOG_LEVEL", "info"),

		BasicAuthUser: getStr("BASIC_AUTH_USER", ""),
		BasicAuthPass: getStr("BASIC_AUTH_PASS", ""),

		LLMMaxRetries:        getInt("LLM_MAX_RETRIES", 3),
		RunTimeout:           getDur("RUN_TIMEOUT", 10*time.Minute),
		CriticMaxIter:        getInt("CRITIC_MAX_ITER", 3),
		CriticScoreThreshold: getInt("CRITIC_SCORE_THRESHOLD", 80),
		MaxTopics:            getInt("MAX_TOPICS", 5),
		RateLimitPerMin:      getInt("RATE_LIMIT_PER_MIN", 30),

		CostPer1KPrompt:     getFloat("COST_PER_1K_PROMPT", 0.00027),
		CostPer1KCompletion: getFloat("COST_PER_1K_COMPLETION", 0.0011),

		WordstatMCPURL:         getStr("WORDSTAT_MCP_URL", ""),
		WordstatMCPUser:        getStr("WORDSTAT_MCP_USER", ""),
		WordstatMCPPass:        getStr("WORDSTAT_MCP_PASS", ""),
		WordstatRegionDefault:  getStr("WORDSTAT_REGION_DEFAULT", DefaultWordstatRegion),
		WordstatMaxCallsPerRun: getInt("WORDSTAT_MAX_CALLS_PER_RUN", 60),

		TraceMode:            getStr("TRACE_MODE", TraceModeSummary),
		TraceRetentionDays:   getInt("TRACE_RETENTION_DAYS", 30),
		TraceMaxPayloadBytes: getInt("TRACE_MAX_PAYLOAD_BYTES", DefaultTraceMaxPayloadBytes),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate проверяет обязательные и диапазонные ограничения.
func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY не задан")
	}
	if c.HTTPAddr == "" {
		return fmt.Errorf("HTTP_ADDR не может быть пустым")
	}
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		return err
	}
	if c.BaseURL == "" {
		return fmt.Errorf("DEEPSEEK_BASE_URL не может быть пустым")
	}

	if (c.BasicAuthUser == "") != (c.BasicAuthPass == "") {
		return fmt.Errorf("BASIC_AUTH_USER и BASIC_AUTH_PASS должны быть заданы вместе")
	}

	if c.LLMMaxRetries < 0 {
		return fmt.Errorf("LLM_MAX_RETRIES должен быть >= 0, получено %d", c.LLMMaxRetries)
	}
	if c.RunTimeout <= 0 {
		return fmt.Errorf("RUN_TIMEOUT должен быть > 0, получено %s", c.RunTimeout)
	}
	if c.CriticMaxIter < 0 {
		return fmt.Errorf("CRITIC_MAX_ITER должен быть >= 0, получено %d", c.CriticMaxIter)
	}
	if c.CriticScoreThreshold < 0 || c.CriticScoreThreshold > 100 {
		return fmt.Errorf("CRITIC_SCORE_THRESHOLD должен быть в диапазоне 0..100, получено %d", c.CriticScoreThreshold)
	}
	if c.MaxTopics <= 0 {
		return fmt.Errorf("MAX_TOPICS должен быть > 0, получено %d", c.MaxTopics)
	}
	if c.RateLimitPerMin < 0 {
		return fmt.Errorf("RATE_LIMIT_PER_MIN должен быть >= 0, получено %d", c.RateLimitPerMin)
	}
	if c.CostPer1KPrompt < 0 {
		return fmt.Errorf("COST_PER_1K_PROMPT должен быть >= 0, получено %v", c.CostPer1KPrompt)
	}
	if c.CostPer1KCompletion < 0 {
		return fmt.Errorf("COST_PER_1K_COMPLETION должен быть >= 0, получено %v", c.CostPer1KCompletion)
	}

	// Wordstat: креды задаются парой, как и basic-auth, а URL проверяем на схему —
	// иначе запуск с опечаткой в адресе падал бы только на первом прогоне.
	if (c.WordstatMCPUser == "") != (c.WordstatMCPPass == "") {
		return fmt.Errorf("WORDSTAT_MCP_USER и WORDSTAT_MCP_PASS должны быть заданы вместе")
	}
	if c.WordstatMCPURL != "" && !strings.HasPrefix(c.WordstatMCPURL, "http://") && !strings.HasPrefix(c.WordstatMCPURL, "https://") {
		return fmt.Errorf("WORDSTAT_MCP_URL должен начинаться с http:// или https://, получено %q", c.WordstatMCPURL)
	}
	if !isGeoID(c.WordstatRegionDefault) {
		return fmt.Errorf("WORDSTAT_REGION_DEFAULT должен быть geo ID Яндекса (только цифры), получено %q", c.WordstatRegionDefault)
	}
	if c.WordstatMaxCallsPerRun <= 0 {
		return fmt.Errorf("WORDSTAT_MAX_CALLS_PER_RUN должен быть > 0, получено %d", c.WordstatMaxCallsPerRun)
	}

	// Трасса: режим проверяем здесь же, чтобы опечатка в .env не превращалась
	// молча в «выключено». Пустое значение — «summary», как и в trace.ParseMode.
	switch strings.ToLower(strings.TrimSpace(c.TraceMode)) {
	case "", TraceModeOff, TraceModeSummary, TraceModeFull:
	default:
		return fmt.Errorf("TRACE_MODE: неизвестный режим %q (off | summary | full)", c.TraceMode)
	}
	if c.TraceRetentionDays < 0 {
		return fmt.Errorf("TRACE_RETENTION_DAYS должен быть >= 0, получено %d", c.TraceRetentionDays)
	}
	if c.TraceMaxPayloadBytes <= 0 {
		return fmt.Errorf("TRACE_MAX_PAYLOAD_BYTES должен быть > 0, получено %d", c.TraceMaxPayloadBytes)
	}
	return nil
}

// databaseURL выбирает адрес БД: DATABASE_URL, иначе сборка из MARIADB_*.
//
// MARIADB_* — те же переменные, что понимает официальный образ MariaDB, поэтому
// один файл backend/.env обслуживает и контейнер БД, и приложение: в compose
// достаточно переопределить MARIADB_HOST на имя сервиса.
func databaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	user := strings.TrimSpace(os.Getenv("MARIADB_USER"))
	pass := os.Getenv("MARIADB_PASSWORD")
	if user == "" || pass == "" {
		return "" // validate объяснит, чего не хватает
	}
	return fmt.Sprintf("mysql://%s:%s@%s:%s/%s",
		url.QueryEscape(user), url.QueryEscape(pass),
		getStr("MARIADB_HOST", DefaultMariaDBHost),
		getStr("MARIADB_PORT", DefaultMariaDBPort),
		getStr("MARIADB_DATABASE", DefaultMariaDBDatabase))
}

// validateDatabaseURL проверяет, что адрес БД — это mysql://-адрес MariaDB.
// Пароль в тексте ошибки не показываем: сообщение попадает в логи.
func validateDatabaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("адрес БД не задан: укажите DATABASE_URL=mysql://user:pass@host:3306/dbname " +
			"или MARIADB_HOST/MARIADB_PORT/MARIADB_USER/MARIADB_PASSWORD/MARIADB_DATABASE")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "mysql", "mariadb":
	case "sqlite", "sqlite3", "file":
		return fmt.Errorf("DATABASE_URL=%s: хранилище переехало с SQLite на MariaDB — укажите mysql://user:pass@host:3306/dbname",
			redactURL(raw))
	case "postgres", "postgresql":
		return fmt.Errorf("DATABASE_URL=%s: этот билд хранит данные в MariaDB — укажите mysql://user:pass@host:3306/dbname",
			redactURL(raw))
	case "":
		// Путь к файлу (прежний SQLite-адрес) схему не задаёт — подсказываем то же.
		return fmt.Errorf("DATABASE_URL=%s: похоже на путь к файлу SQLite, а хранилище теперь MariaDB — укажите mysql://user:pass@host:3306/dbname",
			redactURL(raw))
	default:
		return fmt.Errorf("DATABASE_URL=%s: ожидали mysql:// (MariaDB использует ту же схему)", redactURL(raw))
	}
	if u.Host == "" {
		return fmt.Errorf("DATABASE_URL=%s: не указан хост MariaDB", redactURL(raw))
	}
	if strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("DATABASE_URL=%s: не указано имя базы", redactURL(raw))
	}
	return nil
}

// redactURL прячет пароль в адресе БД.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "mysql://?"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// isGeoID проверяет, что значение — geo ID Яндекса: непустая строка из цифр.
func isGeoID(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// getStr читает строковую переменную окружения, возвращая def, если она не задана.
func getStr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

// getInt читает int из окружения. При невалидном значении возвращает def и
// оставляет предупреждение в stderr через fmt.Fprintf — чтобы опечатка в .env
// не осталась незамеченной.
func getInt(k string, def int) int {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not an int, using default %d: %v\n", k, v, def, err)
		return def
	}
	return n
}

// getFloat читает float64 из окружения.
func getFloat(k string, def float64) float64 {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a float, using default %v: %v\n", k, v, def, err)
		return def
	}
	return f
}

// getDur читает time.Duration из окружения (например, "10m", "30s").
func getDur(k string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(k)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a duration, using default %s: %v\n", k, v, def, err)
		return def
	}
	return d
}
