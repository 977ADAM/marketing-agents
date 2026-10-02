# marketing-agents

Мультиагентный сервис маркетингового контента: Go + DeepSeek. Два режима:

1. **Генерация кампании**: бриф → стратег → копирайтеры (по темам, параллельно) →
   критик → пакет статей.
2. **Проверка готовых текстов**: бриф + готовые статьи прогоняются через двух агентов —
   «соответствие брифу» (продукт/УТП/аудитория/запреты/слоган/возражения) и
   «корректность текста» (факты, орфография, стиль, противоречия) — с итоговым отчётом.

Веб-интерфейс: форма брифа, наблюдение за прогоном кампании в реальном времени,
проверка текстов (вставка или загрузка .docx) и история.

## Структура

```
backend/            Go-сервис (отдельный модуль): API /api/*, /healthz, SQLite
  cmd/server/       точка входа
  internal/         agents, llm, orchestrator, store, httpapi, web
  .env.example      переменные окружения бэкенда (копируется в .env, в git не попадает)
  Dockerfile        образ API: distroless + SQLite
frontend/           SvelteKit 3 (Svelte 5, adapter-node)
  src/routes/       страницы /, /campaigns/[id], /reviews, /reviews/[id]
  src/routes/api/   прокси /api/* на Go-API (endpoint-роут +server.ts)
  src/lib/          api (запросы), stores (состояние, SSE), components, styles
  src/env.ts        объявление переменных окружения фронта
  Dockerfile        образ фронта: сборка + Node-сервер SvelteKit
docker-compose.yml  стек из двух сервисов: backend (внутренний) + frontend (порт 8080)
```

Go-команды запускаются из `backend/`, фронтовые — из `frontend/`.

## Запуск

Внешних сервисов не нужно: БД — файл SQLite, создаётся при первом старте.
Nginx в стек не входит — снаружи стоит nginx сервера и проксирует на контейнер
фронта (единственная опубликованная точка входа).

**Docker Compose** (штатный путь, из корня репозитория):
```bash
cp backend/.env.example backend/.env   # указать DEEPSEEK_API_KEY (+ BASIC_AUTH_USER/PASS для UI)
docker compose up -d --build
curl localhost:8080/healthz            # ok (запрос уходит через фронт в API)
```
- `frontend` публикуется на `127.0.0.1:8080`: SvelteKit отдаёт приложение и сам
  проксирует `/api/*` и `/healthz` в `backend` по внутренней сети compose;
- `backend` наружу не публикуется, БД лежит на volume `sqlite`
  (`/data/marketing.db`), поэтому данные переживают `down/up` и пересборку.

**Локально без Docker**:
```bash
cd backend
cp .env.example .env       # при первом запуске: указать DEEPSEEK_API_KEY
set -a; source .env; set +a
go run ./cmd/server        # БД → backend/data/marketing.db
```
```bash
cd frontend
npm ci
npm run dev                # http://localhost:5173, /api проксируется на :8080
```
Файл `.env` приложением автоматически не читается (это env процесса), поэтому
переменные нужно экспортировать — как выше, либо задать их в окружении.

## API

### Кампании (генерация)
- `POST /api/campaigns` — `{product, goal, audience, tone, client_id?}` → `202 {id, status}`
- `GET /api/campaigns/{id}` — статус и результат (когда `done`)
- `GET /api/campaigns/{id}/events` — SSE-поток живого прогресса прогона (по-темный)
- `GET /api/campaigns` — список всех кампаний

### Проверка готовых текстов
- `POST /api/reviews` — `{brief, texts: [{title, body}, ...]}` → `202 {id, status}`
- `GET /api/reviews/{id}` — статус и отчёт: по каждому тексту `compliance`/`quality`
  (оценки 0–100 + замечания + `severity`), `overall`, `verdict: pass|fix`, `passed`
- `GET /api/reviews/{id}/events` — SSE-поток прогресса проверки
- `GET /api/reviews` — список проверок (с готовым `brief_title` для списка)
- `POST /api/reviews/extract` — разбор .docx → `{title, body, text}` (текст из
  `word/document.xml`, включая таблицы; `title` — первая строка, `body` — остальное)

### Общее
- `GET /healthz`

Всё, что можно посчитать (оценки, градации, сводки, разбор .docx), считает API:
во фронте нет бизнес-логики и валидации — ошибки приходят из API
(`400 {error:{code,message}}`) и показываются в форме.

## Пример

