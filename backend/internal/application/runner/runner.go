package runner

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	"time"

	corelogger "github.com/977ADAM/marketing-agents/internal/core/logger"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// BackgroundRunner выполняет пайплайн в фоне и пишет результат в хранилище.
type BackgroundRunner struct {
	campaigns  campaignservice.Store
	reviews    reviewservice.Store
	orch       CampaignWorkflow
	reviewer   ReviewWorkflow
	baseCtx    context.Context
	runTimeout time.Duration
	logger     corelogger.Logger
	hub        *Hub
	wg         chan struct{} // семафор учёта in-flight для graceful shutdown
}

func NewRunner(baseCtx context.Context, campaigns campaignservice.Store, reviews reviewservice.Store, orch CampaignWorkflow, reviewer ReviewWorkflow, timeout time.Duration, logger corelogger.Logger, hub *Hub) *BackgroundRunner {
	if logger == nil {
		logger = corelogger.Nop()
	}
	return &BackgroundRunner{
		campaigns: campaigns, reviews: reviews, orch: orch, reviewer: reviewer, baseCtx: baseCtx,
		runTimeout: timeout, logger: logger, hub: hub,
		wg: make(chan struct{}, 64),
	}
}

func (r *BackgroundRunner) Start(id string, b campaign.Brief) {
	r.wg <- struct{}{}
	go func() {
		defer func() { <-r.wg }()
		ctx, cancel := context.WithTimeout(r.baseCtx, r.runTimeout)
		defer cancel()
		// Помечаем прогон: по этому идентификатору трасса привязывает события.
		ctx = trace.WithRunID(ctx, id)

		tr := r.hub.Tracker(id)
		if err := r.campaigns.MarkRunning(ctx, id); err != nil {
			r.logger.Error("mark running", "id", id, "err", err)
			tr.Failed()
			return
		}
		res, err := r.orch.Run(ctx, b, tr)
		if err != nil {
			r.logger.Error("run failed", "id", id, "err", err)
			_ = r.campaigns.Fail(context.WithoutCancel(ctx), id, err.Error())
			tr.Failed()
			return
		}
		if err := r.campaigns.Complete(context.WithoutCancel(ctx), id, campaign.Outcome{
			Strategy: res.Strategy, Deliverables: res.Deliverables, CostUSD: res.CostUSD,
		}); err != nil {
			r.logger.Error("complete", "id", id, "err", err)
			_ = r.campaigns.Fail(context.WithoutCancel(ctx), id, "complete: "+err.Error())
			tr.Failed()
			return
		}
		tr.Done()
		r.logger.Info("campaign done", "id", id, "cost_usd", res.CostUSD)
	}()
}

// Drain ждёт завершения in-flight прогонов (для graceful shutdown).
func (r *BackgroundRunner) Drain() {
	for i := 0; i < cap(r.wg); i++ {
		r.wg <- struct{}{}
	}
}

// StartReview запускает фоновую проверку готовых текстов (агенты review).
func (r *BackgroundRunner) StartReview(id string, req review.Request) {
	r.wg <- struct{}{}
	go func() {
		defer func() { <-r.wg }()
		ctx, cancel := context.WithTimeout(r.baseCtx, r.runTimeout)
		defer cancel()
		ctx = trace.WithRunID(ctx, id)

		tr := r.hub.ReviewTracker(id)
		if err := r.reviews.MarkCheckRunning(ctx, id); err != nil {
			r.logger.Error("mark review running", "id", id, "err", err)
			tr.Failed()
			return
		}
		res, err := r.reviewer.Review(ctx, req, tr)
		if err != nil {
			r.logger.Error("review failed", "id", id, "err", err)
			_ = r.reviews.FailCheck(context.WithoutCancel(ctx), id, err.Error())
			tr.Failed()
			return
		}
		if err := r.reviews.CompleteCheck(context.WithoutCancel(ctx), id, res); err != nil {
			r.logger.Error("review complete", "id", id, "err", err)
			_ = r.reviews.FailCheck(context.WithoutCancel(ctx), id, "complete: "+err.Error())
			tr.Failed()
			return
		}
		tr.Done()
		r.logger.Info("review done", "id", id, "texts", len(res.Items), "cost_usd", res.CostUSD)
	}()
}

type CampaignWorkflow interface {
	Run(context.Context, campaign.Brief, run.Progress) (campaignservice.Result, error)
}
type ReviewWorkflow interface {
	Review(context.Context, review.Request, run.Progress) (review.Result, error)
}
