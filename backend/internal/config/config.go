// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

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

// Load читает env, подставляет дефолты и валидирует обязательные поля.
func Load() (*Config, error) {
	dbPath, err := sqlitePath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		HTTPAddr:             getStr("HTTP_ADDR", DefaultHTTPAddr),
		SQLitePath:           dbPath,
		APIKey:               getStr("DEEPSEEK_API_KEY", ""),
		BaseURL:              getStr("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		ModelDefault:         getStr("MODEL_DEFAULT", "deepseek-v4-pro"),
		ModelFast:            getStr("MODEL_FAST", "deepseek-v4-flash"),
		LLMMaxRetries:        getInt("LLM_MAX_RETRIES", 3),
		RunTimeout:           getDur("RUN_TIMEOUT", 10*time.Minute),
		CriticMaxIter:        getInt("CRITIC_MAX_ITER", 3),
		CriticScoreThreshold: getInt("CRITIC_SCORE_THRESHOLD", 80),
		MaxTopics:            getInt("MAX_TOPICS", 5),
		CostPer1KPrompt:      getFloat("COST_PER_1K_PROMPT", 0.00027),
		CostPer1KCompletion:  getFloat("COST_PER_1K_COMPLETION", 0.0011),
		RateLimitPerMin:      getInt("RATE_LIMIT_PER_MIN", 30),
		LogLevel:             getStr("LOG_LEVEL", "info"),
		BasicAuthUser:        getStr("BASIC_AUTH_USER", ""),
		BasicAuthPass:        getStr("BASIC_AUTH_PASS", ""),
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required")
	}
	return cfg, nil
}

// sqlitePath выбирает файл БД: SQLITE_PATH, иначе DATABASE_URL (совместимость
// с прежней конфигурацией, если там путь/URI файла, а не строка подключения
// к Postgres), иначе дефолт.
func sqlitePath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("SQLITE_PATH")); p != "" {
		return p, nil
	}
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			return "", fmt.Errorf("DATABASE_URL looks like a Postgres DSN, but this build stores data in SQLite: set SQLITE_PATH (file path) instead")
		}
		return dsn, nil
	}
	return DefaultSQLitePath, nil
}

func getStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
