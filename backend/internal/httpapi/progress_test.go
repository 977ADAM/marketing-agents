package httpapi_test

import (
	"context"
	"sync"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/campaign"
	"github.com/977ADAM/marketing-agents/internal/httpapi"
	"github.com/977ADAM/marketing-agents/internal/review"
	"github.com/977ADAM/marketing-agents/internal/run"
)

// fakeProgressStore — стор в памяти для тестов Hub.
type fakeProgressStore struct {
	mu      sync.Mutex
	saved   map[string]run.Snapshot
	savedRV map[string]run.Snapshot
	camps   map[string]*campaign.Record
	revs    map[string]*review.Record
}

func newFakePS() *fakeProgressStore {
	return &fakeProgressStore{
		saved:   map[string]run.Snapshot{},
		savedRV: map[string]run.Snapshot{},
		camps:   map[string]*campaign.Record{},
		revs:    map[string]*review.Record{},
	}
}
func (f *fakeProgressStore) SaveProgress(_ context.Context, id string, s run.Snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved[id] = s
	return nil
}
func (f *fakeProgressStore) Get(_ context.Context, id string) (*campaign.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.camps[id]
	if !ok {
		return nil, campaign.ErrNotFound
	}
	return c, nil
}
func (f *fakeProgressStore) SaveReviewProgress(_ context.Context, id string, s run.Snapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.savedRV[id] = s
	return nil
}
func (f *fakeProgressStore) GetReview(_ context.Context, id string) (*review.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.revs[id]
	if !ok {
		return nil, review.ErrNotFound
	}
	return r, nil
}

func TestHubLiveSubscriber(t *testing.T) {
	ps := newFakePS()
	hub := httpapi.NewHub(context.Background(), ps)
	tr := hub.Tracker("c1")

	snap0, ch, cancel := hub.Subscribe("c1")
	defer cancel()
	if snap0.Phase != "" {
		t.Fatalf("initial phase = %q, want empty", snap0.Phase)
	}

	tr.Strategizing()
	tr.TopicsPlanned([]string{"T1", "T2"})
	tr.TopicDone(0, 88)

	var last run.Snapshot
	for i := 0; i < 3; i++ {
		last = <-ch
	}
	if last.TopicsDone != 1 || last.TopicTotal != 2 {
		t.Fatalf("last = %+v", last)
	}
	if ps.saved["c1"].TopicsDone != 1 {
		t.Fatalf("persisted = %+v", ps.saved["c1"])
	}
}

// Подбор тем: в снимке видна фаза researching, подэтап и сеялки как единицы работы.
func TestTrackerResearchProgress(t *testing.T) {
	ps := newFakePS()
	hub := httpapi.NewHub(context.Background(), ps)
	tr := hub.Tracker("r1")

	_, ch, cancel := hub.Subscribe("r1")
	defer cancel()

	tr.Researching(run.StageSeeds)
	tr.ResearchSeeds([]string{"зимняя резина", "какую зимнюю резину"})
	tr.ResearchSeedDone(0)
	tr.Researching(run.StageFetching)
	tr.ResearchSeedDone(1)

	var last run.Snapshot
	for i := 0; i < 5; i++ {
		last = <-ch
	}
	if last.Phase != run.PhaseResearching {
		t.Errorf("Phase = %q, want %q", last.Phase, run.PhaseResearching)
	}
	if last.Stage != string(run.StageFetching) {
		t.Errorf("Stage = %q, want fetching", last.Stage)
	}
	if last.TopicTotal != 2 || last.TopicsDone != 2 {
		t.Errorf("сеялки: total = %d, done = %d", last.TopicTotal, last.TopicsDone)
	}
	if len(last.Topics) != 2 || last.Topics[0].State != run.TopicDone {
		t.Errorf("состояния сеялок = %+v", last.Topics)
	}
	// Процент фазы подбора растёт в диапазоне стратегии, а не прыгает к 95.
	if last.Percent < 5 || last.Percent > 10 {
		t.Errorf("Percent = %d, want 5..10 на этапе подбора", last.Percent)
	}

	// Переход к генерации: сеялки заменяются темами, счётчик обнуляется.
	tr.TopicsPlanned([]string{"Тема A"})
	snap := ps.saved["r1"]
	if snap.Phase != run.PhaseProducing || snap.Stage != "" {
		t.Errorf("после подбора: phase = %q, stage = %q", snap.Phase, snap.Stage)
	}
	if snap.TopicsDone != 0 || snap.TopicTotal != 1 {
		t.Errorf("счётчики генерации = %d/%d, want 0/1", snap.TopicsDone, snap.TopicTotal)
	}
}

func TestHubLateSubscriberFromStore(t *testing.T) {
	ps := newFakePS()
	ps.camps["done1"] = &campaign.Record{ID: "done1", Status: "done",
		Progress: &run.Snapshot{Phase: run.PhaseDone, Percent: 100}}
	hub := httpapi.NewHub(context.Background(), ps)

	snap, ch, cancel := hub.Subscribe("done1")
	defer cancel()
	if snap.Percent != 100 {
		t.Fatalf("snap = %+v", snap)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed for terminal/absent run")
	}
}

func TestHubFinishClosesSubscribers(t *testing.T) {
	hub := httpapi.NewHub(context.Background(), newFakePS())
	tr := hub.Tracker("c2")
	_, ch, cancel := hub.Subscribe("c2")
	defer cancel()

	tr.Done()
	for range ch {
	}
	_, ch2, cancel2 := hub.Subscribe("c2")
	defer cancel2()
	if _, ok := <-ch2; ok {
		t.Fatal("expected closed channel after finish")
	}
}

func TestHubUpdateAfterCancelNoPanic(t *testing.T) {
	hub := httpapi.NewHub(context.Background(), newFakePS())
	tr := hub.Tracker("c3")
	_, _, cancel := hub.Subscribe("c3")

	cancel() // подписчик ушёл

	// отправитель продолжает слать снимки — не должно быть паники send-on-closed
	tr.Strategizing()
	tr.TopicsPlanned([]string{"T1"})
	tr.TopicWriting(0)
	tr.TopicDone(0, 90)
	tr.Done()
}
