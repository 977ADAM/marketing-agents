package wordstat_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	wordstat "github.com/977ADAM/marketing-agents/internal/features/topic/source/wordstat"
)

// Фикстуры — сырые тела ответов живого MCP-сервера (SSE-обёртка целиком),
// снятые 2026-10-03. Тесты на них проверяют и разбор SSE, и соответствие
// структуре structuredContent, не обращаясь к сети.

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("фикстура %s: %v", name, err)
	}
	return string(data)
}

// toolCall — записанный вызов инструмента.
type toolCall struct {
	Name string
	Args map[string]any
}

// fakeMCP — тестовый MCP-сервер: отдаёт фикстуры и ведёт журнал вызовов.
type fakeMCP struct {
	*httptest.Server

	mu        sync.Mutex
	initCalls int
	calls     []toolCall
	sessions  []string // Mcp-Session-Id, пришедшие в tools/call

	// respond возвращает имя фикстуры для вызова инструмента; если не задан,
	// по умолчанию отдаётся top_requests_all.
	respond func(name string, args map[string]any) string
	// failFirstToolCall имитирует потерянную сессию MCP (HTTP 404).
	failFirstToolCall bool
	failedToolCalls   int
}

func newFakeMCP(t *testing.T, fixtures map[string]string) *fakeMCP {
	t.Helper()
	f := &fakeMCP{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			f.mu.Lock()
			f.initCalls++
			f.mu.Unlock()
			w.Header().Set("Mcp-Session-Id", "test-session-1")
			writeSSE(w, fixtures["initialize.sse"])
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted) // как у живого сервера: 202 без тела
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				http.Error(w, "bad params", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.calls = append(f.calls, toolCall{Name: params.Name, Args: params.Arguments})
			f.sessions = append(f.sessions, r.Header.Get("Mcp-Session-Id"))
			lost := f.failFirstToolCall && f.failedToolCalls == 0
			if lost {
				f.failedToolCalls++
			}
			f.mu.Unlock()

			if lost {
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
			name := "top_requests_all.sse"
			if f.respond != nil {
				name = f.respond(params.Name, params.Arguments)
			}
			body, ok := fixtures[name]
			if !ok {
				http.Error(w, "no fixture "+name, http.StatusInternalServerError)
				return
			}
			if strings.Contains(body, `"error":{`) && !strings.Contains(body, `"result"`) {
				// JSON-RPC error верхнего уровня — как у неизвестного инструмента
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
				return
			}
			writeSSE(w, body)
		default:
			http.Error(w, "unknown method "+req.Method, http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Server.Close)
	return f
}

// writeSSE отдаёт тело как text/event-stream.
func writeSSE(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// standardFixtures — набор фикстур, нужный большинству тестов.
func standardFixtures(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"initialize.sse":             fixture(t, "initialize.sse"),
		"tools_list.sse":             fixture(t, "tools_list.sse"),
		"top_requests_all.sse":       fixture(t, "top_requests_all.sse"),
		"top_requests_region.sse":    fixture(t, "top_requests_region.sse"),
		"top_requests_nodata.sse":    fixture(t, "top_requests_nodata.sse"),
		"dynamics_monthly.sse":       fixture(t, "dynamics_monthly.sse"),
		"regions_named.sse":          fixture(t, "regions_named.sse"),
		"error_invalid_argument.sse": fixture(t, "error_invalid_argument.sse"),
		"error_unknown_tool.sse":     fixture(t, "error_unknown_tool.sse"),
	}
}

func newTestClient(f *fakeMCP) *wordstat.Client {
	return wordstat.New(wordstat.Options{URL: f.URL, User: "admin", Pass: "123", HTTP: f.Client(), MaxRetries: 1})
}

func TestTopRequestsParsesStructuredContent(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	c := newTestClient(f)

	top, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "зимняя резина", NumPhrases: 50})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}

	if top.TotalCount != 1028481 {
		t.Errorf("TotalCount = %d, want 1028481", top.TotalCount)
	}
	if !top.HasData {
		t.Error("HasData = false, want true")
	}
	if len(top.Requests) != 50 {
		t.Errorf("len(Requests) = %d, want 50", len(top.Requests))
	}
	if len(top.Associations) != 15 {
		t.Errorf("len(Associations) = %d, want 15", len(top.Associations))
	}
	// Числа приходят в structuredContent как числа, а не строками protobuf.
	if top.Requests[1].Phrase != "купить зимнюю резину" || top.Requests[1].Count != 289429 {
		t.Errorf("Requests[1] = %+v, want «купить зимнюю резину» 289429", top.Requests[1])
	}

	// Сессия из initialize должна уйти в tools/call, а аргументы — дойти как есть.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.initCalls != 1 {
		t.Errorf("initialize вызван %d раз, want 1", f.initCalls)
	}
	if len(f.calls) != 1 {
		t.Fatalf("вызовов инструментов %d, want 1", len(f.calls))
	}
	call := f.calls[0]
	if call.Name != "top_requests" {
		t.Errorf("инструмент = %q, want top_requests", call.Name)
	}
	if call.Args["phrase"] != "зимняя резина" || call.Args["numPhrases"] != float64(50) {
		t.Errorf("аргументы = %+v", call.Args)
	}
	if f.sessions[0] != "test-session-1" {
		t.Errorf("Mcp-Session-Id = %q, want test-session-1", f.sessions[0])
	}
}

