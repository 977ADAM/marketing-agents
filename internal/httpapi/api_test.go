package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// мок репозитория и раннера
type mockRepo struct {
	mu        sync.Mutex
	created   string
	campaigns map[string]*store.Campaign
}

func (m *mockRepo) Create(_ context.Context, _ string, b agents.Brief) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "camp-1"
	m.created = id
	if m.campaigns == nil {
		m.campaigns = map[string]*store.Campaign{}
	}
	m.campaigns[id] = &store.Campaign{ID: id, Status: "pending", Brief: b}
	return id, nil
}
func (m *mockRepo) Get(_ context.Context, id string) (*store.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.campaigns[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}
func (m *mockRepo) ListRecent(_ context.Context, limit int) ([]store.CampaignSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.CampaignSummary, 0, len(m.campaigns))
	for _, c := range m.campaigns {
		out = append(out, store.CampaignSummary{ID: c.ID, Status: c.Status, Brief: c.Brief})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type mockRunner struct {
	called chan string
}

func (r *mockRunner) Start(id string, _ agents.Brief) { r.called <- id }

func TestPostCampaignCreatesAndStartsRunner(t *testing.T) {
	repo := &mockRepo{}
	runner := &mockRunner{called: make(chan string, 1)}
	api := New(repo, runner, 1000)

	body := `{"product":"P","goal":"G","audience":"A","tone":"T"}`
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", rec.Code)
	}
	var resp struct{ ID, Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID != "camp-1" || resp.Status != "pending" {
		t.Errorf("resp = %+v", resp)
	}
	select {
	case got := <-runner.called:
		if got != "camp-1" {
			t.Errorf("runner started for %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("runner not started")
	}
}

func TestPostCampaignValidates(t *testing.T) {
	api := New(&mockRepo{}, &mockRunner{called: make(chan string, 1)}, 1000)
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(`{"product":""}`))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestGetCampaignNotFound(t *testing.T) {
	api := New(&mockRepo{}, &mockRunner{called: make(chan string, 1)}, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/missing", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestListCampaigns(t *testing.T) {
	repo := &mockRepo{}
	_, _ = repo.Create(context.Background(), "", agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"})
	api := New(repo, &mockRunner{called: make(chan string, 1)}, 1000)

	req := httptest.NewRequest("GET", "/api/campaigns", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var items []store.CampaignSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 || items[0].Brief.Product != "P" {
		t.Errorf("items = %+v", items)
	}
}

func TestBasicAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := BasicAuth("u", "p", inner)

	// без креды → 401
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/campaigns", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds: code = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Errorf("missing WWW-Authenticate header on 401")
	}

	// верный логин, неверный пароль → 401
	req := httptest.NewRequest("GET", "/api/campaigns", nil)
	req.SetBasicAuth("u", "WRONG")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad pass: code = %d, want 401", rec.Code)
	}

	// верная кредa → 200
	req = httptest.NewRequest("GET", "/api/campaigns", nil)
	req.SetBasicAuth("u", "p")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("good creds: code = %d, want 200", rec.Code)
	}

	// /healthz без auth
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: code = %d, want 200", rec.Code)
	}

	// пустые креды в конфиге → пропускать всё
	open := BasicAuth("", "", inner)
	rec = httptest.NewRecorder()
	open.ServeHTTP(rec, httptest.NewRequest("GET", "/api/campaigns", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty creds bypass: code = %d, want 200", rec.Code)
	}
}
