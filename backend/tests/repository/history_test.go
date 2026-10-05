package repository_test

import (
	"context"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"testing"
)

func TestHistoryCursorDoesNotSkipOrDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		if _, e := s.campaigns.Create(ctx, "", campaign.Brief{Product: "p"}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.reviews.CreateCheck(ctx, "", "brief"); e != nil {
			t.Fatal(e)
		}
	}
	c, next, e := s.campaigns.ListRecentPage(ctx, 50, 0)
	if e != nil || len(c) != 50 || next == 0 {
		t.Fatalf("page %d %d %v", len(c), next, e)
	}
	seen := map[string]bool{}
	for _, v := range c {
		seen[v.ID] = true
	}
	if _, e = s.campaigns.Create(ctx, "", campaign.Brief{Product: "new"}); e != nil {
		t.Fatal(e)
	}
	rest, last, e := s.campaigns.ListRecentPage(ctx, 50, next)
	if e != nil || len(rest) != 5 || last != 0 {
		t.Fatalf("next page %d %d %v", len(rest), last, e)
	}
	for _, v := range rest {
		if seen[v.ID] {
			t.Fatal("duplicate")
		}
	}
	r, rnext, e := s.reviews.ListChecksPage(ctx, 50, 0)
	rrest, rlast, re := s.reviews.ListChecksPage(ctx, 50, rnext)
	if e != nil || re != nil || len(r) != 50 || len(rrest) != 5 || rlast != 0 {
		t.Fatalf("review pages %d %d %v %v", len(r), len(rrest), e, re)
	}
}
func TestCorruptedStoredReviewReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, e := s.reviews.CreateCheck(ctx, "", "b")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.db.Exec("UPDATE reviews SET result = ? WHERE id = ?", "broken", id).Error; e != nil {
		t.Fatal(e)
	}
	if _, e = s.reviews.GetCheck(ctx, id); e == nil {
		t.Fatal("corrupted result silently ignored")
	}
}