func TestTopRequestsRegionFilter(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "top_requests_region.sse" }
	c := newTestClient(f)

	top, err := c.Demand(context.Background(), topic.DemandParams{
		Phrase: "зимняя резина", NumPhrases: 50, Regions: []string{"213"},
	})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}
	if top.TotalCount != 86563 {
		t.Errorf("TotalCount = %d, want 86563 (только Москва)", top.TotalCount)
	}
	// Эхо-фильтр в доменный тип не выносим: что регион ушёл в аргументах,
	// проверяем ниже по журналу вызовов.

	f.mu.Lock()
	defer f.mu.Unlock()
	regions, _ := f.calls[0].Args["regions"].([]any)
	if len(regions) != 1 || regions[0] != "213" {
		t.Errorf("аргумент regions = %v, want [213]", f.calls[0].Args["regions"])
	}
}

func TestTopRequestsNoDataIsNotAnError(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "top_requests_nodata.sse" }
	c := newTestClient(f)

	top, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "ыфвыфв ыфва", NumPhrases: 5})
	if err != nil {
		t.Fatalf("отсутствие спроса не должно быть ошибкой: %v", err)
	}
	if top.HasData || top.TotalCount != 0 || len(top.Requests) != 0 {
		t.Errorf("ожидался пустой спрос, получили %+v", top)
	}
}

func TestRegionsIncludeNames(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "regions_named.sse" }
	c := newTestClient(f)

	res, err := c.Regions(context.Background(), wordstat.RegionsParams{
		Phrase: "аренда офиса", RegionMode: "regions", IncludeNames: true,
	})
	if err != nil {
		t.Fatalf("Regions: %v", err)
	}
	if len(res.Items) != 293 {
		t.Errorf("len(Items) = %d, want 293", len(res.Items))
	}
	first := res.Items[0]
	if first.RegionID != "225" || first.Name != "Россия" || first.Count != 45299 {
		t.Errorf("Items[0] = %+v, want 225/Россия/45299", first)
	}
	if first.AffinityIndex == 0 {
		t.Error("AffinityIndex не разобран")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls[0].Args["includeNames"] != true {
		t.Errorf("аргумент includeNames = %v, want true", f.calls[0].Args["includeNames"])
	}
}

