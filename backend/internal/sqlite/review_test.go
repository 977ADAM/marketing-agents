package sqlite_test

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
)

func TestReviewRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	id, err := st.reviews.CreateCheck(ctx, "", "Продукт: шины Ikon. Запрет: не упоминать другие бренды.")
	if err != nil {
		t.Fatalf("CreateCheck: %v", err)
	}
	if err := st.reviews.MarkCheckRunning(ctx, id); err != nil {
		t.Fatalf("MarkCheckRunning: %v", err)
	}
	snap := run.Snapshot{
		Phase:      run.PhaseProducing,
		TopicTotal: 1,
		TopicsDone: 0,
		Percent:    10,
		Topics:     []run.TopicProgress{{Index: 0, Title: "Статья", State: run.TopicWriting}},
	}
	if err := st.reviews.SaveCheckProgress(ctx, id, snap); err != nil {
		t.Fatalf("SaveCheckProgress: %v", err)
	}
	res := review.Result{
		Items: []review.TextReport{{
			Title:      "Статья",
			Compliance: review.CheckScore{Score: 90, Issues: []string{"нет слогана"}},
			Quality:    review.CheckScore{Score: 85},
			Overall:    85,
			Verdict:    "pass",
		}},
		CostUSD: 0.05,
	}
	if err := st.reviews.CompleteCheck(ctx, id, res); err != nil {
		t.Fatalf("CompleteCheck: %v", err)
	}

	got, err := st.reviews.GetCheck(ctx, id)
	if err != nil {
		t.Fatalf("GetCheck: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.BriefText != "Продукт: шины Ikon. Запрет: не упоминать другие бренды." {
		t.Errorf("brief_text = %q", got.BriefText)
	}
	if got.Result == nil || len(got.Result.Items) != 1 || got.Result.Items[0].Overall != 85 ||
		got.Result.Items[0].Compliance.Issues[0] != "нет слогана" {
		t.Errorf("result = %+v", got.Result)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.05 {
		t.Errorf("cost = %v", got.CostUSD)
	}
	if got.Progress == nil || got.Progress.Percent != 10 {
		t.Errorf("progress = %+v", got.Progress)
	}

	items, err := st.reviews.ListChecks(ctx, 10)
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Errorf("ListChecks = %+v", items)
	}

	if _, err := st.reviews.GetCheck(ctx, "нет-такой-проверки"); err != review.ErrNotFound {
		t.Errorf("GetCheck(unknown) err = %v, want ErrNotFound", err)
	}
}
