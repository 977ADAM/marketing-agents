// Package briefservice — сценарий интервью по брифу: один ход модели за вызов.
//
// Сервис не хранит диалог: каждый ход — чистая функция от присланной истории и
// прежнего черновика брифа. Модель отвечает прозой, а в конце добавляет машинный
// блок <<<BRIEF с JSON: проза уходит в onDelta сразу, хвост буферизуется, поэтому
// служебный JSON никогда не попадает в реплику.
package briefservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	corelogger "github.com/977ADAM/marketing-agents/internal/core/logger"
	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
)

const (
	// RoleInterviewer — роль модели, ведущей интервью. Уходит в маршрутизацию
	// моделей и в карту скилов.
	RoleInterviewer = "interviewer"

	// MaxMessages — сколько реплик истории принимаем за один ход.
	MaxMessages = 20
	// MaxChars — суммарный лимит текста истории в байтах (32 КиБ).
	MaxChars = 32 << 10

	// Коды валидации истории: их видит клиент, поэтому текст ошибки — сам код.
	codeEmptyHistory   = "empty_history"
	codeHistoryTooLong = "history_too_long"

	// Роли реплик в присланной истории.
	roleUser      = "user"
	roleAssistant = "assistant"
)

// interviewerSystem — инструкция интервьюера.
//
// Текст скила campaign-context и прозаическую оговорку подставляет декоратор
// skills, поэтому здесь только правила самого интервью: лимит уточнений и
// формат машинного хвоста.
const interviewerSystem = `Ты — интервьюер, который помогает клиенту собрать бриф нативной кампании.

Веди разговор по-русски, коротко и по делу. За весь диалог задай не больше трёх уточняющих вопросов и только о том, без чего нельзя выбрать полезный угол: продукт, цель, аудитория, тон. Объединяй вопросы, не допрашивай по одному полю. Если данных достаточно или пользователь просит начать, вопросов не задавай: подведи короткий итог и обнови бриф. Неполный бриф не блокирует работу — просто не выдумывай отсутствующее.

Реплика — проза, без заголовков и служебных пояснений. Когда бриф появился или изменился, в самом конце ответа добавь машинный блок: строку <<<BRIEF и сразу за ней компактный JSON без пояснений и без код-фенса, например:
<<<BRIEF
{"product":"...","goal":"...","audience":"...","tone":"...","region":"225","topics_count":3}

Правила машинного блока:
- он всегда последний и единственный в ответе; после JSON не пиши ничего;
- в JSON только известные или изменившиеся поля; неизвестные не выдумывай;
- region — числовой geo id Яндекса (225 — Россия, 213 — Москва), topics_count —
  сколько статей нужно по медиаплану;
- если бриф не менялся, машинный блок не нужен.`

// Options — зависимости и настройки сервиса интервью.
type Options struct {
	Stream Streamer
	Log    corelogger.Logger
	// MaxMessages и MaxChars переопределяют лимиты истории; нулевые значения
	// заменяются дефолтами MaxMessages и MaxChars, чтобы composition root мог
	// их не указывать.
	MaxMessages, MaxChars int
}

// Service ведёт интервью по брифу.
type Service struct {
	stream Streamer
	log    corelogger.Logger

	maxMessages, maxChars int
}

// New собирает сервис: пустые настройки заменяются дефолтами.
func New(opt Options) *Service {
	s := &Service{
		stream:      opt.Stream,
		log:         opt.Log,
		maxMessages: opt.MaxMessages,
		maxChars:    opt.MaxChars,
	}
	if s.maxMessages <= 0 {
		s.maxMessages = MaxMessages
	}
	if s.maxChars <= 0 {
		s.maxChars = MaxChars
	}
	if s.log == nil {
		s.log = corelogger.Nop()
	}
	return s
}

