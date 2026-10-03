# Трасса прогона — Implementation Plan

**Спека:** `docs/superpowers/specs/2026-10-03-run-trajectory-design.md`
**Область:** фаза 1 — бэкенд (миграция, `internal/trace`, декораторы, решения, API,
конфиг, ретенция). Вьюер и полные трейсы — фазы 2–3.
**Принцип:** каждый таск — с тестом, зелёными `go test ./...` и `npm test`, отдельным
коммитом. Трасса не влияет на прогон: ошибки записи не возвращаются в пайплайн.

---

### Task 1: Пакет `internal/trace`

- [ ] **Step 1: Failing-тесты** — `internal/trace/trace_test.go`:
  `Nop` ничего не пишет; рекордер назначает монотонный `seq` на прогон (и раздельно
  для разных прогонов); в режиме `summary` payload не передаётся в sink, в `full` —
  передаётся; payload больше лимита обрезается с пометкой; ошибка sink не ломает
  запись следующего события и не возвращается наружу.
- [ ] **Step 2: Реализация** — `internal/trace/trace.go`: `Kind`, `Event`, `Sink`,
  `Mode` (`off|summary|full`), `Recorder`, `Nop`. Счётчик `seq` — под мьютексом,
  карта по `run_id`.
- [ ] **Step 3: Проверка** — `go test ./internal/trace/...`.
- [ ] **Step 4: Commit** — `feat(trace): журнал событий прогона`.

---

### Task 2: Стор — таблица и запросы

- [ ] **Step 1: Миграция** — `internal/store/migrations/0002_run_events.sql`
  (таблица `run_events` + индексы по `(run_id, seq)` и `at`).
- [ ] **Step 2: Failing-тесты** — `internal/store/store_test.go`: сохранение события,
  чтение ленты (без payload), чтение полного события по `seq`, отсутствие чужого
  `seq`, ретенция удаляет только старое, повторная миграция идемпотентна.
- [ ] **Step 3: Реализация** — `internal/store/events.go`: `SaveRunEvent`,
  `RunEvents`, `RunEvent`, `DeleteRunEventsBefore`; `Event` принимается как
  `trace.Event` (стор импортирует `trace`, обратной зависимости нет).
- [ ] **Step 4: Проверка** — `go test ./internal/store/...`.
- [ ] **Step 5: Commit** — `feat(store): таблица событий прогона`.

---

### Task 3: Декораторы над `llm` и `wordstat`

- [ ] **Step 1: Failing-тесты** — `internal/llm/tracing_test.go`: роль, модель,
  токены, длительность и статус попадают в событие; ошибка внутреннего клиента даёт
  `status=error` и текст ошибки; в `full` пишется промпт и ответ, в `summary` — нет;
  ключ и заголовки не попадают в payload.
- [ ] **Step 2: Реализация** — `internal/llm/tracing.go`: `TracingClient`.
- [ ] **Step 3: Тесты и реализация для Wordstat** — `internal/wordstat/tracing.go`
  (+ `tracing_test.go`): инструмент, фраза, регион, `numPhrases`, `totalCount`,
  `hasData`, `cacheHit`, длительность, ошибка.
- [ ] **Step 4: Проверка** — `go test ./internal/llm/... ./internal/wordstat/...`.
- [ ] **Step 5: Commit** — `feat(trace): декораторы LLM и Wordstat`.

---

### Task 4: Решения оркестратора и итерации критика

- [ ] **Step 1: Failing-тесты** — `internal/orchestrator/research_test.go` (дополнить)
  и `orchestrator_test.go`: в прогоне появляются события — количество сеялок, число
  собранных фраз по каждой сеялке, число тем от модели, решение по каждому кандидату
  (объём, порог, `selected`), причина fallback, по одной записи на итерацию критика
  со `score`/`verdict`, финальная сводка (токены по ролям, обращения к Wordstat).
