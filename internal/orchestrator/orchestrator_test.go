package orchestrator

import (
	"context"
	"fmt"
	"sync"
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

	res, err := o.Run(context.Background(), brief(), nil)
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

	res, err := o.Run(context.Background(), brief(), nil)
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

	res, err := o.Run(context.Background(), brief(), nil)
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
	if _, err := o.Run(context.Background(), brief(), nil); err == nil {
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

	res, err := o.Run(context.Background(), brief(), nil)
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

// recordProgress фиксирует порядок вызовов Progress (потокобезопасно).
type recordProgress struct {
	mu     sync.Mutex
	events []string
}

func (r *recordProgress) add(e string) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recordProgress) Strategizing()            { r.add("strategizing") }
func (r *recordProgress) TopicsPlanned(t []string) { r.add(fmt.Sprintf("planned:%d", len(t))) }
func (r *recordProgress) TopicWriting(i int)       { r.add(fmt.Sprintf("writing:%d", i)) }
func (r *recordProgress) TopicReviewing(i, it int) { r.add(fmt.Sprintf("reviewing:%d:%d", i, it)) }
func (r *recordProgress) TopicRevising(i, it int)  { r.add(fmt.Sprintf("revising:%d:%d", i, it)) }
func (r *recordProgress) TopicDone(i, sc int)      { r.add(fmt.Sprintf("done:%d:%d", i, sc)) }

func TestRunEmitsProgress(t *testing.T) {
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
	rec := &recordProgress{}

	if _, err := o.Run(context.Background(), brief(), rec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"strategizing", "planned:1", "writing:0", "reviewing:0:1", "revising:0:1", "reviewing:0:2", "done:0:85"}
	if len(rec.events) != len(want) {
		t.Fatalf("events = %v, want %v", rec.events, want)
	}
	for i := range want {
		if rec.events[i] != want[i] {
			t.Fatalf("events = %v, want %v", rec.events, want)
		}
	}
}
