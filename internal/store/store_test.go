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

func TestListRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id1, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create1: %v", err)
	}
	id2, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create2: %v", err)
	}

	items, err := s.ListRecent(ctx, 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("want >= 2 items, got %d", len(items))
	}
	// новые сверху: id2 создан позже id1, поэтому он должен идти раньше id1
	posI1, posI2 := -1, -1
	for i, it := range items {
		if it.ID == id1 {
			posI1 = i
		}
		if it.ID == id2 {
			posI2 = i
		}
	}
	if posI2 == -1 || posI1 == -1 || posI2 > posI1 {
		t.Errorf("expected id2 before id1; posI1=%d posI2=%d", posI1, posI2)
	}
	if items[0].Brief.Product != "P" {
		t.Errorf("brief not loaded: %+v", items[0])
	}
}

func TestListRecentLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}
	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, "", b); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	items, err := s.ListRecent(ctx, 2)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("want 2, got %d", len(items))
	}
}
