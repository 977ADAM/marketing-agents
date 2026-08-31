package httpapi

import (
	"context"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// ProgressStore — то, что Hub'у нужно от стора (для кампаний и проверок).
type ProgressStore interface {
	SaveProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error
	Get(ctx context.Context, id string) (*store.Campaign, error)
	SaveReviewProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error
	GetReview(ctx context.Context, id string) (*store.Review, error)
}

// CampaignProgress — то, что runner получает от Hub: интерфейс прогресса
// (для передачи в orchestrator.Run) плюс терминальные методы Done/Failed.
type CampaignProgress interface {
	orchestrator.Progress
	Done()
	Failed()
}

// runKind — тип живого прогона: какой стор-объект персистим и читаем.
type runKind int

const (
	kindCampaign runKind = iota
	kindReview
)

// Hub держит живые прогоны (кампании и проверки текстов) и рассылает снимки
// прогресса подписчикам.
type Hub struct {
	baseCtx context.Context
	store   ProgressStore
	mu      sync.Mutex
	runs    map[string]*hubRun
}

func NewHub(baseCtx context.Context, st ProgressStore) *Hub {
	return &Hub{baseCtx: baseCtx, store: st, runs: map[string]*hubRun{}}
}

type hubRun struct {
	mu   sync.Mutex
	kind runKind
	snap orchestrator.Snapshot
	subs map[chan orchestrator.Snapshot]struct{}
	done bool
}

// Tracker регистрирует живой прогон кампании и возвращает реализацию Progress.
func (h *Hub) Tracker(id string) CampaignProgress {
	return h.newTracker(id, kindCampaign)
}

// ReviewTracker регистрирует живой прогон проверки текстов.
func (h *Hub) ReviewTracker(id string) CampaignProgress {
	return h.newTracker(id, kindReview)
}

func (h *Hub) newTracker(id string, kind runKind) CampaignProgress {
	r := &hubRun{kind: kind, subs: map[chan orchestrator.Snapshot]struct{}{}}
	h.mu.Lock()
	h.runs[id] = r
	h.mu.Unlock()
	return &tracker{hub: h, id: id, run: r}
}

// Subscribe возвращает текущий снимок, канал будущих снимков и функцию отписки
// для кампании. Если живого прогона нет — снимок берётся из стора, канал закрыт.
func (h *Hub) Subscribe(id string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func()) {
	return h.subscribe(id, kindCampaign)
}

// SubscribeReview — то же для проверки текстов.
func (h *Hub) SubscribeReview(id string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func()) {
	return h.subscribe(id, kindReview)
}

func (h *Hub) subscribe(id string, kind runKind) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func()) {
	h.mu.Lock()
	r, ok := h.runs[id]
	h.mu.Unlock()
	if !ok || r.kind != kind {
		ch := make(chan orchestrator.Snapshot)
		close(ch)
		return h.snapshotFromStore(id, kind), ch, func() {}
	}
	r.mu.Lock()
	if r.done {
		// прогон завершился между чтением hub.runs и захватом r.mu —
		// отдаём снимок из стора и закрытый канал (как для неживого run).
		r.mu.Unlock()
		closed := make(chan orchestrator.Snapshot)
		close(closed)
		return h.snapshotFromStore(id, kind), closed, func() {}
	}
	ch := make(chan orchestrator.Snapshot, 8)
	snap := cloneSnapshot(r.snap)
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	// cancel лишь разрегистрирует подписчика. Канал НЕ закрывается здесь:
	// закрывать его вправе только отправитель (finish), иначе update() может
	// словить панику "send on closed channel". Закрытый по cancel канал
	// никем не читается (SSE-хендлер уже вышел) и будет собран GC.
	cancel := func() {
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}
	return snap, ch, cancel
}

func (h *Hub) snapshotFromStore(id string, kind runKind) orchestrator.Snapshot {
	if kind == kindReview {
		r, err := h.store.GetReview(h.baseCtx, id)
		if err == nil && r != nil && r.Progress != nil {
			return *r.Progress
		}
		if r != nil && r.Status == "done" {
			return orchestrator.Snapshot{Phase: orchestrator.PhaseDone, Percent: 100}
		}
		return orchestrator.Snapshot{Phase: orchestrator.PhaseFailed}
	}
	c, err := h.store.Get(h.baseCtx, id)
	if err == nil && c != nil && c.Progress != nil {
		return *c.Progress
	}
	if c != nil && c.Status == "done" {
		return orchestrator.Snapshot{Phase: orchestrator.PhaseDone, Percent: 100}
	}
	// неизвестная/прерванная кампания без снимка: failed, прогресс неизвестен
	return orchestrator.Snapshot{Phase: orchestrator.PhaseFailed}
}

