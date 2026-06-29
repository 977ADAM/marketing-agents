# Живой прогресс кампании (SSE) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Показывать живой по-темный прогресс прогона кампании через Server-Sent Events.

**Architecture:** Оркестратор объявляет ход работы через интерфейс `orchestrator.Progress` (подход A). Реализация — `tracker`, создаваемый `Hub` в `internal/httpapi`: на каждый вызов обновляет снимок, пишет его в БД (`store.SaveProgress`) и рассылает SSE-подписчикам. Эндпоинт `GET /api/campaigns/{id}/events` стримит снимки; фронт рисует их через `EventSource`.

**Tech Stack:** Go 1.25 (`net/http` SSE, `pgx`), React+Vite+TS (`EventSource`), тесты — Go `testing` + Vitest.

**Spec:** `docs/superpowers/specs/2026-06-29-live-campaign-progress-design.md`

**Branch:** работаем на `feat/live-progress` (создать перед Task 1).

---

### Task 1: Стор — миграция, `SaveProgress`, поле `Progress`

**Files:**
- Create: `internal/store/migrations/0002_progress.sql`
- Modify: `internal/store/store.go` (тип `Campaign`, функция `Get`, новый метод `SaveProgress`)
- Test: `internal/store/store_test.go` (integration)

Зависит от типа `orchestrator.Snapshot` — он создаётся в Task 2. **Делать Task 2 до этого, либо** временно объявить минимальный `Snapshot` в orchestrator. Рекомендуется порядок: Task 2 → Task 1. Ниже код предполагает, что `orchestrator.Snapshot` уже существует.

- [ ] **Step 1: Миграция** — создать `internal/store/migrations/0002_progress.sql`:

```sql
-- 0002_progress.sql
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS progress JSONB;
```

- [ ] **Step 2: Failing integration-тест** — добавить в `internal/store/store_test.go`:

```go
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
	if got.Progress == nil || got.Progress.Percent != 50 || got.Progress.TopicsDone != 1 {
		t.Fatalf("Progress = %+v", got.Progress)
	}
	if len(got.Progress.Topics) != 2 || got.Progress.Topics[0].Score != 88 {
		t.Fatalf("topics = %+v", got.Progress.Topics)
	}
}
```

- [ ] **Step 3: Запустить тест — убедиться, что не компилируется/падает**

Run: `docker compose up -d db && DATABASE_URL=postgres://app:app@localhost:5432/marketing?sslmode=disable go test -tags=integration ./internal/store/ -run TestProgressRoundTrip -v`
Expected: ошибка компиляции (`SaveProgress` и `Campaign.Progress` не существуют).

- [ ] **Step 4: Добавить поле в тип `Campaign`** — в `internal/store/store.go`, в структуру `Campaign` после поля `Deliverables`:

```go
	Progress     *orchestrator.Snapshot `json:"progress,omitempty"`
```

- [ ] **Step 5: Метод `SaveProgress`** — в `internal/store/store.go` (рядом с `MarkRunning`):

```go
// SaveProgress сохраняет снимок прогресса прогона (перезаписывает прошлый).
func (s *Store) SaveProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error {
	b, _ := json.Marshal(snap)
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET progress=$2, updated_at=now() WHERE id=$1`, id, b)
	return err
}
```

- [ ] **Step 6: Читать `progress` в `Get`** — в `internal/store/store.go`, функция `Get`. Заменить SELECT и скан первой строки:

```go
	var c Campaign
	var briefJSON, stratJSON, progressJSON []byte
	var cost *float64
	var errText *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, status, brief, strategy, cost_usd, error, progress, created_at, updated_at
		 FROM campaigns WHERE id=$1`, id).
		Scan(&c.ID, &c.ClientID, &c.Status, &briefJSON, &stratJSON, &cost, &errText, &progressJSON, &c.CreatedAt, &c.UpdatedAt)
```

И после блока с `c.Error`:

```go
	if len(progressJSON) > 0 {
		var snap orchestrator.Snapshot
		if json.Unmarshal(progressJSON, &snap) == nil {
			c.Progress = &snap
		}
	}
```

- [ ] **Step 7: Запустить тест — должен пройти**

Run: `DATABASE_URL=postgres://app:app@localhost:5432/marketing?sslmode=disable go test -tags=integration ./internal/store/ -run TestProgressRoundTrip -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/store/
git commit -m "feat(store): колонка progress + SaveProgress, отдавать снимок в Get"
```

---

### Task 2: Модель прогресса оркестратора

**Files:**
- Create: `internal/orchestrator/progress.go`
- Test: `internal/orchestrator/progress_test.go`

- [ ] **Step 1: Failing-тест** — создать `internal/orchestrator/progress_test.go`:

```go
package orchestrator

import "testing"

func TestComputePercent(t *testing.T) {
	cases := []struct {
		ph          Phase
		done, total int
		want        int
	}{
		{PhaseStrategizing, 0, 0, 5},
		{PhaseProducing, 0, 2, 10},
		{PhaseProducing, 1, 2, 52}, // 10 + 85*1/2 = 52 (округление вниз)
		{PhaseProducing, 2, 2, 95},
		{PhaseDone, 2, 2, 100},
	}
	for _, c := range cases {
		if got := computePercent(c.ph, c.done, c.total); got != c.want {
			t.Errorf("computePercent(%q,%d,%d) = %d, want %d", c.ph, c.done, c.total, got, c.want)
		}
	}
}

func TestNopProgressDoesNotPanic(t *testing.T) {
	var p Progress = NopProgress{}
	p.Strategizing()
	p.TopicsPlanned([]string{"a"})
	p.TopicWriting(0)
	p.TopicReviewing(0, 1)
	p.TopicRevising(0, 1)
	p.TopicDone(0, 90)
}
```

