package llm_test

import (
	"context"
	"errors"
	"github.com/977ADAM/marketing-agents/internal/core/corellm"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/llm"
	"github.com/977ADAM/marketing-agents/internal/trace"
)

// captureRecorder собирает события трассы для проверок.
type captureRecorder struct {
	events []trace.Event
}

func (c *captureRecorder) Event(_ context.Context, ev trace.Event) { c.events = append(c.events, ev) }
func (c *captureRecorder) Enabled() bool                           { return true }

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
	rec := &captureRecorder{}
	client := llm.NewTracing(&stubLLM{usage: corellm.Usage{PromptTokens: 120, CompletionTokens: 340}, model: "deepseek-v4-flash"}, rec)

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
	rec := &captureRecorder{}
	client := llm.NewTracing(&stubLLM{err: errors.New("модель недоступна")}, rec)

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
	client := llm.NewTracing(&stubLLM{usage: corellm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil)
	if _, err := client.Complete(context.Background(), "role", "s", "u", &struct{}{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}
