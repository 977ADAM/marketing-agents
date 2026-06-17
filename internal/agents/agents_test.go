package agents

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/llm"
)

func testBrief() Brief {
	return Brief{Product: "Эко-бутылка", Goal: "Рост продаж", Audience: "ЗОЖ-аудитория 25-40", Tone: "дружелюбный"}
}

func TestStrategistReturnsTopics(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses["strategist"] = []string{
		`{"positioning":"умная гидратация","topics":[{"title":"Зачем пить воду","angle":"польза","points":["а","б"]},{"title":"Эко-выбор","angle":"экология","points":["в"]}]}`,
	}
	s := NewStrategist(fake)

	strat, usage, err := s.Run(context.Background(), testBrief())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(strat.Topics) != 2 {
		t.Fatalf("topics = %d, want 2", len(strat.Topics))
	}
	if strat.Positioning == "" {
		t.Error("positioning empty")
	}
	if usage.PromptTokens == 0 {
		t.Error("usage not captured")
	}
}

func TestStrategistRejectsEmptyTopics(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses["strategist"] = []string{`{"positioning":"x","topics":[]}`}
	s := NewStrategist(fake)
	if _, _, err := s.Run(context.Background(), testBrief()); err == nil {
		t.Fatal("expected error on empty topics")
	}
}

func TestCopywriterWritesArticle(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses["copywriter"] = []string{
		`{"topic":"Зачем пить воду","title":"Пей умно","body":"Текст статьи...","cta":"Купить"}`,
	}
	cw := NewCopywriter(fake)
	topic := Topic{Title: "Зачем пить воду", Angle: "польза", Points: []string{"а"}}

	art, _, err := cw.Run(context.Background(), testBrief(), Strategy{Positioning: "p"}, topic)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if art.Title == "" || art.Body == "" || art.CTA == "" {
		t.Errorf("incomplete article: %+v", art)
	}
}

func TestCopywriterReviseUsesIssues(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses["copywriter"] = []string{
		`{"topic":"t","title":"v2","body":"улучшено","cta":"Жми"}`,
	}
	cw := NewCopywriter(fake)
	prev := Article{Topic: "t", Title: "v1", Body: "слабо", CTA: "Жми"}
	rev := Review{Score: 50, Issues: []string{"слабый заход"}, Verdict: "revise"}

	art, _, err := cw.Revise(context.Background(), prev, rev)
	if err != nil {
		t.Fatalf("Revise: %v", err)
	}
	if art.Title != "v2" {
		t.Errorf("title = %q, want v2", art.Title)
	}
}