- [ ] **Step 2: Запустить — убедиться, что не компилируется**

Run: `go test ./internal/orchestrator/ -run 'TestComputePercent|TestNopProgress' -v`
Expected: ошибка компиляции (типы/функции не существуют).

- [ ] **Step 3: Реализация** — создать `internal/orchestrator/progress.go`:

```go
package orchestrator

// Phase — крупная стадия прогона.
type Phase string

const (
	PhaseStrategizing Phase = "strategizing"
	PhaseProducing    Phase = "producing"
	PhaseDone         Phase = "done"
	PhaseFailed       Phase = "failed"
)

// TopicState — состояние работы над одной темой.
type TopicState string

const (
	TopicPending   TopicState = "pending"
	TopicWriting   TopicState = "writing"
	TopicReviewing TopicState = "reviewing"
	TopicRevising  TopicState = "revising"
	TopicDone      TopicState = "done"
)

// TopicProgress — прогресс по одной теме.
type TopicProgress struct {
	Index int        `json:"index"`
	Title string     `json:"title"`
	State TopicState `json:"state"`
	Iter  int        `json:"iter,omitempty"`  // 1-based итерация критика
	Score int        `json:"score,omitempty"` // последний/финальный score
}

// Snapshot — полный снимок прогресса прогона.
type Snapshot struct {
	Phase      Phase           `json:"phase"`
	Topics     []TopicProgress `json:"topics"`
	TopicTotal int             `json:"topic_total"`
	TopicsDone int             `json:"topics_done"`
	Percent    int             `json:"percent"`
}

// Progress — оркестратор «объявляет», что делает. Реализация concurrency-safe
// (темы обрабатываются параллельно).
type Progress interface {
	Strategizing()
	TopicsPlanned(titles []string)
	TopicWriting(i int)
	TopicReviewing(i, iter int)
	TopicRevising(i, iter int)
	TopicDone(i, score int)
}

// NopProgress — заглушка по умолчанию (для тестов и nil-вызовов).
type NopProgress struct{}

func (NopProgress) Strategizing()           {}
func (NopProgress) TopicsPlanned([]string)  {}
func (NopProgress) TopicWriting(int)        {}
func (NopProgress) TopicReviewing(int, int) {}
func (NopProgress) TopicRevising(int, int)  {}
func (NopProgress) TopicDone(int, int)      {}

// Доли прогресс-бара (настраиваемые).
const (
	pctStrategizing = 5
	pctPlanned      = 10
	pctProducingMax = 95
)

// computePercent — оценка % по фазе и числу готовых тем. Для PhaseFailed
// процент не вычисляется (вызывающая сторона не трогает прошлое значение).
func computePercent(ph Phase, done, total int) int {
	switch ph {
	case PhaseStrategizing:
		return pctStrategizing
	case PhaseProducing:
		if total == 0 {
			return pctPlanned
		}
		return pctPlanned + (pctProducingMax-pctPlanned)*done/total
	case PhaseDone:
		return 100
	}
	return 0
}
```

- [ ] **Step 4: Запустить — должно пройти**

Run: `go test ./internal/orchestrator/ -run 'TestComputePercent|TestNopProgress' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/progress.go internal/orchestrator/progress_test.go
git commit -m "feat(orchestrator): модель прогресса (Snapshot, Progress, NopProgress)"
```

---

### Task 3: Оркестратор пушит события прогресса

**Files:**
- Modify: `internal/orchestrator/orchestrator.go` (`Run`, `produce`)
- Modify: `internal/orchestrator/orchestrator_test.go` (обновить вызовы `Run`, добавить тест событий)

- [ ] **Step 1: Failing-тест** — добавить в `internal/orchestrator/orchestrator_test.go` recorder и тест последовательности событий:

```go
// recordProgress фиксирует порядок вызовов Progress (потокобезопасно).
type recordProgress struct {
	mu     sync.Mutex
	events []string
}

func (r *recordProgress) add(e string) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recordProgress) Strategizing()              { r.add("strategizing") }
func (r *recordProgress) TopicsPlanned(t []string)   { r.add(fmt.Sprintf("planned:%d", len(t))) }
func (r *recordProgress) TopicWriting(i int)         { r.add(fmt.Sprintf("writing:%d", i)) }
func (r *recordProgress) TopicReviewing(i, it int)   { r.add(fmt.Sprintf("reviewing:%d:%d", i, it)) }
func (r *recordProgress) TopicRevising(i, it int)    { r.add(fmt.Sprintf("revising:%d:%d", i, it)) }
func (r *recordProgress) TopicDone(i, sc int)        { r.add(fmt.Sprintf("done:%d:%d", i, sc)) }

func TestRunEmitsProgress(t *testing.T) {
	fake := llm.NewFake()
	fake.Responses[agents.RoleStrategist] = []string{
		`{"positioning":"p","topics":[{"title":"T1","angle":"a","points":["x"]}]}`,
	}
	fake.Responses[agents.RoleCopywriter] = []string{
		`{"topic":"T1","title":"v1","body":"b","cta":"c"}`,
		`{"topic":"T1","title":"v2","body":"b","cta":"c"}`,
	}
	fake.Responses[agents.RoleCritic] = []string{
		`{"score":50,"issues":["слабо"],"verdict":"revise"}`,
		`{"score":85,"issues":[],"verdict":"accept"}`,
	}
	o := New(fake, Options{CriticMaxIter: 3, ScoreThreshold: 80, CostPer1KPrompt: 1, CostPer1KCompletion: 1})
	rec := &recordProgress{}

	if _, err := o.Run(context.Background(), brief(), rec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"strategizing", "planned:1", "writing:0", "reviewing:0:1", "revising:0:1", "reviewing:0:2", "done:0:85"}
	if len(rec.events) != len(want) {
		t.Fatalf("events = %v, want %v", rec.events, want)
	}
	for i := range want {
		if rec.events[i] != want[i] {
			t.Fatalf("events = %v, want %v", rec.events, want)
		}
	}
}
```

