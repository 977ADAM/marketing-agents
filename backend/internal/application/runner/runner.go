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
	campaigns       campaignservice.Store
	reviews         reviewservice.Store
	orch            CampaignWorkflow
	reviewer        ReviewWorkflow
	baseCtx         context.Context
	runTimeout      time.Duration
	finalizeTimeout time.Duration
	logger          corelogger.Logger
	hub             *Hub
	*Admission
}

func NewRunner(baseCtx context.Context, campaigns campaignservice.Store, reviews reviewservice.Store, orch CampaignWorkflow, reviewer ReviewWorkflow, timeout time.Duration, logger corelogger.Logger, hub *Hub, options ...Options) *BackgroundRunner {
	if logger == nil {
		logger = corelogger.Nop()
	}
	opt := Options{Capacity: 64, FinalizeTimeout: 5 * time.Second}
	if len(options) > 0 {
		if options[0].Capacity > 0 {
			opt.Capacity = options[0].Capacity
		}
		if options[0].FinalizeTimeout > 0 {
			opt.FinalizeTimeout = options[0].FinalizeTimeout
		}
	}
	return &BackgroundRunner{
		campaigns: campaigns, reviews: reviews, orch: orch, reviewer: reviewer, baseCtx: baseCtx,
		runTimeout: timeout, logger: logger, hub: hub,
		Admission: NewAdmission(baseCtx, opt.Capacity), finalizeTimeout: opt.FinalizeTimeout,
	}
}

func (r *BackgroundRunner) ExecuteCampaign(parent context.Context, id string, b campaign.Brief) {
	ctx, cancel := context.WithTimeout(parent, r.runTimeout)
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
	final, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), r.finalizeTimeout)
	defer finalCancel()
	if err != nil {
		r.logger.Error("run failed", "id", id, "err", err)
		_ = r.campaigns.Fail(final, id, err.Error())
		tr.Failed()
		return
	}
	if err := r.campaigns.Complete(final, id, campaign.Outcome{
		Strategy: res.Strategy, Deliverables: res.Deliverables, CostUSD: res.CostUSD,
	}); err != nil {
		r.logger.Error("complete", "id", id, "err", err)
		_ = r.campaigns.Fail(final, id, "complete: "+err.Error())
		tr.Failed()
		return
	}
	tr.Done()
	r.logger.Info("campaign done", "id", id, "cost_usd", res.CostUSD)
}

// Drain waits for submitted tasks, cancelling them when the grace expires.
func (r *BackgroundRunner) Drain(contexts ...context.Context) error {
	ctx := context.Background()
	cancel := func() {}
	if len(contexts) > 0 {
		ctx = contexts[0]
	} else {
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	}
	defer cancel()
	return r.Admission.Drain(ctx)
}
func (r *BackgroundRunner) Start(id string, b campaign.Brief) error {
	slot, err := r.Reserve(context.Background())
	if err != nil {
		return err
	}
	return slot.Submit(func(ctx context.Context) { r.ExecuteCampaign(ctx, id, b) })
}
func (r *BackgroundRunner) StartReview(id string, req review.Request) error {
	slot, err := r.Reserve(context.Background())
	if err != nil {
		return err
	}
	return slot.Submit(func(ctx context.Context) { r.ExecuteReview(ctx, id, req) })
}

// StartReview запускает фоновую проверку готовых текстов (агенты review).
func (r *BackgroundRunner) ExecuteReview(parent context.Context, id string, req review.Request) {
	ctx, cancel := context.WithTimeout(parent, r.runTimeout)
	defer cancel()
	ctx = trace.WithRunID(ctx, id)

	tr := r.hub.ReviewTracker(id)
	if err := r.reviews.MarkCheckRunning(ctx, id); err != nil {
		r.logger.Error("mark review running", "id", id, "err", err)
		tr.Failed()
		return
	}
	res, err := r.reviewer.Review(ctx, req, tr)
	final, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), r.finalizeTimeout)
	defer finalCancel()
	if err != nil {
		r.logger.Error("review failed", "id", id, "err", err)
		_ = r.reviews.FailCheck(final, id, err.Error())
		tr.Failed()
		return
	}
	if err := r.reviews.CompleteCheck(final, id, res); err != nil {
		r.logger.Error("review complete", "id", id, "err", err)
		_ = r.reviews.FailCheck(final, id, "complete: "+err.Error())
		tr.Failed()
		return
	}
	tr.Done()
	r.logger.Info("review done", "id", id, "texts", len(res.Items), "cost_usd", res.CostUSD)
}

type CampaignWorkflow interface {
	Run(context.Context, campaign.Brief, run.Progress) (campaignservice.Result, error)
}
type ReviewWorkflow interface {
	Review(context.Context, review.Request, run.Progress) (review.Result, error)
}

type Options struct {
	Capacity        int
	FinalizeTimeout time.Duration
}
