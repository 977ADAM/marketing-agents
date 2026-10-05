package repository_test

import (
	"context"
	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	"testing"
)

func TestProgressRejectsOlderRevisionAndLateUpdates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, err := s.campaigns.Create(ctx, "", campaign.Brief{Product: "P"})
	if err != nil {
		t.Fatal(err)
	}
	for _, snap := range []run.Snapshot{{Revision: 2, Phase: run.PhaseProducing}, {Revision: 1, Phase: run.PhaseStrategizing}} {
		if err := s.campaigns.SaveProgress(ctx, id, snap); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.campaigns.Get(ctx, id)
	if err != nil || c.Progress.Revision != 2 {
		t.Fatalf("record=%+v err=%v", c, err)
	}
	if err := s.campaigns.Complete(ctx, id, campaign.Outcome{}); err != nil {
		t.Fatal(err)
	}
	if err := s.campaigns.SaveProgress(ctx, id, run.Snapshot{Revision: 3, Phase: run.PhaseProducing}); err != nil {
		t.Fatal(err)
	}
	c, err = s.campaigns.Get(ctx, id)
	if err != nil || c.Status != "done" || c.Progress.Phase != run.PhaseDone {
		t.Fatalf("terminal=%+v err=%v", c, err)
	}
	rid, err := s.reviews.CreateCheck(ctx, "", "B")
	if err != nil {
		t.Fatal(err)
	}
	for _, snap := range []run.Snapshot{{Revision: 2, Phase: run.PhaseProducing}, {Revision: 1, Phase: run.PhaseStrategizing}} {
		if err := s.reviews.SaveCheckProgress(ctx, rid, snap); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.reviews.FailCheck(ctx, rid, "failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.reviews.SaveCheckProgress(ctx, rid, run.Snapshot{Revision: 3, Phase: run.PhaseProducing}); err != nil {
		t.Fatal(err)
	}
	r, err := s.reviews.GetCheck(ctx, rid)
	if err != nil || r.Progress.Revision != 2 || r.Progress.Phase != run.PhaseFailed {
		t.Fatalf("review=%+v err=%v", r, err)
	}
}
