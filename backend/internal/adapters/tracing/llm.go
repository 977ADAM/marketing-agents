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

	payload := map[string]any{"system": system, "user": user}
	if namer, ok := c.inner.(modelNamer); ok {
		payload["model"] = namer.ModelFor(role)
	}

	ev := trace.Event{
		Kind:             trace.KindLLM,
		Name:             role,
		Status:           trace.StatusOK,
		DurationMS:       time.Since(start).Milliseconds(),
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		Payload:          payload,
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = fmt.Sprintf("%s: ошибка вызова модели", role)
	} else {
		ev.Summary = fmt.Sprintf("%s: %d → %d токенов за %d мс",
			role, usage.PromptTokens, usage.CompletionTokens, ev.DurationMS)
	}
	c.rec.Event(ctx, ev)

	return usage, err
}
