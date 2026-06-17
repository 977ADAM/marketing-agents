// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	APIKey       string
	BaseURL      string
	ModelDefault string

	LLMMaxRetries        int
	RunTimeout           time.Duration
	CriticMaxIter        int
	CriticScoreThreshold int

	CostPer1KPrompt     float64
	CostPer1KCompletion float64

	RateLimitPerMin int
	LogLevel        string
}

// Load читает env, подставляет дефолты и валидирует обязательные поля.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:             getStr("HTTP_ADDR", ":8080"),
		DatabaseURL:          getStr("DATABASE_URL", ""),
		APIKey:               getStr("DEEPSEEK_API_KEY", ""),
		BaseURL:              getStr("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		ModelDefault:         getStr("MODEL_DEFAULT", "deepseek-v4-pro"),
		LLMMaxRetries:        getInt("LLM_MAX_RETRIES", 3),
		RunTimeout:           getDur("RUN_TIMEOUT", 10*time.Minute),
		CriticMaxIter:        getInt("CRITIC_MAX_ITER", 3),
		CriticScoreThreshold: getInt("CRITIC_SCORE_THRESHOLD", 80),
		CostPer1KPrompt:      getFloat("COST_PER_1K_PROMPT", 0.00027),
		CostPer1KCompletion:  getFloat("COST_PER_1K_COMPLETION", 0.0011),
		RateLimitPerMin:      getInt("RATE_LIMIT_PER_MIN", 30),
		LogLevel:             getStr("LOG_LEVEL", "info"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required")
	}
	return cfg, nil
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
