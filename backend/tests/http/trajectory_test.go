package http_test

import (
	"encoding/json"
	server "github.com/977ADAM/marketing-agents/internal/core/transport/http/server"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// repoWithTrail — репозиторий с одной кампанией и одним прогоном проверки,
// у каждого своя лента событий.
func repoWithTrail() *mockRepo {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	return &mockRepo{
		campaigns: map[string]*campaign.Record{"camp-1": {ID: "camp-1", Status: "done"}},
		reviews:   map[string]*review.Record{"rev-1": {ID: "rev-1", Status: "done"}},
		events: map[string][]trace.Row{
			"camp-1": {
				{
					Seq: 1, At: at, Kind: "llm", Name: "semanticist_seeds", Status: "ok",
					Summary:    "semanticist_seeds: 100 → 200 токенов за 900 мс",
					DurationMS: 900, PromptTokens: 100, CompletionTokens: 200,
					Payload: `{"system":"промпт","user":"бриф"}`, HasPayload: true,
				},
				{
					Seq: 2, At: at.Add(time.Second), Kind: "decision", Name: "topic_decision",
					Status: "ok", Summary: "«Как выбрать шины»: объём 92398 — отобрана в генерацию",
				},
			},
			"rev-1": {
				{Seq: 1, At: at, Kind: "llm", Name: "compliance", Status: "ok", Summary: "проверка"},
			},
		},
	}
}

func getJSON(t *testing.T, api *server.Router, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec, body
}

func TestCampaignTrajectoryFeed(t *testing.T) {
	api := newAPI(repoWithTrail(), repoWithTrail(), repoWithTrail(), &mockRunner{called: make(chan string, 1)}, nil, 1000)

	rec, body := getJSON(t, api, "/api/campaigns/camp-1/trajectory")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if body["id"] != "camp-1" || body["total"] != float64(2) {
		t.Errorf("ответ = %#v", body)
	}

	events, _ := body["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("событий %d, want 2", len(events))
	}
	first, _ := events[0].(map[string]any)
	if first["kind"] != "llm" || first["name"] != "semanticist_seeds" || first["status"] != "ok" {
		t.Errorf("первое событие = %#v", first)
	}
	if first["prompt_tokens"] != float64(100) || first["completion_tokens"] != float64(200) {
		t.Errorf("токены не отданы: %#v", first)
	}
	if first["has_payload"] != true {
		t.Errorf("признак тела потерян: %#v", first)
	}
	// Лента не тащит тела: иначе ответ на прогон с 5 темами станет мегабайтным.
	if _, ok := first["payload"]; ok {
		t.Errorf("в ленте не должно быть payload: %#v", first)
	}
}

func TestCampaignTrajectoryEventDetail(t *testing.T) {
	api := newAPI(repoWithTrail(), repoWithTrail(), repoWithTrail(), &mockRunner{called: make(chan string, 1)}, nil, 1000)

	rec, body := getJSON(t, api, "/api/campaigns/camp-1/trajectory/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	payload, ok := body["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload не разобран в объект: %#v", body["payload"])
	}
	if payload["user"] != "бриф" {
		t.Errorf("payload = %#v", payload)
	}
	if body["summary"] == "" || body["seq"] != float64(1) {
		t.Errorf("событие = %#v", body)
	}
}

func TestCampaignTrajectoryNotFound(t *testing.T) {
	api := newAPI(repoWithTrail(), repoWithTrail(), repoWithTrail(), &mockRunner{called: make(chan string, 1)}, nil, 1000)

	cases := []string{
		"/api/campaigns/nope/trajectory",      // кампании нет
		"/api/campaigns/camp-1/trajectory/99", // события с таким номером нет
		"/api/reviews/nope/trajectory",
		"/api/reviews/rev-1/trajectory/99",
	}
	for _, path := range cases {
		rec, _ := getJSON(t, api, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: code = %d, want 404", path, rec.Code)
		}
	}
}

// Прогон без событий (старые кампании или TRACE_MODE=off) — пустая лента, а не 404.
func TestCampaignTrajectoryEmpty(t *testing.T) {
	repo := repoWithTrail()
	repo.events = nil
	api := newAPI(repo, repo, repo, &mockRunner{called: make(chan string, 1)}, nil, 1000)

	rec, body := getJSON(t, api, "/api/campaigns/camp-1/trajectory")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if body["total"] != float64(0) {
		t.Errorf("total = %v, want 0", body["total"])
	}
	events, ok := body["events"].([]any)
	if !ok || len(events) != 0 {
		t.Errorf("events = %#v, want пустой список", body["events"])
	}
}

func TestReviewTrajectoryFeed(t *testing.T) {
	api := newAPI(repoWithTrail(), repoWithTrail(), repoWithTrail(), &mockRunner{called: make(chan string, 1)}, nil, 1000)

	rec, body := getJSON(t, api, "/api/reviews/rev-1/trajectory")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if body["total"] != float64(1) {
		t.Errorf("total = %v, want 1", body["total"])
	}
}

func TestTrajectoryValidation(t *testing.T) {
	api := newAPI(repoWithTrail(), repoWithTrail(), repoWithTrail(), &mockRunner{called: make(chan string, 1)}, nil, 1000)

	rec, _ := getJSON(t, api, "/api/campaigns/camp-1/trajectory/abc")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("нечисловой seq: code = %d, want 400", rec.Code)
	}
}

// Сбой стора — 500, а не пустая лента: иначе проблема выглядела бы как «событий нет».
func TestTrajectoryRepoError(t *testing.T) {
	api := newAPI(errRepo{}, errRepo{}, errRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)

	// errRepo отдаёт ошибку и на Get, и на RunEvents — проверяем, что не 200.
	rec, _ := getJSON(t, api, "/api/campaigns/camp-1/trajectory")
	if rec.Code == http.StatusOK {
		t.Errorf("code = %d, want ошибку", rec.Code)
	}
}
