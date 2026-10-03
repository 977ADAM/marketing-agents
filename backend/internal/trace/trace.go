// Package trace — журнал событий прогона: что делали агенты и инструменты.
//
// Трасса пишется «в стороне» от пайплайна: ошибки записи не возвращаются
// вызывающему и не влияют на прогон. Режимы определяют, сколько деталей попадает
// в хранилище: summary — метаданные, решения и метрики; full — ещё и тела
// запросов/ответов (бриф и черновики статей, поэтому включается осознанно).
package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Kind — вид события в ленте.
type Kind string

const (
	KindLLM      Kind = "llm"      // вызов модели
	KindWordstat Kind = "wordstat" // обращение к Wordstat
	KindDecision Kind = "decision" // решение кода: отбор, порог, fallback
	KindPhase    Kind = "phase"    // смена этапа прогона
	KindResult   Kind = "result"   // итог прогона
)

// Status — исход события.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// Mode — сколько деталей пишем.
type Mode string

const (
	ModeOff     Mode = "off"
	ModeSummary Mode = "summary"
	ModeFull    Mode = "full"
)

// ParseMode разбирает режим из конфига. Пустая строка — summary.
func ParseMode(v string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(v))) {
	case "":
		return ModeSummary, nil
	case ModeOff:
		return ModeOff, nil
	case ModeSummary:
		return ModeSummary, nil
	case ModeFull:
		return ModeFull, nil
	}
	return "", fmt.Errorf("invalid trace mode %q: allowed off, summary, full", v)
}

// Event — то, что хочет записать вызывающий код.
type Event struct {
	Kind             Kind
	Name             string
	Status           Status
	Summary          string
	DurationMS       int64
	PromptTokens     int
	CompletionTokens int
	// Payload — детали (промпт и ответ, параметры запроса). Сериализуется в JSON
	// и попадает в хранилище только в режиме full.
	Payload any
	Error   string
}

// Record — событие в том виде, в каком оно уходит в хранилище: с номером,
// временем и уже сериализованным (при необходимости обрезанным) payload.
type Record struct {
	RunID            string
	Seq              int64
	At               time.Time
	Kind             Kind
	Name             string
	Status           Status
	DurationMS       int64
	PromptTokens     int
	CompletionTokens int
	Summary          string
	PayloadJSON      string
	Error            string
}

// Sink — хранилище событий (реализуется стором).
type Sink interface {
	SaveRunEvent(ctx context.Context, rec Record) error
}

// Recorder — то, что нужно вызывающему коду: записать событие прогона.
type Recorder interface {
	// Event записывает событие; ошибки не возвращаются наружу.
	Event(ctx context.Context, ev Event)
	// Enabled сообщает, пишется ли трасса вообще.
	Enabled() bool
}

// --- прогон в контексте ---

type runIDKey struct{}

// WithRunID помечает контекст прогоном: все события трассы внутри него будут
// привязаны к этому run_id.
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey{}, runID)
}

// RunIDFrom достаёт идентификатор прогона из контекста (пусто — не размечен).
func RunIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runID, _ := ctx.Value(runIDKey{}).(string)
	return runID
}

// Nop — рекордер-заглушка: трасса выключена или не настроена.
type Nop struct{}

func (Nop) Event(context.Context, Event) {}
func (Nop) Enabled() bool                { return false }

// OrNop подстраховывает от nil-рекордера.
func OrNop(r Recorder) Recorder {
	if r == nil {
		return Nop{}
	}
	return r
}

// Config — настройки рекордера.
type Config struct {
	Mode Mode
	// MaxPayloadBytes ограничивает один payload; 0 — дефолт.
	MaxPayloadBytes int
	// Now подменяется в тестах.
	Now func() time.Time
	// OnError получает ошибки записи (логирование); nil — молча.
	OnError func(error)
}

// DefaultMaxPayloadBytes — лимит payload по умолчанию: бриф и черновик статьи
// целиком не нужны, а мегабайты в БД не нужны тем более.
const DefaultMaxPayloadBytes = 32 << 10

type recorder struct {
	cfg  Config
	sink Sink

	mu  sync.Mutex
	seq map[string]int64
}

// New создаёт рекордер поверх хранилища.
func New(sink Sink, cfg Config) Recorder {
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = DefaultMaxPayloadBytes
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeSummary
	}
	if sink == nil || cfg.Mode == ModeOff {
		return Nop{}
	}
	return &recorder{cfg: cfg, sink: sink, seq: map[string]int64{}}
}

// Enabled сообщает, пишется ли трасса. Nil-получатель безопасен: рекордер —
// необязательная инфраструктура, и её отсутствие не должно ничего ломать.
func (r *recorder) Enabled() bool { return r != nil }

// Event записывает событие прогона. Ошибка сериализации или записи не влияет на
// прогон: она уходит в OnError (если задан) и всё.
func (r *recorder) Event(ctx context.Context, ev Event) {
	if !r.Enabled() {
		return
	}
	runID := RunIDFrom(ctx)
	if runID == "" {
		return // событие не к чему привязать
	}
	if ev.Status == "" {
		ev.Status = StatusOK
	}

	payload, err := r.encodePayload(ev.Payload)
	if err != nil && r.cfg.OnError != nil {
		r.cfg.OnError(fmt.Errorf("trace: payload %s/%s: %w", runID, ev.Name, err))
	}

	rec := Record{
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
	if payload == nil || r.cfg.Mode != ModeFull {
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
