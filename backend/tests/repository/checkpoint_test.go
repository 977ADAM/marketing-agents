package repository_test

import (
	"context"
	"errors"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	"testing"
)

func TestResumeAvailabilityAndAtomicRequeue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, err := s.campaigns.Create(ctx, "", campaign.Brief{Product: "P"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.campaigns.Fail(ctx, id, "error"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.campaigns.Get(ctx, id)
	if !c.ResumeAvailable {
		t.Fatal("input not saved")
	}
	if err := s.campaigns.Requeue(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.campaigns.Requeue(ctx, id); !errors.Is(err, run.ErrInvalidState) {
		t.Fatalf("second retry=%v", err)
	}
	old, err := s.reviews.CreateCheck(ctx, "", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.reviews.FailCheck(ctx, old, "error")
	if err := s.reviews.Requeue(ctx, old); !errors.Is(err, run.ErrResumeUnavailable) {
		t.Fatalf("legacy retry=%v", err)
	}
	fresh, err := s.reviews.CreateReview(ctx, "", review.Request{BriefText: "B", Texts: []review.TextToReview{{Body: "T"}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.reviews.FailCheck(ctx, fresh, "error")
	var input review.Request
	found, err := s.reviews.LoadCheckpoint(ctx, fresh, "input", 0, &input)
	if err != nil || !found || len(input.Texts) != 1 {
		t.Fatalf("input=%+v found=%v err=%v", input, found, err)
	}
}
