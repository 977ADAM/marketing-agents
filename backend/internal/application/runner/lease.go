package runner

import (
	"context"
	"fmt"
	llm "github.com/977ADAM/marketing-agents/internal/core/llm"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	"time"
)

func (r *BackgroundRunner) own(ctx context.Context, id string, store any, cancel context.CancelFunc) (context.Context, func(), error) {
	leases, ok := store.(run.LeaseStore)
	if !ok {
		return ctx, func() {}, fmt.Errorf("store does not support ownership")
	}
	attempt, acquired, err := leases.AcquireLease(ctx, id, r.owner, r.leaseTTL)
	if err != nil {
		return ctx, func() {}, err
	}
	if !acquired {
		return ctx, func() {}, run.ErrLeaseLost
	}
	ctx = run.WithOwnership(ctx, run.Ownership{Owner: r.owner, Attempt: attempt})
	if sink, ok := store.(llm.UsageStore); ok {
		ctx = llm.WithUsageSink(ctx, func(ctx context.Context, e llm.UsageEntry) error { return sink.AppendUsage(ctx, id, e) })
	}
	beatCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-ticker.C:
				if err := leases.RenewLease(beatCtx, id, r.owner, attempt, r.leaseTTL); err != nil {
					r.logger.Error("renew lease", "id", id, "attempt", attempt, "err", err)
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { stop(); <-done }, nil
}
