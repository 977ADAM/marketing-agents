package main

import (
	"context"
	"github.com/977ADAM/marketing-agents/internal/adapters/accounting"
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
		retentionCtx, retentionCancel := context.WithTimeout(baseCtx, 5*time.Second)
		defer retentionCancel()
		if n, err := events.DeleteRunEventsBefore(retentionCtx, cutoff); err != nil {
			logger.Warn("trace retention", "err", err)
		} else if n > 0 {
			logger.Info("trace retention", "deleted", n, "days", cfg.TraceRetentionDays)
		}
	}

	if cfg.TraceRetentionDays > 0 {
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-baseCtx.Done():
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
					_, err := events.DeleteRunEventsBefore(ctx, time.Now().AddDate(0, 0, -cfg.TraceRetentionDays))
					cancel()
					if err != nil {
						logger.Error("trace retention", "err", err)
					}
				}
			}
		}()
	}

	baseLLM := llm.New(cfg.APIKey, cfg.BaseURL, cfg.ModelDefault, cfg.LLMMaxRetries, nil)
	// Копирайтеры — на быструю/дешёвую модель; стратег и критик остаются на сильной (дефолтной).
	baseLLM.SetRoleModel(campaignservice.RoleCopywriter, cfg.ModelFast)
	llmClient := accounting.New(tracing.NewLLM(baseLLM, recorder))

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

	orch := campaignservice.NewWorkflow(llmClient, campaignservice.Options{Prices: &cfg.ModelPrices,
		Checkpoints:         campaigns,
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
	hub := runner.NewHub(baseCtx, campaigns, reviews, sloglogger.New(logger))
	background := runner.NewRunner(baseCtx, campaigns, reviews, orch, reviewservice.NewWorkflow(llmClient, reviewservice.Options{Recorder: recorder, Prices: &cfg.ModelPrices, Checkpoints: reviews, CostPer1KPrompt: cfg.CostPer1KPrompt, CostPer1KCompletion: cfg.CostPer1KCompletion, ParallelTexts: cfg.Limits.ParallelTexts}), cfg.RunTimeout, sloglogger.New(logger), hub, runner.Options{Capacity: cfg.RunnerCapacity, FinalizeTimeout: cfg.FinalizeTimeout})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-baseCtx.Done():
				return
			case <-ticker.C:
				if _, err := runner.RecoverInterrupted(baseCtx, campaigns, reviews); err != nil {
					logger.Error("recover expired runs", "err", err)
				}
			}
		}
	}()
	campaignService := campaignservice.NewService(campaigns, background, cfg.Limits)
	reviewService := reviewservice.NewService(reviews, background, cfg.Limits)
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
	srv := server.NewHTTPServer(server.Config{Addr: cfg.HTTPAddr, ShutdownTimeout: cfg.ShutdownGrace}, handler)
	stop, stopCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopCancel()
	logger.Info("listening", "addr", cfg.HTTPAddr)
	if err := srv.Run(stop); err != nil {
		logger.Error("serve", "err", err)
	}
	logger.Info("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutCancel()
	if err := background.Drain(shutCtx); err != nil {
		logger.Warn("background drain", "err", err)
		finalCtx, finalCancel := context.WithTimeout(context.Background(), cfg.FinalizeTimeout)
		defer finalCancel()
		_ = background.Drain(finalCtx)
	}
	baseCancel()
}
