# Агент подбора тем по спросу (Wordstat) — Implementation Plan

**Спека:** `docs/superpowers/specs/2026-10-03-wordstat-semanticist-design.md`
**Область:** фаза 1 — только backend (без UI), фронт минимально (типы и лейблы)
**Принцип:** каждый таск — с тестом, зелёными `go test ./...`, отдельным коммитом.
Тела функций в плане не дублируются (контракты — в спеке); в шагах указаны файлы, сигнатуры,
что именно проверяет тест и команда проверки.

---

### Task 0: Фикстуры ответов MCP

Нужны, чтобы тесты не жгли квоты и работали без сети. Снимаются с живого MCP
(`dev.coresmart.tech/wordstat-mcp/mcp`, basic-auth — креды только в `backend/.env`, в git не попадают).

- [ ] **Step 1: Снять сырые тела ответов.** По каждому вызову сохранить **сырое тело HTTP**
  (SSE-обёртка `event: message` + `data: {...}`) — так тест проверяет и разбор SSE, и
  `structuredContent`. Файлы в `backend/internal/wordstat/testdata/`:
  - `initialize.sse`, `tools_list.sse`
  - `top_requests_all.sse` — «зимняя резина», `numPhrases: 50`
  - `top_requests_region.sse` — то же с `regions: ["213"]`
  - `top_requests_nodata.sse` — фраза без спроса (`hasData: false`, `totalCount: 0`)
  - `dynamics_monthly.sse` — «зимняя резина», `period: monthly`
  - `regions_named.sse` — «аренда офиса», `regionMode: regions`, `includeNames: true`
  - `error_invalid_argument.sse` — `regions: ["abc"]` (ответ с `isError: true`)
  - `error_unknown_tool.sse` — вызов несуществующего инструмента
- [ ] **Step 2: Проверить, что в файлах нет секретов** (`grep -i "api-key\|wordstat_api_key"`).
- [ ] **Step 3: Commit** — `test(wordstat): зафиксировать ответы Wordstat MCP как фикстуры`.

---

### Task 1: Пакет `internal/wordstat` — клиент MCP

- [ ] **Step 1: Типы и ошибки.** `internal/wordstat/types.go`:
  `Top{Phrase, TotalCount, Requests []PhraseCount, Associations []PhraseCount, HasData, CacheHit, Regions, Devices}`,
  `PhraseCount{Phrase, Count int64}`, `Dynamics{Phrase, Period, FromDate, ToDate, Points []DynamicsPoint}`,
  `DynamicsPoint{Date, Count int64, Share float64}`, `Regions{Phrase, Region, Items []RegionItem}`,
  `RegionItem{RegionID, Name, Count int64, Share, AffinityIndex float64}`.
  `internal/wordstat/errors.go`: sentinels `ErrInvalidArgument`, `ErrQuotaExceeded`, `ErrUnavailable`,
  `ErrInternal` + `Retryable(err) bool`.
- [ ] **Step 2: Failing-тест на клиент.** `internal/wordstat/client_test.go`: `httptest`-сервер
  отдаёт `initialize.sse` → `tools_list.sse` → `top_requests_all.sse`; тест проверяет:
  - клиент вызвал `initialize`, затем `notifications/initialized`, затем `tools/call` с именем
    `top_requests` и аргументами из `TopParams` (включая `regions`/`devices`, если заданы);
  - из SSE извлечён `structuredContent` и разобран в `Top` (числа — `int64`, не строки);
  - `Mcp-Session-Id` из ответа `initialize` уходит в последующие запросы.
- [ ] **Step 3: Реализация** — `internal/wordstat/mcp.go`: `New(Options{URL, User, Pass, HTTPClient, MaxRetries})`,
  `TopRequests/Dynamics/Regions`, разбор SSE (`data:` строки), переиспользование сессии на прогон,
  переподключение при ошибке «session not found» (повторный `initialize`), заголовок
  `Authorization: Basic ...`.
- [ ] **Step 4: Тесты на ошибки** — по фикстурам `error_invalid_argument.sse` (→ `errors.Is(err, ErrInvalidArgument)`,
  `Retryable(err) == false`) и `error_unknown_tool.sse` (→ `ErrInternal`); плюс тест на `hasData=false`
  (`top_requests_nodata.sse`) — это **не ошибка**, `HasData == false`.
