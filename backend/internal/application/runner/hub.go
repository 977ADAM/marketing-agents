package runner

import (
	"context"
	"github.com/977ADAM/marketing-agents/internal/core/logger"
	"sync"
	"time"

	run "github.com/977ADAM/marketing-agents/internal/core/run"
	campaign "github.com/977ADAM/marketing-agents/internal/features/campaign/domain"
	review "github.com/977ADAM/marketing-agents/internal/features/review/domain"
)

// CampaignProgressStore — что Hub'у нужно от хранилища кампаний.
type CampaignProgressStore interface {
	SaveProgress(ctx context.Context, id string, snap run.Snapshot) error
	Get(ctx context.Context, id string) (*campaign.Record, error)
}

// ReviewProgressStore — то же для проверок текстов.
type ReviewProgressStore interface {
	SaveCheckProgress(ctx context.Context, id string, snap run.Snapshot) error
	GetCheck(ctx context.Context, id string) (*review.Record, error)
}

// CampaignProgress — то, что runner получает от Hub: интерфейс прогресса
// (для передачи в orchestrator.Run) плюс терминальные методы Done/Failed.
// Включает и подбор тем: трекер умеет показывать этап researching.
type CampaignProgress interface {
	run.Progress
	run.ResearchProgress
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
	baseCtx   context.Context
	campaigns CampaignProgressStore
	reviews   ReviewProgressStore
	mu        sync.Mutex
	runs      map[string]*hubRun
	logger    corelogger.Logger
}

func NewHub(baseCtx context.Context, campaigns CampaignProgressStore, reviews ReviewProgressStore, loggers ...corelogger.Logger) *Hub {
	log := corelogger.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		log = loggers[0]
	}
	return &Hub{baseCtx: baseCtx, campaigns: campaigns, reviews: reviews, runs: map[string]*hubRun{}, logger: log}
}

type hubRun struct {
	mu   sync.Mutex
	kind runKind
	snap run.Snapshot
	subs map[chan run.Snapshot]struct{}
	done bool
	ctx  context.Context
}

// Tracker регистрирует живой прогон кампании и возвращает реализацию Progress.
func (h *Hub) Tracker(id string, contexts ...context.Context) CampaignProgress {
	return h.newTracker(id, kindCampaign, contexts...)
}

// ReviewTracker регистрирует живой прогон проверки текстов.
func (h *Hub) ReviewTracker(id string, contexts ...context.Context) CampaignProgress {
	return h.newTracker(id, kindReview, contexts...)
}

func (h *Hub) newTracker(id string, kind runKind, contexts ...context.Context) CampaignProgress {
	ctx := h.baseCtx
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	r := &hubRun{kind: kind, ctx: ctx, subs: map[chan run.Snapshot]struct{}{}}
	r.snap.Revision = h.snapshotFromStore(id, kind).Revision
	h.mu.Lock()
	h.runs[id] = r
	h.mu.Unlock()
	return &tracker{hub: h, id: id, run: r}
}

// Subscribe возвращает текущий снимок, канал будущих снимков и функцию отписки
// для кампании. Если живого прогона нет — снимок берётся из стора, канал закрыт.
func (h *Hub) Subscribe(id string) (run.Snapshot, <-chan run.Snapshot, func()) {
	return h.subscribe(id, kindCampaign)
}

// SubscribeReview — то же для проверки текстов.
func (h *Hub) SubscribeReview(id string) (run.Snapshot, <-chan run.Snapshot, func()) {
	return h.subscribe(id, kindReview)
}

