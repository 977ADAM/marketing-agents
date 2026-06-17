//go:build integration

package store

import (
	"context"
	"os"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestStore(t *testing.T) *Store {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

func TestCampaignRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.MarkRunning(ctx, id); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	res := orchestrator.Result{
		Strategy:     agents.Strategy{Positioning: "p", Topics: []agents.Topic{{Title: "T1"}}},
		Deliverables: []agents.Deliverable{{Article: agents.Article{Topic: "T1", Title: "A", Body: "B", CTA: "C"}, Review: agents.Review{Score: 90, Verdict: "accept"}}},
		CostUSD:      0.12,
	}
	if err := s.Complete(ctx, id, res); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "done" || len(got.Deliverables) != 1 || got.Deliverables[0].Review.Score != 90 {
		t.Errorf("got = %+v", got)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.12 {
		t.Errorf("cost = %v", got.CostUSD)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get(context.Background(), "00000000-0000-0000-0000-0000000000ff"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
