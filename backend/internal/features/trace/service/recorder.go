package traceservice

import (
	"context"
	"encoding/json"
	"fmt"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
	"strings"
	"sync"
	"time"
)

type recorder struct {
	cfg  trace.Config
	sink trace.Sink

	mu  sync.Mutex
	seq map[string]int64
}

// New создаёт рекордер поверх хранилища.
func New(sink trace.Sink, cfg trace.Config) trace.Recorder {
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = trace.DefaultMaxPayloadBytes
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Mode == "" {
		cfg.Mode = trace.ModeSummary
	}
	if sink == nil || cfg.Mode == trace.ModeOff {
		return trace.Nop{}
	}
	return &recorder{cfg: cfg, sink: sink, seq: map[string]int64{}}
}

// Enabled сообщает, пишется ли трасса. Nil-получатель безопасен: рекордер —
// необязательная инфраструктура, и её отсутствие не должно ничего ломать.
func (r *recorder) Enabled() bool { return r != nil }

// trace.Event записывает событие прогона. Ошибка сериализации или записи не влияет на
// прогон: она уходит в OnError (если задан) и всё.
func (r *recorder) Event(ctx context.Context, ev trace.Event) {
	if !r.Enabled() {
		return
	}
	runID := trace.RunIDFrom(ctx)
	if runID == "" {
		return // событие не к чему привязать
	}
	if ev.Status == "" {
		ev.Status = trace.StatusOK
	}

	payload, err := r.encodePayload(ev.Payload)
	if err != nil && r.cfg.OnError != nil {
		r.cfg.OnError(fmt.Errorf("trace: payload %s/%s: %w", runID, ev.Name, err))
	}

	rec := trace.Record{
		RunID:            runID,
		Seq:              r.nextSeq(runID),
		At:               r.cfg.Now(),
		Kind:             ev.Kind,
		Name:             ev.Name,
		Status:           ev.Status,
		DurationMS:       ev.DurationMS,
		PromptTokens:     ev.PromptTokens,
		CompletionTokens: ev.CompletionTokens,
		Summary:          ev.Summary,
		PayloadJSON:      payload,
		Error:            ev.Error,
	}
	if err := r.sink.SaveRunEvent(ctx, rec); err != nil && r.cfg.OnError != nil {
		r.cfg.OnError(fmt.Errorf("trace: сохранить %s/%s: %w", runID, ev.Name, err))
	}
}

// nextSeq выдаёт следующий номер события внутри прогона.
func (r *recorder) nextSeq(runID string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq[runID]++
	return r.seq[runID]
}

// encodePayload сериализует payload: в summary он не пишется вовсе, в full
// обрезается по лимиту.
func (r *recorder) encodePayload(payload any) (string, error) {
	if payload == nil || r.cfg.Mode != trace.ModeFull {
		return "", nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return truncate(string(data), r.cfg.MaxPayloadBytes), nil
}

// truncateMark — пометка об обрезке: по ней видно, что payload неполный.
const truncateMark = "…(обрезано)"

// truncate обрезает строку по лимиту, не ломая UTF-8.
func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := s[:limit]
	cut = strings.ToValidUTF8(cut, "")
	return cut + truncateMark
}
