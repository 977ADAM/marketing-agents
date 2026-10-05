# Раскладка §5: домен, порты, адаптеры — Implementation Plan

**Дата:** 2026-10-05
**Область:** `backend/internal`, `backend/cmd/server`
**Принцип:** каждый шаг — отдельным коммитом с зелёным `make verify`; поведение не
меняется, меняется только раскладка и направление зависимостей.

## Цель

Домен в центре, адаптеры снаружи, зависимости текут внутрь:

```
backend/internal/
  campaign/     campaign.go (Brief, Topic, Strategy, Article, Review, Deliverable)
                store.go    (порт Store), service.go, handler.go
  review/       review.go   (TextToReview, CheckScore, TextReport, Request, Result)
                store.go    (порт Store), service.go, handler.go
  topic/        topic.go    (Briefing, TopicDraft, TopicCandidate, PhraseCount,
                             Seasonality, RegionShare, источники)
                source.go   (порт Source — спрос из Wordstat), service.go
  score/        градации оценок для UI (общий kernel для campaign и review)
  run/          runner.go (async-жизненный цикл), hub.go (прогресс и подписки SSE)
  sqlite/       sqlite.go, campaign.go, review.go, trace.go — реализации портов
  http/         server.go, campaign.go, review.go, trajectory.go — транспорт
  llm/          адаптер DeepSeek (промпты и роли переезжают сюда из agents)
  wordstat/     адаптер Wordstat MCP
  trace/        журнал событий прогона (leaf-пакет без зависимостей)
  mock/         тест-двойники, вне пакетов-продюсеров
  config/
backend/cmd/server/main.go — composition root: единственное место сборки графа
```

Правила, по которым это делается (сверено с первоисточниками):

