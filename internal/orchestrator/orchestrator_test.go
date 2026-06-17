package orchestrator

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/llm"
)

func brief() agents.Brief {
	return agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}
}

// fanout: 2 темы, критик сразу accept → 2 deliverables, по одному вызову критика.
func TestRunFanOutAcceptsImmediately(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]},{"title":"T2","angle":"a","points":["y"]}]}`,
	}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"T1","title":"A1","body":"b1","cta":"c1"}`,
		`{"topic":"T2","title":"A2","body":"b2","cta":"c2"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":88,"issues":[],"verdict":"accept"}`,
	}
	o := New(fake, Options{CriticMaxIter: 3, ScoreThreshold: 80, CostPer1KPrompt: 1, CostPer1KCompletion: 1})

	res, err := o.Run(context.Background(), brief())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Deliverables) != 2 {
		t.Fatalf("deliverables = %d, want 2", len(res.Deliverables))
	}
	if res.CostUSD <= 0 {
		t.Error("cost not computed")
	}
}

// цикл критика: первый черновик ниже порога → ревизия → второй проходит.
func TestRunCriticReviseLoop(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]}]}`,
	}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"T1","title":"v1","body":"b","cta":"c"}`,
		`{"topic":"T1","title":"v2","body":"b","cta":"c"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":50,"issues":["слабо"],"verdict":"revise"}`,
		`{"score":85,"issues":[],"verdict":"accept"}`,
	}
	o := New(fake, Options{CriticMaxIter: 3, ScoreThreshold: 80, CostPer1KPrompt: 1, CostPer1KCompletion: 1})

	res, err := o.Run(context.Background(), brief())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Deliverables[0].Title != "v2" {
		t.Errorf("title = %q, want v2 after revise", res.Deliverables[0].Title)
	}
	if res.Deliverables[0].Review.Score != 85 {
		t.Errorf("final score = %d, want 85", res.Deliverables[0].Review.Score)
	}
}

// maxIter исчерпан → берём лучший по score черновик.
func TestRunPicksBestWhenMaxIter(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]}]}`,
	}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"T1","title":"v1","body":"b","cta":"c"}`,
		`{"topic":"T1","title":"v2","body":"b","cta":"c"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":70,"issues":["x"],"verdict":"revise"}`,
		`{"score":40,"issues":["y"],"verdict":"revise"}`,
	}
	o := New(fake, Options{CriticMaxIter: 2, ScoreThreshold: 80, CostPer1KPrompt: 1, CostPer1KCompletion: 1})

	res, err := o.Run(context.Background(), brief())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Deliverables[0].Review.Score != 70 {
		t.Errorf("score = %d, want best 70", res.Deliverables[0].Review.Score)
	}
	if res.Deliverables[0].Title != "v1" {
		t.Errorf("title = %q, want v1 (best draft)", res.Deliverables[0].Title)
	}
}

func TestRunFailsWhenStrategistErrors(t *testing.T) {
	fake := llm.NewFake() // нет ответов → стратег вернёт ошибку
	o := New(fake, Options{CriticMaxIter: 1, ScoreThreshold: 80})
	if _, err := o.Run(context.Background(), brief()); err == nil {
		t.Fatal("expected error")
	}
}

// MaxTopics ограничивает число обрабатываемых тем сверху.
func TestRunCapsTopics(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1"},{"title":"T2"},{"title":"T3"}]}`,
	}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"T1","title":"A1","body":"b","cta":"c"}`,
		`{"topic":"T2","title":"A2","body":"b","cta":"c"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":90,"issues":[],"verdict":"accept"}`,
		`{"score":90,"issues":[],"verdict":"accept"}`,
	}
	o := New(fake, Options{CriticMaxIter: 1, ScoreThreshold: 80, MaxTopics: 2, CostPer1KPrompt: 1, CostPer1KCompletion: 1})

	res, err := o.Run(context.Background(), brief())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Deliverables) != 2 {
		t.Fatalf("deliverables = %d, want 2 (capped)", len(res.Deliverables))
	}
	if len(res.Strategy.Topics) != 2 {
		t.Errorf("strategy topics = %d, want 2 (truncated)", len(res.Strategy.Topics))
	}
}
