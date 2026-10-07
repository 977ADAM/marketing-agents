// Package briefhttp — SSE-эндпоинт интервью по брифу.
//
// Один POST-запрос — один ход диалога: история приходит в теле, ответ уходит
// потоком кадров SSE. Заголовки потока пишутся лениво, на первом кадре: до него
// ошибку ещё можно отдать обычным JSON-ответом, а после — только кадром error,
// потому что статус уже отправлен.
package briefhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	response "github.com/977ADAM/marketing-agents/internal/core/transport/http/response"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
)

// interviewRoute — маршрут одного хода интервью.
const interviewRoute = "POST /api/briefs/interview"

// modelErrorMessage — публичная фраза о сбое модели. Ни текст ошибки, ни сырой
// ответ модели наружу не уходят: в кадре error только эта фраза.
const modelErrorMessage = "не удалось получить ответ интервьюера"

// validationMessages — словарь понятных пользователю фраз по кодам валидации
// сервиса: код — это текст ошибки, отдельного поля у неё нет.
var validationMessages = map[string]string{
	"empty_history":    "история диалога пуста — отправьте хотя бы одну реплику",
	"history_too_long": "история диалога слишком длинная: не больше 20 сообщений и 32 КиБ текста",
}

// validationMessage переводит код валидации сервиса в понятную фразу.
func validationMessage(code string) string {
	if msg, ok := validationMessages[code]; ok {
		return msg
	}
	return "история диалога не принята"
}

// InterviewService — то, что транспорту нужно от сервиса интервью: один ход
// диалога с доставкой прозы по мере генерации.
type InterviewService interface {
	Ask(ctx context.Context, msgs []brief.Message, prev brief.Draft, onDelta func(string)) (brief.Result, corellm.Usage, error)
}

// Handler — SSE-эндпоинт интервью по брифу.
type Handler struct {
	interviews InterviewService
	limiter    *middleware.RateLimiter
	limits     limits.Limits
}

// NewHandler собирает хендлер; пустые лимиты заменяются общими дефолтами.
func NewHandler(s InterviewService, limiter *middleware.RateLimiter, opt ...limits.Limits) *Handler {
	l := limits.Defaults()
	if len(opt) > 0 {
		l = limits.Normalize(opt[0])
	}
	return &Handler{interviews: s, limiter: limiter, limits: l}
}

// Routes — маршруты транспорта интервью.
func (h *Handler) Routes() []server.Route {
	return []server.Route{
		{Pattern: interviewRoute, Handler: h.interview},
	}
}

// interviewReq — тело запроса: история диалога и необязательный черновик.
//
// Сервер состояния не хранит: черновик, полученный в предыдущем кадре brief,
// присылает обратно клиент. Сервис не собирает бриф из одного ответа модели —
// он накладывает разобранный хвост на prev по непустым полям и возвращает prev
// без изменений, если хвоста нет или он битый.
type interviewReq struct {
	Messages []brief.Message `json:"messages"`
	Draft    brief.Draft     `json:"draft"`
}

// interview ведёт один ход интервью и отдаёт его потоком кадров.
func (h *Handler) interview(w http.ResponseWriter, r *http.Request) {
	if !h.limiter.Allow() {
		response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "слишком много запросов")
		return
	}
	var req interviewReq
	if !response.DecodeJSON(w, r, h.limits.MaxJSONBytes, &req) {
		return
	}
	// Стримить нечем: без Flush кадры не дойдут до браузера. Отказываем до
	// вызова модели, чтобы не платить за ответ, который некуда отдать.
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, "internal", "потоковая отдача не поддерживается")
		return
	}

	stream := &sseStream{w: w, flusher: flusher}
	// Прежний черновик приходит в теле; отсутствующий или пустой draft — это
	// нулевой черновик, поэтому первый ход работает как раньше.
	res, _, err := h.interviews.Ask(r.Context(), req.Messages, req.Draft, stream.delta)
	if err != nil {
		var validation *limits.ValidationError
		// Валидация происходит до вызова модели, поэтому заголовки ещё не
		// отправлены и ошибку можно отдать обычным JSON: код — сообщение
		// сервиса, текст — фраза из словаря хендлера.
		if errors.As(err, &validation) && !stream.started {
			response.WriteError(w, http.StatusBadRequest, validation.Error(), validationMessage(validation.Error()))
			return
		}
		// После первого кадра статус уже 200: сбой модели уходит кадром error,
		// следом закрываем поток кадром done.
		if stream.started {
			stream.frame(errorFrame{Type: "error", Message: modelErrorMessage})
			stream.frame(doneFrame{Type: "done"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "internal", modelErrorMessage)
		return
	}

	stream.frame(briefFrame{Type: "brief", Brief: res.Draft, Missing: res.Missing, Status: res.Status})
	stream.frame(doneFrame{Type: "done"})
}

// Кадры потока: их состав и порядок — публичный контракт эндпоинта.
type (
	// deltaFrame — фрагмент реплики интервьюера.
	deltaFrame struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	// briefFrame — состояние брифа на этот ход.
	briefFrame struct {
		Type    string      `json:"type"`
		Brief   brief.Draft `json:"brief"`
		Missing []string    `json:"missing"`
		Status  string      `json:"status"`
	}
	// errorFrame — публичное сообщение о сбое.
	errorFrame struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	// doneFrame — конец хода.
	doneFrame struct {
		Type string `json:"type"`
	}
)

// sseStream пишет кадры SSE и откладывает заголовки потока до первого кадра:
// пока заголовки не отправлены, наружу ещё можно вернуть обычный JSON.
type sseStream struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

// delta пишет кадр с фрагментом реплики интервьюера.
func (s *sseStream) delta(text string) {
	s.frame(deltaFrame{Type: "delta", Text: text})
}

// frame сериализует кадр и сразу сбрасывает буфер, чтобы браузер видел
// фрагменты по мере генерации.
func (s *sseStream) frame(v any) {
	// Сериализуем до отправки заголовков: если кадр не собрался, поток ещё не
	// начат и наружу можно вернуть обычный JSON, а не пустой поток со статусом 200.
	b, err := json.Marshal(v)
	if err != nil {
		// Кадр не сериализуется — пропускаем: ронять поток из-за этого нельзя.
		return
	}
	if !s.started {
		h := s.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no") // не буферизировать SSE за nginx
		s.started = true
	}
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.flusher.Flush()
}