func cloneSnapshot(s orchestrator.Snapshot) orchestrator.Snapshot {
	cp := s
	cp.Topics = append([]orchestrator.TopicProgress(nil), s.Topics...)
	return cp
}

// tracker реализует orchestrator.Progress: мутирует снимок, персистит, рассылает.
type tracker struct {
	hub *Hub
	id  string
	run *hubRun
}

func (t *tracker) update(fn func(s *orchestrator.Snapshot)) {
	t.run.mu.Lock()
	fn(&t.run.snap)
	if t.run.snap.Phase != orchestrator.PhaseFailed {
		t.run.snap.Percent = orchestrator.Percent(t.run.snap.Phase, t.run.snap.TopicsDone, t.run.snap.TopicTotal)
	}
	snap := cloneSnapshot(t.run.snap)
	subs := make([]chan orchestrator.Snapshot, 0, len(t.run.subs))
	for c := range t.run.subs {
		subs = append(subs, c)
	}
	t.run.mu.Unlock()

	switch t.run.kind {
	case kindReview:
		_ = t.hub.store.SaveReviewProgress(t.hub.baseCtx, t.id, snap)
	default:
		_ = t.hub.store.SaveProgress(t.hub.baseCtx, t.id, snap)
	}
	for _, c := range subs {
		select {
		case c <- snap:
		default:
		}
	}
}

func (t *tracker) setTopic(i int, fn func(*orchestrator.TopicProgress)) {
	t.update(func(s *orchestrator.Snapshot) {
		if i >= 0 && i < len(s.Topics) {
			fn(&s.Topics[i])
		}
	})
}

func (t *tracker) Strategizing() {
	t.update(func(s *orchestrator.Snapshot) { s.Phase = orchestrator.PhaseStrategizing })
}
func (t *tracker) TopicsPlanned(titles []string) {
	t.update(func(s *orchestrator.Snapshot) {
		s.Phase = orchestrator.PhaseProducing
		s.TopicTotal = len(titles)
		s.Topics = make([]orchestrator.TopicProgress, len(titles))
		for i, ti := range titles {
			s.Topics[i] = orchestrator.TopicProgress{Index: i, Title: ti, State: orchestrator.TopicPending}
		}
	})
}
func (t *tracker) TopicWriting(i int) {
	t.setTopic(i, func(tp *orchestrator.TopicProgress) { tp.State = orchestrator.TopicWriting })
}
func (t *tracker) TopicReviewing(i, iter int) {
	t.setTopic(i, func(tp *orchestrator.TopicProgress) { tp.State = orchestrator.TopicReviewing; tp.Iter = iter })
}
func (t *tracker) TopicRevising(i, iter int) {
	t.setTopic(i, func(tp *orchestrator.TopicProgress) { tp.State = orchestrator.TopicRevising; tp.Iter = iter })
}
func (t *tracker) TopicDone(i, score int) {
	t.update(func(s *orchestrator.Snapshot) {
		if i >= 0 && i < len(s.Topics) {
			s.Topics[i].State = orchestrator.TopicDone
			s.Topics[i].Score = score
		}
		s.TopicsDone++
	})
}

// Done/Failed вызывает runner по завершении прогона: терминальная фаза + закрытие подписчиков.
func (t *tracker) Done()   { t.finish(orchestrator.PhaseDone) }
func (t *tracker) Failed() { t.finish(orchestrator.PhaseFailed) }

// finish ставит терминальную фазу и закрывает каналы подписчиков. ВАЖНО: финальный
// снимок рассылается тем же неблокирующим fan-out, что и остальные, поэтому при
// переполненном буфере подписчика он может НЕ дойти — закрытие канала и есть сигнал
// «прогон завершён», а финальную фазу потребитель дочитывает из стора (Subscribe
// после завершения берёт снимок из БД).
func (t *tracker) finish(ph orchestrator.Phase) {
	t.update(func(s *orchestrator.Snapshot) { s.Phase = ph })
	t.run.mu.Lock()
	t.run.done = true
	for c := range t.run.subs {
		delete(t.run.subs, c)
		close(c)
	}
	t.run.mu.Unlock()
	t.hub.mu.Lock()
	delete(t.hub.runs, t.id)
	t.hub.mu.Unlock()
}
