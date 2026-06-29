package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/config"
	"github.com/977ADAM/marketing-agents/internal/httpapi"
	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
	"github.com/977ADAM/marketing-agents/internal/web"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	baseCtx, baseCancel := context.WithCancel(context.Background())
	defer baseCancel()

	pool, err := pgxpool.New(baseCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := store.Migrate(baseCtx, pool); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	st := store.New(pool)
	if n, err := st.RecoverInterrupted(baseCtx); err != nil {
		logger.Error("recover interrupted", "err", err)
		os.Exit(1)
	} else if n > 0 {
		logger.Info("recovered interrupted campaigns", "count", n)
	}
	llmClient := llm.New(cfg.APIKey, cfg.BaseURL, cfg.ModelDefault, cfg.LLMMaxRetries, nil)
	// Копирайтеры — на быструю/дешёвую модель; стратег и критик остаются на сильной (дефолтной).
	llmClient.SetRoleModel(agents.RoleCopywriter, cfg.ModelFast)
	orch := orchestrator.New(llmClient, orchestrator.Options{
		CriticMaxIter:       cfg.CriticMaxIter,
		ScoreThreshold:      cfg.CriticScoreThreshold,
		CostPer1KPrompt:     cfg.CostPer1KPrompt,
		CostPer1KCompletion: cfg.CostPer1KCompletion,
		MaxTopics:           cfg.MaxTopics,
	})
	hub := httpapi.NewHub(baseCtx, st)
	runner := httpapi.NewRunner(baseCtx, st, orch, cfg.RunTimeout, logger, hub)
	api := httpapi.New(st, runner, cfg.RateLimitPerMin)

	// общий роутинг: /api/* и /healthz → API, всё остальное → SPA.
	root := http.NewServeMux()
	apiHandler := api.Handler()
	root.Handle("/api/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.Handle("/", web.Handler())

	handler := httpapi.BasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass, root)
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler}

	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx) // прекращаем приём новых запросов
	runner.Drain()            // ждём текущие прогоны
	baseCancel()              // отменяем всё, что не успело
}
