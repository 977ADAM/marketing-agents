// handler_test.go — внешние тесты SSE-эндпоинта интервью: состав и порядок
// кадров, валидация истории и сбой модели. Пакет внешний (briefhttp_test),
// поэтому тесты видят только NewHandler и Routes — то, чем пользуется
// composition root.
package briefhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/core/limits"
	corellm "github.com/977ADAM/marketing-agents/internal/core/llm"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
	briefservice "github.com/977ADAM/marketing-agents/internal/features/brief/service"
	briefhttp "github.com/977ADAM/marketing-agents/internal/features/brief/transport/http"
)

// interviewRoute — адрес маршрута интервью: обработчик берётся из Routes(),
// поэтому тесты проверяют и состав маршрутов, и сам хендлер.
const interviewRoute = "POST /api/briefs/interview"

// fakeService — подмена сервиса интервью: тест задаёт поведение одного хода.
type fakeService struct {
	ask func(context.Context, []brief.Message, brief.Draft, func(string)) (brief.Result, corellm.Usage, error)
}

func (f *fakeService) Ask(ctx context.Context, msgs []brief.Message, prev brief.Draft, onDelta func(string)) (brief.Result, corellm.Usage, error) {
	return f.ask(ctx, msgs, prev, onDelta)
}

// contractFake повторяет контракт сервиса: пустую и переросшую историю он
// отвергает ошибкой валидации (код — текст ошибки), иначе отдаёт прозу и бриф.
func contractFake() *fakeService {
	return &fakeService{ask: func(_ context.Context, msgs []brief.Message, _ brief.Draft, onDelta func(string)) (brief.Result, corellm.Usage, error) {
		switch {
		case len(msgs) == 0:
			return brief.Result{}, corellm.Usage{}, limits.Invalid("empty_history")
		case len(msgs) > 20:
			return brief.Result{}, corellm.Usage{}, limits.Invalid("history_too_long")
		}
		onDelta("При")
		onDelta("вет")
		return brief.Result{
			Reply:   "Привет",
			Draft:   brief.Draft{Product: "Термокружка «Север»"},
			Missing: []string{"goal", "audience", "tone"},
			Status:  brief.StatusNeedsInput,
		}, corellm.Usage{}, nil
	}}
}

// newHandler собирает хендлер с выключенным лимитером: частота запросов —
// не предмет этих тестов.
func newHandler(svc briefhttp.InterviewService) *briefhttp.Handler {
	return briefhttp.NewHandler(svc, middleware.NewRateLimiter(0))
}

// interviewHandler достаёт обработчик маршрута интервью из Routes().
func interviewHandler(t *testing.T, h *briefhttp.Handler) http.HandlerFunc {
	t.Helper()
	for _, route := range h.Routes() {
		if route.Pattern == interviewRoute {
			return route.Handler
		}
	}
	t.Fatalf("маршрут %q не зарегистрирован", interviewRoute)
	return nil
}

// newInterviewRequest собирает POST-запрос с телом в формате эндпоинта.
func newInterviewRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/briefs/interview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return req
}

