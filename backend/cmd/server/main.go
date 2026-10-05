package main

import (
	"context"
	"errors"
	llm "github.com/977ADAM/marketing-agents/internal/adapters/llm/deepseek"
	sloglogger "github.com/977ADAM/marketing-agents/internal/adapters/logger/slog"
	tracing "github.com/977ADAM/marketing-agents/internal/adapters/tracing"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	config "github.com/977ADAM/marketing-agents/internal/core/config"
	schema "github.com/977ADAM/marketing-agents/internal/core/repository/mariadb"
	"github.com/977ADAM/marketing-agents/internal/core/repository/mariadb/pool"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	campaignrepo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	campaignhttp "github.com/977ADAM/marketing-agents/internal/features/campaign/transport/http"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	reviewhttp "github.com/977ADAM/marketing-agents/internal/features/review/transport/http"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
	wordstat "github.com/977ADAM/marketing-agents/internal/features/topic/source/wordstat"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	tracerepo "github.com/977ADAM/marketing-agents/internal/features/trace/repository/mariadb"
	traceservice "github.com/977ADAM/marketing-agents/internal/features/trace/service"
	tracehttp "github.com/977ADAM/marketing-agents/internal/features/trace/transport/http"

	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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

	// MariaDB: соединение открывает pool, а схему применяет отдельный сервис
	// миграций (в compose — migrate, локально — make migrate). Сервер только
	// проверяет готовность схемы и не стартует на неподготовленной БД.
	db, err := pool.Open(baseCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db", "target", pool.Target(cfg.DatabaseURL), "err", err)
		os.Exit(1)
	}
	version, err := schema.CheckSchema(baseCtx, db)
	if err != nil {
		logger.Error("db schema", "target", pool.Target(cfg.DatabaseURL), "err", err)
		_ = pool.Close(db)
		os.Exit(1)
	}
	// Три хранилища поверх одного соединения: у каждого свой порт.
	campaigns := campaignrepo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	events := tracerepo.NewEvents(db)
	defer func() { _ = pool.Close(db) }()
	logger.Info("db ready", "target", pool.Target(cfg.DatabaseURL), "schema_version", version)

	if n, err := runner.RecoverInterrupted(baseCtx, campaigns, reviews); err != nil {
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
	recorder := traceservice.New(events, trace.Config{
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
	baseLLM.SetRoleModel(campaignservice.RoleCopywriter, cfg.ModelFast)
	llmClient := tracing.NewLLM(baseLLM, recorder)

	// Подбор тем по поисковому спросу включается наличием адреса MCP-сервера
	// Wordstat. Без него работает прежний путь: темы придумывает стратег.
	var source topicservice.Source
	if cfg.WordstatMCPURL != "" {
		source = tracing.NewWordstat(wordstat.New(wordstat.Options{
			URL:  cfg.WordstatMCPURL,
			User: cfg.WordstatMCPUser,
			Pass: cfg.WordstatMCPPass,
		}), recorder)
		logger.Info("подбор тем включён", "wordstat", cfg.WordstatMCPURL, "region", cfg.WordstatRegionDefault)
	} else {
		logger.Warn("WORDSTAT_MCP_URL не задан: подбор тем по спросу выключен, темы даёт стратег")
	}

	orch := campaignservice.NewWorkflow(llmClient, campaignservice.Options{
		CriticMaxIter:       cfg.CriticMaxIter,
		ScoreThreshold:      cfg.CriticScoreThreshold,
		CostPer1KPrompt:     cfg.CostPer1KPrompt,
		CostPer1KCompletion: cfg.CostPer1KCompletion,
		MaxTopics:           cfg.MaxTopics,
		ParallelTexts:       cfg.Limits.ParallelTexts,

		Wordstat:         source,
		MaxWordstatCalls: cfg.WordstatMaxCallsPerRun,
		DefaultRegion:    cfg.WordstatRegionDefault,

		Recorder: recorder,
	})
	hub := runner.NewHub(baseCtx, campaigns, reviews)
	runner := runner.NewRunner(baseCtx, campaigns, reviews, orch, reviewservice.NewWorkflow(llmClient, reviewservice.Options{CostPer1KPrompt: cfg.CostPer1KPrompt, CostPer1KCompletion: cfg.CostPer1KCompletion, ParallelTexts: cfg.Limits.ParallelTexts}), cfg.RunTimeout, sloglogger.New(logger), hub)
	campaignService := campaignservice.NewService(campaigns, runner, cfg.Limits)
	reviewService := reviewservice.NewService(reviews, runner, cfg.Limits)
	limiter := middleware.NewRateLimiter(cfg.RateLimitPerMin)
	api := server.New(cfg.Limits)
	api.RegisterRoutes(campaignhttp.NewHandler(campaignService, hub, limiter, cfg.Limits).Routes()...)
	api.RegisterRoutes(reviewhttp.NewHandler(reviewService, hub, limiter, cfg.Limits).Routes()...)
	api.RegisterRoutes(tracehttp.NewHandler(campaignService, reviewService, traceservice.NewQuery(events)).Routes()...)

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

	handler := middleware.BasicAuth(cfg.BasicAuthUser, cfg.BasicAuthPass, root)
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