func (h *Hub) subscribe(id string, kind runKind) (run.Snapshot, <-chan run.Snapshot, func()) {
	h.mu.Lock()
	r, ok := h.runs[id]
	h.mu.Unlock()
	if !ok || r.kind != kind {
		return h.subscribeRemote(id, kind)
	}
	r.mu.Lock()
	if r.done {
		// прогон завершился между чтением hub.runs и захватом r.mu —
		// отдаём снимок из стора и закрытый канал (как для неживого run).
		r.mu.Unlock()
		closed := make(chan run.Snapshot)
		close(closed)
		return h.snapshotFromStore(id, kind), closed, func() {}
	}
	ch := make(chan run.Snapshot, 8)
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

func (h *Hub) snapshotFromStore(id string, kind runKind) run.Snapshot {
	ctx, cancel := context.WithTimeout(h.baseCtx, 5*time.Second)
	defer cancel()
	snap, _ := h.storedSnapshot(ctx, id, kind)
	return snap
}
func (h *Hub) storedSnapshot(ctx context.Context, id string, kind runKind) (run.Snapshot, error) {
	var status string
	var progress *run.Snapshot
	if kind == kindReview {
		r, err := h.reviews.GetCheck(ctx, id)
		if err != nil {
			return run.Snapshot{}, err
		}
		status = r.Status
		progress = r.Progress
	} else {
		r, err := h.campaigns.Get(ctx, id)
		if err != nil {
			return run.Snapshot{}, err
		}
		status = r.Status
		progress = r.Progress
	}
	snap := run.Snapshot{Phase: run.PhasePending}
	if progress != nil {
		snap = cloneSnapshot(*progress)
	}
	switch status {
	case "done":
		snap.Phase = run.PhaseDone
		snap.Percent = 100
	case "failed":
		snap.Phase = run.PhaseFailed
	}
	return snap, nil
}
func (h *Hub) subscribeRemote(id string, kind runKind) (run.Snapshot, <-chan run.Snapshot, func()) {
	ctx, cancel := context.WithCancel(h.baseCtx)
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	snap, err := h.storedSnapshot(readCtx, id, kind)
	readCancel()
	ch := make(chan run.Snapshot, 8)
	if err != nil || snap.Phase == run.PhaseDone || snap.Phase == run.PhaseFailed {
		close(ch)
		return snap, ch, cancel
	}
	go func() {
		defer close(ch)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		last := snap.Revision
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
				next, err := h.storedSnapshot(readCtx, id, kind)
				readCancel()
				if err != nil {
					h.logger.Error("poll progress", "id", id, "err", err)
					continue
				}
				terminal := next.Phase == run.PhaseDone || next.Phase == run.PhaseFailed
				if next.Revision > last || terminal {
					select {
					case ch <- next:
					default:
					}
					last = next.Revision
				}
				if terminal {
					return
				}
			}
		}
	}()
	return snap, ch, cancel
}

func cloneSnapshot(s run.Snapshot) run.Snapshot {
	cp := s
	cp.Topics = append([]run.TopicProgress(nil), s.Topics...)
	return cp
}

// tracker реализует run.Progress: мутирует снимок, персистит, рассылает.
type tracker struct {
	hub *Hub
	id  string
	run *hubRun
}

func (t *tracker) update(fn func(s *run.Snapshot)) {
	t.run.mu.Lock()
	defer t.run.mu.Unlock()
	if t.run.done {
		return
	}
	t.updateLocked(fn)
}

// One lock covers mutation, persistence and publication so updates cannot pass
// each other or send to a channel after finish closes it.
func (t *tracker) updateLocked(fn func(s *run.Snapshot)) {
	fn(&t.run.snap)
	t.run.snap.Revision++
	if t.run.snap.Phase != run.PhaseFailed {
		t.run.snap.Percent = run.Percent(t.run.snap.Phase, t.run.snap.TopicsDone, t.run.snap.TopicTotal)
	}
	snap := cloneSnapshot(t.run.snap)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.run.ctx), 5*time.Second)
	defer cancel()
	var err error
	if t.run.kind == kindReview {
		err = t.hub.reviews.SaveCheckProgress(ctx, t.id, snap)
	} else {
		err = t.hub.campaigns.SaveProgress(ctx, t.id, snap)
	}
	if err != nil {
		t.hub.logger.Error("save progress", "id", t.id, "revision", snap.Revision, "err", err)
	}
	for c := range t.run.subs {
		select {
		case c <- snap:
		default:
		}
	}
}

func (t *tracker) setTopic(i int, fn func(*run.TopicProgress)) {
	t.update(func(s *run.Snapshot) {
		if i >= 0 && i < len(s.Topics) {
			fn(&s.Topics[i])
		}
	})
}

