# Feature Layers and Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Перестроить сервис по фичам и слоям, исправив перечисленные в спецификации проблемы без смены технологического стека.

**Architecture:** Core содержит общие контракты и инфраструктуру, features — domain/service/repository/transport. Application runner исполняет задачи через контракты; main собирает граф. Перенос пакетов отделён от изменения поведения, чтобы на каждом этапе проект собирался.

**Tech Stack:** Go 1.25, MariaDB 11.4, GORM, dbmate, DeepSeek, Wordstat MCP, SvelteKit 3/Svelte 5, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-05-feature-layers-design.md`

## Global Constraints

- Сохраняются существующие `/api/*` и `/healthz`, JSON имена существующих полей и форма массивов истории.
- Core не импортирует features/application; domain не импортирует GORM/HTTP/service/transport; HTTP не вызывает repository напрямую.
- Проверенные цитаты Wordstat и реальные частотности сохраняются; при наличии спроса автоматического добора выдуманных тем нет.
- Ноль итераций критика означает сохранение статьи с явным отсутствием проверки.
- JSON: 2 MiB; проверка: максимум 20 текстов, каждый до 512 KiB; стартовый предел параллелизма внутри прогона 4.
- RATE_LIMIT_PER_MIN=0 отключает rate limiter. Перегрузка admission: HTTP 503, code `busy`.
- Idempotency-Key: повтор прежнего входа возвращает исходный id, другой вход с тем же ключом — 409.
- Миграции внешние; DB-тесты нельзя считать прошедшими при skip. Платные API для регрессионных тестов не используются.
- Локальная правка пустых строк в main не удаляется как отдельное исправление. Секреты из .env не читаются в вывод и не коммитятся.
- Работать последовательно; после каждого проверяемого этапа — отдельный commit только относящихся к нему файлов.

## Review Focus

- Два одновременных POST с одним ключом: одна запись и одно выполнение; проверяет task 9.
- Поздний progress после terminal: запись и SSE не возвращаются в running; проверяет task 8.
- Старый worker после истечения lease: не завершает задачу нового владельца; проверяет task 10.
- Старые записи без входных текстов/checkpoints: читаются, но resume возвращает понятную ошибку; проверяет task 11.
- Поздний ответ загрузки/GET после изменения формы: не меняет новую строку или более свежий результат; проверяет task 14.

## Карта переноса

| Текущий путь под backend/internal | Новый путь | Ответственность |
|---|---|---|
| config | core/config | чтение и проверка env |
| core/corelogger, sloglogger | core/logger, adapters/logger/slog | контракт и конкретный логгер |
| core/corellm, llm/openai.go | core/llm, adapters/llm/deepseek/client.go | контракт модели и HTTP-клиент |
| run, score | core/run, core/score | общие чистые типы |
| campaign/*.go | features/campaign/domain, service | сущности и агенты |
| review/*.go | features/review/domain, service | сущности, агенты, use cases |
| topic/*.go, orchestrator/research.go | features/topic/domain, service | спрос и research |
| orchestrator/orchestrator.go, review.go | features/campaign/service/workflow.go, features/review/service/workflow.go | два отдельных workflow |
| repository/{campaign,review,trace}.go | features/{campaign,review,trace}/repository/mariadb | отдельные модели и запросы |
| repository/schema.go | core/repository/mariadb/schema.go | общая проверка схемы |
| trace/*.go | features/trace/domain, service | контракты и recorder |
| llm/tracing.go, wordstat/tracing.go | adapters/tracing/{llm,wordstat}.go | декораторы |
| wordstat | features/topic/source/wordstat | MCP-адаптер |
| http/*.go | features/*/transport/http, core/transport/http | обработчики и общая инфраструктура |
| runner | application/runner | admission, исполнение, hub, shutdown |
| mock, testdb | testkit/mock, testkit/testdb | тестовые двойники и DB |

Соседние тесты и export_test.go перемещаются вместе с кодом; public package aliases применяются только на переходе.

### Task 0: Baseline и среда

