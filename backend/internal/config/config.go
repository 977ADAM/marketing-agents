// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/trace"
)

// Config — конфигурация сервиса, собранная из переменных окружения.
type Config struct {
	HTTPAddr     string
	SQLitePath   string // файл БД (SQLite); каталог создаётся при старте
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
	WordstatMCPURL            string
	WordstatMCPUser           string
	WordstatMCPPass           string
	WordstatRegionDefault     string  // geo ID Яндекса: 225 Россия, 1 Москва и область, 213 Москва
	WordstatMinVolume         int     // минимальный объём темы, показов за 30 дней
	WordstatSeasonalityFactor float64 // во сколько раз пик за 12 месяцев должен превышать порог
	WordstatMaxCallsPerRun    int     // лимит обращений к Wordstat на прогон
	TopicsMultiplier          int     // сколько идей предлагать на одну статью (×2)

	// Трасса прогона: журнал того, что делали агенты.
	TraceMode            string // off | summary | full
	TraceRetentionDays   int    // срок хранения событий
	TraceMaxPayloadBytes int    // обрезка одного payload
}

// DefaultSQLitePath — путь к файлу БД по умолчанию (относительно рабочего каталога).
const DefaultSQLitePath = "data/marketing.db"

// DefaultHTTPAddr — адрес прослушивания по умолчанию: только loopback, чтобы
// локальный запуск не торчал в сеть (у API по умолчанию выключен basic-auth).
// В docker-compose переменная переопределяется на ":8080": внутри контейнера
// слушать нужно все интерфейсы, иначе фронт не достучится до бэкенда по
// внутренней сети, а проброс порта не заработает. Наружу контейнер при этом
// не выставлен — порт публикуется только на 127.0.0.1 (см. docker-compose.yml).
const DefaultHTTPAddr = "127.0.0.1:8080"

// DefaultWordstatRegion — регион по умолчанию для подбора тем: 225 — Россия.
const DefaultWordstatRegion = "225"

// Load читает env, подставляет дефолты и валидирует обязательные поля.
func Load() (*Config, error) {
	// .env опционален: если файла нет — читаем только реальное окружение.
	_ = godotenv.Load()

	dbPath, err := sqlitePath()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		HTTPAddr:     getStr("HTTP_ADDR", DefaultHTTPAddr),
		SQLitePath:   dbPath,
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

		WordstatMCPURL:            getStr("WORDSTAT_MCP_URL", ""),
		WordstatMCPUser:           getStr("WORDSTAT_MCP_USER", ""),
		WordstatMCPPass:           getStr("WORDSTAT_MCP_PASS", ""),
		WordstatRegionDefault:     getStr("WORDSTAT_REGION_DEFAULT", DefaultWordstatRegion),
		WordstatMinVolume:         getInt("WORDSTAT_MIN_VOLUME", 300),
		WordstatSeasonalityFactor: getFloat("WORDSTAT_SEASONALITY_FACTOR", 3),
		WordstatMaxCallsPerRun:    getInt("WORDSTAT_MAX_CALLS_PER_RUN", 60),
		TopicsMultiplier:          getInt("TOPICS_MULTIPLIER", 2),

		TraceMode:            getStr("TRACE_MODE", string(trace.ModeSummary)),
		TraceRetentionDays:   getInt("TRACE_RETENTION_DAYS", 30),
		TraceMaxPayloadBytes: getInt("TRACE_MAX_PAYLOAD_BYTES", trace.DefaultMaxPayloadBytes),
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
	if c.SQLitePath == "" {
		return fmt.Errorf("SQLitePath не может быть пустым")
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
	if c.WordstatMinVolume < 0 {
		return fmt.Errorf("WORDSTAT_MIN_VOLUME должен быть >= 0, получено %d", c.WordstatMinVolume)
	}
	if c.WordstatSeasonalityFactor < 1 {
		return fmt.Errorf("WORDSTAT_SEASONALITY_FACTOR должен быть >= 1, получено %v", c.WordstatSeasonalityFactor)
	}
	if c.WordstatMaxCallsPerRun <= 0 {
		return fmt.Errorf("WORDSTAT_MAX_CALLS_PER_RUN должен быть > 0, получено %d", c.WordstatMaxCallsPerRun)
	}
	if c.TopicsMultiplier < 1 {
		return fmt.Errorf("TOPICS_MULTIPLIER должен быть >= 1, получено %d", c.TopicsMultiplier)
	}

	// Трасса: режим проверяем разбором, чтобы опечатка в .env не превращалась
	// молча в «выключено».
	if _, err := trace.ParseMode(c.TraceMode); err != nil {
		return fmt.Errorf("TRACE_MODE: %w", err)
	}
	if c.TraceRetentionDays < 0 {
		return fmt.Errorf("TRACE_RETENTION_DAYS должен быть >= 0, получено %d", c.TraceRetentionDays)
	}
	if c.TraceMaxPayloadBytes <= 0 {
		return fmt.Errorf("TRACE_MAX_PAYLOAD_BYTES должен быть > 0, получено %d", c.TraceMaxPayloadBytes)
	}
	return nil
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

// sqlitePath выбирает файл БД: SQLITE_PATH, иначе DATABASE_URL (совместимость
// с прежней конфигурацией, если там путь/URI файла, а не строка подключения
// к сетевой СУБД), иначе дефолт.
func sqlitePath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("SQLITE_PATH")); p != "" {
		return p, nil
	}
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		// Отклоняем любые URL-схемы, кроме file:// — этот билд хранит данные
		// в SQLite, а не в сетевой СУБД.
		if i := strings.Index(dsn, "://"); i > 0 {
			scheme := strings.ToLower(dsn[:i])
			if scheme != "file" {
				return "", fmt.Errorf(
					"DATABASE_URL=%q looks like a connection string (%s://), but this build stores data in SQLite: set SQLITE_PATH (file path) instead",
					dsn, scheme,
				)
			}
		}
		return dsn, nil
	}
	return DefaultSQLitePath, nil
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
