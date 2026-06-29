# Живой прогресс кампании по этапам (SSE)

**Дата:** 2026-06-29
**Статус:** утверждён (брейншторм)
**Область:** `internal/orchestrator`, `internal/httpapi`, `internal/store`, `frontend`

## Проблема

Статус кампании грубый: `pending → running → done/failed`. Фронт опрашивает общий статус,
пользователь не видит, что происходит внутри прогона. Пайплайн же многоэтапный: стратег (1 раз,
выдаёт N тем) → по каждой теме **параллельно** цикл «копирайтер → критик (до `CriticMaxIter`
итераций) → ревизия». Хочется живой видимости: какая стадия идёт и в каком состоянии каждая тема.

## Решение

Транспорт — **SSE** (`GET /api/campaigns/{id}/events`): оркестратор пушит события мгновенно.
Гранулярность — **по-темная**: отдельное состояние каждой темы (`writing` / `reviewing` /
`revising` / `done`) + итерация критика + score. Снимок прогресса хранится **в БД (JSONB) + hub
в памяти**: hub обслуживает живых подписчиков, БД даёт состояние позднему клиенту / при
перезагрузке страницы.

Связь оркестратора с прогрессом — через **интерфейс-эмиттер** (подход A): оркестратор «объявляет»,
что делает, через зависимость `Progress`; реализация (сбор снимка, запись в БД, рассылка SSE)
живёт в слое runner/hub. Оркестратор не знает ни про БД, ни про SSE.

(Отклонены: канал событий — лишняя возня с закрытием/буферизацией в параллельных горутинах;
прямая запись в стор из оркестратора — ломает чистоту слоёв.)

## Архитектура

### 1. Модель прогресса — `internal/orchestrator/progress.go` (новый файл)

```go
type Phase string      // "strategizing" | "producing" | "done" | "failed"
type TopicState string // "pending" | "writing" | "reviewing" | "revising" | "done"

type TopicProgress struct {
    Index int        `json:"index"`
    Title string     `json:"title"`
    State TopicState  `json:"state"`
    Iter  int         `json:"iter,omitempty"`  // 1-based итерация критика
    Score int         `json:"score,omitempty"` // последний/финальный score
}

type Snapshot struct {
    Phase      Phase           `json:"phase"`
    Topics     []TopicProgress `json:"topics"`
    TopicTotal int             `json:"topic_total"`
    TopicsDone int             `json:"topics_done"`
    Percent    int             `json:"percent"`
}

// Progress — оркестратор «объявляет», что делает. Реализация concurrency-safe.
type Progress interface {
    Strategizing()
    TopicsPlanned(titles []string)
    TopicWriting(i int)
    TopicReviewing(i, iter int)
    TopicRevising(i, iter int)
    TopicDone(i, score int)
}

type NopProgress struct{} // no-op дефолт для тестов/nil; реализует Progress пустыми методами
```

Терминальные фазы `done`/`failed` оркестратор **не** выставляет — их ставит runner через
методы реализации (`Done()`/`Failed()`), не входящие в интерфейс `Progress`.

### 2. Оркестратор — `internal/orchestrator/orchestrator.go`

Сигнатура: `Run(ctx, b, p Progress)`. Если `p == nil` — используется `NopProgress{}`.
Вставка вызовов:
- `p.Strategizing()` — перед `strategist.Run`;
- `p.TopicsPlanned(titles)` — после капа `MaxTopics` (titles из `strat.Topics`);
- в `produce` (получает индекс темы `i`): `p.TopicWriting(i)` перед `copywriter.Run`;
  в цикле критика `p.TopicReviewing(i, iter+1)`; перед `copywriter.Revise` —
  `p.TopicRevising(i, iter+1)`; на финале темы `p.TopicDone(i, score)`.

Темы идут параллельно (errgroup) → корректность обеспечивает потокобезопасная реализация эмиттера.
`orchestrator_test.go` обновляется под новую сигнатуру; добавляется тест порядка событий
(см. «Тестирование»).

### 3. Tracker + Hub — `internal/httpapi/progress.go` (новый файл)

`Hub` держит `map[campaignID]*run` под мьютексом; `run` хранит текущий `Snapshot` и набор
подписчиков (каналов `chan orchestrator.Snapshot`).

- `hub.Tracker(id) *tracker` — возвращает реализацию `orchestrator.Progress`, привязанную к
  кампании. Каждый вызов метода: обновляет снимок (под мьютексом run), вычисляет `Percent`,
  пишет снимок в БД (`store.SaveProgress`), рассылает копию снимка подписчикам
  (неблокирующая отправка). Дополнительно методы `Done()` / `Failed()` — для runner: ставят
  терминальную фазу, финально персистят, закрывают каналы подписчиков, удаляют run из map.
- `hub.Subscribe(id) (snap Snapshot, ch <-chan Snapshot, cancel func())` — отдаёт текущий снимок
  + канал будущих + отписку. Если живого run нет (рестарт / кампания уже завершилась) — снимок
  берётся из БД (`store.Get`), `ch` возвращается уже закрытым.

Публикуется **полный снимок** (он мелкий) — без дельт. Отправка подписчикам неблокирующая
(буфер 1 / `select default`), медленный клиент не тормозит прогон.

### 4. Стор — `internal/store`

- Миграция `internal/store/migrations/0002_progress.sql`:
  `ALTER TABLE campaigns ADD COLUMN progress JSONB;`
- `SaveProgress(ctx, id string, snap orchestrator.Snapshot) error` — `UPDATE campaigns SET
  progress=$2, updated_at=now() WHERE id=$1`.
