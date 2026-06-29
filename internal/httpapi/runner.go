package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// BackgroundRunner выполняет пайплайн в фоне и пишет результат в стор.
type BackgroundRunner struct {
	store      *store.Store
	orch       *orchestrator.Orchestrator
	baseCtx    context.Context
	runTimeout time.Duration
	logger     *slog.Logger
	wg         chan struct{} // семафор учёта in-flight для graceful shutdown
}

func NewRunner(baseCtx context.Context, st *store.Store, orch *orchestrator.Orchestrator, timeout time.Duration, logger *slog.Logger) *BackgroundRunner {
	return &BackgroundRunner{
		store: st, orch: orch, baseCtx: baseCtx,
		runTimeout: timeout, logger: logger,
		wg: make(chan struct{}, 64),
	}
}

func (r *BackgroundRunner) Start(id string, b agents.Brief) {
	r.wg <- struct{}{}
	go func() {
		defer func() { <-r.wg }()
		ctx, cancel := context.WithTimeout(r.baseCtx, r.runTimeout)
		defer cancel()

		if err := r.store.MarkRunning(ctx, id); err != nil {
			r.logger.Error("mark running", "id", id, "err", err)
			return
		}
		res, err := r.orch.Run(ctx, b, nil)
		if err != nil {
			r.logger.Error("run failed", "id", id, "err", err)
			_ = r.store.Fail(context.WithoutCancel(ctx), id, err.Error())
			return
		}
		if err := r.store.Complete(context.WithoutCancel(ctx), id, res); err != nil {
			r.logger.Error("complete", "id", id, "err", err)
			return
		}
		r.logger.Info("campaign done", "id", id, "cost_usd", res.CostUSD)
	}()
}

// Drain ждёт завершения in-flight прогонов (для graceful shutdown).
func (r *BackgroundRunner) Drain() {
	for i := 0; i < cap(r.wg); i++ {
		r.wg <- struct{}{}
	}
}
