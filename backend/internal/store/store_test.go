package store_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/agents"
	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// newTestStore открывает отдельную SQLite-БД в t.TempDir(): тесты изолированы
// и не требуют внешнего сервера (в отличие от прежнего Postgres-варианта).
// Схему готовит applyMigrations — в приложении это делает сервис migrate.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applyMigrations(t, st.DB())
	return st
}

// Стратегия с кандидатами тем и счётчиком обращений к Wordstat переживает
// запись и чтение: стратегия лежит в JSON-поле, поэтому миграция не нужна.
func TestStrategyWithTopicCandidatesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.Create(ctx, "", agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T", Region: "213", TopicsCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	strategy := agents.Strategy{
		Positioning: "позиционирование",
		Topics:      []agents.Topic{{Title: "Как выбрать зимние шины", Angle: "поймать в момент выбора", Points: []string{"какую зимнюю резину"}}},
		TopicCandidates: []agents.TopicCandidate{{
			ID: "t1", Title: "Как выбрать зимние шины", Goal: "поймать", Task: "дать чек-лист",
			Source: agents.SourceWordstat, Selected: true, Volume: 92398, Head: "какую зимнюю резину",
			Queries: []agents.PhraseCount{{Phrase: "какую зимнюю резину", Count: 92398}},
			Season:  &agents.Seasonality{Peak: 1700930, PeakMonth: "2025-10", Trough: 192109, Ratio: 8.85, Seasonal: true},
		}, {
			ID: "t2", Title: "Тема от модели", Goal: "g", Task: "t", Source: agents.SourceLLM,
		}},
		WordstatCalls: 4,
	}
	if err := s.Complete(ctx, id, orchestrator.Result{Strategy: strategy}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Brief.Region != "213" || got.Brief.TopicsCount != 2 {
		t.Errorf("бриф: region = %q, topics_count = %d", got.Brief.Region, got.Brief.TopicsCount)
	}
	if got.Strategy == nil || len(got.Strategy.TopicCandidates) != 2 {
		t.Fatalf("кандидаты не сохранились: %+v", got.Strategy)
	}
	first := got.Strategy.TopicCandidates[0]
	if first.Volume != 92398 || first.Source != agents.SourceWordstat || !first.Selected {
		t.Errorf("кандидат 1 = %+v", first)
	}
	if first.Season == nil || first.Season.Peak != 1700930 || !first.Season.Seasonal {
		t.Errorf("сезонность = %+v", first.Season)
	}
	if len(first.Queries) != 1 || first.Queries[0].Phrase != "какую зимнюю резину" {
		t.Errorf("цитаты = %+v", first.Queries)
	}
	if got.Strategy.WordstatCalls != 4 {
		t.Errorf("WordstatCalls = %d, want 4", got.Strategy.WordstatCalls)
	}
	if got.Strategy.TopicCandidates[1].Source != agents.SourceLLM {
		t.Errorf("источник кандидата 2 = %q", got.Strategy.TopicCandidates[1].Source)
	}
}

func TestCampaignRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.MarkRunning(ctx, id); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	res := orchestrator.Result{
		Strategy:     agents.Strategy{Positioning: "p", Topics: []agents.Topic{{Title: "T1"}}},
		Deliverables: []agents.Deliverable{{Article: agents.Article{Topic: "T1", Title: "A", Body: "B", CTA: "C"}, Review: agents.Review{Score: 90, Verdict: "accept"}}},
		CostUSD:      0.12,
	}
	if err := s.Complete(ctx, id, res); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "done" || len(got.Deliverables) != 1 || got.Deliverables[0].Review.Score != 90 {
		t.Errorf("got = %+v", got)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.12 {
		t.Errorf("cost = %v", got.CostUSD)
	}
	if got.Brief.Product != "P" || got.Strategy == nil || got.Strategy.Positioning != "p" {
		t.Errorf("brief/strategy not loaded: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps not loaded: %v / %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get(context.Background(), "00000000-0000-0000-0000-0000000000ff"); err != store.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id1, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create1: %v", err)
	}
	id2, err := s.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create2: %v", err)
	}

	items, err := s.ListRecent(ctx, 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("want >= 2 items, got %d", len(items))
	}
	// новые сверху: id2 создан позже id1, поэтому он должен идти раньше id1
	posI1, posI2 := -1, -1
	for i, it := range items {
		if it.ID == id1 {
			posI1 = i
		}
		if it.ID == id2 {
			posI2 = i
		}
	}
	if posI2 == -1 || posI1 == -1 || posI2 > posI1 {
		t.Errorf("expected id2 before id1; posI1=%d posI2=%d", posI1, posI2)
	}
	if items[0].Brief.Product != "P" {
		t.Errorf("brief not loaded: %+v", items[0])
	}
}

func TestListRecentLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}
	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, "", b); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	items, err := s.ListRecent(ctx, 2)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("want 2, got %d", len(items))
	}
}

func TestProgressRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, err := s.Create(ctx, "", agents.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// до сохранения прогресса — nil
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress != nil {
		t.Fatalf("Progress = %+v, want nil", got.Progress)
	}

	snap := orchestrator.Snapshot{
		Phase:      orchestrator.PhaseProducing,
		TopicTotal: 2,
		TopicsDone: 1,
		Percent:    50,
		Topics: []orchestrator.TopicProgress{
			{Index: 0, Title: "T1", State: orchestrator.TopicDone, Score: 88},
			{Index: 1, Title: "T2", State: orchestrator.TopicWriting},
		},
	}
	if err := s.SaveProgress(ctx, id, snap); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}

	got, err = s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress == nil || got.Progress.Phase != orchestrator.PhaseProducing || got.Progress.Percent != 50 || got.Progress.TopicsDone != 1 {
		t.Fatalf("Progress = %+v", got.Progress)
	}
	if len(got.Progress.Topics) != 2 || got.Progress.Topics[0].Score != 88 {
		t.Fatalf("topics = %+v", got.Progress.Topics)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Сбросить возможные «осиротевшие» от прошлых тестов, чтобы count был детерминирован.
	if _, err := st.RecoverInterrupted(ctx); err != nil {
		t.Fatalf("pre-drain: %v", err)
	}

	pendingID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	runningID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create running: %v", err)
	}
	if err := st.MarkRunning(ctx, runningID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	doneID, err := st.Create(ctx, "", agents.Brief{})
	if err != nil {
		t.Fatalf("create done: %v", err)
	}
	if err := st.Complete(ctx, doneID, orchestrator.Result{}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	n, err := st.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered count = %d, want 2", n)
	}

	for _, id := range []string{pendingID, runningID} {
		c, err := st.Get(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if c.Status != "failed" {
			t.Errorf("campaign %s status = %q, want failed", id, c.Status)
		}
		if c.Error != "прервано рестартом сервиса" {
			t.Errorf("campaign %s error = %q, want «прервано рестартом сервиса»", id, c.Error)
		}
	}

	done, err := st.Get(ctx, doneID)
	if err != nil {
		t.Fatalf("get done: %v", err)
	}
	if done.Status != "done" {
		t.Errorf("done campaign status = %q, want done (не тронута)", done.Status)
	}

	again, err := st.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover again: %v", err)
	}
	if again != 0 {
		t.Errorf("second recover count = %d, want 0", again)
	}
}

func TestReviewRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	id, err := st.CreateReview(ctx, "", "Продукт: шины Ikon. Запрет: не упоминать другие бренды.")
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if err := st.MarkReviewRunning(ctx, id); err != nil {
		t.Fatalf("MarkReviewRunning: %v", err)
	}
	snap := orchestrator.Snapshot{
		Phase:      orchestrator.PhaseProducing,
		TopicTotal: 1,
		TopicsDone: 0,
		Percent:    10,
		Topics:     []orchestrator.TopicProgress{{Index: 0, Title: "Статья", State: orchestrator.TopicWriting}},
	}
	if err := st.SaveReviewProgress(ctx, id, snap); err != nil {
		t.Fatalf("SaveReviewProgress: %v", err)
	}
	res := orchestrator.ReviewResult{
		Items: []agents.TextReport{{
			Title:      "Статья",
			Compliance: agents.CheckScore{Score: 90, Issues: []string{"нет слогана"}},
			Quality:    agents.CheckScore{Score: 85},
			Overall:    85,
			Verdict:    "pass",
		}},
		CostUSD: 0.05,
	}
	if err := st.CompleteReview(ctx, id, res); err != nil {
		t.Fatalf("CompleteReview: %v", err)
	}

	got, err := st.GetReview(ctx, id)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.BriefText != "Продукт: шины Ikon. Запрет: не упоминать другие бренды." {
		t.Errorf("brief_text = %q", got.BriefText)
	}
	if got.Result == nil || len(got.Result.Items) != 1 || got.Result.Items[0].Overall != 85 ||
		got.Result.Items[0].Compliance.Issues[0] != "нет слогана" {
		t.Errorf("result = %+v", got.Result)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.05 {
		t.Errorf("cost = %v", got.CostUSD)
	}
	if got.Progress == nil || got.Progress.Percent != 10 {
		t.Errorf("progress = %+v", got.Progress)
	}

	items, err := st.ListReviews(ctx, 10)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Errorf("ListReviews = %+v", items)
	}

	if _, err := st.GetReview(ctx, "нет-такой-проверки"); err != store.ErrNotFound {
		t.Errorf("GetReview(unknown) err = %v, want ErrNotFound", err)
	}
}

// Данные должны переживать перезапуск процесса: тот же файл, новое соединение.
func TestDataSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reopen.db")

	first, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	applyMigrations(t, first.DB())
	id, err := first.Create(ctx, "", agents.Brief{Product: "Эко-бутылка"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := first.Complete(ctx, id, orchestrator.Result{
		Strategy:     agents.Strategy{Positioning: "p"},
		Deliverables: []agents.Deliverable{{Article: agents.Article{Topic: "T", Title: "A", Body: "B", CTA: "C"}}},
		CostUSD:      0.01,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	got, err := second.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Status != "done" || len(got.Deliverables) != 1 || got.Brief.Product != "Эко-бутылка" {
		t.Errorf("after reopen: %+v", got)
	}
}

// DSN включает foreign_keys=1: деливерабл с несуществующей кампанией не вставится.
func TestForeignKeysEnforced(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO deliverables (id, campaign_id, topic, title, body, cta, review)
		 VALUES ('d-1','00000000-0000-0000-0000-00000000dead','t','a','b','c','{}')`); err == nil {
		t.Fatal("ожидали ошибку FOREIGN KEY: pragma foreign_keys=1 не применилась")
	}
}

// DSN: нужные pragma на месте, готовый URI не переписывается.
func TestDSN(t *testing.T) {
	dsn := store.DSN("data/x.db")
	for _, want := range []string{"file:data/x.db?", "busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "_txlock=immediate"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN = %q, нет %q", dsn, want)
		}
	}
	custom := "file:/tmp/x.db?_pragma=foreign_keys(1)"
	if got := store.DSN(custom); got != custom {
		t.Errorf("DSN(готовый URI) = %q, want %q", got, custom)
	}
	if dsn := store.DSN(":memory:"); dsn == "" || strings.Contains(dsn, "journal_mode") {
		t.Errorf("DSN(:memory:) = %q — WAL для памяти не нужен", dsn)
	}
}

// Прогресс пишется из параллельных горутин (по теме на горутину) — проверяем,
// что WAL + busy_timeout + _txlock=immediate не дают «database is locked».
func TestConcurrentProgressWrites(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.Create(ctx, "", agents.Brief{Product: "P"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			snap := orchestrator.Snapshot{
				Phase:      orchestrator.PhaseProducing,
				TopicTotal: 5,
				TopicsDone: n % 5,
				Percent:    n,
				Topics:     []orchestrator.TopicProgress{{Index: 0, Title: "T", State: orchestrator.TopicWriting, Iter: n}},
			}
			if err := st.SaveProgress(ctx, id, snap); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("SaveProgress: %v", err)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress == nil {
		t.Fatal("Progress is nil after concurrent writes")
	}
}
