package httpapi

import (
	"context"
	"testing"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// fakeProgressStore — стор в памяти для тестов Hub.
type fakeProgressStore struct {
	saved map[string]orchestrator.Snapshot
	camps map[string]*store.Campaign
}

func newFakePS() *fakeProgressStore {
	return &fakeProgressStore{saved: map[string]orchestrator.Snapshot{}, camps: map[string]*store.Campaign{}}
}
func (f *fakeProgressStore) SaveProgress(_ context.Context, id string, s orchestrator.Snapshot) error {
	f.saved[id] = s
	return nil
}
func (f *fakeProgressStore) Get(_ context.Context, id string) (*store.Campaign, error) {
	c, ok := f.camps[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func TestHubLiveSubscriber(t *testing.T) {
	ps := newFakePS()
	hub := NewHub(context.Background(), ps)
	tr := hub.Tracker("c1")

	snap0, ch, cancel := hub.Subscribe("c1")
	defer cancel()
	if snap0.Phase != "" {
		t.Fatalf("initial phase = %q, want empty", snap0.Phase)
	}

	tr.Strategizing()
	tr.TopicsPlanned([]string{"T1", "T2"})
	tr.TopicDone(0, 88)

	var last orchestrator.Snapshot
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

func TestHubLateSubscriberFromStore(t *testing.T) {
	ps := newFakePS()
	ps.camps["done1"] = &store.Campaign{ID: "done1", Status: "done",
		Progress: &orchestrator.Snapshot{Phase: orchestrator.PhaseDone, Percent: 100}}
	hub := NewHub(context.Background(), ps)

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
	hub := NewHub(context.Background(), newFakePS())
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
	hub := NewHub(context.Background(), newFakePS())
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
