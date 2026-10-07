# Чат вместо формы брифа — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Заменить форму брифа чатом: свободный текст → уточнения модели → структурированный `Brief` → обычный запуск кампании, с прогрессом и статьями в той же ленте.

**Architecture:** Новый stateless-эндпоинт `POST /api/briefs/interview` стримит SSE-кадры; модель отвечает прозой и хвостовым JSON с брифом. Сервер ничего не хранит — историю держит клиент. Стрим идёт через ту же цепочку декораторов, что и обычные вызовы, поэтому роль `interviewer` получает скил `campaign-context` из существующей карты. Запуск кампании не меняется.

**Tech Stack:** Go 1.25 (`http.Flusher`, `github.com/sashabaranov/go-openai` streaming), SvelteKit 3 / Svelte 5 (runes, `fetch` + `ReadableStream`), vitest.

**Spec:** `docs/superpowers/specs/2026-10-07-chat-brief-design.md`

## Global Constraints

- **Не меняется:** домен кампании и `Brief`, JSON-контракты девяти агентов, схема БД и миграции, `POST /api/campaigns` и его идемпотентность, страницы `/campaigns/[id]` и `/reviews`, промпты остальных ролей, существующий нестриминговый `Complete`, прокси `frontend/src/routes/api/[...path]/+server.ts`.
- **Эндпоинт:** `POST /api/briefs/interview`, тело `{messages:[{role,content}]}`, `Accept: text/event-stream`. Кадры: `{"type":"delta","text":…}`, `{"type":"brief","brief":{…},"missing":[…],"status":"ready"|"needs_input"}`, `{"type":"error","message":…}`, `{"type":"done"}` — формат `data: {…}\n\n`.
- **Лимиты:** 20 сообщений и 32 КиБ суммарного текста истории (жёстко, на сервере); «не больше трёх уточнений» — только инструкция в промпте.
- **Бриф:** поля `product`, `goal`, `audience`, `tone`, `region`, `topics_count`. Обязательные для `ready` — первые четыре; `missing` и `status` считает **сервис**, а не модель. Хвостовой JSON **сливается по непустым полям** с прежним брифом.
- **Роль `interviewer`** — десятая привязка в `cmd/server/skills.go` → `campaign-context`. Роль **прозаическая**: декоратор скилов подставляет для неё короткую оговорку вместо контрактной (см. Task 2).
- **Ответственность за формат:** интервьюер отвечает прозой, маркер `<<<BRIEF` и JSON — в конце реплики; сервер стримит наружу только текст до маркера.
- **Клиент:** история в `localStorage` под ключом `interview:v1` (сообщения, бриф, id последней кампании). Сервер диалог не хранит.
- **Осознанный пробел:** вызовы интервьюера не попадают в трассу и учёт стоимости (нет `run_id`); в логах — `info` с ролью, длительностью и токенами. Отражается в README.
- **Стиль проекта:** комментарии и тексты по-русски, тесты black-box (`package <pkg>_test`), швы — через `export_test.go`; новых зависимостей и переменных окружения нет.

## Review Focus

1. **Хвост не разобрался** (нет маркера, битый JSON, JSON в код-фенсе) — бриф не меняется, реплика доходит целиком, чат не рвётся (Task 3).
2. **Частичный хвост затирает собранное** — слияние по непустым полям сохраняет уже известные product/goal/audience/tone (Task 3).
3. **Стрим от провайдера вырожденный** (ни одного чанка, EOF без usage, ошибка в середине потока) — накопленный текст не теряется, приходит понятная ошибка (Tasks 1 и 4).
4. **Поток доходит до браузера фрагментами**, а не одним куском в конце (Task 7 и приёмка).
5. **История пустая или переросшая** — `400` с отдельными кодами, клиент после 20 сообщений блокирует ввод (Tasks 3, 4, 6).

## File Structure

