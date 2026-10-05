package repository_test

import (
	"context"
	"errors"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	repo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	"testing"
	"time"
)

func TestLeasesFencePreviousWorker(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	c := repo.NewCampaigns(s.db, func() time.Time { return now })
	ctx := context.Background()
	id, err := c.Create(ctx, "", campaign.Brief{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, acquired, err := c.AcquireLease(ctx, id, "first", 30*time.Second)
	if err != nil || !acquired {
		t.Fatalf("acquire %v %v", acquired, err)
	}
	if _, ok, err := c.AcquireLease(ctx, id, "second", 30*time.Second); err != nil || ok {
		t.Fatalf("second acquired live lease: %v %v", ok, err)
	}
	if n, err := c.RecoverInterrupted(ctx); err != nil || n != 0 {
		t.Fatalf("live recovered: %d %v", n, err)
	}
	now = now.Add(31 * time.Second)
	next, ok, err := c.AcquireLease(ctx, id, "second", 30*time.Second)
	if err != nil || !ok || next <= attempt {
		t.Fatalf("takeover %d %v %v", next, ok, err)
	}
	stale := run.WithOwnership(ctx, run.Ownership{Owner: "first", Attempt: attempt})
	if err := c.Complete(stale, id, campaign.Outcome{}); !errors.Is(err, run.ErrLeaseLost) {
		t.Fatalf("stale complete: %v", err)
	}
	if err := c.Fail(stale, id, "late"); !errors.Is(err, run.ErrLeaseLost) {
		t.Fatalf("stale fail: %v", err)
	}
	now = now.Add(31 * time.Second)
	if n, err := c.RecoverInterrupted(ctx); err != nil || n != 1 {
		t.Fatalf("expired recovery: %d %v", n, err)
	}
}
func TestFreshUnownedPendingHasAcquisitionGrace(t *testing.T) {
	s := newTestStore(t)
	id, err := s.campaigns.Create(context.Background(), "", campaign.Brief{})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.campaigns.RecoverInterrupted(context.Background()); err != nil || n != 0 {
		t.Fatalf("fresh pending failed: %d %v", n, err)
	}
	c, _ := s.campaigns.Get(context.Background(), id)
	if c.Status != "pending" {
		t.Fatal(c.Status)
	}
}
