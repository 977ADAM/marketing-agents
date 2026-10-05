package campaignservice

import (
	"context"
	"fmt"
	"time"

	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// Запись трассы прогона. Рекордер необязателен: без него (или в режиме off)
// события просто никуда не идут, пайплайн об этом не знает.

// traceEvent пишет произвольное событие.
func (o *Workflow) traceEvent(ctx context.Context, ev trace.Event) {
	o.trace.Event(ctx, ev)
}

// traceDecision пишет решение кода: сеялки, отбор с порогами, fallback, итерации
// критика. Такие события декораторами не поймать — их фиксирует сам оркестратор.
func (o *Workflow) traceDecision(ctx context.Context, name, summary string, payload any) {
	o.trace.Event(ctx, trace.Event{
		Kind:    trace.KindDecision,
		Name:    name,
		Status:  trace.StatusOK,
		Summary: summary,
		Payload: payload,
	})
}

// traceResult пишет итог прогона: сколько тем, обращений к Wordstat и денег.
func (o *Workflow) traceResult(ctx context.Context, res Result, err error) {
	ev := trace.Event{
		Kind:             trace.KindResult,
		Name:             "run",
		Status:           trace.StatusOK,
		PromptTokens:     res.Usage.PromptTokens,
		CompletionTokens: res.Usage.CompletionTokens,
		Payload: map[string]any{
			"topics":            len(res.Strategy.Topics),
			"candidates":        len(res.Strategy.TopicCandidates),
			"deliverables":      len(res.Deliverables),
			"wordstat_calls":    res.Strategy.WordstatCalls,
			"cost_usd":          res.CostUSD,
			"prompt_tokens":     res.Usage.PromptTokens,
			"completion_tokens": res.Usage.CompletionTokens,
		},
		Summary: fmt.Sprintf("готово: статей %d, тем рассмотрено %d, обращений к Wordstat %d, $%.4f",
			len(res.Deliverables), len(res.Strategy.TopicCandidates), res.Strategy.WordstatCalls, res.CostUSD),
	}
	if err != nil {
		ev.Status = trace.StatusError
		ev.Error = err.Error()
		ev.Summary = "прогон прерван: " + err.Error()
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	o.trace.Event(final, ev)
	trace.FinishRun(o.trace, trace.RunIDFrom(ctx))
}
