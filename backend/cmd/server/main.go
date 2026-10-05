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
	"github.com/977ADAM/marketing-agents/internal/sqlite"
	"github.com/977ADAM/marketing-agents/internal/topic"
	"github.com/977ADAM/marketing-agents/internal/trace"
	"github.com/977ADAM/marketing-agents/internal/wordstat"
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

	// SQLite: соединение открывается здесь, а схему применяет отдельный сервис
	// миграций (в compose — migrate, локально — make migrate). Сервер только
	// проверяет готовность схемы и не стартует на неподготовленной БД.
	db, err := sqlite.OpenDB(baseCtx, cfg.SQLitePath)
	if err != nil {
		logger.Error("db", "path", cfg.SQLitePath, "err", err)
		os.Exit(1)
	}
	version, err := sqlite.CheckSchema(baseCtx, db)
	if err != nil {
		logger.Error("db schema", "path", cfg.SQLitePath, "err", err)
		_ = db.Close()
		os.Exit(1)
	}
	// Три хранилища поверх одного соединения: у каждого свой порт.
	campaigns := sqlite.NewCampaigns(db)
	reviews := sqlite.NewReviews(db)
	events := sqlite.NewEvents(db)
	defer db.Close()
	logger.Info("db ready", "path", cfg.SQLitePath, "schema_version", version)

	if n, err := sqlite.RecoverInterrupted(baseCtx, db); err != nil {
		logger.Error("recover interrupted", "err", err)
		os.Exit(1)
	} else if n > 0 {
		logger.Info("recovered interrupted campaigns", "count", n)
	}
	// Трасса прогона: журнал событий (по умолчанию summary — без тел промптов).
	mode, err := trace.ParseMode(cfg.TraceMode)
	if err != nil {
		logger.Error("trace mode", "err", err)
		os.Exit(1)
	}
	recorder := trace.New(events, trace.Config{
		Mode:            mode,
		MaxPayloadBytes: cfg.TraceMaxPayloadBytes,
		OnError:         func(err error) { logger.Warn("trace", "err", err) },
	})
	// Уборку делаем независимо от режима: если трассу выключили, старые события
	// всё равно надо чистить, иначе БД будет расти без ограничения.
	logger.Info("trace", "mode", string(mode), "retention_days", cfg.TraceRetentionDays,
		"max_payload_bytes", cfg.TraceMaxPayloadBytes)
	if cfg.TraceRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.TraceRetentionDays)
		if n, err := events.DeleteRunEventsBefore(baseCtx, cutoff); err != nil {
			logger.Warn("trace retention", "err", err)
		} else if n > 0 {
			logger.Info("trace retention", "deleted", n, "days", cfg.TraceRetentionDays)
		}
	}

	baseLLM := llm.New(cfg.APIKey, cfg.BaseURL, cfg.ModelDefault, cfg.LLMMaxRetries, nil)
	// Копирайтеры — на быструю/дешёвую модель; стратег и критик остаются на сильной (дефолтной).
	baseLLM.SetRoleModel(agents.RoleCopywriter, cfg.ModelFast)
	llmClient := llm.NewTracing(baseLLM, recorder)

	// Подбор тем по поисковому спросу включается наличием адреса MCP-сервера
	// Wordstat. Без него работает прежний путь: темы придумывает стратег.
	var source topic.Source
	if cfg.WordstatMCPURL != "" {
		source = wordstat.NewTracing(wordstat.New(wordstat.Options{
			URL:  cfg.WordstatMCPURL,
			User: cfg.WordstatMCPUser,
			Pass: cfg.WordstatMCPPass,
		}), recorder)
		logger.Info("подбор тем включён", "wordstat", cfg.WordstatMCPURL, "region", cfg.WordstatRegionDefault)
	} else {
		logger.Warn("WORDSTAT_MCP_URL не задан: подбор тем по спросу выключен, темы даёт стратег")
	}

	orch := orchestrator.New(llmClient, orchestrator.Options{
		CriticMaxIter:       cfg.CriticMaxIter,
		ScoreThreshold:      cfg.CriticScoreThreshold,
		CostPer1KPrompt:     cfg.CostPer1KPrompt,
		CostPer1KCompletion: cfg.CostPer1KCompletion,
		MaxTopics:           cfg.MaxTopics,

		Wordstat:         source,
		Select:           orchestrator.SelectOptions{MinVolume: int64(cfg.WordstatMinVolume), SeasonalityFactor: cfg.WordstatSeasonalityFactor},
		TopicsMultiplier: cfg.TopicsMultiplier,
		MaxWordstatCalls: cfg.WordstatMaxCallsPerRun,
		DefaultRegion:    cfg.WordstatRegionDefault,

		Recorder: recorder,
	})
	hub := httpapi.NewHub(baseCtx, campaigns, reviews)
	runner := httpapi.NewRunner(baseCtx, campaigns, reviews, orch, cfg.RunTimeout, logger, hub)
	api := httpapi.New(campaigns, reviews, events, runner, hub, cfg.RateLimitPerMin)

	// Роутинг: /api/* и /healthz → API. Веб-интерфейс бэкенд не отдаёт —
	// приложение обслуживает фронтенд (frontend/, SvelteKit), который и
	// проксирует /api на этот сервис. На корне — подсказка для curl.
	root := http.NewServeMux()
	apiHandler := api.Handler()
	root.Handle("/api/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("marketing-agents API: /api/*, /healthz. Веб-интерфейс отдаёт фронтенд (frontend/).\n"))
	})

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