Добавить в импорты теста `"fmt"` и `"sync"`.

- [ ] **Step 2: Обновить существующие вызовы `Run`** — во всех тестах в `orchestrator_test.go` заменить `o.Run(context.Background(), brief())` на `o.Run(context.Background(), brief(), nil)` (5 мест: `TestRunFanOutAcceptsImmediately`, `TestRunCriticReviseLoop`, `TestRunPicksBestWhenMaxIter`, `TestRunFailsWhenStrategistErrors`, `TestRunCapsTopics`).

- [ ] **Step 3: Запустить — должно не компилироваться (сигнатура `Run`)**

Run: `go test ./internal/orchestrator/ -run TestRun -v`
Expected: ошибка компиляции (`Run` принимает 2 аргумента).

- [ ] **Step 4: Изменить `Run`** — в `internal/orchestrator/orchestrator.go`. Сигнатура и тело (вставки помечены):

```go
func (o *Orchestrator) Run(ctx context.Context, b agents.Brief, p Progress) (Result, error) {
	if p == nil {
		p = NopProgress{}
	}
	var mu sync.Mutex
	total := llm.Usage{}
	addUsage := func(u llm.Usage) {
		mu.Lock()
		total = total.Add(u)
		mu.Unlock()
	}

	p.Strategizing()
	strat, u, err := o.strategist.Run(ctx, b)
	if err != nil {
		return Result{}, err
	}
	addUsage(u)

	if o.opt.MaxTopics > 0 && len(strat.Topics) > o.opt.MaxTopics {
		strat.Topics = strat.Topics[:o.opt.MaxTopics]
	}

	titles := make([]string, len(strat.Topics))
	for i, t := range strat.Topics {
		titles[i] = t.Title
	}
	p.TopicsPlanned(titles)

	deliverables := make([]agents.Deliverable, len(strat.Topics))
	g, gctx := errgroup.WithContext(ctx)
	for i, topic := range strat.Topics {
		i, topic := i, topic
		g.Go(func() error {
			d, u, err := o.produce(gctx, b, strat, i, topic, p)
			addUsage(u)
			if err != nil {
				return fmt.Errorf("topic %q: %w", topic.Title, err)
			}
			deliverables[i] = d
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	return Result{
		Strategy:     strat,
		Deliverables: deliverables,
		CostUSD:      o.cost(total),
	}, nil
}
```

- [ ] **Step 5: Изменить `produce`** — там же, добавить параметры `i int` и `p Progress` и вызовы:

```go
func (o *Orchestrator) produce(ctx context.Context, b agents.Brief, s agents.Strategy, i int, t agents.Topic, p Progress) (agents.Deliverable, llm.Usage, error) {
	total := llm.Usage{}
	p.TopicWriting(i)
	art, u, err := o.copywriter.Run(ctx, b, s, t)
	total = total.Add(u)
	if err != nil {
		return agents.Deliverable{}, total, err
	}

	best := agents.Deliverable{Article: art}
	bestSet := false
	for iter := 0; iter < o.opt.CriticMaxIter; iter++ {
		p.TopicReviewing(i, iter+1)
		rev, u, err := o.critic.Run(ctx, b, art)
		total = total.Add(u)
		if err != nil {
			return agents.Deliverable{}, total, err
		}
		if !bestSet || rev.Score > best.Review.Score {
			best = agents.Deliverable{Article: art, Review: rev}
			bestSet = true
		}
		if rev.Verdict == "accept" || rev.Score >= o.opt.ScoreThreshold {
			p.TopicDone(i, rev.Score)
			return agents.Deliverable{Article: art, Review: rev}, total, nil
		}
		if iter == o.opt.CriticMaxIter-1 {
			break
		}
		p.TopicRevising(i, iter+1)
		art, u, err = o.copywriter.Revise(ctx, art, rev)
		total = total.Add(u)
		if err != nil {
			return agents.Deliverable{}, total, err
		}
	}
	p.TopicDone(i, best.Review.Score)
	return best, total, nil
}
```

- [ ] **Step 6: Запустить весь пакет — зелено**

Run: `go test ./internal/orchestrator/ -v`
Expected: PASS (включая `TestRunEmitsProgress` и обновлённые старые тесты)

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/orchestrator.go internal/orchestrator/orchestrator_test.go
git commit -m "feat(orchestrator): эмиссия событий прогресса по стадиям и темам"
```

---

### Task 4: Hub + tracker в httpapi

**Files:**
- Create: `internal/httpapi/progress.go`
- Test: `internal/httpapi/progress_test.go`

- [ ] **Step 1: Failing-тест** — создать `internal/httpapi/progress_test.go`:

```go
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
	// дренируем до закрытия
	for range ch {
	}
	// после Done подписка на c2 берётся из стора (нет живого run) → закрытый канал
	_, ch2, cancel2 := hub.Subscribe("c2")
	defer cancel2()
	if _, ok := <-ch2; ok {
		t.Fatal("expected closed channel after finish")
	}
}
```

- [ ] **Step 2: Запустить — не компилируется**

Run: `go test ./internal/httpapi/ -run TestHub -v`
Expected: ошибка компиляции (`NewHub`/`Tracker`/`Subscribe` не существуют).

- [ ] **Step 3: Реализация** — создать `internal/httpapi/progress.go`:

```go
package httpapi