func TestDynamicsMonthly(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "dynamics_monthly.sse" }
	c := newTestClient(f)

	res, err := c.Dynamics(context.Background(), topic.DynamicsParams{Phrase: "зимняя резина", Period: "monthly"})
	if err != nil {
		t.Fatalf("Dynamics: %v", err)
	}
	if res.Period != "PERIOD_MONTHLY" {
		t.Errorf("Period = %q", res.Period)
	}
	if len(res.Points) != 12 {
		t.Fatalf("len(Points) = %d, want 12", len(res.Points))
	}
	if res.Points[0].Count != 1700930 || res.Points[0].Date != "2025-10-01T00:00:00Z" {
		t.Errorf("Points[0] = %+v", res.Points[0])
	}
	// Размах сезонности из фикстуры: октябрь против июня — на этом строится
	// сезонная поправка в отборе тем.
	if res.Points[0].Count <= res.Points[8].Count*8 {
		t.Errorf("ожидался размах ≥8x, получили %d против %d", res.Points[0].Count, res.Points[8].Count)
	}
}

func TestToolErrorInvalidArgument(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "error_invalid_argument.sse" }
	c := newTestClient(f)

	_, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "зимняя резина", Regions: []string{"abc"}})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !errors.Is(err, wordstat.ErrInvalidArgument) {
		t.Errorf("errors.Is(ErrInvalidArgument) = false, err = %v", err)
	}
	if wordstat.Retryable(err) {
		t.Error("invalid_argument не должен считаться retryable")
	}
	if !strings.Contains(err.Error(), "invalid region") {
		t.Errorf("текст ошибки MCP потерян: %v", err)
	}
}

func TestUnknownToolIsInternal(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.respond = func(string, map[string]any) string { return "error_unknown_tool.sse" }
	c := newTestClient(f)

	_, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "зимняя резина"})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !errors.Is(err, wordstat.ErrInternal) {
		t.Errorf("errors.Is(ErrInternal) = false, err = %v", err)
	}
	if wordstat.Retryable(err) {
		t.Error("ошибка протокола не должна считаться retryable")
	}
}

func TestSessionReconnectOnLostSession(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	f.failFirstToolCall = true
	c := newTestClient(f)

	top, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "зимняя резина", NumPhrases: 50})
	if err != nil {
		t.Fatalf("после переподключения ожидался успех: %v", err)
	}
	if top.TotalCount != 1028481 {
		t.Errorf("TotalCount = %d", top.TotalCount)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.initCalls != 2 {
		t.Errorf("initialize вызван %d раз, want 2 (переподключение)", f.initCalls)
	}
	if len(f.calls) != 2 {
		t.Errorf("tools/call вызван %d раз, want 2 (потеря сессии + повтор)", len(f.calls))
	}
}

func TestUnauthorizedIsInternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="clustering"`)
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := wordstat.New(wordstat.Options{URL: srv.URL, User: "admin", Pass: "wrong", HTTP: srv.Client()})
	_, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "зимняя резина"})
	if err == nil {
		t.Fatal("ожидалась ошибка авторизации")
	}
	if !errors.Is(err, wordstat.ErrInternal) {
		t.Errorf("errors.Is(ErrInternal) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("в ошибке нет статуса: %v", err)
	}
}

func TestEmptyPhraseFailsFast(t *testing.T) {
	f := newFakeMCP(t, standardFixtures(t))
	c := newTestClient(f)

	_, err := c.Demand(context.Background(), topic.DemandParams{Phrase: "   "})
	if !errors.Is(err, wordstat.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.initCalls != 0 || len(f.calls) != 0 {
		t.Error("пустая фраза не должна доходить до сети")
	}
}

func TestParseExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sse", "event: message\ndata: {\"a\":1}\n\n", `{"a":1}`},
		{"plain", `{"a":1}`, `{"a":1}`},
		{"ping-prefix", ": ping\n\nevent: message\ndata: {\"a\":1}\n\n", `{"a":1}`},
		{"multi-data", "event: message\ndata: {\"a\":\ndata: 1}\n\n", "{\"a\":\n1}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := wordstat.ExtractJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("extractJSON: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("extractJSON = %q, want %q", got, tc.want)
			}
		})
	}

	if _, err := wordstat.ExtractJSON([]byte("event: message\n\n")); err == nil {
		t.Error("ожидалась ошибка на теле без data-строки")
	}
}
