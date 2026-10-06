package tracing

import (
	"context"
	"fmt"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	"time"

	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// TracingClient оборачивает клиента и пишет вызовы в трассу прогона: роль, модель,
// токены, длительность и статус.
//
// Тела промптов передаются в событие всегда, а решает о их записи рекордер: в
// режиме summary они не сохраняются. Так декоратор не знает про режимы, а трасса
// остаётся единственным местом, где определяется, что писать в БД.
type TracingClient struct {
	inner corellm.Client
	rec   trace.Recorder
}

// NewTracing оборачивает клиент. Рекордер nil-безопасен: без него клиент работает
// как обычно, просто без трассы.
func NewLLM(inner corellm.Client, rec trace.Recorder) *TracingClient {
	return &TracingClient{inner: inner, rec: trace.OrNop(rec)}
}

// modelNamer — необязательная возможность клиента сообщить модель роли.
type modelNamer interface {
	ModelFor(role string) string
}

func (c *TracingClient) Complete(ctx context.Context, role, system, user string, out any) (corellm.Usage, error) {
	start := time.Now()
	usage, err := c.inner.Complete(ctx, role, system, user, out)

	ev := trace.Event{
		Kind:             trace.KindLLM,
		Name:             role,
		Status:           trace.StatusOK,
		DurationMS:       time.Since(start).Milliseconds(),
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		Payload:          c.payload(role, system, user, out, usage, err),
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = fmt.Sprintf("%s: ошибка вызова модели", role)
	} else {
		ev.Summary = summarize(role, usage, ev.DurationMS)
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.rec.Event(final, ev)

	return usage, err
}

// payload собирает тело события: метаданные вызова и длинные тексты — промпты,
// размышления модели и её ответ. Каждый текст обрезается отдельно, а имена
// обрезанных полей перечисляются рядом: иначе по одному большому промпту нельзя
// было бы понять, что размышления и ответ в трассу не поместились.
func (c *TracingClient) payload(role, system, user string, out any, usage corellm.Usage, err error) map[string]any {
	p := map[string]any{"role": role}
	if namer, ok := c.inner.(modelNamer); ok {
		if model := namer.ModelFor(role); model != "" {
			p["model"] = model
		}
	}
	if usage.FinishReason != "" {
		p["finish_reason"] = usage.FinishReason
	}
	if usage.ReasoningTokens > 0 {
		p["reasoning_tokens"] = usage.ReasoningTokens
	}

	var truncated []string
	put := func(name, text string) {
		if text == "" {
			return
		}
		cut, wasCut := trace.TruncateText(text, trace.MaxBodyBytes)
		p[name] = cut
		if wasCut {
			truncated = append(truncated, name)
		}
	}
	put("system", system)
	put("user", user)
	put("reasoning", usage.Reasoning)
	switch {
	case usage.Response != "":
		// Сырой ответ модели: в нём видно и опечатки, и обёртки, и отказ.
		put("response", usage.Response)
	case err == nil:
		// Модель не вернула текст, но ответ разобран — отдаём структуру.
		p["response"] = out
	}
	if len(truncated) > 0 {
		p["truncated_fields"] = truncated
	}
	return p
}

// summarize — строка события: счётчик размышлений виден и в режиме summary,
// где тела не сохраняются.
func summarize(role string, usage corellm.Usage, durationMS int64) string {
	if usage.ReasoningTokens > 0 {
		return fmt.Sprintf("%s: %d → %d токенов (из них %d размышления) за %d мс",
			role, usage.PromptTokens, usage.CompletionTokens, usage.ReasoningTokens, durationMS)
	}
	return fmt.Sprintf("%s: %d → %d токенов за %d мс",
		role, usage.PromptTokens, usage.CompletionTokens, durationMS)
}

func (c *TracingClient) ModelFor(role string) string {
	if n, ok := c.inner.(modelNamer); ok {
		return n.ModelFor(role)
	}
	return ""
}