// Ask ведёт один ход интервью: проверяет историю, стримит реплику модели и
// возвращает обновлённый бриф.
//
// В onDelta уходит только проза до маркера — сразу по мере генерации; всё от
// маркера и дальше копится в буфере и разбирается по завершении. Битый или
// отсутствующий хвост не ошибка: реплика доходит целиком, бриф остаётся прежним.
func (s *Service) Ask(ctx context.Context, msgs []brief.Message, prev brief.Draft, onDelta func(string)) (res brief.Result, usage corellm.Usage, err error) {
	started := time.Now()
	defer func() {
		s.log.Info("interview",
			"duration_ms", time.Since(started).Milliseconds(),
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"messages", len(msgs),
			"status", res.Status,
		)
	}()

	if err = s.validateHistory(msgs); err != nil {
		return brief.Result{}, corellm.Usage{}, err
	}

	var (
		split tailSplitter
		prose strings.Builder
	)
	forward := func(text string) {
		if text == "" {
			return
		}
		prose.WriteString(text)
		if onDelta != nil {
			onDelta(text)
		}
	}

	usage, err = s.stream.CompleteStream(ctx, RoleInterviewer, interviewerSystem, buildUser(msgs), func(delta string) {
		forward(split.Push(delta))
	})
	reply := func() string { return strings.TrimSpace(prose.String()) }
	if err != nil {
		// Хвост не применяем: он мог прийти обрезанным. Незавершённый маркер из
		// буфера не отдаём — это служебный текст, а не проза.
		return stateResult(prev, reply()), usage, fmt.Errorf("интервью: %w", err)
	}
	forward(split.Flush())

	draft := prev
	if tail, ok := ParseTail(split.Tail()); ok {
		draft = mergeDraft(prev, tail)
	}
	return stateResult(draft, reply()), usage, nil
}

// validateHistory отвергает пустую и переросшую историю до вызова модели.
func (s *Service) validateHistory(msgs []brief.Message) error {
	if len(msgs) == 0 {
		return limits.Invalid(codeEmptyHistory)
	}
	if len(msgs) > s.maxMessages {
		return limits.Invalid(codeHistoryTooLong)
	}
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	if total > s.maxChars {
		return limits.Invalid(codeHistoryTooLong)
	}
	return nil
}

// buildUser собирает промпт пользователя из присланной истории.
func buildUser(msgs []brief.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		if m.Role == roleAssistant {
			b.WriteString("Ассистент: ")
		} else {
			b.WriteString("Пользователь: ")
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

// stateResult собирает итог хода: реплику и посчитанное сервисом состояние брифа.
// Missing и Status модель не определяет — только четыре обязательных поля.
func stateResult(draft brief.Draft, reply string) brief.Result {
	status := brief.StatusNeedsInput
	if draft.HasRequired() {
		status = brief.StatusReady
	}
	return brief.Result{
		Reply:   reply,
		Draft:   draft,
		Missing: missingFields(draft),
		Status:  status,
	}
}

// missingFields перечисляет незаполненные обязательные поля в порядке брифа.
// Возвращает пустой, но не nil список: в JSON кадра должен уйти [], а не null.
func missingFields(d brief.Draft) []string {
	missing := make([]string, 0, 4)
	if !filled(d.Product) {
		missing = append(missing, "product")
	}
	if !filled(d.Goal) {
		missing = append(missing, "goal")
	}
	if !filled(d.Audience) {
		missing = append(missing, "audience")
	}
	if !filled(d.Tone) {
		missing = append(missing, "tone")
	}
	return missing
}

// tailSplitter отделяет прозу от машинного хвоста по мере поступления
// фрагментов. Маркер может прийти по частям, поэтому его незавершённое начало в
// конце буфера придерживается до следующего фрагмента.
//
// Хвост открывает первый увиденный маркер: до него проза уже ушла клиенту, а
// более поздние вхождения разбирает ParseTail — он берёт последнее.
type tailSplitter struct {
	pending string
	tail    strings.Builder
	inTail  bool
}

// Push принимает фрагмент и возвращает прозу, готовую к отправке клиенту.
func (s *tailSplitter) Push(delta string) string {
	if s.inTail {
		s.tail.WriteString(delta)
		return ""
	}
	s.pending += delta
	if i := strings.Index(s.pending, tailMarker); i >= 0 {
		prose := s.pending[:i]
		s.tail.WriteString(s.pending[i:])
		s.pending = ""
		s.inTail = true
		return prose
	}
	hold := markerPrefixLen(s.pending)
	if hold == 0 {
		prose := s.pending
		s.pending = ""
		return prose
	}
	prose := s.pending[:len(s.pending)-hold]
	s.pending = s.pending[len(s.pending)-hold:]
	return prose
}

// Flush отдаёт остаток буфера, если маркер так и не встретился.
func (s *tailSplitter) Flush() string {
	if s.inTail {
		return ""
	}
	prose := s.pending
	s.pending = ""
	return prose
}

// Tail возвращает накопленный машинный хвост (вместе с маркером).
func (s *tailSplitter) Tail() string { return s.tail.String() }

// markerPrefixLen — длина самого длинного суффикса, который может оказаться
// началом маркера и потому должен подождать следующий фрагмент.
func markerPrefixLen(s string) int {
	limit := len(tailMarker) - 1
	if len(s) < limit {
		limit = len(s)
	}
	for k := limit; k > 0; k-- {
		if strings.HasSuffix(s, tailMarker[:k]) {
			return k
		}
	}
	return 0
}