- `backend/internal/core/llm/llm.go` — интерфейс `Streamer` рядом с `Client`.
- `backend/internal/adapters/llm/deepseek/stream.go` — стриминговый вызов без `json_object`.
- `backend/internal/adapters/skills/skills.go` — прозаическая оговорка (`Options.Prose`) и проброс стрима.
- `backend/internal/adapters/tracing/llm.go`, `backend/internal/adapters/accounting/llm.go` — проброс стрима.
- `backend/internal/features/brief/domain/{brief.go}` — сообщение, черновик, результат, статусы.
- `backend/internal/features/brief/service/{service.go,ports.go,tail.go,export_test.go}` — промпт, лимиты, разбор хвоста, `missing`/`status`.
- `backend/internal/features/brief/transport/http/handler.go` — SSE-эндпоинт.
- `backend/cmd/server/{skills.go,llm.go,main.go}` — десятая привязка, маршрут, сервис.
- `frontend/src/lib/sse.ts`, `frontend/src/lib/api/briefs.ts`, `frontend/src/lib/stores/interview.ts` — транспорт и состояние.
- `frontend/src/lib/components/{ChatThread.svelte,BriefSummary.svelte}` — лента и сводка брифа.
- `frontend/src/routes/+page.svelte`, `frontend/src/lib/styles/components.css` — страница-чат и стили.
- `README.md` — эндпоинт, роль, ограничения.

---

### Task 1: Стриминговый путь в DeepSeek-адаптере

**Files:**
- Modify: `backend/internal/core/llm/llm.go` (после интерфейса `Client`)
- Create: `backend/internal/adapters/llm/deepseek/stream.go`
- Test: `backend/internal/adapters/llm/deepseek/stream_test.go`

**Interfaces:**
- Consumes: `corellm.Usage`, `corellm.UsageEntry`.
- Produces: `corellm.Streamer` — `CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (Usage, error)`; `(*OpenAIClient).CompleteStream` реализует его.

- [ ] **Step 1: Добавить интерфейс в ядро**

```go
// Streamer — необязательная возможность клиента отдавать ответ по фрагментам.
// Нужна чату брифа: пользователь должен видеть текст до конца генерации.
type Streamer interface {
	CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (Usage, error)
}
```

- [ ] **Step 2: Написать падающий тест**

`stream_test.go`, `package llm_test`. `httptest`-сервер отдаёт SSE-поток OpenAI-совместимого формата (`data: {"choices":[{"delta":{"content":"При"}}]}`), последний чанк — с `usage` и пустой дельтой, затем `data: [DONE]`. Тесты:

- `TestCompleteStreamAssemblesFragments` — три чанка склеиваются в `Ответ`, `onDelta` вызван трижды с теми же фрагментами, `usage.PromptTokens/CompletionTokens` заполнены, `usage.Response` — полный текст.
- `TestCompleteStreamReturnsErrorMidStream` — сервер отдаёт один чанк и закрывает соединение с 500 → ошибка, при этом первый фрагмент уже отдан в `onDelta`, а `usage.Response` содержит накопленное.
- `TestCompleteStreamEmptyStream` — поток без чанков → ошибка, паники нет, `onDelta` не вызван.
- `TestCompleteStreamDoesNotRequestJSONMode` — сервер проверяет тело запроса: `response_format` отсутствует, `stream` = true, `stream_options.include_usage` = true.

- [ ] **Step 3: Убедиться, что тест падает**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/adapters/llm/deepseek/ -run TestCompleteStream -v`
Expected: FAIL — `c.CompleteStream undefined`.

- [ ] **Step 4: Реализовать `stream.go`**

```go
func (c *OpenAIClient) CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error)
```

`openai.ChatCompletionRequest{Model: c.ModelFor(role), Messages: […], StreamOptions: &openai.StreamOptions{IncludeUsage: true}}` — без `ResponseFormat`; `c.api.CreateChatCompletionStream(ctx, req)`, цикл `stream.Recv()` до `io.EOF`. Накопление: `strings.Builder` + вызов `onDelta` на непустую дельту. `usage` заполняется из последнего чанка с `Usage`, `model` — из первого чанка (пусто → `req.Model`), `Entries` — одна запись с моделью и ролью. Ретраев нет: частичный ответ нельзя переигрывать — ошибка уходит наверх, пользователь повторит ход. `defer stream.Close()`.

- [ ] **Step 5: Убедиться, что тесты проходят**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/adapters/llm/deepseek/ -v`
Expected: PASS, включая существующие тесты пакета.

