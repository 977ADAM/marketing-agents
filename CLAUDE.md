# marketing-agents

Мультиагентный сервис маркетингового контента на **Go + DeepSeek**.
Два пайплайна:
- генерация: `бриф → стратег → копирайтеры (параллельно по темам) → критик → пакет статей`;
- проверка готовых текстов: `бриф + тексты → compliance-агент (соответствие брифу) +
  quality-агент (корректность текста) → отчёт {compliance, quality, overall, verdict}`.
Веб-интерфейс (React SPA) встроен в один бинарь через `go:embed`.

## Архитектура

**Backend (Go 1.25):**
- `cmd/server/main.go` — точка входа: загрузка конфига, пул pgx, миграции,
  `RecoverInterrupted`, сборка LLM-клиента/оркестратора/runner, роутинг, graceful shutdown.
- `internal/config` — все настройки из env (`Load()`); `DATABASE_URL` и `DEEPSEEK_API_KEY` обязательны.
- `internal/agents` — роли `strategist`, `copywriter`, `critic` (генерация) +
  `compliance`, `quality` (проверка текстов, см. `reviewer.go`) + общие `types`.
- `internal/orchestrator` — `Run` (пайплайн генерации, итерации критика, кап тем) и
  `Review` (параллельная проверка текстов двумя агентами); подсчёт стоимости.
- `internal/llm` — openai-совместимый клиент под DeepSeek (`openai.go`), ретраи; `fake.go` для тестов.
  Модели разносятся по ролям через `SetRoleModel`.
- `internal/httpapi` — REST (`api.go` + `reviews.go`), basic-auth (`auth.go`), фоновый `runner.go`
  (`Start`/`StartReview`), `progress.go` — Hub для кампаний и проверок (kind), разбор `.docx`
  в `reviews.go` (stdlib zip+xml, включая таблицы).
- `internal/store` — Postgres через pgx; миграции в `store/migrations/*.sql`;
  таблицы `campaigns`+`deliverables` и `reviews` (миграция 0003); `RecoverInterrupted`
  помечает осиротевшие `running`-записи обоих типов как `failed` при старте.
- `internal/web` — `go:embed` фронта из `internal/web/dist`.

**Frontend (Vite + React + TS):** в `frontend/src` — форма брифа (`NewCampaign`),
проверка текстов (`ReviewForm` с загрузкой .docx), наблюдение за прогонами через polling/SSE
(`useCampaignProgress`/`useReviewProgress`), история (`Sidebar`/`useCampaigns`/`useReviews`).
Сборка пишет в `internal/web/dist`.

## Роутинг и доступ
- `/api/*` и `/healthz` → API; всё остальное → SPA.
- Всё за basic-auth (`BASIC_AUTH_USER`/`BASIC_AUTH_PASS`); пустые значения = без пароля (только локалка).
- `/healthz` тоже под общим хендлером basic-auth — учитывай при healthcheck'ах.

## API
- `POST /api/campaigns` — `{product, goal, audience, tone, client_id?}` → `202 {id, status}`
- `GET /api/campaigns/{id}` — статус и результат (когда `done`)
- `GET /api/campaigns` — список всех кампаний
- `POST /api/reviews` — `{brief, texts: [{title, body}], client_id?}` → `202 {id, status}`
- `GET /api/reviews/{id}` — статус и отчёт (`items[]`: compliance/quality/overall/verdict)
- `GET /api/reviews/{id}/events` — SSE-прогресс проверки
- `GET /api/reviews` — список проверок
- `POST /api/reviews/extract` — multipart `file` (.docx) → `{title, text}`
- `GET /healthz`

## Команды

```bash
# Go
go build ./...
go test ./...

# Интеграционные тесты стора (нужен Postgres)
docker compose up -d db
DATABASE_URL=postgres://app:app@localhost:5432/marketing?sslmode=disable \
  go test -tags=integration ./internal/store/

# Фронтенд
cd frontend && npm ci && npm run build   # → internal/web/dist
cd frontend && npm test                  # Vitest
cd frontend && npm run dev               # dev-сервер с прокси на :8080

# Полный запуск
cp .env.example .env   # вписать DEEPSEEK_API_KEY (+ BASIC_AUTH_*)
docker compose up -d --build
curl localhost:8080/healthz
```

## Конфигурация (env)
Дефолты — в `internal/config/config.go`, пример — `.env.example`. Ключевое:
- `MODEL_DEFAULT` (`deepseek-v4-pro`) — стратег и критик; `MODEL_FAST` (`deepseek-v4-flash`) — копирайтеры.
- `CRITIC_MAX_ITER`, `CRITIC_SCORE_THRESHOLD` — цикл доработки критиком.
- `MAX_TOPICS` — кап числа тем (контроль стоимости).
- `RATE_LIMIT_PER_MIN`, `RUN_TIMEOUT`, `LLM_MAX_RETRIES`.
- `COST_PER_1K_PROMPT`/`COMPLETION` — расчёт стоимости прогона.

> Примечание: README местами говорит «модель по умолчанию для всех ролей» — это устарело,
> фактически копирайтеры идут на `MODEL_FAST` (см. `cmd/server/main.go`).

## Соглашения
- Коммиты — на русском, в формате `type(scope): описание` (feat/fix/test/docs/security/build).
- Не коммитить пересобранный фронт под `internal/web/dist/`: в репо держится только
  плейсхолдер `index.html`, а `dist/assets/` — в `.gitignore`.
- Порты db(5432)/app(8080) в compose привязаны к `127.0.0.1`.
- Доки фаз: `docs/superpowers/{plans,specs}`.