```bash
curl -XPOST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"product":"Эко-бутылка","goal":"рост продаж","audience":"ЗОЖ 25-40","tone":"дружелюбный"}'

# Проверка готового текста по брифу
curl -XPOST localhost:8080/api/reviews -H 'Content-Type: application/json' \
  -d '{"brief":"Продукт: шины Ikon. Запрет: не упоминать другие бренды.","texts":[{"title":"Статья","body":"..."}]}'
```

## Веб-интерфейс

Фронт — приложение **SvelteKit 3** (Svelte 5, runes) в режиме SPA
(`ssr = false` в `src/routes/+layout.ts`) на адаптере `@sveltejs/adapter-node`:
прод-сервер — Node, статика и роутинг — SvelteKit, прокси API — endpoint-роут
`src/routes/api/[...path]/+server.ts`.

```bash
cd frontend
npm run dev      # разработка (Vite, /api → localhost:8080)
npm run build    # сборка в build/ (Node-сервер)
npm run check    # svelte-check: типы и замечания Svelte
```

Детали, важные при доработке:
- ссылки и переходы — только через `resolve()` из `$app/paths`
  (`base` из `$app/paths` в SvelteKit 3 удалён; `resolve()` сам подставляет
  базовый путь приложения);
- модули внутри `src/lib` импортируются как `#lib/...` с расширением
  (`#lib/api/client.js`) — прежний алиас `$lib` в SvelteKit 3 удалён;
- загрузка .docx уходит на фронт-сервер сырыми байтами
  (`Content-Type: application/octet-stream`), а multipart для Go-API собирает
  прокси: так запрос не попадает под CSRF-защиту SvelteKit, которая блокирует
  POST с form-content-type без совпадающего `Origin`. Если обращаетесь к
  `/api/reviews/extract` не из браузера, используйте тот же octet-stream
  (или добавьте заголовок `Origin` вашего сайта).

Публикация в подпуть (interpool): сборка с `PUBLIC_BASE=/marketing/` —
от неё зависят ссылки, ассеты и префикс `/api`. Nginx при этом **не должен**
срезать префикс: запросы `/marketing/...` приходят в контейнер как есть, а
префикс для API снимает SvelteKit.

## Тесты

```bash
cd backend && go test ./...     # агенты, оркестратор, httpapi, стор (SQLite во временном каталоге)
cd frontend && npm run check    # svelte-check: типы и Svelte-диагностики
```

## Хранилище данных

БД — один файл SQLite (`SQLITE_PATH`, по умолчанию `data/marketing.db` относительно
рабочего каталога), драйвер `modernc.org/sqlite` (чистый Go: CGO не нужен, бинарь
остаётся статическим, рантайм-образ — distroless). Каталог под файл создаётся при
старте, миграции применяются автоматически; применённые версии отмечаются в
`schema_migrations`.

Соединение открывается с `journal_mode=WAL`, `busy_timeout=5000`,
`foreign_keys=1` и `_txlock=immediate` — параллельные прогоны (каждая тема
пишет прогресс из своей горутины) не ловят «database is locked».

Бэкап — копия файла; на работающем сервисе лучше через сам SQLite:
```bash
sqlite3 backend/data/marketing.db ".backup backup.db"
```

> Данные прежней Postgres-версии автоматически не переносятся — нужен отдельный
> перенос (`pg_dump` → вставка в SQLite). Строка `postgres://...` в
> `DATABASE_URL` приводит к ошибке старта с подсказкой про `SQLITE_PATH`.

## Конфигурация

**Бэкенд** — env, см. `backend/.env.example`. Файл БД — `SQLITE_PATH`
(`DATABASE_URL` с путём к файлу ещё принимается для совместимости). Модели
разнесены по ролям: `MODEL_DEFAULT` (`deepseek-v4-pro`) — стратег и критик,
`MODEL_FAST` (`deepseek-v4-flash`) — копирайтеры; привязка ролей — через
`SetRoleModel` (см. `backend/cmd/server/main.go`). Доступ к API закрыт
basic-auth (`BASIC_AUTH_USER`/`BASIC_AUTH_PASS`); `/healthz` всегда открыт.

**Фронтенд** — объявлены в `frontend/src/env.ts`:
- `BACKEND_URL` (приватная, рантайм) — адрес Go-API для прокси, по умолчанию
  `http://backend:8080`;
- `PUBLIC_BASE` (build-arg образа) — базовый путь приложения, по умолчанию `/`.

## Известное ограничение

Если процесс упадёт во время прогона, кампания останется в статусе `running`,
а работа LLM потеряется. Восстановление после сбоя — предмет Фазы 2 (частично
закрыто: прогресс персистится, при рестарте `RecoverInterrupted` помечает
зависшие прогоны как `failed`).