- `Campaign` получает поле `Progress *orchestrator.Snapshot` (`json:"progress,omitempty"`),
  заполняется в `Get` из колонки (NULL → nil). Циклов импорта нет: `store` уже импортирует
  `orchestrator`.

### 5. Runner — `internal/httpapi/runner.go`

`BackgroundRunner` получает `*Hub`. В `Start`: `tr := hub.Tracker(id)`; `orch.Run(ctx, b, tr)`;
успех → `tr.Done()`; ошибка → `tr.Failed()` (частичные состояния тем остаются в БД).
`MarkRunning`/`Complete`/`Fail` сохраняются как есть (статус кампании), прогресс — параллельный
слой данных в той же строке.

### 6. SSE-эндпоинт — `internal/httpapi/api.go`

`GET /api/campaigns/{id}/events`, регистрируется в `api.Handler()`. `Hub` кладётся в `API`.
- Нет кампании (`repo.Get` → `ErrNotFound`) → 404.
- Заголовки: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`.
- `snap, ch, cancel := hub.Subscribe(id)`; пишем начальный снимок как `data: {json}\n\n`, флашим
  (`http.Flusher`). Цикл `select`: из `ch` — пишем снимок + флаш; `r.Context().Done()` — клиент
  ушёл, `cancel()` и выход; `ch` закрыт (терминал) — шлём `event: done\ndata: {finalSnapshot}\n\n`
  и выходим.
- Heartbeat: тикер ~25с шлёт комментарий `: ping\n\n` против idle-таймаутов прокси.
- Auth: покрыт глобальным `BasicAuth`-врапом; `EventSource` шлёт сессионные креды (same-origin),
  отдельной авторизации не требуется.

### 7. Фронтенд — `frontend/src`

- `hooks/useCampaignEvents.ts`: открывает `EventSource` на `{base}/api/campaigns/{id}/events`
  (тот же resolver base, что у API-клиента — учитывает `PUBLIC_BASE`), парсит `data` в `Snapshot`,
  кладёт в state. На `event: done` (или фаза `failed`) закрывает поток и делает один финальный
  `GET /api/campaigns/{id}` за полным результатом (deliverables). `onerror` — EventSource
  реконнектится сам; хук помечает «переподключение».
- `components/ProgressPanel.tsx` (новый): шапка с фазой + прогресс-бар (`percent`) + список тем
  с бейджами состояния (`writing` / `reviewing · iter k` / `revising · iter k` / `done · score`).
- `CampaignView.tsx`: пока кампания не терминальна — рендерит `ProgressPanel` по данным
  `useCampaignEvents`; на терминале показывает результат (существующая логика).
- Типы `Snapshot`/`TopicProgress` добавляются в TS-зеркало (`api/client.ts` рядом с прочими).

## Прогресс-формула (настраиваемая)

`strategizing` → 5%; после `TopicsPlanned` → 10%; `producing` → `10 + 85*(TopicsDone/TopicTotal)`
(округление вниз); `done` → 100%. Значения вынесены в константы, легко тюнить.

## Обработка ошибок и крайние случаи

- **Рестарт процесса:** снимок в БД устаревает; `RecoverInterrupted` помечает кампанию `failed`.
  SSE для такого id живого run не находит → `Subscribe` отдаёт сохранённый снимок и закрытый канал
  → клиент получает снимок + `done` и закрывается.
- **Ошибка оркестратора:** `tr.Failed()`, частичные состояния тем сохранены в БД.
- **Разрыв соединения:** отписка через `r.Context().Done()` → `cancel()` убирает канал из run.
- **Late join / перезагрузка вкладки:** `GET /{id}` уже отдаёт `progress`, плюс SSE начинает с
  текущего снимка.

## Тестирование

- **orchestrator** (`orchestrator_test.go`): захватывающая реализация `Progress` проверяет порядок
  событий на fake-LLM: `Strategizing → TopicsPlanned → (по темам) TopicWriting → TopicReviewing →
  [TopicRevising →] TopicDone`. Обновить существующие вызовы `Run` под новую сигнатуру.
- **hub** (`progress_test.go`): подписка до и после событий; поздний подписчик получает актуальный
  снимок; терминал закрывает канал; два подписчика получают одинаковые снимки; неблокирующая
  отправка не виснет на «медленном» подписчике.
- **SSE-хендлер** (`api_test.go`): `httptest`, читаем поток, проверяем кадры (начальный снимок,
  обновления, финальный `done`); 404 на несуществующий id.
- **store** (integration, `store_test.go`): round-trip `SaveProgress` → `Get` возвращает снимок;
  NULL `progress` → `nil`.
- **frontend**: `useCampaignEvents` обновляется от мок-`EventSource`; `ProgressPanel` рендерит все
  состояния тем; терминальное событие триггерит финальный `GET`.

Обычный `go test ./...` остаётся зелёным; интеграционный тест — под `-tags=integration`.

## Вне области (YAGNI)

- Отмена кампании (отдельная фича).
- Дельта-события / сжатие трафика (снимок мелкий).
- Persisted event log / реплей полной истории событий (храним только последний снимок).
- WebSocket / двунаправленность (SSE достаточно).

## Рабочий процесс

Фичу делаем на отдельной ветке (например, `feat/live-progress`): миграция + стор → модель
прогресса + оркестратор → hub + runner → SSE-эндпоинт → фронтенд, с тестами на каждом слое.
Затем merge в `main`.