- интерфейсы (порты) объявляются у потребителя, не у реализации
  ([Go Code Review Comments](https://go.dev/wiki/CodeReviewComments#interfaces));
- адаптеры импортируют домен, домен не импортирует адаптеры
  ([Cockburn, ports & adapters](https://alistair.cockburn.us/hexagonal-architecture/));
- пакеты называются по зависимости, файлы — по сущности: никаких `platform/`,
  `repository.go`, `handler.go`
  ([Ben Johnson, Standard Package Layout](https://github.com/fwojciec/jira4claude/tree/main/.claude/skills/go-standard-package-layout));
- тест-двойники не живут в пакетах-продюсерах (там же).

## Шаги

### Шаг 1. Домен: campaign, topic, review, score

Типы уезжают из `agents`/`orchestrator` в доменные пакеты, поведение не трогаем.

- `internal/campaign/campaign.go`: `Brief`, `Topic`, `Strategy`, `Article`,
  `Review` (выход критика), `Deliverable`.
- `internal/topic/topic.go`: `Briefing` (то, что подбору тем нужно от брифа: продукт,
  цель, ЦА, тон), `TopicDraft`, `TopicCandidate`, `PhraseCount`, `Seasonality`,
  `RegionShare`, `SourceWordstat`/`SourceLLM`.
- `internal/review/review.go`: `TextToReview`, `CheckScore`, `TextReport`,
  `PassThreshold`, `Request`, `Result` (бывшие `ReviewRequest`/`ReviewResult`).
- `internal/score/score.go`: `Grade(score)` и градации — общий kernel, потому что
  градация нужна и критику (campaign), и проверке текстов (review).

**Развязка цикла.** Наивная раскладка даёт `campaign ↔ topic`: `Strategy` содержит
`[]TopicCandidate`, а подбору тем нужен бриф. Поэтому у `topic` свой вход
`Briefing`, а `campaign` конвертирует в него `Brief` — стрелка только
`campaign → topic`.

**Проверка:** `go build ./...`, `go vet ./...`, `go test ./...`, `make verify`;
поведение и набор тестов не меняются.

### Шаг 2. Порты — к потребителям

- `topic.Source` вместо `wordstat.Source` (интерфейс объявляется там, где
  потребляется; DTO спроса — типы `topic`), `wordstat` реализует его;
- `campaign.Store`, `review.Store` вместо `httpapi.Repo` (DTO — типы домена, а не
  `store`), `campaign.Topics` — порт подбора тем, который реализует `topic`;
- реализации возвращают конкретные типы.

### Шаг 3. store → sqlite

`internal/store` → `internal/sqlite`, файлы по сущностям (`sqlite.go`,
`campaign.go`, `review.go`, `trace.go`), импорт `orchestrator` уходит: запись/чтение
описываются структурами домена. Миграции остаются внешними (`backend/migrations` +
сервис `migrate`), Go-кода миграций в проекте нет.

**Статус: сделан целиком.** Часть 1 (`3c138fa`): пакет и файлы переименованы.
Часть 2: один тип `Store` разделён на `Campaigns`, `Reviews`, `Events` (общий
`*sql.DB`, конструкторы `NewCampaigns`/`NewReviews`/`NewEvents`),
`RecoverInterrupted` — функция пакета, `Open`/`New`/`Close` убраны (в composition
root остаётся `OpenDB` + `*sql.DB`), в порту проверок суффикс `Review` заменён на
`Check` (одна реализация обслуживает оба порта, а одноимённые методы в Go
несовместимы), тесты разложены по сущностям (`campaign_test.go`, `review_test.go`,
`trace_test.go`, `schema_test.go`, `sqlite_test.go`).

### Шаг 4. run: async-прогоны и hub

`httpapi/runner.go` и `httpapi/progress.go` → `internal/run`; типы прогресса
(`Phase`, `Snapshot`, `TopicProgress`, `Progress`) переезжают туда же, `httpapi`
перестаёт владеть жизненным циклом прогонов.

**Статус: сделано, с уточнением.** Типы прогресса — в `internal/run` (`e2073de`,
лист без зависимостей). Раннер и hub — в `internal/runner`, а не в `run`: доменные
записи (`campaign.Record.Progress`, `review.Record.Progress`) ссылаются на
`run.Snapshot`, поэтому `run → campaign/review` дало бы цикл. `runner` же зависит от
`campaign`, `review`, `run`, `orchestrator` и `trace`, и его никто, кроме
composition root, не импортирует.

### Шаг 5. agents → topic/campaign/review/llm

Промпты и вызовы модели разъезжаются по доменам, `llm` остаётся адаптером
(клиент, ретраи, роли, декоратор трассы). Пакет `agents` исчезает.

### Шаг 6. httpapi → http

`internal/httpapi` → `internal/http`: `server.go` (роуты, middleware, auth),
`campaign.go`, `review.go`, `trajectory.go` — тонкий транспорт поверх портов.

**Статус: сделано.** Имя `http` конфликтует с `net/http` в файлах, которым нужны
оба, поэтому в `main` и тестах пакет подключён как `apihttp` (это осознанная плата
за имя по §5).

### Шаг 7. Моки и config

`llm/fake.go` и `wordstat/fake.go` → `internal/mock`; `config` перестаёт
импортировать доменный пакет (`trace`), режим трассы парсится в самом конфиге.

## Приёмка

- `make verify` зелёный на каждом шаге;
- направление зависимостей: `http → campaign/review/run → topic/score`,
  `sqlite → campaign/review/trace`, домен не импортирует адаптеры;
- `go list -deps` не показывает циклов, `internal/*/fake.go` вне прод-пакетов;
- тесты остаются black-box (`package <pkg>_test`) — переезд пакетов их почти не
  задевает (см. коммит `3591bd6`).

## Риски

- **Объём шага 5.** Растворение `agents` — самая крупная правка; делать после того,
  как домен и порты уже на месте, иначе конфликты правок на каждом шаге.
- **DTO спроса.** Если `topic` объявит свои типы спроса, `wordstat` начнёт
  маппить ответы MCP в них — это правка адаптера и его фикстур, но именно она
  убирает импорт адаптера из домена.
- **Двойные имена на переходе.** Чтобы каждый шаг оставался зелёным, допустимы
  временные алиасы в старых пакетах; они удаляются на шагах 3–5, к финалу в
  `internal` не остаётся ни одного.