- [ ] **Step 5: `fake.go`** — `Fake` с очередями ответов по имени инструмента и счётчиками вызовов
  (по образцу `llm.FakeClient`), чтобы оркестратор тестировался без сети.
- [ ] **Step 6: Проверка** — `cd backend && go test ./internal/wordstat/...`.
- [ ] **Step 7: Commit** — `feat(wordstat): клиент Wordstat MCP с фикстурами`.

---

### Task 2: Конфиг

- [ ] **Step 1: Failing-тест** — `internal/config/config_test.go`: дефолты новых полей.
- [ ] **Step 2: Реализация** — `internal/config/config.go`: `WordstatMCPURL`, `WordstatMCPUser`,
  `WordstatMCPPass`, `WordstatRegionDefault` (`225`), `WordstatMinVolume` (`300`),
  `WordstatSeasonalityFactor` (`3`), `WordstatMaxCallsPerRun` (`60`), `TopicsMultiplier` (`2`).
  В `backend/.env.example` — те же ключи с комментариями.
- [ ] **Step 3: Проверка** — `go test ./internal/config/...`.
- [ ] **Step 4: Commit** — `feat(config): параметры Wordstat и множитель тем`.

---

### Task 3: Агент `Semanticist`

- [ ] **Step 1: Типы** — `internal/agents/semanticist.go`: `TopicDraft{Title, Goal, Task, Queries []string, Intent}`,
  роли `RoleSeeds = "semanticist_seeds"`, `RoleCluster = "semanticist_cluster"`.
- [ ] **Step 2: Failing-тесты** — `internal/agents/semanticist_test.go` на `llm.FakeClient`:
  `Seeds` возвращает список из брифа; `Cluster` принимает полученные фразы и возвращает драфты;
  **негативный кейс**: модель сослалась на фразу, которой нет во входном списке → ошибка
  `ErrUnknownQuery` (валидация цитат); пустой ответ → ошибка.
- [ ] **Step 3: Реализация** — промпты (сеялки: 10–15 фраз «как люди вводят в поиск»; кластеризация:
  группировка только из переданного списка, обязательные `goal`/`task`, без цифр в ответе),
  разбор JSON, валидация цитат по множеству входных фраз.
- [ ] **Step 4: Проверка** — `go test ./internal/agents/...`.
- [ ] **Step 5: Commit** — `feat(agents): агент семантики — сеялки и кластеризация`.

---

### Task 4: Правила отбора (чистые функции)

- [ ] **Step 1: Failing-тесты** — `internal/orchestrator/select_test.go` (или `internal/semantic/rules_test.go`):
  table-driven на реальных цифрах из фикстур:
  - объём темы берётся из `totalCount` головной фразы, а не из суммы формулировок
    («зимняя резина»: 1 028 481, не 3 487 006);
  - интент-фильтр: `зимняя резина 205 55 16` — не тема; `какую зимнюю резину` — тема;
  - отбор N из 2N: сверху по объёму, ровно N;
  - порог: ниже `WordstatMinVolume` — тема не проходит, если нет сезонной поправки;
  - сезонная поправка: пик/дно ≥ `WordstatSeasonalityFactor` → тема остаётся с флагом `seasonal`;
  - fallback: кандидатов меньше 2N → добираем LLM-темы с `Source = "llm"` и без цифр.
- [ ] **Step 2: Реализация** — `internal/orchestrator/select.go`: `intentOf(phrase)`, `pickTopics(candidates, n, cfg)`,
  `applySeasonality(...)`, `fillFromLLM(...)`.
- [ ] **Step 3: Проверка** — `go test ./internal/orchestrator/...`.
- [ ] **Step 4: Commit** — `feat(orchestrator): отбор тем по спросу с сезонной поправкой`.

---

### Task 5: Этап `researching` в прогоне

- [ ] **Step 1: Модель прогресса** — `internal/orchestrator/progress.go`: `PhaseResearching`,
  методы `Progress.Researching(stage string)`, `Progress.SeedState(i int, state TopicState)`.
  `frontend/src/lib/api/types.ts` и `labels.ts` — значение `researching` (см. Task 7).
