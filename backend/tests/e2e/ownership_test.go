package e2e_test

import (
	"context"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	repo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	"github.com/977ADAM/marketing-agents/internal/testkit/testdb"
	"sync/atomic"
	"testing"
	"time"
)

type blockingWorkflow struct {
	calls            atomic.Int32
	started, release chan struct{}
}

func (w *blockingWorkflow) Run(ctx context.Context, _ campaign.Brief, p run.Progress) (service.Result, error) {
	if w.calls.Add(1) == 1 {
		close(w.started)
	}
	p.Strategizing()
	select {
	case <-w.release:
		return service.Result{}, nil
	case <-ctx.Done():
		return service.Result{}, ctx.Err()
	}
}
func TestTwoRunnersExecuteOneLeaseAndRemoteSSE(t *testing.T) {
	db, _ := testdb.New(t)
	ctx := context.Background()
	c := repo.NewCampaigns(db)
	rev := reviewrepo.NewReviews(db)
	id, err := c.Create(ctx, "", campaign.Brief{})
	if err != nil {
		t.Fatal(err)
	}
	w := &blockingWorkflow{started: make(chan struct{}), release: make(chan struct{})}
	h1, h2 := runner.NewHub(ctx, c, rev), runner.NewHub(ctx, c, rev)
	r1, r2 := runner.NewRunner(ctx, c, rev, w, nil, time.Minute, nil, h1), runner.NewRunner(ctx, c, rev, w, nil, time.Minute, nil, h2)
	if err := r1.Start(id, campaign.Brief{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.started:
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}
	if err := r2.Start(id, campaign.Brief{}); err != nil {
		t.Fatal(err)
	}
	if err := r2.Drain(); err != nil {
		t.Fatal(err)
	}
	if w.calls.Load() != 1 {
		t.Fatalf("calls=%d", w.calls.Load())
	}
	snap, ch, cancel := h2.Subscribe(id)
	defer cancel()
	if snap.Phase == run.PhaseFailed {
		t.Fatal("foreign live run failed")
	}
	select {
	case <-ch:
		t.Fatal("remote subscription prematurely closed")
	default:
	}
	close(w.release)
	if err := r1.Drain(); err != nil {
		t.Fatal(err)
	}
	select {
	case final := <-ch:
		if final.Phase != run.PhaseDone {
			t.Fatalf("phase=%s", final.Phase)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("remote terminal missing")
	}
}