- [ ] **Step 6: Коммит**

```bash
git add backend/internal/core/llm/llm.go backend/internal/adapters/llm/deepseek
git commit -m "feat(llm): стриминговый вызов без JSON-режима"
```

---

### Task 2: Проброс стрима через декораторы и прозаическая оговорка

**Files:**
- Modify: `backend/internal/adapters/skills/skills.go`, `backend/internal/adapters/tracing/llm.go`, `backend/internal/adapters/accounting/llm.go`
- Test: `backend/internal/adapters/skills/skills_test.go`, `backend/internal/adapters/tracing/llm_test.go`, `backend/internal/adapters/accounting/*_test.go`

**Interfaces:**
- Consumes: `corellm.Streamer` (Task 1).
- Produces: `skills.Options{Bindings map[string]string, Prose map[string]bool}`; каждый декоратор реализует `CompleteStream` и удовлетворяет `corellm.Streamer`.

Почему это нужно: цепочка в `cmd/server` заканчивается `accounting`, а сервису интервью нужен стриминговый клиент — значит, все три декоратора обязаны пробрасывать метод, иначе утверждение типа на `corellm.Streamer` в composition root не соберётся.

- [ ] **Step 1: Написать падающие тесты**

- `skills`: роль из `Prose` получает промпт с короткой оговоркой и **без** фразы «Ответ — только JSON по схеме»; обычная роль — с ней; стрим собирает `system` так же, как `Complete` (те же три части), `onDelta` пробрасывается наружу без изменений.
- `tracing`: стриминговый вызов оставляет событие LLM с накопленным `response` и токенами.
- `accounting`: запись usage создаётся из `Usage.Entries` стримингового вызова; при пустых `Entries` и известной роли модель берётся через `ModelFor`.

- [ ] **Step 2: Убедиться, что тесты падают**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/adapters/skills/ ./internal/adapters/tracing/ ./internal/adapters/accounting/ -v`
Expected: FAIL — `CompleteStream` отсутствует у декораторов.

- [ ] **Step 3: Реализовать прозаическую оговорку и стрим в `skills`**

В `skills.go`: поле `prose map[string]bool` в `Client`, заполняется из `Options.Prose` в `New`; константа

```go
const proseCaveat = `Файлы не создаются: раздел «Результат: …» выше описывает требуемое
содержание, а не файл на диске. Формат ответа задан ниже и важнее инструкций выше.`
```

`CompleteStream` для прозаической роли вставляет `proseCaveat`, для остальных — `contractCaveat`; далее делегирует `corellm.Streamer` внутреннего клиента, а если тот не умеет стримить — возвращает понятную ошибку `skills: клиент не поддерживает стриминг`.

- [ ] **Step 4: Реализовать стрим в `tracing`**

`CompleteStream` вызывает внутренний, по завершении пишет событие `KindLLM` с накопленным `usage.Response` и токенами (payload — как у `Complete`, включая `reasoning` и `finish_reason`).

- [ ] **Step 5: Реализовать стрим в `accounting`**

`CompleteStream` вызывает внутренний и повторяет логику записи usage из `Complete`: записи из `Usage.Entries`, а при их отсутствии — синтетическая запись с моделью из `ModelFor(role)`.

- [ ] **Step 6: Убедиться, что тесты проходят**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/adapters/... ./cmd/server/ -v`
Expected: PASS.

- [ ] **Step 7: Коммит**

```bash
git add backend/internal/adapters
git commit -m "feat(adapters): проброс стрима и прозаическая оговорка для интервьюера"
```

---

### Task 3: Пакет brief — домен и сервис интервью

**Files:**
- Create: `backend/internal/features/brief/domain/brief.go`
- Create: `backend/internal/features/brief/service/{ports.go,service.go,tail.go}`
- Test: `backend/internal/features/brief/service/{tail_test.go,service_test.go}`