- [ ] **Step 2: Failing-тесты** — `internal/orchestrator/orchestrator_test.go`: прогон с `wordstat.Fake`
  и `llm.FakeClient`: порядок фаз (`researching` → `producing` → `done`), в `Strategy.TopicCandidates`
  лежат все 2N, в `Strategy.Topics` — выбранные N, копирайтеры получают именно их; лимит
  `WordstatMaxCallsPerRun` прекращает раскрытие; `hasData=false` → fallback; ошибка источника
  → прогон падает с ошибкой.
- [ ] **Step 3: Реализация** — `internal/orchestrator/orchestrator.go`: `Run` вызывает
  `research()` до `produce()`; `Options` получает `Wordstat wordstat.Source`, `Semanticist`,
  лимиты; `Result.Strategy.TopicCandidates`.
- [ ] **Step 4: Прогресс в httpapi** — `internal/httpapi/progress.go`: обработка `researching`
  (сеялки как `TopicProgress`), `Percent` для новой фазы (`internal/orchestrator/progress.go`).
- [ ] **Step 5: Тесты** — `internal/httpapi/progress_test.go` (SSE-кадр с `researching`).
- [ ] **Step 6: Проверка** — `go test ./...`.
- [ ] **Step 7: Commit** — `feat(orchestrator): этап подбора тем по спросу в прогоне кампании`.

---

### Task 6: API и бриф

- [ ] **Step 1: Бриф** — `internal/agents/types.go`: `Brief.Region`, `Brief.TopicsCount`;
  `internal/httpapi/api.go`: приём и валидация (`Region` — цифры, `TopicsCount` 1..20, дефолты).
- [ ] **Step 2: Failing-тесты** — `internal/httpapi/api_test.go`: `POST /api/campaigns` с `region`
  и `topics_count`; невалидный регион → 400 `validation`; без полей — дефолты.
- [ ] **Step 3: Проверка, что стор не требует миграции** — `internal/store/store_test.go`:
  стратегия с `topic_candidates` сохраняется и читается (JSON-поле `strategy`).
- [ ] **Step 4: Проверка** — `go test ./...`.
- [ ] **Step 5: Commit** — `feat(api): регион и число статей в брифе`.

---

### Task 7: Фронтенд — типы и лейблы

- [ ] **Step 1: Типы** — `frontend/src/lib/api/types.ts`: `TopicCandidate`, `PhraseCount`,
  `Seasonality`, `Phase` + `'researching'`, `Strategy.topic_candidates`.
- [ ] **Step 2: Лейблы** — `labels.ts`: `PHASE_LABELS.researching`, `SEED_LABELS`
  (сеялки/сбор спроса/кластеризация), бейджи источника (`wordstat`/`llm`).
- [ ] **Step 3: Тесты** — `labels.test.ts` и новый `types`-тест (в существующем стиле vitest).
- [ ] **Step 4: Проверка** — `cd frontend && npm test && npm run check`.
- [ ] **Step 5: Commit** — `feat(frontend): типы и лейблы этапа подбора тем`.

---

## Приёмка фазы 1

- [ ] `cd backend && go test ./...` — зелено.
- [ ] `cd frontend && npm test && npm run check && npm run build` — зелено.
- [ ] Ручной прогон на живом MCP: `make run-backend`, затем
  `POST /api/campaigns` с брифом (продукт «зимняя резина», регион 225, `topics_count: 3`) →
  в результате `strategy.topic_candidates` содержит 6 кандидатов с объёмами и запросами,
  `strategy.topics` — 3 выбранных, статьи сгенерированы по ним; в SSE видна фаза `researching`.
- [ ] Один прогон с локальным фейком (тестами) — сеть не нужна.

## Риски и заметки

- **Зависимость от MCP-сервиса:** недоступность → прогон `failed` с понятной ошибкой; молчаливой
  подмены тем выдумкой нет (fallback только при `hasData=false`, то есть при живом сервисе).
- **Квоты:** лимит `WordstatMaxCallsPerRun` считается по фактическим вызовам; в результате
  сохраняем `wordstat_calls` для наглядности (поле в `Strategy`).
- **Промпты** — самое хрупкое место: фиксируем контракт тестами и держим валидацию цитат
  обязательной, чтобы модель не могла «дорисовать» частотности.
