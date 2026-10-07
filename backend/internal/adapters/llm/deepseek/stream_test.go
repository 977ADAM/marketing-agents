package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llm "github.com/977ADAM/marketing-agents/internal/adapters/llm/deepseek"
)

// sseChunk собирает кадр SSE в формате OpenAI. usage передаётся только в
// последнем кадре потока: провайдер шлёт его вместе с пустой дельтой.
func sseChunk(model, delta string, usage map[string]any) string {
	choices := []map[string]any{{"index": 0, "delta": map[string]any{"content": delta}}}
	if usage != nil {
		choices = []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}
	}
	body := map[string]any{
		"id":      "x",
		"object":  "chat.completion.chunk",
		"model":   model,
		"choices": choices,
	}
	if usage != nil {
		body["usage"] = usage
	}
	b, _ := json.Marshal(body)
	return "data: " + string(b) + "\n\n"
}

// sseStream — httptest-сервер, который отдаёт заданные кадры и, если спросили,
// завершает поток терминальным [DONE].
func sseStream(frames ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, frame := range frames {
			io.WriteString(w, frame)
			flusher.Flush()
		}
	}))
}

// Три фрагмента склеиваются в полный ответ, onDelta видит каждый из них,
// а расход токенов приходит из последнего кадра с usage.
func TestCompleteStreamAssemblesFragments(t *testing.T) {
	fragments := []string{"При", "вет, ", "мир"}
	frames := make([]string, 0, len(fragments)+2)
	for _, f := range fragments {
		frames = append(frames, sseChunk("deepseek-chat", f, nil))
	}
	frames = append(frames,
		sseChunk("deepseek-chat", "", map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18}),
		"data: [DONE]\n\n",
	)
	srv := sseStream(frames...)
	defer srv.Close()

	client := llm.New("key", srv.URL, "model-default", 3, nil)
	var got []string
	usage, err := client.CompleteStream(context.Background(), "interviewer", "SYS", "USER", func(delta string) {
		got = append(got, delta)
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if usage.Response != "Привет, мир" {
		t.Errorf("Response = %q, want %q", usage.Response, "Привет, мир")
	}
	if len(got) != len(fragments) || strings.Join(got, "") != usage.Response {
		t.Errorf("onDelta = %q, want фрагменты %q", got, fragments)
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want prompt 11 / completion 7", usage)
	}
	if len(usage.Entries) != 1 || usage.Entries[0].Model != "deepseek-chat" || usage.Entries[0].Role != "interviewer" {
		t.Errorf("Entries = %+v, want одну запись с моделью и ролью", usage.Entries)
	}
}

// Обрыв посреди потока (провайдер ответил 500 уже после первых кадров) — ошибка;
// первый фрагмент уже отдан наружу, накопленный текст не теряется, ретраев нет.
func TestCompleteStreamReturnsErrorMidStream(t *testing.T) {
	requests := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sseChunk("deepseek-chat", "При", nil))
		w.(http.Flusher).Flush()
		// Соединение рвётся без терминального [DONE]: частичный поток нельзя
		// переиграть повтором запроса.
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	client := llm.New("key", srv.URL, "model-default", 3, nil)
	var got []string
	usage, err := client.CompleteStream(context.Background(), "interviewer", "SYS", "USER", func(delta string) {
		got = append(got, delta)
	})
	if err == nil {
		t.Fatal("ожидалась ошибка обрыва потока")
	}
	if len(got) != 1 || got[0] != "При" {
		t.Errorf("onDelta = %q, want [\"При\"] до обрыва", got)
	}
	if usage.Response != "При" {
		t.Errorf("Response = %q, want накопленное «При»", usage.Response)
	}
	if len(requests) != 1 {
		t.Errorf("запросов = %d, want 1 (стрим не ретраим)", len(requests))
	}
}

// Поток без единого кадра: ошибка вместо пустого успеха, паники нет.
func TestCompleteStreamEmptyStream(t *testing.T) {
	srv := sseStream("data: [DONE]\n\n")
	defer srv.Close()

	client := llm.New("key", srv.URL, "model-default", 3, nil)
	called := false
	usage, err := client.CompleteStream(context.Background(), "interviewer", "SYS", "USER", func(string) {
		called = true
	})
	if err == nil {
		t.Fatal("ожидалась ошибка на пустом потоке")
	}
	if called {
		t.Error("onDelta не должен вызываться на пустом потоке")
	}
	if usage.Response != "" {
		t.Errorf("Response = %q, want пусто", usage.Response)
	}
}

// Стриминговый вызов идёт прозой: без response_format, с stream и include_usage.
func TestCompleteStreamDoesNotRequestJSONMode(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sseChunk("deepseek-chat", "ок", nil))
		io.WriteString(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	client := llm.New("key", srv.URL, "model-default", 3, nil)
	if _, err := client.CompleteStream(context.Background(), "interviewer", "SYS", "USER", func(string) {}); err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}

	var body map[string]any
	select {
	case body = <-bodies:
	default:
		t.Fatal("запрос не дошёл до сервера")
	}
	if _, ok := body["response_format"]; ok {
		t.Errorf("response_format = %v, want отсутствие JSON-режима", body["response_format"])
	}
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	opts, ok := body["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage: true", body["stream_options"])
	}
}
