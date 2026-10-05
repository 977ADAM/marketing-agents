package mariadb_test

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/run"
	"github.com/977ADAM/marketing-agents/internal/testdb"
	"github.com/977ADAM/marketing-agents/internal/topic"
)

// Стратегия с кандидатами тем и счётчиком обращений к Wordstat переживает
// запись и чтение: стратегия лежит в JSON-поле, поэтому миграция не нужна.
func TestStrategyWithTopicCandidatesRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.campaigns.Create(ctx, "", campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T", Region: "213", TopicsCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	strategy := campaign.Strategy{
		Positioning: "позиционирование",
		Topics:      []campaign.Topic{{Title: "Как выбрать зимние шины", Angle: "поймать в момент выбора", Points: []string{"какую зимнюю резину"}}},
		TopicCandidates: []topic.TopicCandidate{{
			ID: "t1", Title: "Как выбрать зимние шины", Goal: "поймать", Task: "дать чек-лист",
			Source: topic.SourceWordstat, Selected: true, Volume: 92398, Head: "какую зимнюю резину",
			Queries: []topic.PhraseCount{{Phrase: "какую зимнюю резину", Count: 92398}},
			Season:  &topic.Seasonality{Peak: 1700930, PeakMonth: "2025-10", Trough: 192109, Ratio: 8.85, Seasonal: true},
		}, {
			ID: "t2", Title: "Тема от модели", Goal: "g", Task: "t", Source: topic.SourceLLM,
		}},
		WordstatCalls: 4,
	}
	if err := s.campaigns.Complete(ctx, id, campaign.Outcome{Strategy: strategy}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.campaigns.Get(ctx, id)
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
	if first.Volume != 92398 || first.Source != topic.SourceWordstat || !first.Selected {
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
	if got.Strategy.TopicCandidates[1].Source != topic.SourceLLM {
		t.Errorf("источник кандидата 2 = %q", got.Strategy.TopicCandidates[1].Source)
	}
}

func TestCampaignRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id, err := s.campaigns.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.campaigns.MarkRunning(ctx, id); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	res := campaign.Outcome{
		Strategy:     campaign.Strategy{Positioning: "p", Topics: []campaign.Topic{{Title: "T1"}}},
		Deliverables: []campaign.Deliverable{{Article: campaign.Article{Topic: "T1", Title: "A", Body: "B", CTA: "C"}, Review: campaign.Review{Score: 90, Verdict: "accept"}}},
		CostUSD:      0.12,
	}
	if err := s.campaigns.Complete(ctx, id, res); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := s.campaigns.Get(ctx, id)
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
	if _, err := s.campaigns.Get(context.Background(), "00000000-0000-0000-0000-0000000000ff"); err != campaign.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}

	id1, err := s.campaigns.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create1: %v", err)
	}
	id2, err := s.campaigns.Create(ctx, "", b)
	if err != nil {
		t.Fatalf("Create2: %v", err)
	}

	items, err := s.campaigns.ListRecent(ctx, 50)
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
	b := campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"}
	for i := 0; i < 3; i++ {
		if _, err := s.campaigns.Create(ctx, "", b); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	items, err := s.campaigns.ListRecent(ctx, 2)
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
	id, err := s.campaigns.Create(ctx, "", campaign.Brief{Product: "P", Goal: "G", Audience: "A", Tone: "T"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// до сохранения прогресса — nil
	got, err := s.campaigns.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress != nil {
		t.Fatalf("Progress = %+v, want nil", got.Progress)
	}

	snap := run.Snapshot{
		Phase:      run.PhaseProducing,
		TopicTotal: 2,
		TopicsDone: 1,
		Percent:    50,
		Topics: []run.TopicProgress{
			{Index: 0, Title: "T1", State: run.TopicDone, Score: 88},
			{Index: 1, Title: "T2", State: run.TopicWriting},
		},
	}
	if err := s.campaigns.SaveProgress(ctx, id, snap); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}

	got, err = s.campaigns.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Progress == nil || got.Progress.Phase != run.PhaseProducing || got.Progress.Percent != 50 || got.Progress.TopicsDone != 1 {
		t.Fatalf("Progress = %+v", got.Progress)
	}
	if len(got.Progress.Topics) != 2 || got.Progress.Topics[0].Score != 88 {
		t.Fatalf("topics = %+v", got.Progress.Topics)
	}
}

// Данные должны переживать перезапуск процесса: та же база, новое соединение.
func TestDataSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewDSN(t)

	first := openStores(t, dsn)
	id, err := first.campaigns.Create(ctx, "", campaign.Brief{Product: "Эко-бутылка"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := first.campaigns.Complete(ctx, id, campaign.Outcome{
		Strategy:     campaign.Strategy{Positioning: "p"},
		Deliverables: []campaign.Deliverable{{Article: campaign.Article{Topic: "T", Title: "A", Body: "B", CTA: "C"}}},
		CostUSD:      0.01,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := first.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := openStores(t, dsn)

	got, err := second.campaigns.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.Status != "done" || len(got.Deliverables) != 1 || got.Brief.Product != "Эко-бутылка" {
		t.Errorf("after reopen: %+v", got)
	}
}