// frame — надмножество полей всех кадров: так один разбор покрывает delta,
// brief, error и done.
type frame struct {
	Type    string      `json:"type"`
	Text    string      `json:"text"`
	Brief   brief.Draft `json:"brief"`
	Missing []string    `json:"missing"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
}

// parseFrames разбирает тело ответа на кадры SSE вида «data: {…}\n\n».
func parseFrames(t *testing.T, body string) []frame {
	t.Helper()
	var frames []frame
	for _, raw := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "data: ") {
			t.Fatalf("кадр %q не начинается с %q", raw, "data: ")
		}
		var f frame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, "data: ")), &f); err != nil {
			t.Fatalf("кадр %q не разбирается как JSON: %v", raw, err)
		}
		frames = append(frames, f)
	}
	return frames
}

// errorBody — форма JSON-ошибки общего вида: {"error":{"code","message"}}.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestInterviewStreamsFrames(t *testing.T) {
	var gotMsgs []brief.Message
	var gotPrev brief.Draft
	svc := &fakeService{ask: func(_ context.Context, msgs []brief.Message, prev brief.Draft, onDelta func(string)) (brief.Result, corellm.Usage, error) {
		gotMsgs, gotPrev = msgs, prev
		onDelta("При")
		onDelta("вет")
		return brief.Result{
			Reply:   "Привет",
			Draft:   brief.Draft{Product: "Термокружка «Север»", Goal: "Переход в карточку"},
			Missing: []string{"audience", "tone"},
			Status:  brief.StatusNeedsInput,
		}, corellm.Usage{}, nil
	}}

	rec := httptest.NewRecorder()
	interviewHandler(t, newHandler(svc))(rec, newInterviewRequest(t,
		`{"messages":[{"role":"user","content":"Нужна кампания"}]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("статус = %d, ожидался %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, ожидался %q", ct, "text/event-stream")
	}
	if !rec.Flushed {
		t.Error("кадры не сброшены через http.Flusher")
	}
	if len(gotMsgs) != 1 || gotMsgs[0].Content != "Нужна кампания" {
		t.Errorf("история, переданная сервису = %+v, ожидалась одна реплика «Нужна кампания»", gotMsgs)
	}
	if gotPrev != (brief.Draft{}) {
		t.Errorf("прежний черновик = %+v, ожидался пустой: клиент присылает только историю", gotPrev)
	}

	want := []frame{
		{Type: "delta", Text: "При"},
		{Type: "delta", Text: "вет"},
		{
			Type:    "brief",
			Brief:   brief.Draft{Product: "Термокружка «Север»", Goal: "Переход в карточку"},
			Missing: []string{"audience", "tone"},
			Status:  brief.StatusNeedsInput,
		},
		{Type: "done"},
	}
	got := parseFrames(t, rec.Body.String())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("кадры = %+v, ожидались %+v", got, want)
	}

	// Машинный хвост ответа модели — служебный: в дельтах только проза.
	if strings.Contains(rec.Body.String(), "<<<BRIEF") {
		t.Error("маркер машинного хвоста утёк в кадры")
	}
	for _, f := range got {
		if f.Type == "delta" && strings.ContainsAny(f.Text, "{}") {
			t.Errorf("в кадре delta служебный JSON: %q", f.Text)
		}
	}
}

func TestInterviewRejectsEmptyHistory(t *testing.T) {
	rec := httptest.NewRecorder()
	interviewHandler(t, newHandler(contractFake()))(rec, newInterviewRequest(t, `{"messages":[]}`))

	assertErrorJSON(t, rec, http.StatusBadRequest, "empty_history")
}

func TestInterviewRejectsTooLongHistory(t *testing.T) {
	msgs := make([]brief.Message, 0, 21)
	for i := 0; i < 21; i++ {
		msgs = append(msgs, brief.Message{Role: "user", Content: "реплика"})
	}
	body, err := json.Marshal(map[string]any{"messages": msgs})
	if err != nil {
		t.Fatalf("тело запроса не собралось: %v", err)
	}

	rec := httptest.NewRecorder()
	interviewHandler(t, newHandler(contractFake()))(rec, newInterviewRequest(t, string(body)))

	assertErrorJSON(t, rec, http.StatusBadRequest, "history_too_long")
}

func TestInterviewStreamsErrorFrame(t *testing.T) {
	svc := &fakeService{ask: func(_ context.Context, _ []brief.Message, _ brief.Draft, onDelta func(string)) (brief.Result, corellm.Usage, error) {
		onDelta("При")
		return brief.Result{Reply: "При"}, corellm.Usage{}, errors.New("модель ответила 500: секретный ответ")
	}}

	rec := httptest.NewRecorder()
	interviewHandler(t, newHandler(svc))(rec, newInterviewRequest(t,
		`{"messages":[{"role":"user","content":"Привет"}]}`))

	// Заголовки уже отправлены первым кадром — статус остаётся 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("статус = %d, ожидался %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, ожидался %q", ct, "text/event-stream")
	}

	frames := parseFrames(t, rec.Body.String())
	if len(frames) != 3 {
		t.Fatalf("кадров = %d (%+v), ожидались delta, error, done", len(frames), frames)
	}
	if frames[0].Type != "delta" || frames[0].Text != "При" {
		t.Errorf("первый кадр = %+v, ожидался delta «При»", frames[0])
	}
	if frames[1].Type != "error" || frames[1].Message == "" {
		t.Errorf("второй кадр = %+v, ожидался error с публичным сообщением", frames[1])
	}
	if frames[2].Type != "done" {
		t.Errorf("последний кадр = %+v, ожидался done: поток закрывается", frames[2])
	}
	if !strings.HasSuffix(rec.Body.String(), "\n\n") {
		t.Error("поток не закрыт пустой строкой кадра")
	}
	// Сырой ответ модели и текст ошибки наружу не уходят.
	for _, leak := range []string{"секретный ответ", "модель ответила 500"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("в поток утёк текст ошибки: %q", leak)
		}
	}
}