- [ ] **Step 2: Реализация** — `internal/orchestrator/trace.go` (хелперы записи) и
  вызовы в `research.go`/`orchestrator.go`/`select.go`; поле `Recorder` в `Options`.
  `Options.Recorder == nil` → `trace.Nop()`.
- [ ] **Step 3: Проверка** — `go test ./internal/orchestrator/...`.
- [ ] **Step 4: Commit** — `feat(orchestrator): трасса решений и итераций критика`.

---

### Task 5: Конфиг и проводка

- [ ] **Step 1: Failing-тесты** — `internal/config/config_test.go`: дефолты
  `TRACE_MODE=summary`, `TRACE_RETENTION_DAYS=30`, `TRACE_MAX_PAYLOAD_BYTES=32768`;
  невалидный режим → ошибка валидации.
- [ ] **Step 2: Реализация** — `internal/config/config.go` + `backend/.env.example`.
- [ ] **Step 3: Проводка** — `cmd/server/main.go`: рекордер поверх стора, обёртки
  `llm.TracingClient` и `wordstat.TracingSource`, передача в `orchestrator.Options`;
  ретенция при старте (ошибка чистки только логируется).
- [ ] **Step 4: Проверка** — `go test ./...` (в т.ч. `cmd/server` собирается).
- [ ] **Step 5: Commit** — `feat(config): режим и ретенция трассы`.

---

### Task 6: API трассы

- [ ] **Step 1: Failing-тесты** — `internal/httpapi/trajectory_test.go`: лента по
  кампании отдаёт события без payload с `has_payload`; детальный эндпоинт отдаёт
  payload; чужой `seq` → 404; неизвестный прогон → 404; пустая трасса → `events: []`
  и `total: 0`; те же два эндпоинта для проверок текстов.
- [ ] **Step 2: Реализация** — `internal/httpapi/trajectory.go` + маршруты в
  `api.go`; интерфейс `TrajectoryRepo` (лента и событие) в `Repo`-стиле проекта.
- [ ] **Step 3: Проверка** — `go test ./internal/httpapi/...`.
- [ ] **Step 4: Commit** — `feat(httpapi): эндпоинты трассы прогона`.

---

### Task 7: Типы фронта

- [ ] **Step 1: Типы** — `frontend/src/lib/api/types.ts`: `TrajectoryEvent`,
  `Trajectory`, `TraceKind`, `TraceStatus`, поля `has_payload`.
- [ ] **Step 2: Лейблы** — `labels.ts`: `TRACE_KIND_LABELS` (LLM, Wordstat, решение,
  фаза, итог), `TRACE_STATUS_LABELS`.
- [ ] **Step 3: Тесты** — `labels.test.ts` (виды и статусы покрыты полностью).
- [ ] **Step 4: Проверка** — `npm test && npm run check`.
- [ ] **Step 5: Commit** — `feat(frontend): типы трассы прогона`.

---

## Приёмка фазы 1

- [ ] `make verify` — зелёный (go vet, go test, svelte-check, vitest, сборка).
- [ ] Живой прогон: кампания с `TRACE_MODE=summary` → в `GET /api/campaigns/{id}/trajectory`
      видна лента: сеялки, сбор спроса по каждой фразе, кластеризация, решения отбора,
      итерации критика, финальная сводка по ролям; ни одного промпта в ответе.
- [ ] Проверка `TRACE_MODE=off`: прогон работает, таблица событий не растёт.
- [ ] Проверка ретенции: событие старше срока удаляется на старте сервиса.

## Риски и заметки

- **Объём:** в `summary` событие — сотни байт; полный прогон с 5 темами даёт десятки
  событий. В `full` тела промптов и статей обрезаются лимитом, но всё равно требуют
  ретенции.
- **Приватность:** `full` содержит бриф и черновики; basic-auth по умолчанию выключен,
  поэтому режим документируется как отладочный.
- **Синхронная запись:** выбрана осознанно ради выживания событий при падении; при
  росте числа событий (например, потоковый вывод статей) понадобится буферизация.