**Files:** существующие backend/frontend; отдельного product-кода нет.
**Interfaces:** существующий API — baseline будущих изменений.

- [x] Прочитать spec/plan и обнаруженные AGENTS.md; проверить status, current branch и worktree. Создание изоляции — через using-git-worktrees при начале исполнения.
- [x] Проверить Go/Node/npm/Docker. Запустить `cd backend && go build ./...`, `go vet ./...`, `go test ./...`; сохранить список skips.
- [x] Запустить `cd frontend && npm test`, `npm run check`, `npm run build` с установленными зависимостями.
- [x] На доступной MariaDB выполнить `make test-backend`; отдельно зафиксировать baseline падения, не относя их к миграции. Без БД продолжить независимые задачи, DB verification оставить открытой.

### Task 1: Общие контракты, domain и адаптеры

**Files:** пути core/domain/adapters/testkit из карты; `backend/cmd/server/main.go`, все внутренние imports, `backend/tests/{e2e,live}`.
**Interfaces:** сохраняются Client.Complete, Logger, Snapshot, Brief/Strategy/Article, review.Request/Result и topic.Briefing без изменения JSON.

- [x] Переместить чистые core и domain типы, а затем клиентов и mocks; переписать imports и package declarations вместе с существующими тестами.
- [x] Переместить промпты/агентов в service, оставив workflow временно в orchestrator; исключить domain → service imports.
- [x] Запустить `cd backend && go build ./... && go vet ./... && go test ./...`; ожидается baseline без новых ошибок.
- [x] Проверить внутренние зависимости через `go list -json ./internal/...`: core не зависит от features, domain — от adapters/transport/repository.
- [x] Commit: `refactor: separate core contracts and feature domains`.

### Task 2: Репозитории по фичам

**Files:** `features/{campaign,review,trace}/repository/mariadb/*.go`, `core/repository/mariadb/schema.go`, `testkit/testdb`, main, существующие repository tests.
**Interfaces:** конкретные `Campaigns`, `Reviews`, `Events` получают `*gorm.DB`; порты чтения/записи объявляются в потребляющих service. Общий pool остаётся единственным владельцем подключения.

- [x] Перенести модели и запросы по сущностям, сохранив транзакцию Complete; генератор UUID и UTC clock разместить в небольшом core-пакете, без зависимости repository между фичами.
- [x] Переместить CheckSchema в core repository, recovery временно оставить application-функцией над существующими портами.
- [x] Выполнить существующие repository и e2e tests на MariaDB; сравнить порядок истории/статей и round-trip JSON с baseline.
- [x] Commit: `refactor: move repositories into feature layers`.

### Task 3: Feature services, transports и composition root

**Files:** `features/{campaign,review}/service/{service,workflow,ports}.go`, `features/topic/service/research.go`, `features/trace/service/query.go`, feature transport handlers, `core/transport/http/{server,middleware}`, `application/runner`, main.
**Interfaces:** feature transport потребляет `Create(ctx, input) (recordID string, err error)`, `Get(ctx, id)`, `List(ctx, page)`. Topic workflow: `Research(ctx, Briefing, Options, Progress) (ResearchResult, Usage, error)`. Runner исполняет переданный `Task`, а не создаёт Orchestrator.

- [x] Написать service tests: Create сохраняет валидный input и отправляет одну задачу; ошибка repository не запускает задачу; transport tests проверяют прежние status/body/routes.
- [x] Запустить новые тесты до выделения сервисов; ожидается отсутствие новых конструкторов/интерфейсов.
- [x] Разделить campaign/review workflows; research принимает topic.Briefing и возвращает topic.ResearchResult, campaign преобразует его в Strategy. Transport оставляет decoding/status/headers, use cases уходят в service.
- [x] Выделить общий HTTP server и middleware без нового API version router; main явно собирает фичи. Сохранить basic auth и streaming proxy.
- [x] Запустить все Go-тесты и frontend check; удалить orchestrator/http/repository и временные aliases после смены всех imports.
- [x] Commit: `refactor: assemble feature services and HTTP transports`.