func TestInterviewWithoutFlusher(t *testing.T) {
	svc := &fakeService{ask: func(_ context.Context, _ []brief.Message, _ brief.Draft, _ func(string)) (brief.Result, corellm.Usage, error) {
		return brief.Result{}, corellm.Usage{}, nil
	}}

	rec := httptest.NewRecorder()
	interviewHandler(t, newHandler(svc))(notFlushingWriter{rec}, newInterviewRequest(t,
		`{"messages":[{"role":"user","content":"Привет"}]}`))

	assertErrorJSON(t, rec, http.StatusInternalServerError, "internal")
}

// notFlushingWriter вручную повторяет http.ResponseWriter без метода Flush:
// так выглядит обёртка, которая не умеет стримить.
type notFlushingWriter struct{ rec *httptest.ResponseRecorder }

func (w notFlushingWriter) Header() http.Header         { return w.rec.Header() }
func (w notFlushingWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w notFlushingWriter) WriteHeader(status int)      { w.rec.WriteHeader(status) }

// fakeStreamer отдаёт заранее заданные фрагменты ответа модели.
type fakeStreamer struct{ chunks []string }

func (f fakeStreamer) CompleteStream(_ context.Context, _, _, _ string, onDelta func(string)) (corellm.Usage, error) {
	for _, chunk := range f.chunks {
		onDelta(chunk)
	}
	return corellm.Usage{}, nil
}

// TestInterviewWithRealService — сквозная проверка связки транспорта и сервиса:
// интерфейс InterviewService обязан совпасть с настоящим сервисом, а его ошибка
// валидации — дойти до клиента кодом empty_history без потока SSE.
func TestInterviewWithRealService(t *testing.T) {
	streaming := briefservice.New(briefservice.Options{
		Stream: fakeStreamer{chunks: []string{"При", "вет", `<<<BRIEF
{"product":"Термокружка «Север»"}`}},
	})

	t.Run("кадры настоящего сервиса", func(t *testing.T) {
		rec := httptest.NewRecorder()
		interviewHandler(t, newHandler(streaming))(rec, newInterviewRequest(t,
			`{"messages":[{"role":"user","content":"Нужна кампания"}]}`))

		want := []frame{
			{Type: "delta", Text: "При"},
			{Type: "delta", Text: "вет"},
			{
				Type:    "brief",
				Brief:   brief.Draft{Product: "Термокружка «Север»"},
				Missing: []string{"goal", "audience", "tone"},
				Status:  brief.StatusNeedsInput,
			},
			{Type: "done"},
		}
		if got := parseFrames(t, rec.Body.String()); !reflect.DeepEqual(got, want) {
			t.Fatalf("кадры = %+v, ожидались %+v", got, want)
		}
	})

	t.Run("пустая история", func(t *testing.T) {
		rec := httptest.NewRecorder()
		interviewHandler(t, newHandler(streaming))(rec, newInterviewRequest(t, `{"messages":[]}`))

		assertErrorJSON(t, rec, http.StatusBadRequest, "empty_history")
	})
}

// assertErrorJSON проверяет обычный (не потоковый) JSON-ответ об ошибке.
func assertErrorJSON(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("статус = %d, ожидался %d (тело: %s)", rec.Code, wantStatus, rec.Body.String())
	}
	// Заголовки SSE не должны быть выставлены: до первого кадра ошибка — JSON.
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, ожидался %q", ct, "application/json")
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело ошибки не разбирается как JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != wantCode {
		t.Errorf("код = %q, ожидался %q", body.Error.Code, wantCode)
	}
	if body.Error.Message == "" {
		t.Error("сообщение об ошибке пустое: пользователю нужна понятная фраза")
	}
}