import (
	"context"
	"sync"

	"github.com/977ADAM/marketing-agents/internal/orchestrator"
	"github.com/977ADAM/marketing-agents/internal/store"
)

// ProgressStore — то, что Hub'у нужно от стора.
type ProgressStore interface {
	SaveProgress(ctx context.Context, id string, snap orchestrator.Snapshot) error
	Get(ctx context.Context, id string) (*store.Campaign, error)
}

// Hub держит живые прогоны и рассылает снимки прогресса подписчикам.
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
	snap orchestrator.Snapshot
	subs map[chan orchestrator.Snapshot]struct{}
}

// Tracker регистрирует живой прогон и возвращает реализацию orchestrator.Progress.
func (h *Hub) Tracker(id string) *tracker {
	r := &hubRun{subs: map[chan orchestrator.Snapshot]struct{}{}}
	h.mu.Lock()
	h.runs[id] = r
	h.mu.Unlock()
	return &tracker{hub: h, id: id, run: r}
}

// Subscribe возвращает текущий снимок, канал будущих снимков и функцию отписки.
// Если живого прогона нет (рестарт/завершён) — снимок берётся из стора, канал закрыт.
func (h *Hub) Subscribe(id string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func()) {
	h.mu.Lock()
	r, ok := h.runs[id]
	h.mu.Unlock()
	if !ok {
		ch := make(chan orchestrator.Snapshot)
		close(ch)
		return h.snapshotFromStore(id), ch, func() {}
	}
	ch := make(chan orchestrator.Snapshot, 8)
	r.mu.Lock()
	snap := cloneSnapshot(r.snap)
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	cancel := func() {
		r.mu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
	return snap, ch, cancel
}

func (h *Hub) snapshotFromStore(id string) orchestrator.Snapshot {
	c, err := h.store.Get(h.baseCtx, id)
	if err == nil && c != nil && c.Progress != nil {
		return *c.Progress
	}
	ph := orchestrator.PhaseFailed
	if c != nil && c.Status == "done" {
		ph = orchestrator.PhaseDone
	}
	return orchestrator.Snapshot{Phase: ph, Percent: 100}
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
		t.run.snap.Percent = computePercent(t.run.snap.Phase, t.run.snap.TopicsDone, t.run.snap.TopicTotal)
	}
	snap := cloneSnapshot(t.run.snap)
	subs := make([]chan orchestrator.Snapshot, 0, len(t.run.subs))
	for c := range t.run.subs {
		subs = append(subs, c)
	}
	t.run.mu.Unlock()

	_ = t.hub.store.SaveProgress(t.hub.baseCtx, t.id, snap)
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

func (t *tracker) finish(ph orchestrator.Phase) {
	t.update(func(s *orchestrator.Snapshot) { s.Phase = ph })
	t.run.mu.Lock()
	for c := range t.run.subs {
		delete(t.run.subs, c)
		close(c)
	}
	t.run.mu.Unlock()
	t.hub.mu.Lock()
	delete(t.hub.runs, t.id)
	t.hub.mu.Unlock()
}
```

- [ ] **Step 4: Запустить — зелено**

Run: `go test ./internal/httpapi/ -run TestHub -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/progress.go internal/httpapi/progress_test.go
git commit -m "feat(httpapi): Hub + tracker — снимок прогресса, персист, рассылка"
```

---

### Task 5: Runner использует Hub

**Files:**
- Modify: `internal/httpapi/runner.go`

Юнит-тест не добавляем: `BackgroundRunner` завязан на `*store.Store` (нужен реальный Postgres), отдельного харнесса для него нет. Покрытие — через build + интеграцию + тесты Hub/SSE. Верификация шага — компиляция и зелёный `go test ./...`.

- [ ] **Step 1: Добавить поле и параметр** — в `internal/httpapi/runner.go`. В структуру:

```go
type BackgroundRunner struct {
	store      *store.Store
	orch       *orchestrator.Orchestrator
	baseCtx    context.Context
	runTimeout time.Duration
	logger     *slog.Logger
	hub        *Hub
	wg         chan struct{}
}
```

И конструктор:

```go
func NewRunner(baseCtx context.Context, st *store.Store, orch *orchestrator.Orchestrator, timeout time.Duration, logger *slog.Logger, hub *Hub) *BackgroundRunner {
	return &BackgroundRunner{
		store: st, orch: orch, baseCtx: baseCtx,
		runTimeout: timeout, logger: logger, hub: hub,
		wg: make(chan struct{}, 64),
	}
}
```

- [ ] **Step 2: Прокинуть tracker в прогон** — переписать тело горутины в `Start`:

```go
func (r *BackgroundRunner) Start(id string, b agents.Brief) {
	r.wg <- struct{}{}
	go func() {
		defer func() { <-r.wg }()
		ctx, cancel := context.WithTimeout(r.baseCtx, r.runTimeout)
		defer cancel()

		tr := r.hub.Tracker(id)
		if err := r.store.MarkRunning(ctx, id); err != nil {
			r.logger.Error("mark running", "id", id, "err", err)
			tr.Failed()
			return
		}
		res, err := r.orch.Run(ctx, b, tr)
		if err != nil {
			r.logger.Error("run failed", "id", id, "err", err)
			_ = r.store.Fail(context.WithoutCancel(ctx), id, err.Error())
			tr.Failed()
			return
		}
		if err := r.store.Complete(context.WithoutCancel(ctx), id, res); err != nil {
			r.logger.Error("complete", "id", id, "err", err)
			tr.Failed()
			return
		}
		tr.Done()
		r.logger.Info("campaign done", "id", id, "cost_usd", res.CostUSD)
	}()
}
```

- [ ] **Step 3: Проверить компиляцию (main.go ещё не обновлён — пакет httpapi должен собираться)**

Run: `go build ./internal/httpapi/`
Expected: успех (ошибки будут только в `cmd/server`, его чиним в Task 7).

- [ ] **Step 4: Commit**

```bash
git add internal/httpapi/runner.go
git commit -m "feat(httpapi): runner прокидывает tracker прогресса в прогон"
```

---

### Task 6: SSE-эндпоинт

**Files:**
- Modify: `internal/httpapi/api.go` (`API`, `New`, `Handler`, новый хендлер + хелпер)
- Test: `internal/httpapi/api_test.go` (обновить вызовы `New`, добавить SSE-тесты)

- [ ] **Step 1: Обновить существующие вызовы `New` в `api_test.go`** — найти все `New(repo, runner, <N>)` и `New(<repo>, <runner>, <N>)` и вставить третьим аргументом `nil` (Subscriber не нужен этим тестам): `New(repo, runner, nil, <N>)`. Проверить grep'ом:

Run: `grep -n "New(" internal/httpapi/api_test.go`
Обновить каждую строку конструктора API.

- [ ] **Step 2: Failing-тесты SSE** — добавить в `internal/httpapi/api_test.go`:

```go
// fakeSub — подписчик для SSE-тестов.
type fakeSub struct {
	snap orchestrator.Snapshot
	ch   chan orchestrator.Snapshot
}

func (f *fakeSub) Subscribe(string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func()) {
	return f.snap, f.ch, func() {}
}

func TestCampaignEventsNotFound(t *testing.T) {
	repo := &mockRepo{}
	api := New(repo, &mockRunner{called: make(chan string, 1)},
		&fakeSub{ch: make(chan orchestrator.Snapshot)}, 1000)
	req := httptest.NewRequest("GET", "/api/campaigns/nope/events", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", w.Code)
	}
}

func TestCampaignEventsStream(t *testing.T) {
	repo := &mockRepo{campaigns: map[string]*store.Campaign{"camp-1": {ID: "camp-1", Status: "running"}}}
	ch := make(chan orchestrator.Snapshot, 4)
	sub := &fakeSub{snap: orchestrator.Snapshot{Phase: orchestrator.PhaseStrategizing, Percent: 5}, ch: ch}
	api := New(repo, &mockRunner{called: make(chan string, 1)}, sub, 1000)
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/campaigns/camp-1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	ch <- orchestrator.Snapshot{Phase: orchestrator.PhaseProducing, Percent: 50}
	close(ch)

	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if sc.Text() == "event: done" {
			break
		}
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
```

Добавить в импорты `api_test.go`: `"bufio"`, `"strings"`, и `"github.com/977ADAM/marketing-agents/internal/orchestrator"`.

- [ ] **Step 3: Запустить — не компилируется**

Run: `go test ./internal/httpapi/ -run TestCampaignEvents -v`
Expected: ошибка компиляции (`Subscriber`, поле `sub`, новая сигнатура `New`, хендлер).

- [ ] **Step 4: Расширить `API` и `New`** — в `internal/httpapi/api.go`. Добавить интерфейс и поле:

```go
// Subscriber — источник снимков прогресса для SSE.
type Subscriber interface {
	Subscribe(id string) (orchestrator.Snapshot, <-chan orchestrator.Snapshot, func())
}

type API struct {
	repo    Repo
	runner  Runner
	sub     Subscriber
	limiter *rate.Limiter
}

func New(repo Repo, runner Runner, sub Subscriber, ratePerMin int) *API {
	lim := rate.NewLimiter(rate.Limit(float64(ratePerMin)/60.0), ratePerMin)
	return &API{repo: repo, runner: runner, sub: sub, limiter: lim}
}
```

Добавить импорты в `api.go`: `"time"` и `"github.com/977ADAM/marketing-agents/internal/orchestrator"`.

- [ ] **Step 5: Зарегистрировать роут** — в `Handler()` добавить после `GET /api/campaigns/{id}`:

```go
	mux.HandleFunc("GET /api/campaigns/{id}/events", a.campaignEvents)
```

- [ ] **Step 6: Хендлер и хелпер** — добавить в `api.go`:

```go
func (a *API) campaignEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.repo.Get(r.Context(), id); err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load campaign")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	snap, ch, cancel := a.sub.Subscribe(id)
	defer cancel()
	writeSSE(w, "", snap)
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	last := snap
	for {
		select {
		case <-r.Context().Done():
			return
		case s, ok := <-ch:
			if !ok {
				writeSSE(w, "done", last)
				flusher.Flush()
				return
			}
			last = s
			writeSSE(w, "", s)
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, snap orchestrator.Snapshot) {
	b, _ := json.Marshal(snap)
	if event != "" {
		_, _ = w.Write([]byte("event: " + event + "\n"))
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
}
```

- [ ] **Step 7: Запустить SSE-тесты — зелено**

Run: `go test ./internal/httpapi/ -run TestCampaignEvents -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/httpapi/api.go internal/httpapi/api_test.go
git commit -m "feat(httpapi): SSE-эндпоинт GET /api/campaigns/{id}/events"
```

---

### Task 7: Сборка приложения (main.go)

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Создать Hub и прокинуть в runner/api** — в `cmd/server/main.go` заменить блок создания runner/api:

```go
	hub := httpapi.NewHub(baseCtx, st)
	runner := httpapi.NewRunner(baseCtx, st, orch, cfg.RunTimeout, logger, hub)
	api := httpapi.New(st, runner, hub, cfg.RateLimitPerMin)
```

(строки `runner := httpapi.NewRunner(...)` и `api := httpapi.New(...)` — заменяются; добавляется строка `hub := ...` перед ними)

- [ ] **Step 2: Собрать весь проект**

Run: `go build ./...`
Expected: успех, без ошибок.

- [ ] **Step 3: Прогнать все Go-тесты**

Run: `go test ./...`
Expected: PASS во всех пакетах.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): подключить Hub прогресса к runner и API"
```

---

### Task 8: Фронт — типы и хук `useCampaignProgress`

**Files:**
- Modify: `frontend/src/api/client.ts` (типы + `eventsUrl`)
- Create: `frontend/src/hooks/useCampaignProgress.ts`
- Test: `frontend/src/hooks/useCampaignProgress.test.tsx`

- [ ] **Step 1: Типы и URL** — в `frontend/src/api/client.ts`. Добавить после `Deliverable`:

```ts
export type Phase = 'strategizing' | 'producing' | 'done' | 'failed'
export type TopicState = 'pending' | 'writing' | 'reviewing' | 'revising' | 'done'
export interface TopicProgress {
  index: number
  title: string
  state: TopicState
  iter?: number
  score?: number
}
export interface Snapshot {
  phase: Phase
  topics: TopicProgress[]
  topic_total: number
  topics_done: number
  percent: number
}
```

В интерфейс `Campaign` добавить поле:

```ts
  progress?: Snapshot
```

И экспорт URL событий (после строки `const API = ...`):

```ts
export function eventsUrl(id: string): string {
  return `${API}/campaigns/${id}/events`
}
```

- [ ] **Step 2: Failing-тест хука** — создать `frontend/src/hooks/useCampaignProgress.test.tsx`:

```tsx
import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useCampaignProgress } from './useCampaignProgress'

class MockEventSource {
  static last: MockEventSource
  url: string
  onmessage: ((e: MessageEvent) => void) | null = null
  listeners: Record<string, (e: MessageEvent) => void> = {}
  closed = false
  constructor(url: string) {
    this.url = url
    MockEventSource.last = this
  }
  addEventListener(type: string, cb: (e: MessageEvent) => void) {
    this.listeners[type] = cb
  }
  close() {
    this.closed = true
  }
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent)
  }
  emitEvent(type: string, data: unknown) {
    this.listeners[type]?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

beforeEach(() => {
  vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
})

describe('useCampaignProgress', () => {
  it('обновляет снимок из onmessage и помечает terminal на done', () => {
    const { result } = renderHook(() => useCampaignProgress('c1'))
    expect(result.current.snapshot).toBeNull()

    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [], topic_total: 1, topics_done: 0, percent: 30 })
    })
    expect(result.current.snapshot?.percent).toBe(30)
    expect(result.current.terminal).toBe(false)

    act(() => {
      MockEventSource.last.emitEvent('done', { phase: 'done', topics: [], topic_total: 1, topics_done: 1, percent: 100 })
    })
    expect(result.current.terminal).toBe(true)
    expect(result.current.snapshot?.percent).toBe(100)
    expect(MockEventSource.last.closed).toBe(true)
  })
})
```

- [ ] **Step 3: Запустить — упадёт (хук не существует)**

Run: `cd frontend && npx vitest run src/hooks/useCampaignProgress.test.tsx`
Expected: FAIL (модуль не найден).

- [ ] **Step 4: Реализация хука** — создать `frontend/src/hooks/useCampaignProgress.ts`:

```ts
import { useEffect, useState } from 'react'
import { eventsUrl, type Snapshot } from '../api/client'

export function useCampaignProgress(id: string) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [terminal, setTerminal] = useState(false)

  useEffect(() => {
    setSnapshot(null)
    setTerminal(false)
    const es = new EventSource(eventsUrl(id))

    es.onmessage = (e) => {
      setSnapshot(JSON.parse(e.data) as Snapshot)
    }
    es.addEventListener('done', (e) => {
      setSnapshot(JSON.parse((e as MessageEvent).data) as Snapshot)
      setTerminal(true)
      es.close()
    })

    return () => es.close()
  }, [id])

  return { snapshot, terminal }
}
```

- [ ] **Step 5: Запустить — зелено**

Run: `cd frontend && npx vitest run src/hooks/useCampaignProgress.test.tsx`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/hooks/useCampaignProgress.ts frontend/src/hooks/useCampaignProgress.test.tsx
git commit -m "feat(frontend): типы Snapshot + хук useCampaignProgress (EventSource)"
```

---

### Task 9: Фронт — `ProgressPanel` и интеграция в `CampaignView`

**Files:**
- Create: `frontend/src/components/ProgressPanel.tsx`
- Test: `frontend/src/components/ProgressPanel.test.tsx`
- Modify: `frontend/src/components/CampaignView.tsx`
- Modify: `frontend/src/components/CampaignView.test.tsx`
- Modify: `frontend/src/styles.css`
- Delete: `frontend/src/hooks/useCampaign.ts` (становится мёртвым кодом)

- [ ] **Step 1: Failing-тест `ProgressPanel`** — создать `frontend/src/components/ProgressPanel.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ProgressPanel } from './ProgressPanel'
import type { Snapshot } from '../api/client'

const snap: Snapshot = {
  phase: 'producing',
  percent: 60,
  topic_total: 2,
  topics_done: 1,
  topics: [
    { index: 0, title: 'Тема 1', state: 'done', score: 88 },
    { index: 1, title: 'Тема 2', state: 'reviewing', iter: 2 },
  ],
}

describe('ProgressPanel', () => {
  it('рендерит фазу, темы и состояния', () => {
    render(<ProgressPanel product="Вода" snapshot={snap} />)
    expect(screen.getByText('Вода')).toBeInTheDocument()
    expect(screen.getByText('Генерация статей')).toBeInTheDocument()
    expect(screen.getByText('Тема 1')).toBeInTheDocument()
    expect(screen.getByText(/готово · 88/)).toBeInTheDocument()
    expect(screen.getByText(/на ревью · итер\. 2/)).toBeInTheDocument()
  })

  it('показывает заглушку без снимка', () => {
    render(<ProgressPanel product="Вода" snapshot={null} />)
    expect(screen.getByText('Подключение…')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Запустить — упадёт**

Run: `cd frontend && npx vitest run src/components/ProgressPanel.test.tsx`
Expected: FAIL (модуль не найден).

- [ ] **Step 3: Реализация `ProgressPanel`** — создать `frontend/src/components/ProgressPanel.tsx`:

```tsx
import type { Snapshot, TopicState } from '../api/client'

const PHASE_LABEL: Record<string, string> = {
  strategizing: 'Стратегия',
  producing: 'Генерация статей',
  done: 'Готово',
  failed: 'Ошибка',
}

const TOPIC_LABEL: Record<TopicState, string> = {
  pending: 'в очереди',
  writing: 'пишется',
  reviewing: 'на ревью',
  revising: 'доработка',
  done: 'готово',
}

function topicSuffix(state: TopicState, iter?: number, score?: number): string {
  if ((state === 'reviewing' || state === 'revising') && iter) return ` · итер. ${iter}`
  if (state === 'done' && score != null) return ` · ${score}`
  return ''
}

export function ProgressPanel({ product, snapshot }: { product: string; snapshot: Snapshot | null }) {
  return (
    <div className="progress">
      <h2>{product}</h2>
      <p>{snapshot ? PHASE_LABEL[snapshot.phase] : 'Подключение…'}</p>
      <div className="bar">
        <div className="bar-fill" style={{ width: `${snapshot?.percent ?? 0}%` }} />
      </div>
      <ul className="topics">
        {snapshot?.topics.map((t) => (
          <li key={t.index} className={`topic topic-${t.state}`}>
            <span className="topic-title">{t.title}</span>
            <span className="topic-state">
              {TOPIC_LABEL[t.state]}
              {topicSuffix(t.state, t.iter, t.score)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
```

- [ ] **Step 4: Запустить — зелено**

Run: `cd frontend && npx vitest run src/components/ProgressPanel.test.tsx`
Expected: PASS

- [ ] **Step 5: Минимальный CSS** — добавить в конец `frontend/src/styles.css`:

```css
.bar { height: 8px; background: #e5e7eb; border-radius: 4px; overflow: hidden; margin: 12px 0; }
.bar-fill { height: 100%; background: #2563eb; transition: width .3s ease; }
.topics { list-style: none; padding: 0; margin: 0; }
.topic { display: flex; justify-content: space-between; padding: 6px 0; border-bottom: 1px solid #f0f0f0; }
.topic-done .topic-state { color: #16a34a; }
.topic-state { color: #6b7280; font-size: .9em; }
```

- [ ] **Step 6: Переписать `CampaignView`** — заменить содержимое `frontend/src/components/CampaignView.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { getCampaign, type Campaign } from '../api/client'
import { useCampaignProgress } from '../hooks/useCampaignProgress'
import { ArticleCard } from './ArticleCard'
import { ProgressPanel } from './ProgressPanel'

export function CampaignView() {
  const { id = '' } = useParams()
  const { snapshot, terminal } = useCampaignProgress(id)
  const [campaign, setCampaign] = useState<Campaign | null>(null)

  // первичная загрузка (бриф для заголовка + случай уже завершённой кампании)
  useEffect(() => {
    getCampaign(id).then(setCampaign).catch(() => {})
  }, [id])

  // на терминальном событии — дочитываем финальный результат
  useEffect(() => {
    if (terminal) getCampaign(id).then(setCampaign).catch(() => {})
  }, [terminal, id])

  if (!campaign) return <p className="muted">Загрузка…</p>

  if (campaign.status === 'failed') {
    return (
      <div className="failed">
        <h2>Ошибка</h2>
        <p className="error">{campaign.error}</p>
      </div>
    )
  }

  if (campaign.status === 'done') {
    return (
      <div className="result">
        <h2>{campaign.brief.product}</h2>
        {campaign.strategy && (
          <div className="positioning">
            <h3>Позиционирование</h3>
            <p>{campaign.strategy.positioning}</p>
          </div>
        )}
        {campaign.cost_usd != null && (
          <p className="muted">Стоимость прогона: ${campaign.cost_usd.toFixed(4)}</p>
        )}
        <div className="articles">
          {campaign.deliverables?.map((d, i) => <ArticleCard key={i} d={d} />)}
        </div>
      </div>
    )
  }

  // pending / running — живой прогресс
  return <ProgressPanel product={campaign.brief.product} snapshot={snapshot} />
}
```

- [ ] **Step 7: Переписать тест `CampaignView`** — заменить содержимое `frontend/src/components/CampaignView.test.tsx`:

```tsx
import { render, screen, act, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { CampaignView } from './CampaignView'
import type { Campaign } from '../api/client'

const getCampaign = vi.fn()
vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, getCampaign: (id: string) => getCampaign(id) }
})

class MockEventSource {
  static last: MockEventSource
  onmessage: ((e: MessageEvent) => void) | null = null
  listeners: Record<string, (e: MessageEvent) => void> = {}
  closed = false
  constructor() {
    MockEventSource.last = this
  }
  addEventListener(type: string, cb: (e: MessageEvent) => void) {
    this.listeners[type] = cb
  }
  close() {
    this.closed = true
  }
  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent)
  }
  emitEvent(type: string, data: unknown) {
    this.listeners[type]?.({ data: JSON.stringify(data) } as MessageEvent)
  }
}

const running: Campaign = {
  id: 'x', client_id: 'c', status: 'running',
  brief: { product: 'Вода', goal: '', audience: '', tone: '' },
  created_at: '', updated_at: '',
}

function renderView() {
  render(
    <MemoryRouter initialEntries={['/campaigns/x']}>
      <Routes>
        <Route path="/campaigns/:id" element={<CampaignView />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  getCampaign.mockReset()
  vi.stubGlobal('EventSource', MockEventSource as unknown as typeof EventSource)
})

describe('CampaignView', () => {
  it('показывает прогресс, затем результат после terminal-события', async () => {
    getCampaign.mockResolvedValueOnce(running) // первичная загрузка
    renderView()

    await waitFor(() => expect(screen.getByText('Вода')).toBeInTheDocument())

    act(() => {
      MockEventSource.last.emit({ phase: 'producing', topics: [{ index: 0, title: 'Тема', state: 'writing' }], topic_total: 1, topics_done: 0, percent: 30 })
    })
    expect(screen.getByText('Тема')).toBeInTheDocument()

    // финальный fetch после done
    getCampaign.mockResolvedValueOnce({
      ...running, status: 'done',
      strategy: { positioning: 'Поз', topics: [] },
      cost_usd: 0.01,
      deliverables: [{ topic: 't', title: 'Статья', body: 'b', cta: 'c', review: { score: 90, issues: [], verdict: 'accept' } }],
    })
    act(() => {
      MockEventSource.last.emitEvent('done', { phase: 'done', topics: [], topic_total: 1, topics_done: 1, percent: 100 })
    })

    await waitFor(() => expect(screen.getByText('Статья')).toBeInTheDocument())
    expect(screen.getByText('Поз')).toBeInTheDocument()
  })

  it('рендерит ошибку для failed-кампании', async () => {
    getCampaign.mockResolvedValue({ ...running, status: 'failed', error: 'boom' })
    renderView()
    await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument())
  })
})
```

- [ ] **Step 8: Удалить мёртвый хук**

Run: `rm frontend/src/hooks/useCampaign.ts`
(После рефакторинга `useCampaign` больше нигде не импортируется — проверить: `grep -rn "useCampaign\b" frontend/src` должно дать только `useCampaigns`/`useCampaignProgress`.)

- [ ] **Step 9: Прогнать весь фронт-тест и сборку**

Run: `cd frontend && npm test && npm run build`
Expected: все тесты PASS, сборка успешна (пишет в `internal/web/dist`).

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/ProgressPanel.tsx frontend/src/components/ProgressPanel.test.tsx \
        frontend/src/components/CampaignView.tsx frontend/src/components/CampaignView.test.tsx \
        frontend/src/styles.css
git rm frontend/src/hooks/useCampaign.ts
git commit -m "feat(frontend): ProgressPanel + живой прогресс в CampaignView через SSE"
```

---

### Task 10: Финальная проверка и README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Прогнать всё**

Run: `go build ./... && go test ./... && cd frontend && npm test`
Expected: всё зелёное.

- [ ] **Step 2: Интеграционный тест стора**

Run: `docker compose up -d db && DATABASE_URL=postgres://app:app@localhost:5432/marketing?sslmode=disable go test -tags=integration ./internal/store/`
Expected: PASS.

- [ ] **Step 3: Обновить README** — в `README.md`, в разделе API после строки `GET /api/campaigns/{id}` добавить:

```markdown
- `GET /api/campaigns/{id}/events` — SSE-поток прогресса прогона (по-темный)
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: README — SSE-эндпоинт прогресса"
```

---

## Замечания по реализации

- **Порядок Task 1/2:** Task 1 (стор) использует `orchestrator.Snapshot` — реализовать Task 2 первым.
- **Не коммитить `internal/web/dist/assets/`** — он в `.gitignore`; коммитим только исходники фронта.
- **`EventSource` и basic-auth:** браузер шлёт сессионные креды same-origin, отдельной авторизации в хуке не нужно. В dev-режиме (`npm run dev`) прокси на :8080 должен пропускать `/api/.../events` без буферизации (Vite proxy это делает по умолчанию).