### Task 4: Контекст и валидация агентов

**Files:** `features/campaign/service/{critic,copywriter,workflow}.go`, `features/review/service/checkers.go`, domain модели результатов; frontend API types и ArticleCard.
**Interfaces:** `Revise(ctx, Brief, Strategy, Topic, Article, Review) (Article, Usage, error)`; critic получает те же Brief/Strategy/Topic. `Deliverable.Review` становится nullable с прежним именем JSON поля.

- [x] Добавить `TestRevisionPreservesBrief`, `TestCriticReceivesGoalAndProduct`, `TestZeroCriticIterationsPreservesArticle`, `TestMissingScoreRejected`, `TestInvalidVerdictRejected`, `TestWhitespaceArticleRejected`. Проверить отсутствие score отдельно от score=0 и nil review отдельно от нулевой оценки.
- [x] Запустить `go test ./internal/features/campaign/... ./internal/features/review/...`; сначала подтвердить текущие дефекты воспроизведением через существующие моки.
- [x] Ввести внутренние response DTO с `*int` score, валидировать обязательные поля/verdict; сохранить исходный бриф при critique/revise. Ноль итераций возвращает написанную статью без review.
- [x] Обновить UI: nil review отображается как «Не проверено», старые результаты с review читаются без изменения.
- [x] Повторить целевые Go-тесты и `npm run check`; commit `fix: validate agent responses and preserve campaign context`.

### Task 5: Входные лимиты и число тем

**Files:** core config, feature create services/HTTP handlers, topic/campaign workflows, frontend create form, Node API proxy.
**Interfaces:** `Limits{MaxJSONBytes, MaxTexts, MaxTextBytes, MaxTopics, ParallelTexts}`; validation выполняется до repository и LLM. `TopicsCount=0` разрешает default; ненулевое значение строго <= MaxTopics.

- [x] Добавить boundary tests: JSON 2 MiB+1 → 413, 21 текст → 400, body 512 KiB+1 → 400; repository/LLM call counts равны нулю. Для HTTP decoder запретить лишнее JSON значение после первого.
- [x] Добавить `TestTopicsCountWithoutWordstat`, `TestRequestedTopicsOverConfiguredCapRejected`, `TestFewerTopicsHasWarning`; проверить frontend/API одинаковый предел.
- [x] Реализовать Limits defaults 2 MiB/20/512 KiB/parallel=4, передать count стратегу, убрать молчаливое обрезание принятого count и сохранить предупреждение research.
- [x] Node proxy проверяет Content-Length и считает фактические байты потока; oversized upload отклоняется до arrayBuffer/FormData. Backend независимо ограничивает вход.
- [x] Запустить feature HTTP/service tests и frontend tests/check; commit `fix: enforce request budgets and topic counts`.

### Task 6: MCP-сессии, research coverage и бюджет

**Files:** `features/topic/source/wordstat/{mcp,errors}.go`, `features/topic/service/research.go`, mocks, MCP fixtures/tests.
**Interfaces:** Budget передаётся через контекст прогона; `ConsumeToolCall(ctx) error` вызывается непосредственно перед каждым tools/call HTTP, включая retries. Инициализация учитывается отдельной метрикой.

- [x] Написать httptest cases: успешный ответ со словом session вызывает один tools/call; потерянная сессия пересоздаётся; поздняя ошибка старой сессии не сбрасывает новую; бюджет исчерпан на retry → дополнительного tool call нет.
- [x] Написать fixture-based coverage test: requests каждой сеялки представлены при ограниченном входе, associations сохраняют отдельное происхождение; неизвестная цитата по-прежнему отклоняется.
- [x] Реализовать сброс только конкретного устаревшего sid и structured-error matching; отделить tool calls/retries/init counters. Отбор фраз сделать round-robin по сеялкам, requests перед associations, с dedup и конечным maxPhrases.
- [x] Запустить `go test ./internal/features/topic/... ./internal/adapters/tracing/...`; реальные внешние API не вызываются.
- [x] Commit: `fix: bound MCP retries and preserve research coverage`.

