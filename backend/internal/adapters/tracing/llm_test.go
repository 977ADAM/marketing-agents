package tracing_test

import (
	"context"
	"errors"
	tracing "github.com/977ADAM/marketing-agents/internal/adapters/tracing"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	"strings"
	"testing"
)

// llmCaptureRecorder собирает события трассы для проверок.
type llmCaptureRecorder struct {
	events []trace.Event
}

func (c *llmCaptureRecorder) Event(_ context.Context, ev trace.Event) {
	c.events = append(c.events, ev)
}
func (c *llmCaptureRecorder) Enabled() bool { return true }

// stubLLM — подменённый клиент модели.
type stubLLM struct {
	usage corellm.Usage
	err   error
	model string
}

func (s *stubLLM) Complete(context.Context, string, string, string, any) (corellm.Usage, error) {
	return s.usage, s.err
}

func (s *stubLLM) ModelFor(string) string { return s.model }

func TestTracingClientRecordsCall(t *testing.T) {
	rec := &llmCaptureRecorder{}
	client := tracing.NewLLM(&stubLLM{usage: corellm.Usage{PromptTokens: 120, CompletionTokens: 340}, model: "deepseek-v4-flash"}, rec)

	usage, err := client.Complete(context.Background(), "copywriter", "система", "пользователь", &struct{}{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if usage.PromptTokens != 120 || usage.CompletionTokens != 340 {
		t.Errorf("usage = %+v", usage)
	}
	if len(rec.events) != 1 {
		t.Fatalf("событий %d, want 1", len(rec.events))
	}

	ev := rec.events[0]
	if ev.Kind != trace.KindLLM || ev.Name != "copywriter" {
		t.Errorf("вид/имя = %q/%q", ev.Kind, ev.Name)
	}
	if ev.Status != trace.StatusOK {
		t.Errorf("Status = %q, want ok", ev.Status)
	}
	if ev.PromptTokens != 120 || ev.CompletionTokens != 340 {
		t.Errorf("токены не записаны: %+v", ev)
	}
	if !strings.Contains(ev.Summary, "copywriter") || !strings.Contains(ev.Summary, "340") {
		t.Errorf("summary = %q", ev.Summary)
	}

	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	if payload["system"] != "система" || payload["user"] != "пользователь" {
		t.Errorf("промпты потерялись: %#v", payload)
	}
	if payload["model"] != "deepseek-v4-flash" {
		t.Errorf("модель не записана: %#v", payload)
	}
}

func TestTracingClientRecordsError(t *testing.T) {
	rec := &llmCaptureRecorder{}
	client := tracing.NewLLM(&stubLLM{err: errors.New("модель недоступна")}, rec)

	if _, err := client.Complete(context.Background(), "critic", "s", "u", &struct{}{}); err == nil {
		t.Fatal("ожидалась ошибка")
	}

	ev := rec.events[0]
	if ev.Status != trace.StatusError {
		t.Errorf("Status = %q, want error", ev.Status)
	}
	if !strings.Contains(ev.Error, "модель недоступна") {
		t.Errorf("текст ошибки потерян: %q", ev.Error)
	}
}

// Без рекордера (или с nil) клиент обязан работать как обычно.
func TestTracingClientWithoutRecorderStillWorks(t *testing.T) {
	client := tracing.NewLLM(&stubLLM{usage: corellm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil)
	if _, err := client.Complete(context.Background(), "role", "s", "u", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

// Размышления модели — то, ради чего трасса включается: они должны попадать в
// тело события вместе со счётчиком, а не теряться.
func TestTracingClientRecordsReasoning(t *testing.T) {
	rec := &llmCaptureRecorder{}
	usage := corellm.Usage{
		PromptTokens: 100, CompletionTokens: 150, ReasoningTokens: 120,
		Response: `{"score":7}`, Reasoning: "Сначала посчитаю: 2+2=4", FinishReason: "stop",
	}
	client := tracing.NewLLM(&stubLLM{usage: usage, model: "deepseek-v4-pro"}, rec)

	if _, err := client.Complete(context.Background(), "critic", "система", "вопрос", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	ev := rec.events[0]
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	if payload["reasoning"] != "Сначала посчитаю: 2+2=4" {
		t.Errorf("размышления потерялись: %#v", payload)
	}
	if payload["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %#v", payload["finish_reason"])
	}
	if payload["reasoning_tokens"] != 120 {
		t.Errorf("reasoning_tokens = %#v", payload["reasoning_tokens"])
	}
	if payload["response"] != `{"score":7}` {
		t.Errorf("response = %#v", payload["response"])
	}
	if _, cut := payload["truncated_fields"]; cut {
		t.Errorf("короткие поля не должны помечаться обрезкой: %#v", payload)
	}
	if !strings.Contains(ev.Summary, "размышления") || !strings.Contains(ev.Summary, "120") {
		t.Errorf("summary = %q, want упоминание размышлений", ev.Summary)
	}
}

// Длинное поле обрезается само, но не вытесняет остальные: ответ остаётся целым.
func TestTracingClientTruncatesLongBodiesSeparately(t *testing.T) {
	long := strings.Repeat("я", trace.MaxBodyBytes+100)
	rec := &llmCaptureRecorder{}
	usage := corellm.Usage{Response: "короткий ответ", Reasoning: long}
	client := tracing.NewLLM(&stubLLM{usage: usage}, rec)

	if _, err := client.Complete(context.Background(), "strategist", "с", "u", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	payload, ok := rec.events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", rec.events[0].Payload)
	}
	got, _ := payload["reasoning"].(string)
	if len(got) > trace.MaxBodyBytes {
		t.Errorf("размышления не обрезаны: %d байт", len(got))
	}
	if !strings.HasSuffix(got, trace.TruncatedMark) {
		t.Errorf("нет пометки об обрезке: %q", got[len(got)-20:])
	}
	fields, _ := payload["truncated_fields"].([]string)
	if len(fields) != 1 || fields[0] != "reasoning" {
		t.Errorf("truncated_fields = %#v, want [reasoning]", payload["truncated_fields"])
	}
	if payload["response"] != "короткий ответ" {
		t.Errorf("короткий ответ пострадал: %#v", payload["response"])
	}
}