**Interfaces:**
- Consumes: `corellm.Usage`, `corelogger.Logger` (порт ядра), `corellm.Streamer` (Task 1).
- Produces: `domain.Message{Role, Content string}`; `domain.Draft{Product, Goal, Audience, Tone, Region string; TopicsCount int}`; `domain.Result{Reply string; Draft Draft; Missing []string; Status string}`; константы `StatusReady = "ready"`, `StatusNeedsInput = "needs_input"`; `(Draft).HasRequired() bool`; порт `service.Streamer` (та же форма, что `corellm.Streamer`); `service.Options{Stream Streamer; Log corelogger.Logger; MaxMessages, MaxChars int}`; `service.New(Options) *Service`; `(*Service).Ask(ctx context.Context, msgs []domain.Message, prev domain.Draft, onDelta func(string)) (domain.Result, corellm.Usage, error)`. Конвертацию `Draft` → `campaign.Brief` делает транспорт.

- [ ] **Step 1: Написать падающие тесты парсера хвоста**

`tail_test.go`, таблица `TestParseTail`:

| Вход | Ожидание |
|---|---|
| `"Текст\n<<<BRIEF\n{\"product\":\"Кружка\"}"` | бриф с продуктом, `ok=true` |
| `"Текст\n<<<BRIEF\n```json\n{\"goal\":\"Рост\"}\n```"` | код-фенс снят, бриф разобран |
| `"Текст без маркера"` | `ok=false`, прежний бриф сохраняется |
| `"Текст\n<<<BRIEF\n{битый"` | `ok=false` |
| `"Проза со словом <<<BRIEF внутри и валидный JSON"` | разбор по **последнему** вхождению маркера |
| `"Текст\n<<<BRIEF\n{}"` | `ok=true`, все поля пустые → слияние ничего не меняет |

`TestMergeDraftKeepsKnownFields` — частичный хвост (`{"tone":"дружелюбный"}`) поверх собранного брифа не затирает `product`/`goal`/`audience`.

- [ ] **Step 2: Написать падающие тесты сервиса**

`service_test.go` (фейковый стример отдаёт заранее заданный текст чанками):

- `TestAskSendsProseToClientAndParsesTail` — `onDelta` получил только текст до маркера, хвост наружу не утёк; `Result.Reply` — проза; `Result.Draft` — из хвоста.
- `TestAskComputesMissingAndStatus` — после хвоста с двумя полями `Missing` = остальные обязательные, `Status` = `needs_input`; после хвоста со всеми четырьмя — `Missing` пуст, `Status` = `ready`.
- `TestAskKeepsPreviousDraftOnBrokenTail` — битый хвост: `Draft` равен присланному прежнему, `Reply` содержит прозу, ошибки нет.
- `TestAskPromptCarriesTurnLimit` — в `system` есть инструкция про не больше трёх уточнений и маркер `<<<BRIEF`.
- `TestAskPropagatesStreamError` — ошибка стримера возвращается вместе с накопленным `Reply`.
- `TestAskLogsDurationAndTokens` — фейковый `corelogger.Logger` получил `Info("interview", …)` с длительностью и токенами; при `Log` = nil паники нет.

- [ ] **Step 3: Убедиться, что тесты падают**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/features/brief/... -v`
Expected: FAIL — пакета нет.

- [ ] **Step 4: Реализовать домен и порт**

`domain/brief.go` — `Message`, `Draft`, `Result`, статусы и `func (d Draft) HasRequired() bool` (непусты product, goal, audience, tone). `service/ports.go`:

```go
// Streamer — то, что сервису нужно от вызова модели: ответ по фрагментам.
type Streamer interface {
	CompleteStream(ctx context.Context, role, system, user string, onDelta func(string)) (corellm.Usage, error)
}
```

- [ ] **Step 5: Реализовать разбор хвоста**

`service/tail.go`: `ParseTail(text string) (domain.Draft, bool)` — последнее вхождение `<<<BRIEF`, снятие код-фенсов, `json.Unmarshal` в `domain.Draft`, `false` при любой неудаче. Разбор не бросает панику ни на каком входе.

- [ ] **Step 6: Реализовать сервис**

`service/service.go`: роль `RoleInterviewer = "interviewer"`, лимиты `MaxMessages = 20`, `MaxChars = 32 << 10` (нулевые значения в `Options` заменяются этими дефолтами, чтобы composition root мог их не указывать), `Log` по умолчанию `corelogger.Nop()`. `Ask` валидирует историю (`limits.Invalid` при пустой/переросшей — коды `empty_history`, `history_too_long`), собирает `user` из истории (формат `Пользователь: …` / `Ассистент: …`), `system` — инструкция интервьюера (три уточнения, маркер `<<<BRIEF`, компактный JSON с полями брифа), вызывает `Stream.CompleteStream` со своей обёрткой `onDelta`, которая держит буфер: текст до маркера уходит наружу сразу, всё после маркера — только в буфер. По завершении: `ParseTail` + слияние с `prev domain.Draft` (непустые поля перекрывают прежние), `Missing`/`Status` считает сервис, в журнал уходит одна строка `Info("interview", "duration_ms", …, "prompt_tokens", …, "completion_tokens", …, "messages", len(msgs), "status", res.Status)`.

- [ ] **Step 7: Убедиться, что тесты проходят**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/features/brief/... -v`
Expected: PASS.

- [ ] **Step 8: Коммит**

```bash
git add backend/internal/features/brief
git commit -m "feat(brief): сервис интервью с разбором хвостового JSON"
```

---

### Task 4: SSE-эндпоинт интервью

**Files:**
- Create: `backend/internal/features/brief/transport/http/handler.go`
- Test: `backend/internal/features/brief/transport/http/handler_test.go`

**Interfaces:**
- Consumes: `service.New`, `(*Service).Ask`, `domain.Message`, `domain.Result` (Task 3); `server.Route`, `middleware.RateLimiter`, `response.WriteJSON` — по образцу `internal/features/campaign/transport/http/handler.go`.
- Produces: `http.NewHandler(s InterviewService, limiter *middleware.RateLimiter) *Handler`; `(*Handler).Routes() []server.Route` с `POST /api/briefs/interview`.

- [ ] **Step 1: Написать падающие тесты**

`handler_test.go`, `package http_test`, сервис подменён интерфейсом с фейком:

- `TestInterviewStreamsFrames` — ответ `200`, `Content-Type: text/event-stream`, кадры по порядку `delta` (`При`), `delta` (`вет`), `brief` (с `missing`/`status`), `done`; хвост JSON в кадры не попал.
- `TestInterviewRejectsEmptyHistory` — пустой `messages` → `400` с кодом `empty_history`.
- `TestInterviewRejectsTooLongHistory` — 21 сообщение → `400` с кодом `history_too_long`.
- `TestInterviewStreamsErrorFrame` — сервис вернул ошибку посреди работы → кадры `delta…`, затем `error`, поток закрыт, статус `200` (заголовки уже отправлены).

Ответы пишутся через `http.Flusher`; хендлер проверяет его наличие и при отсутствии возвращает `500` с понятным сообщением.

- [ ] **Step 2: Убедиться, что тесты падают**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/features/brief/transport/... -v`
Expected: FAIL — пакета нет.

- [ ] **Step 3: Реализовать хендлер**

Декодирование тела (лимит JSON — общий `limits`), вызов `Ask` с колбэком, который пишет кадр `delta` и вызывает `Flush`; после — кадр `brief` (бриф + `missing` + `status`) и `done`; ошибка → кадр `error` с текстом публичного сообщения (сырой ответ модели не утекает), затем `done`. Лимитер — как у соседних хендлеров.

- [ ] **Step 4: Убедиться, что тесты проходят**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/features/brief/... -v`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
git add backend/internal/features/brief/transport
git commit -m "feat(brief): SSE-эндпоинт интервью"
```

---

### Task 5: Composition root: десятая роль, маршрут, сквозной стрим

**Files:**
- Modify: `backend/cmd/server/skills.go`, `backend/cmd/server/llm.go`, `backend/cmd/server/main.go`
- Test: `backend/cmd/server/skills_test.go`

**Interfaces:**
- Consumes: `skills.Options.Prose` (Task 2), `briefservice.New` (Task 3), `briefhttp.NewHandler` (Task 4).
- Produces: `skillBindings()` с десятью парами; зарегистрированный маршрут `POST /api/briefs/interview`.

- [ ] **Step 1: Написать падающий тест**

- `TestSkillBindingsCoverAllRoles` расширяется десятой парой `briefservice.RoleInterviewer → "campaign-context"`.
- `TestInterviewerSkillIsProse` — цепочка из `newLLMClient` приводится к `corellm.Streamer` (утверждение обязано удаться), и стриминговый вызов роли `interviewer` получает промпт с короткой оговоркой и **без** «Ответ — только JSON по схеме», а роль `copywriter` — с ней. Этот тест падает, если декораторы перестанут пробрасывать стрим.

- [ ] **Step 2: Убедиться, что тест падает**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./cmd/server/ -v`
Expected: FAIL — десятая пара отсутствует.

- [ ] **Step 3: Реализовать**

`skills.go`: добавить пару `briefservice.RoleInterviewer: "campaign-context"`. `llm.go`: в `newLLMClient` передать `skills.Options{Bindings: skillBindings(), Prose: map[string]bool{briefservice.RoleInterviewer: true}}`. `main.go`: получить стриминговый клиент утверждением типа с понятным отказом старта, а не паникой:

```go
stream, ok := llmClient.(corellm.Streamer)
if !ok {
	logger.Error("brief", "err", "клиент модели не поддерживает стриминг")
	os.Exit(1)
}
api.RegisterRoutes(briefhttp.NewHandler(briefservice.New(briefservice.Options{Stream: stream, Log: sloglogger.New(logger)}), limiter).Routes()...)
```

Отказ старта здесь — страховка на случай, если кто-то поменяет цепочку декораторов и стрим перестанет доходить до внешнего слоя; тест `TestInterviewerSkillIsProse` проверяет ту же цепочку.

- [ ] **Step 4: Убедиться, что тесты и сборка проходят**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./cmd/server/ -v && GOCACHE=/tmp/marketing-agents-gocache go build ./...`
Expected: PASS; сборка без ошибок (утверждение типа на `corellm.Streamer` компилируется).

- [ ] **Step 5: Коммит**

```bash
git add backend/cmd/server
git commit -m "feat(brief): роль interviewer со скилом campaign-context и маршрут интервью"
```

---

### Task 6: Фронтенд — разбор SSE, клиент и стор интервью

**Files:**
- Create: `frontend/src/lib/sse.ts`, `frontend/src/lib/api/briefs.ts`, `frontend/src/lib/stores/interview.ts`
- Test: `frontend/src/lib/sse.test.ts`, `frontend/src/lib/api/briefs.test.ts`, `frontend/src/lib/stores/interview.test.ts`

**Interfaces:**
- Consumes: `#lib/api/client.js` (`apiUrl`, `errorMessage`), типы из `#lib/api/types.js`.
- Produces: `parseSSE(chunk: string, carry: string): {frames: unknown[]; carry: string}`; `streamInterview(messages, handlers, signal?)`; фабрика `createInterview(storage?: Storage)` и модульный синглтон `interview` с writable-сторами `messages`, `draft`, `missing`, `status`, `streaming`, `error`, `campaignId` и действиями `send(text)`, `retry()`, `reset()`, `brief()` (конвертация `Draft` → `Brief` для запуска).

- [ ] **Step 1: Написать падающие тесты**

- `sse.test.ts`: кадр, разрезанный между чанками, склеивается; многострочный `data:` собирается в одну строку через `\n`; хвостовой огрызок без `\n\n` остаётся в `carry` и дополняется следующим чанком; мусорные строки (комментарии `:`) игнорируются; битый JSON в кадре не роняет парсер, а пропускается.
- `briefs.test.ts`: `streamInterview` на фейковом `fetch` с `ReadableStream` вызывает `onDelta` по кадрам `delta`, `onBrief` на кадре `brief`, `onDone` на `done`, `onError` на кадре `error` и на сетевой ошибке; запрос уходит методом POST с `Accept: text/event-stream` и телом `{messages}`.
- `interview.test.ts` (через `createInterview(fakeStorage)`, чтобы тесты не делили общий `localStorage`): `send` добавляет реплику пользователя и накапливает дельты в реплику ассистента; кадр `brief` обновляет `draft`/`missing`/`status`; ошибка оставляет накопленный текст и выставляет `error`; `reset` очищает ленту; состояние восстанавливается из `fakeStorage`; при переполнении квоты самые старые сообщения вытесняются; после 20 сообщений `send` не отправляет запрос и выставляет признак переросшей истории.

- [ ] **Step 2: Убедиться, что тесты падают**

Run: `cd frontend && npm test -- sse briefs interview`
Expected: FAIL — модулей нет.

- [ ] **Step 3: Реализовать модули**

`sse.ts` — чистый парсер кадров без побочных эффектов. `briefs.ts` — `fetch` + `response.body.getReader()` + `TextDecoder`, кадры прогоняются через `parseSSE`, JSON-кадры разбираются в типизированные события. `interview.ts` — фабрика `createInterview(storage)` и модульный синглтон `interview`, собранные как обычные writable-сторы (`svelte/store`), как в `#lib/stores/history.ts` и `progress.ts`, а не на рунах: файл не `.svelte.ts`, а стор нужен обоим компонентам. Состояние пишется в `localStorage` под ключом `interview:v1` (сообщения, бриф, `campaignId`) и восстанавливается при создании; при ошибке записи (квота) самые старые сообщения вытесняются и запись повторяется; после 20 сообщений `send` не отправляет запрос, а выставляет признак переросшей истории.

- [ ] **Step 4: Убедиться, что тесты проходят**

Run: `cd frontend && npm test`
Expected: PASS, включая существующие тесты.

- [ ] **Step 5: Коммит**

```bash
git add frontend/src/lib
git commit -m "feat(chat): разбор SSE, клиент интервью и стор ленты"
```

---

### Task 7: Фронтенд — страница-чат с прогоном в ленте

**Files:**
- Create: `frontend/src/lib/components/ChatThread.svelte`, `frontend/src/lib/components/BriefSummary.svelte`
- Modify: `frontend/src/routes/+page.svelte`, `frontend/src/lib/styles/components.css`
- Test: `cd frontend && npm run check` (компоненты юнит-тестами не покрываются — так принято в проекте)

**Interfaces:**
- Consumes: стор `interview` (Task 6), `createCampaign` (`#lib/api/campaigns.js`), `campaignRun(id)` (`#lib/stores/run.js`), `ProgressPanel`, `ArticleCard`, `TrajectoryPanel`, `resolve` из `$app/paths`.
- Produces: главная страница — лента.

- [ ] **Step 1: Написать `BriefSummary.svelte`**

Read-only сводка: поля `product`, `goal`, `audience`, `tone`, `region`, `topics_count`; список `missing` (когда непуст — предупреждение о пробелах); кнопка «Запустить кампанию». Она вызывает `createCampaign(brief)` (внутри уже есть `Idempotency-Key`), кладёт id в стор и запускает `refreshHistory()`; на ошибке — `toast.error(errorMessage(err))` и текст ошибки у кнопки. Пока идёт стрим — кнопка недоступна.

- [ ] **Step 2: Написать `ChatThread.svelte`**

Лента: реплики пользователя и ассистента (последняя — с живыми дельтами), форма ввода (одно поле, `Enter` отправляет, `Shift+Enter` переносит строку), индикатор «печатает», кнопка «Новый диалог», блок ошибки с кнопкой «Повторить» (повторная отправка той же истории), `BriefSummary` под лентой и, если кампания запущена, блок прогона: `ProgressPanel` во время работы, `ArticleCard` по статьям, `TrajectoryPanel` внизу и ссылка на `/campaigns/[id]` через `resolve()`.

- [ ] **Step 3: Переписать `+page.svelte`**

Форма с четырьмя `textarea` удаляется; страница рендерит `ChatThread`. Загрузчик `+page.ts` (лимит тем из `/api/limits`) остаётся и передаёт лимит в сводку брифа.

- [ ] **Step 4: Добавить стили**

В `#lib/styles/components.css` — классы ленты и пузырей в существующей палитре (`--color-canvas`, `--color-ink`, `--color-accent`, `--color-line`), ничего не переиспользуя из удалённой формы.

- [ ] **Step 5: Проверить типы и сборку**

Run: `cd frontend && npm run check && npm run build`
Expected: `svelte-check` без ошибок и предупреждений, сборка проходит.

- [ ] **Step 6: Коммит**

```bash
git add frontend/src
git commit -m "feat(chat): главная страница — лента брифа с прогоном"
```

---

### Task 8: Документация

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Обновить README**

Добавить: эндпоинт `POST /api/briefs/interview` (тело, кадры, лимиты 20 сообщений и 32 КиБ), роль `interviewer` со скилом `campaign-context`, что главная страница — чат, что история живёт в `localStorage` и сервер её не хранит, и в «Известные ограничения» — что вызовы интервьюера не попадают в трассу и в оценку стоимости (нет `run_id`), а видны только в логах сервера.

- [ ] **Step 2: Проверить, что README не противоречит коду**

Run: `cd backend && GOCACHE=/tmp/marketing-agents-gocache go test ./internal/features/brief/... ./cmd/server/ && cd .. && grep -n "briefs/interview\|interviewer" README.md`
Expected: тесты проходят, упоминания на месте.

- [ ] **Step 3: Коммит**

```bash
git add README.md
git commit -m "docs: чат брифа, роль interviewer и ограничения интервью"
```

---

### Task 9: Приёмка

**Files:** изменений нет.

- [ ] **Step 1: Полная проверка**

Run: `make verify`
Expected: vet и svelte-check без замечаний, тесты Go (с MariaDB) и фронта зелёные, обе сборки успешны.

- [ ] **Step 2: Поднять локальный стек**

Run: `make db-up && make migrate`, затем `cd backend && go run ./cmd/server` и `cd frontend && npm run dev`
Expected: API на 8080, dev-сервер на свободном порту (5173 может быть занят другим проектом — смотреть вывод Vite).

- [ ] **Step 3: Проверить стрим напрямую**

Run:
```bash
curl -N -sS -XPOST localhost:8080/api/briefs/interview -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"Хочу кампанию для термокружки «Север», аудитория — велокоммьютеры, цель — переход в карточку"}]}'
```
Expected: кадры `delta` приходят **по мере генерации** (видно по паузам и постепенному тексту), затем `brief` с непустым `draft` и списком `missing`, затем `done`; в тексте дельт нет хвостового JSON и маркера.

- [ ] **Step 4: Проверить стрим через прокси фронта**

Тот же запрос на адрес dev-сервера (`/api/briefs/interview`) — фрагменты должны приходить так же постепенно, а не одним куском в конце.
Expected: постепенный поток (это и есть проверка отсутствия буферизации на прокси).

- [ ] **Step 5: Живой прогон в браузере**

Открыть ленту, написать бриф свободным текстом, получить уточнение, ответить, дождаться сводки, нажать «Запустить кампанию».
Expected: прогресс, затем статьи появляются **в ленте**; ссылка на `/campaigns/[id]` ведёт на прежнюю страницу, где прогон и трасса на месте.

- [ ] **Step 6: Проверить перезагрузку и очистку**

Обновить страницу посреди прогона — лента и блок прогона восстанавливаются, подписка на прогресс возобновляется. Нажать «Новый диалог» — лента очищается, `localStorage` больше не содержит прежней истории.
Expected: восстановление работает, очистка работает.

- [ ] **Step 7: Прибрать окружение**

Остановить API и dev-сервер, затем `make db-down`.
Expected: порты свободны, том MariaDB сохранён, `git status --porcelain` чист (кроме untracked `AGENTS.md`).