### Task 7: Конфиг, URI и схема

**Files:** core config/pool/schema, `docker-compose.yml`, Makefile, `.env.example`, testkit и config tests.
**Interfaces:** `BuildDatabaseURL(user, pass, host, port, database string) (string, error)`; `CheckSchema(ctx, db, requiredVersions []string) error`; HTTP limiter с явным disabled mode.

- [x] Написать round-trip tests credentials с пробелом, `+`, `@`, `:`, `/`, `%`; проверить backend и URI для migrate/testdb.
- [x] Написать MariaDB test: только 0001 при требовании 0001+0002 не проходит; все требуемые версии проходят. Rate=0 позволяет POST, rate>0 сохраняет ограничение.
- [x] Строить URI через url.URL/UserPassword; для compose migrate добавить отдельный `backend/cmd/dburl/main.go`, возвращающий URL только в pipe/env миграционного процесса, без логирования. Использовать его из отдельного build stage migrate, сохранив официальный dbmate binary. Makefile получает тестовый адрес тем же кодом без echo секрета.
- [x] RequiredVersions хранить в `backend/migrations/manifest.go` отдельным Go-пакетом, с тестом соответствия SQL файлам; обновлять вместе с каждой миграцией.
- [x] Выполнить config/pool/schema tests и `docker compose config --quiet`; commit `fix: validate database setup and rate limit configuration`.

### Task 8: Admission, упорядоченный progress и shutdown

**Files:** `application/runner/{admission,runner,hub,shutdown}.go`, core/run, feature services/repositories, main; `backend/migrations/0003_progress_revision.sql`.
**Interfaces:** `Reserve(ctx) (Reservation, error)`, `Reservation.Submit(Task) error`, `Reservation.Release()`; `Drain(ctx) error`. Snapshot включает `Revision int64`; repository сохраняет только revision выше текущей, terminal state не допускает отката.

- [x] Написать concurrency tests: capacity=1, второе Reserve сразу ErrBusy; отменённый ctx не резервирует; Release при Create error; shutdown запрещает Submit, истечение grace отменяет worker.
- [x] Написать DB/race tests: revision 2 затем 1 остаётся 2; terminal затем late progress остаётся terminal; slow subscriber/update/finish не вызывают send-on-closed-channel; ошибку SaveProgress видно logger.
- [x] Реализовать неблокирующий admission и task accounting отдельно, per-run сериализацию обновлений, bounded finalization context. State transition и terminal progress сохранять согласованно.
- [x] Добавить defaults: capacity 64, shutdown grace 30s, finalize timeout 5s; ограничение parallel texts=4 выполнить в workflow. Main использует signal.NotifyContext и bounded Drain вместо прежнего семафорного ожидания.
- [x] Запустить `go test -race ./internal/application/runner/...` и DB integration tests; commit `fix: bound background execution and order progress updates`.

### Task 9: Идемпотентное создание

**Files:** campaign/review service и repositories, transport headers, `backend/migrations/0004_idempotency.sql`, frontend request/create modules.
**Interfaces:** `CreateOnce(ctx, clientID, key, inputHash, input) (id string, created bool, err error)`; уникальность `(client_id, run_kind, key)`, conflict — domain.ErrIdempotencyConflict.

- [x] Написать DB concurrency test: два запроса с одним key/hash возвращают один id, created=true только у одного. Другой hash → conflict; повтор уже созданного прогона возвращается и при заполненном admission.
- [x] Hash считать по канонической сериализации валидированного входа; ledger и run создавать одной транзакцией. Резервацию освобождать для duplicate/conflict; только created run отправлять на выполнение.
- [x] Фронт генерирует crypto.randomUUID key для входа и переиспользует его при сетевой ошибке; изменение входа создаёт новый key.
- [x] Прогнать DB tests, HTTP 202/409 tests, frontend retry test; commit `feat: make run creation idempotent`.

### Task 10: Владение задачами и recovery

