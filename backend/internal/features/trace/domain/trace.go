// Package trace — журнал событий прогона: что делали агенты и инструменты.
//
// Трасса пишется «в стороне» от пайплайна: ошибки записи не возвращаются
// вызывающему и не влияют на прогон. Режимы определяют, сколько деталей попадает
// в хранилище: summary — метаданные, решения и метрики; full — ещё и тела
// запросов/ответов (бриф и черновики статей, поэтому включается осознанно).
package trace

import (
	"context"
	"fmt"
	"strings"
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

// DefaultMode — режим, когда TRACE_MODE не задан: агенты должны быть видны
// («что и как думали»), поэтому по умолчанию пишутся и тела.
const DefaultMode = ModeFull

// ParseMode разбирает режим из конфига. Пустая строка — full: прозрачность
// прогона важнее места в БД, а размер тела всё равно ограничен бюджетом payload.
func ParseMode(v string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(v))) {
	case "":
		return DefaultMode, nil
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

// DefaultMaxPayloadBytes — бюджет тела одного события по умолчанию. Он рассчитан
// на четыре текстовых поля LLM-события (system, user, reasoning, response) по
// MaxBodyBytes каждое: иначе длинный промпт вытеснил бы размышления и ответ.
const DefaultMaxPayloadBytes = 256 << 10

// MaxBodyBytes — сколько байт одного текста (промпт, размышления, ответ)
// попадает в трассу. Обрезка по полям, а не по событию целиком: при обрезке
// конвертом теряется всё, что не поместилось, включая ответ модели.
const MaxBodyBytes = 32 << 10

// TruncatedMark — пометка об обрезке: по ней видно, что текст неполный.
const TruncatedMark = "…(обрезано)"

// TruncateText обрезает текст до limit байт, не ломая UTF-8, и сообщает, была ли
// обрезка. limit <= 0 — без ограничения. Пометка входит в лимит.
func TruncateText(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	cut := limit - len(TruncatedMark)
	if cut < 0 {
		cut = 0
	}
	return strings.ToValidUTF8(s[:cut], "") + TruncatedMark, true
}

// SequencedSink allocates and persists an event atomically across processes.
type SequencedSink interface {
	SaveSequencedEvent(context.Context, Record) error
}

func FinishRun(rec Recorder, id string) {
	if f, ok := rec.(interface{ FinishRun(string) }); ok {
		f.FinishRun(id)
	}
}
