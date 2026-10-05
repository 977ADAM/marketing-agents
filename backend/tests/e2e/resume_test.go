package e2e_test

import (
	"context"
	"github.com/977ADAM/marketing-agents/internal/adapters/accounting"
	runner "github.com/977ADAM/marketing-agents/internal/application/runner"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	repo "github.com/977ADAM/marketing-agents/internal/features/campaign/repository/mariadb"
	service "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	reviewrepo "github.com/977ADAM/marketing-agents/internal/features/review/repository/mariadb"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	"github.com/977ADAM/marketing-agents/internal/testkit/mock"
	"github.com/977ADAM/marketing-agents/internal/testkit/testdb"
	"strings"
	"testing"
	"time"
)

func TestResumeSkipsSavedArticleAndStrategy(t *testing.T) {
	db, _ := testdb.New(t)
	ctx := context.Background()
	c := repo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	f := mock.NewLLM()
	f.Responses["strategist"] = []string{`{"positioning":"P","topics":[{"title":"one"},{"title":"two"}]}`}
	f.Responses["copywriter"] = []string{`{"title":"one","body":"ready"}`, `{"title":"","body":"invalid"}`, `{"title":"two","body":"ready"}`}
	workflow := service.NewWorkflow(accounting.New(f), service.Options{ParallelTexts: 1, Checkpoints: c, CostPer1KPrompt: 1})
	hub := runner.NewHub(ctx, c, reviews)
	first := runner.NewRunner(ctx, c, reviews, workflow, nil, time.Minute, nil, hub)
	b := campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}
	id, err := service.NewService(c, first).Create(ctx, "", b)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Drain(); err != nil {
		t.Fatal(err)
	}
	partial, err := c.Get(ctx, id)
	if err != nil || partial.Status != "failed" || len(partial.Deliverables) != 1 || partial.Strategy == nil || !partial.ResumeAvailable || partial.CostUSD == nil {
		t.Fatalf("partial=%+v err=%v", partial, err)
	}
	second := runner.NewRunner(ctx, c, reviews, workflow, nil, time.Minute, nil, runner.NewHub(ctx, c, reviews))
	if _, err := service.NewService(c, second).Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := second.Drain(); err != nil {
		t.Fatal(err)
	}
	final, err := c.Get(ctx, id)
	if err != nil || final.Status != "done" || len(final.Deliverables) != 2 || f.Calls["strategist"] != 1 || f.Calls["copywriter"] != 3 || final.Usage == nil || final.Usage.PromptTokens != 40 || final.CostUSD == nil || *final.CostUSD != 0.04 {
		t.Fatalf("final=%+v calls=%v err=%v", final, f.Calls, err)
	}
}

func TestReviewResumeUsesPersistedTexts(t *testing.T) {
	db, _ := testdb.New(t)
	ctx := context.Background()
	c := repo.NewCampaigns(db)
	reviews := reviewrepo.NewReviews(db)
	f := mock.NewLLM()
	f.Responses["compliance"] = []string{`{"score":90,"issues":[]}`, `{"score":90,"issues":[]}`, `{"score":90,"issues":[]}`}
	f.Responses["quality"] = []string{`{"score":90,"issues":[]}`, `{"issues":[]}`, `{"score":90,"issues":[]}`}
	workflow := reviewservice.NewWorkflow(accounting.New(f), reviewservice.Options{ParallelTexts: 1, Checkpoints: reviews, CostPer1KPrompt: 1})
	first := runner.NewRunner(ctx, c, reviews, nil, workflow, time.Minute, nil, runner.NewHub(ctx, c, reviews))
	req := review.Request{BriefText: "B", Texts: []review.TextToReview{{Title: "one", Body: "first body"}, {Title: "two", Body: "second body"}}}
	id, err := reviewservice.NewService(reviews, first).Create(ctx, "", req)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Drain(); err != nil {
		t.Fatal(err)
	}
	partial, err := reviews.GetCheck(ctx, id)
	if err != nil || partial.Result == nil || len(partial.Result.Items) != 1 || !partial.ResumeAvailable {
		t.Fatalf("partial=%+v err=%v", partial, err)
	}
	second := runner.NewRunner(ctx, c, reviews, nil, workflow, time.Minute, nil, runner.NewHub(ctx, c, reviews))
	if _, err := reviewservice.NewService(reviews, second).Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := second.Drain(); err != nil {
		t.Fatal(err)
	}
	final, err := reviews.GetCheck(ctx, id)
	if err != nil || final.Status != "done" || len(final.Result.Items) != 2 || f.Calls["compliance"] != 3 || f.Calls["quality"] != 3 || final.Usage == nil || final.Usage.PromptTokens != 60 {
		t.Fatalf("final=%+v err=%v calls=%v", final, err, f.Calls)
	}
	last, _ := f.LastRequest()
	if !strings.Contains(last.User, "second body") {
		t.Fatal("persisted text lost")
	}
}