**Files:** application runner/recovery, feature repositories/services, `backend/migrations/0005_run_leases.sql`, hub subscriber.
**Interfaces:** `AcquireLease(ctx, id, owner, ttl) (attempt int64, acquired bool, err error)`, `RenewLease(ctx, id, owner, attempt) error`; записи checkpoint/terminal требуют тот же owner/attempt. Defaults: lease TTL 30s, heartbeat 10s.

- [x] Написать DB tests с подменяемыми часами: старт второго процесса не меняет чужой живой run; expired lease помечается failed; поздний worker предыдущего attempt не пишет Complete/Fail/checkpoint.
- [x] Реализовать conditional updates и fencing attempt; recovery очищает status и terminal progress только для expired/unowned pending/running, учитывая окно между Create и первым AcquireLease.
- [x] Hub для чужого живого прогона читает status/progress каждые 2s с контекстом SSE и закрывается только по terminal, не выдавая failed при отсутствии локального tracker.
- [x] Прогнать два runner экземпляра в e2e и race tests; commit `fix: recover runs using ownership leases`.

### Task 11: Checkpoints, исходные тексты и resume

**Files:** feature repositories/workflows/domain/transport, `backend/migrations/0006_run_checkpoints.sql`, e2e tests, frontend partial result components.
**Interfaces:** campaign `SaveResearch`, `SaveStrategy`, `SaveDeliverable(position, result)`; review `SaveInputTexts`, `SaveTextReport(position, result)`; все записи fence owner/attempt. `Resume(ctx, id) error` только для failed с сохранённым полным входом.

- [x] Добавить e2e: статья 1 готова, статья 2 падает → первая и research читаются; resume не вызывает готовые этапы повторно и не дублирует результат позиции. Review переживает restart с исходными texts.
- [x] Добавить tests старых records: чтение совместимо; отсутствие checkpoint input → 409 `resume_unavailable`; done/running → 409 `invalid_state`. Повторная resume гонка запускает ровно один attempt.
- [x] Добавить persisted checkpoints с input fingerprint, unique `(run_id, position)` и atomic checkpoint writes. Final Complete подтверждает все обязательные результаты; Fail не удаляет уже сохранённую работу.
- [x] Добавить `POST /api/campaigns/{id}/retry` и reviews аналог; retry использует тот же run id, новый fenced attempt и продолжает историю trace seq. Новый изменённый бриф создаётся отдельным run.
- [x] UI показывает partial deliverables/reports при failed, предупреждения и кнопку «Продолжить» только при resume_available.
- [x] Прогнать migrations/DB/e2e tests; commit `feat: persist run checkpoints and resume failed stages`.

### Task 12: Usage и оценка стоимости

**Files:** core/llm, LLM adapter, feature workflows/domain/repositories, config/env docs, frontend format/result components.
**Interfaces:** `UsageEntry{Model, Role, PromptTokens, CompletionTokens}`, `Prices.Estimate(entries) (amount float64, known bool)`; MODEL_PRICES_JSON задаёт ставки за 1000 prompt/completion tokens по моделям.

- [x] Написать tests двух моделей с различными ставками, parse failure с usage, research failure с usage, частичной проверки. Неизвестная ставка даёт known=false, не фиктивный бесплатный run.
- [x] Собирать usage на каждом вызове и persist инкрементально вместе с checkpoint/attempt; итог исключает повторное суммирование прежних событий. Совместимые старые COST_PER_1K_* используются как явно задокументированная fallback ставка.
- [x] UI пишет «Оценочная стоимость», неизвестную показывает отдельно; failed/partial сохраняют потраченные usage/cost.
- [x] Прогнать workflow/price tests и frontend checks; commit `fix: preserve usage and estimate costs per model`.

### Task 13: Полная и безопасная трасса

**Files:** trace service/repository/domain/transport, adapters/tracing, LLM adapter, runner finalize, `backend/migrations/0007_trace_sequence.sql`, frontend trajectory API/panel.
**Interfaces:** trace recorder `FinishRun(id)` освобождает локальный state; durable sequence allocator не повторяет seq на resume/другом owner. Payload: `{data, truncated}`; pagination `after_seq`, `has_more`, действительный total.