func (t *tracker) Strategizing() {
	t.update(func(s *run.Snapshot) { s.Phase = run.PhaseStrategizing })
}
func (t *tracker) TopicsPlanned(titles []string) {
	t.update(func(s *run.Snapshot) {
		s.Phase = run.PhaseProducing
		s.Stage = ""
		s.TopicTotal = len(titles)
		// Подбор тем мог успеть отметить сеялки готовыми — счётчик начинаем заново,
		// иначе процент генерации поедет вверх с запасом.
		s.TopicsDone = 0
		s.Topics = make([]run.TopicProgress, len(titles))
		for i, ti := range titles {
			s.Topics[i] = run.TopicProgress{Index: i, Title: ti, State: run.TopicPending}
		}
	})
}

// --- подбор тем по спросу (необязательная часть прогресса) ---

// Researching объявляет текущий подэтап подбора тем.
func (t *tracker) Researching(stage run.ResearchStage) {
	t.update(func(s *run.Snapshot) {
		s.Phase = run.PhaseResearching
		s.Stage = string(stage)
	})
}

// ResearchSeeds объявляет сеялки единицами работы: по каждой придёт SeedDone.
func (t *tracker) ResearchSeeds(seeds []string) {
	t.update(func(s *run.Snapshot) {
		s.Phase = run.PhaseResearching
		s.TopicTotal = len(seeds)
		s.TopicsDone = 0
		s.Topics = make([]run.TopicProgress, len(seeds))
		for i, seed := range seeds {
			s.Topics[i] = run.TopicProgress{Index: i, Title: seed, State: run.TopicPending}
		}
	})
}

// ResearchSeedDone отмечает, что спрос по сеялке собран.
func (t *tracker) ResearchSeedDone(i int) {
	t.update(func(s *run.Snapshot) {
		if i >= 0 && i < len(s.Topics) {
			s.Topics[i].State = run.TopicDone
		}
		s.TopicsDone++
	})
}
func (t *tracker) TopicWriting(i int) {
	t.setTopic(i, func(tp *run.TopicProgress) { tp.State = run.TopicWriting })
}
func (t *tracker) TopicReviewing(i, iter int) {
	t.setTopic(i, func(tp *run.TopicProgress) { tp.State = run.TopicReviewing; tp.Iter = iter })
}
func (t *tracker) TopicRevising(i, iter int) {
	t.setTopic(i, func(tp *run.TopicProgress) { tp.State = run.TopicRevising; tp.Iter = iter })
}
func (t *tracker) TopicDone(i, score int) {
	t.update(func(s *run.Snapshot) {
		if i >= 0 && i < len(s.Topics) {
			s.Topics[i].State = run.TopicDone
			s.Topics[i].Score = score
		}
		s.TopicsDone++
	})
}

// Done/Failed вызывает runner по завершении прогона: терминальная фаза + закрытие подписчиков.
func (t *tracker) Done()   { t.finish(run.PhaseDone) }
func (t *tracker) Failed() { t.finish(run.PhaseFailed) }

// finish ставит терминальную фазу и закрывает каналы подписчиков. ВАЖНО: финальный
// снимок рассылается тем же неблокирующим fan-out, что и остальные, поэтому при
// переполненном буфере подписчика он может НЕ дойти — закрытие канала и есть сигнал
// «прогон завершён», а финальную фазу потребитель дочитывает из стора (Subscribe
// после завершения берёт снимок из БД).
func (t *tracker) finish(ph run.Phase) {
	t.run.mu.Lock()
	if t.run.done {
		t.run.mu.Unlock()
		return
	}
	t.updateLocked(func(s *run.Snapshot) { s.Phase = ph })
	t.run.done = true
	for c := range t.run.subs {
		delete(t.run.subs, c)
		close(c)
	}
	t.run.mu.Unlock()
	t.hub.mu.Lock()
	if t.hub.runs[t.id] == t.run {
		delete(t.hub.runs, t.id)
	}
	t.hub.mu.Unlock()
}
