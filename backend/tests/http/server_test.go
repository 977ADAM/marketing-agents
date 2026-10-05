package http_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	middleware "github.com/977ADAM/marketing-agents/internal/core/transport/http/middleware"
	reviewhttp "github.com/977ADAM/marketing-agents/internal/features/review/transport/http"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
	trace "github.com/977ADAM/marketing-agents/internal/features/trace/domain"
)

// мок репозитория и раннера
type mockRepo struct {
	mu        sync.Mutex
	created   string
	campaigns map[string]*campaign.Record
	reviews   map[string]*review.Record
	events    map[string][]trace.Row
}

// RunEvents отдаёт ленту событий прогона.
func (m *mockRepo) RunEvents(_ context.Context, runID string, limit int) ([]trace.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.events[runID]
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return append([]trace.Row(nil), rows...), nil
}

// RunEvent отдаёт одно событие по номеру.
func (m *mockRepo) RunEvent(_ context.Context, runID string, seq int64) (*trace.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.events[runID] {
		if row.Seq == seq {
			cp := row
			return &cp, nil
		}
	}
	return nil, trace.ErrNotFound
}

func (m *mockRepo) Create(_ context.Context, _ string, b campaign.Brief) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "camp-1"
	m.created = id
	if m.campaigns == nil {
		m.campaigns = map[string]*campaign.Record{}
	}
	m.campaigns[id] = &campaign.Record{ID: id, Status: "pending", Brief: b}
	return id, nil
}
func (m *mockRepo) Get(_ context.Context, id string) (*campaign.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.campaigns[id]
	if !ok {
		return nil, campaign.ErrNotFound
	}
	return c, nil
}
func (m *mockRepo) ListRecent(_ context.Context, limit int) ([]campaign.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]campaign.Summary, 0, len(m.campaigns))
	for _, c := range m.campaigns {
		out = append(out, campaign.Summary{ID: c.ID, Status: c.Status, Brief: c.Brief})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (m *mockRepo) CreateCheck(_ context.Context, _, briefText string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "rev-1"
	m.reviews = map[string]*review.Record{id: {ID: id, Status: "pending", BriefText: briefText}}
	return id, nil
}
func (m *mockRepo) GetCheck(_ context.Context, id string) (*review.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.reviews[id]
	if !ok {
		return nil, review.ErrNotFound
	}
	return r, nil
}
func (m *mockRepo) ListChecks(_ context.Context, limit int) ([]review.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]review.Summary, 0, len(m.reviews))
	for _, r := range m.reviews {
		out = append(out, review.Summary{ID: r.ID, Status: r.Status, BriefText: r.BriefText})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type mockRunner struct {
	called chan string
}

func (r *mockRunner) Start(id string, _ campaign.Brief)       { r.called <- id }
func (r *mockRunner) StartReview(id string, _ review.Request) { r.called <- id }

// errRepo возвращает ошибку на всех операциях — для проверки 500-веток.
type errRepo struct{}

func (errRepo) Create(context.Context, string, campaign.Brief) (string, error) {
	return "", errors.New("boom")
}
func (errRepo) Get(context.Context, string) (*campaign.Record, error) {
	return nil, errors.New("boom")
}
func (errRepo) ListRecent(context.Context, int) ([]campaign.Summary, error) {
	return nil, errors.New("boom")
}
func (errRepo) CreateCheck(context.Context, string, string) (string, error) {
	return "", errors.New("boom")
}
func (errRepo) GetCheck(context.Context, string) (*review.Record, error) {
	return nil, errors.New("boom")
}
func (errRepo) ListChecks(context.Context, int) ([]review.Summary, error) {
	return nil, errors.New("boom")
}
func (errRepo) RunEvents(context.Context, string, int) ([]trace.Row, error) {
	return nil, errors.New("boom")
}
func (errRepo) RunEvent(context.Context, string, int64) (*trace.Row, error) {
	return nil, errors.New("boom")
}

func TestPostCampaignCreatesAndStartsRunner(t *testing.T) {
	repo := &mockRepo{}
	runner := &mockRunner{called: make(chan string, 1)}
	api := newAPI(repo, repo, repo, runner, nil, 1000)

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

// Регион и число статей доезжают до брифа: от них зависит подбор тем.
func TestPostCampaignPassesRegionAndTopicsCount(t *testing.T) {
	repo := &mockRepo{}
	runner := &mockRunner{called: make(chan string, 1)}
	api := newAPI(repo, repo, repo, runner, nil, 1000)

	body := `{"product":"P","goal":"G","audience":"A","tone":"T","region":"213","topics_count":4}`
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	campaign, err := repo.Get(context.Background(), "camp-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if campaign.Brief.Region != "213" {
		t.Errorf("Region = %q, want 213", campaign.Brief.Region)
	}
	if campaign.Brief.TopicsCount != 4 {
		t.Errorf("TopicsCount = %d, want 4", campaign.Brief.TopicsCount)
	}
}

func TestPostCampaignValidatesRegionAndTopicsCount(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"регион не число", `{"product":"P","goal":"G","audience":"A","tone":"T","region":"Москва"}`},
		{"регион с пробелом", `{"product":"P","goal":"G","audience":"A","tone":"T","region":"21 3"}`},
		{"слишком много статей", `{"product":"P","goal":"G","audience":"A","tone":"T","topics_count":25}`},
		{"отрицательное число статей", `{"product":"P","goal":"G","audience":"A","tone":"T","topics_count":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
			req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			api.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "validation") {
				t.Errorf("body = %s, want код validation", rec.Body.String())
			}
		})
	}
}

// Без региона и числа статей бриф остаётся с нулями — подставит конфиг.
func TestPostCampaignDefaultsRegionAndTopicsCount(t *testing.T) {
	repo := &mockRepo{}
	api := newAPI(repo, repo, repo, &mockRunner{called: make(chan string, 1)}, nil, 1000)

	body := `{"product":"P","goal":"G","audience":"A","tone":"T"}`
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	campaign, err := repo.Get(context.Background(), "camp-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if campaign.Brief.Region != "" || campaign.Brief.TopicsCount != 0 {
		t.Errorf("ожидались пустые значения, получили %q/%d", campaign.Brief.Region, campaign.Brief.TopicsCount)
	}
}

func TestPostCampaignBadJSON(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(`{not json`))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestPostCampaignRateLimited(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 4)}, nil, 1) // burst 1
	body := `{"product":"P","goal":"G","audience":"A","tone":"T"}`
	send := func() int {
		req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		api.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if c := send(); c != http.StatusAccepted {
		t.Fatalf("first code = %d, want 202", c)
	}
	if c := send(); c != http.StatusTooManyRequests {
		t.Fatalf("second code = %d, want 429", c)
	}
}

func TestPostCampaignRepoError(t *testing.T) {
	api := newAPI(errRepo{}, errRepo{}, errRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	body := `{"product":"P","goal":"G","audience":"A","tone":"T"}`
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
}

func TestGetCampaignInternalError(t *testing.T) {
	api := newAPI(errRepo{}, errRepo{}, errRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/x", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
}

func TestListCampaignsInternalError(t *testing.T) {
	api := newAPI(errRepo{}, errRepo{}, errRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
}

func TestListCampaignsLimitClamp(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns?limit=9999", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}

func TestPostCampaignValidates(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("POST", "/api/campaigns", bytes.NewBufferString(`{"product":""}`))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestGetCampaignNotFound(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/missing", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestListCampaigns(t *testing.T) {
	repo := &mockRepo{}
	_, _ = repo.Create(context.Background(), "", campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"})
	api := newAPI(repo, repo, repo, &mockRunner{called: make(chan string, 1)}, nil, 1000)

	req := httptest.NewRequest("GET", "/api/campaigns", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var items []campaign.Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 || items[0].Brief.Product != "P" {
		t.Errorf("items = %+v", items)
	}
}

func TestBasicAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := middleware.BasicAuth("u", "p", inner)

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
	open := middleware.BasicAuth("", "", inner)
	rec = httptest.NewRecorder()
	open.ServeHTTP(rec, httptest.NewRequest("GET", "/api/campaigns", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty creds bypass: code = %d, want 200", rec.Code)
	}
}

// fakeSub — подписчик для SSE-тестов.
type fakeSub struct {
	snap run.Snapshot
	ch   chan run.Snapshot
}

func (f *fakeSub) Subscribe(string) (run.Snapshot, <-chan run.Snapshot, func()) {
	return f.snap, f.ch, func() {}
}
func (f *fakeSub) SubscribeReview(string) (run.Snapshot, <-chan run.Snapshot, func()) {
	return f.snap, f.ch, func() {}
}

func TestCampaignEventsNotFound(t *testing.T) {
	repo := &mockRepo{}
	api := newAPI(repo, repo, repo, &mockRunner{called: make(chan string, 1)},
		&fakeSub{ch: make(chan run.Snapshot)}, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/nope/events", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", w.Code)
	}
}

func TestCampaignEventsStream(t *testing.T) {
	repo := &mockRepo{campaigns: map[string]*campaign.Record{"camp-1": {ID: "camp-1", Status: "running"}}}
	ch := make(chan run.Snapshot, 4)
	sub := &fakeSub{snap: run.Snapshot{Phase: run.PhaseStrategizing, Percent: 5}, ch: ch}
	api := newAPI(repo, repo, repo, &mockRunner{called: make(chan string, 1)}, sub, 1000)
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/campaigns/camp-1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	ch <- run.Snapshot{Phase: run.PhaseProducing, Percent: 50}
	close(ch)

	done := make(chan struct{})
	var lines []string
	go func() {
		defer close(done)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if sc.Text() == "event: done" {
				break
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SSE stream did not deliver done frame in time")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, `"percent":5`) {
		t.Errorf("no initial snapshot:\n%s", joined)
	}
	if !strings.Contains(joined, `"percent":50`) {
		t.Errorf("no update:\n%s", joined)
	}
	if !strings.Contains(joined, "event: done") {
		t.Errorf("no done:\n%s", joined)
	}
}

func TestCampaignEventsInternalError(t *testing.T) {
	api := newAPI(errRepo{}, errRepo{}, errRepo{}, &mockRunner{called: make(chan string, 1)},
		&fakeSub{ch: make(chan run.Snapshot)}, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/x/events", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.Code)
	}
}

func TestPostReviewCreatesAndStartsRunner(t *testing.T) {
	repo := &mockRepo{}
	runner := &mockRunner{called: make(chan string, 1)}
	api := newAPI(repo, repo, repo, runner, nil, 1000)

	body := `{"brief":"бриф","texts":[{"title":"T1","body":"текст"}]}`
	req := httptest.NewRequest("POST", "/api/reviews", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", rec.Code)
	}
	var resp struct{ ID, Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID != "rev-1" || resp.Status != "pending" {
		t.Errorf("resp = %+v", resp)
	}
	select {
	case got := <-runner.called:
		if got != "rev-1" {
			t.Errorf("runner started for %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("runner not started")
	}
}

func TestPostReviewValidates(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	cases := []string{
		`{"texts":[{"title":"T","body":"b"}]}`,              // нет брифа
		`{"brief":"б"}`,                                     // нет текстов
		`{"brief":"б","texts":[{"title":"T","body":"  "}]}`, // пустое тело
	}
	for _, body := range cases {
		req := httptest.NewRequest("POST", "/api/reviews", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		api.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, rec.Code)
		}
	}
}

func TestGetCheckNotFound(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	req := httptest.NewRequest("GET", "/api/reviews/missing", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestReviewEventsNotFound(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)},
		&fakeSub{ch: make(chan run.Snapshot)}, 1000)
	req := httptest.NewRequest("GET", "/api/reviews/nope/events", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

// minimalDocx собирает .docx с двумя абзацами в памяти.
func minimalDocx(t *testing.T, paras ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		doc.WriteString("<w:p><w:r><w:t>" + p + "</w:t></w:r></w:p>")
	}
	doc.WriteString(`</w:body></w:document>`)
	if _, err := w.Write([]byte(doc.String())); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractDocxEndpoint(t *testing.T) {
	data := minimalDocx(t, "Как выбрать шины", "Первый абзац текста.", "Второй абзац.")
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "article.docx")
	_, _ = fw.Write(data)
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/reviews/extract", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var out reviewhttp.ExtractResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Title != "Как выбрать шины" {
		t.Errorf("title = %q", out.Title)
	}
	if out.Text != "Как выбрать шины\nПервый абзац текста.\nВторой абзац." {
		t.Errorf("text = %q", out.Text)
	}
}

func TestExtractDocxBadFile(t *testing.T) {
	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "not.docx")
	_, _ = fw.Write([]byte("not a zip"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/reviews/extract", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

// docx с таблицей: текст внутри w:tbl/w:tr/w:tc тоже должен извлекаться.
func TestExtractDocxTable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>Заголовок</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Бренд</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>Колесо.ру</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
		`</w:body></w:document>`
	_, _ = w.Write([]byte(doc))
	_ = zw.Close()

	api := newAPI(&mockRepo{}, &mockRepo{}, &mockRepo{}, &mockRunner{called: make(chan string, 1)}, nil, 1000)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "brief.docx")
	_, _ = fw.Write(buf.Bytes())
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/reviews/extract", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var out reviewhttp.ExtractResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	want := "Заголовок\nБренд\nКолесо.ру"
	if out.Text != want {
		t.Errorf("text = %q, want %q", out.Text, want)
	}
}

// Методы ниже нужны, чтобы мок удовлетворял полным портам campaignservice.Store и
// reviewservice.Store: хендлеры их не дергают (прогон ведёт раннер), поэтому заглушки.
func (m *mockRepo) MarkRunning(context.Context, string) error                     { return nil }
func (m *mockRepo) SaveProgress(context.Context, string, run.Snapshot) error      { return nil }
func (m *mockRepo) Complete(context.Context, string, campaign.Outcome) error      { return nil }
func (m *mockRepo) Fail(context.Context, string, string) error                    { return nil }
func (m *mockRepo) MarkCheckRunning(context.Context, string) error                { return nil }
func (m *mockRepo) SaveCheckProgress(context.Context, string, run.Snapshot) error { return nil }
func (m *mockRepo) CompleteCheck(context.Context, string, review.Result) error    { return nil }
func (m *mockRepo) FailCheck(context.Context, string, string) error               { return nil }

func (errRepo) MarkRunning(context.Context, string) error                { return errors.New("boom") }
func (errRepo) SaveProgress(context.Context, string, run.Snapshot) error { return errors.New("boom") }
func (errRepo) Complete(context.Context, string, campaign.Outcome) error { return errors.New("boom") }
func (errRepo) Fail(context.Context, string, string) error               { return errors.New("boom") }
func (errRepo) MarkCheckRunning(context.Context, string) error           { return errors.New("boom") }
func (errRepo) SaveCheckProgress(context.Context, string, run.Snapshot) error {
	return errors.New("boom")
}
func (errRepo) CompleteCheck(context.Context, string, review.Result) error {
	return errors.New("boom")
}
func (errRepo) FailCheck(context.Context, string, string) error { return errors.New("boom") }