- [x] Написать tests: summary parse error не содержит content; full содержит response/Wordstat results; timeout оставляет итог; review имеет result event; resume продолжает seq; recorder освобождает per-run entries.
- [x] Разделить публичную error и full diagnostics в adapter; truncated payload остаётся валидным JSON. Отдельный bounded context для завершающих trace events не зависит от отменённого worker.
- [x] Durable seq выделять атомарно на run; retention выполнять при старте и каждые 24h с bounded context. Ошибка sink логируется, workflow не падает.
- [x] Добавить cursor pagination ленты; UI загружает следующие события и опрашивает открытую активную трассу раз в 2s, прекращая polling при закрытии/terminal/destroy.
- [x] Прогнать trace/adapter DB/race tests, Vitest trajectory tests; commit `fix: make run traces complete bounded and observable`.

### Task 14: DOCX, история и актуальность UI

**Files:** `features/review/service/docx.go`, review transport, frontend review form/stores/history/run/progress/Modal, feature repositories and list transport.
**Interfaces:** `ExtractDOCX(data []byte, limits ExtractLimits) (Document, error)` с XML limit 4 MiB, text limit 512 KiB и zip upload limit 20 MiB. History cursor — последний seq, сортировка seq DESC; массив ответа сохраняется, continuation через `X-Next-Cursor`.

- [x] Написать tests malformed XML → ошибка без partial success; XML с большим markup, но допустимым текстом проходит; превышение text/XML/upload лимита отклоняется. Сохранить обработку таблиц, tabs и line breaks.
- [x] Написать frontend tests stable text id: удалённая строка не получает upload; старый upload не перезаписывает новый; submit недоступен при loading uploads. Для асинхронных GET latest request wins, destroy игнорирует поздний ответ.
- [x] Реализовать helper upload coordinator, generation counters/AbortController в stores; stable keyed rows; reconnect отображать для reviews так же, как campaigns.
- [x] Добавить history keyset pagination в repository/service/transport; UI «Загрузить ещё». Ошибка JSON stored data возвращает storage error, done без result отображает явную ошибку, не бесконечный progress.
- [x] Добавить regression tests initial 50 + next page без пропуска/дублей и corrupted stored review.
- [x] Прогнать Go DOCX/HTTP/repository tests и `npm test`, `npm run check`, `npm run build`; commit `fix: preserve uploaded inputs and expose complete run history`.

### Task 15: Финальная проверка и документация

**Files:** README, env examples, Makefile, Dockerfiles/compose, specs/plan progress, все изменённые import paths.

- [x] Обновить README по фактическим путям и новым лимитам/checkpoints/leases/идемпотентности/тарифам; удалить устаревшие комментарии package httpapi/sqlite.
- [x] Запустить `make verify` на живой MariaDB; отдельно `cd backend && go test -race ./internal/application/... ./internal/features/trace/...` и concurrency DB tests. Сохранить фактические результаты/skips.
- [x] Проверить миграцию старой базы 0001/0002 до актуальной и чтение старых done/failed данных. Проверить rollback новой миграции только на временной тестовой базе, не на пользовательских данных.
- [x] Проверить composition root и go list: transport зависит от service contracts; core/domain не импортируют наружные слои; старые пакеты/aliases отсутствуют.
- [x] Проверить backend/frontend Docker build и локальный HTTP/SSE smoke с mocked внешними клиентами; убедиться, что ошибки overload/timeout/resume понятны пользователю.
- [ ] Провести review всего diff относительно spec, устранить actionable замечания и повторить только затронутые проверки. Commit документации и сообщить результат, ограничения среды и непроверенные сценарии.

## Исполнение

Рекомендуется native, последовательно в этом чате: большинство задач меняют общие
контракты, их параллельная реализация создаст конфликты. Независимый review в конце
допустим после выбора пользователем метода, с сохранением правил среды о subagents.
План необходимо рассмотреть до начала изменений product-кода.
