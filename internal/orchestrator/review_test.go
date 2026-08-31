package orchestrator

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
)

// Review: два текста, у каждого два агента (compliance, quality).
// Тексты обрабатываются параллельно, поэтому порядок ответов ролей недетерминирован:
// один текст получает (90,85) → pass/85, другой (70,95) → fix/70.
func TestReviewTwoTexts(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleCompliance] = []string{
		`{"score":90,"issues":[]}`,
		`{"score":70,"issues":["не отражено УТП"]}`,
	}
	fake.Responses[agents.RoleQuality] = []string{
		`{"score":85,"issues":["мелкая опечатка"]}`,
		`{"score":95,"issues":[]}`,
	}
	o := New(fake, Options{CostPer1KPrompt: 1, CostPer1KCompletion: 1})

	req := ReviewRequest{BriefText: "бриф", Texts: []agents.TextToReview{
		{Title: "Статья 1", Body: "текст 1"},
		{Title: "Статья 2", Body: "текст 2"},
	}}
	res, err := o.Review(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(res.Items))
	}

	got := map[string]int{}
	for _, it := range res.Items {
		got[it.Verdict]++
		// overall = min(compliance, quality)
		wantOverall := it.Compliance.Score
		if it.Quality.Score < wantOverall {
			wantOverall = it.Quality.Score
		}
		if it.Overall != wantOverall {
			t.Errorf("%s: overall = %d, want %d", it.Title, it.Overall, wantOverall)
		}
	}
	if got["pass"] != 1 || got["fix"] != 1 {
		t.Errorf("verdicts = %+v, want one pass and one fix", got)
	}
	if res.CostUSD <= 0 {
		t.Error("cost not computed")
	}
}

// Пустые заголовки заменяются на «Текст N» в прогресс-тайтлах.
func TestReviewTitleFallback(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleCompliance] = []string{`{"score":81,"issues":[]}`}
	fake.Responses[agents.RoleQuality] = []string{`{"score":82,"issues":[]}`}
	o := New(fake, Options{})

	var got []string
	rec := &recordingProgress{onPlanned: func(titles []string) { got = append(got, titles...) }}
	req := ReviewRequest{BriefText: "б", Texts: []agents.TextToReview{{Body: "x"}}}
	if _, err := o.Review(context.Background(), req, rec); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(got) != 1 || got[0] != "Текст 1" {
		t.Errorf("titles = %v", got)
	}
}

type recordingProgress struct {
	onPlanned func(titles []string)
}

func (r *recordingProgress) Strategizing()           {}
func (r *recordingProgress) TopicsPlanned(t []string) { r.onPlanned(t) }
func (r *recordingProgress) TopicWriting(int)        {}
func (r *recordingProgress) TopicReviewing(int, int) {}
func (r *recordingProgress) TopicRevising(int, int)  {}
func (r *recordingProgress) TopicDone(int, int)      {}
