# marketing-agents

Мультиагентный сервис генерации маркетинговых статей: бриф → стратег →
копирайтеры (по темам, параллельно) → критик → пакет статей. Go + DeepSeek.
Встроенный веб-интерфейс (React SPA): форма брифа, наблюдение за прогоном
кампании и история — всё в одном бинаре.

## Запуск

```bash
cp .env.example .env   # указать DEEPSEEK_API_KEY (+ BASIC_AUTH_USER/PASS для UI)
docker compose up -d --build
curl localhost:8080/healthz   # ok
```

Веб-интерфейс открывается на `http://localhost:8080/` (за basic-auth).

## API

- `POST /api/campaigns` — `{product, goal, audience, tone, client_id?}` → `202 {id, status}`
- `GET /api/campaigns/{id}` — статус и результат (когда `done`)
- `GET /api/campaigns` — список всех кампаний
- `GET /healthz`

## Пример

```bash
curl -XPOST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"product":"Эко-бутылка","goal":"рост продаж","audience":"ЗОЖ 25-40","tone":"дружелюбный"}'
```

## Веб-интерфейс

Фронтенд (React+Vite) лежит в `frontend/` и встраивается в бинарь через
`go:embed`. Сборка фронта пишет в `internal/web/dist`.

Локально:
```bash
cd frontend && npm ci && npm run build   # → internal/web/dist
cd .. && go build ./cmd/server
```

Dev-режим фронта (с прокси на :8080): `cd frontend && npm run dev`.

API доступен под префиксом `/api` (`POST /api/campaigns`,
`GET /api/campaigns`, `GET /api/campaigns/{id}`); `/healthz` — на корне.
Доступ к UI/API закрыт basic-auth (`BASIC_AUTH_USER`/`BASIC_AUTH_PASS`);
`/healthz` всегда открыт.

> Не коммитьте пересобранные файлы под `internal/web/dist/` — в репозитории
> держится только плейсхолдер `index.html`, а `assets/` в `.gitignore`.

## Тесты

```bash
go test ./...                                   # unit (Go)
cd frontend && npm test                         # фронтенд (Vitest)
docker compose up -d db
DATABASE_URL=postgres://app:app@localhost:5432/marketing?sslmode=disable \
  go test -tags=integration ./internal/store/   # интеграционные (стор)
```

## Конфигурация

Все настройки — через env, см. `.env.example`. Модель по умолчанию
`deepseek-v4-pro` для всех ролей; разнести модели по ролям можно через
`SetRoleModel` в LLM-клиенте.

## Известное ограничение

Если процесс упадёт во время прогона, кампания останется в статусе `running`,
а работа LLM потеряется. Восстановление после сбоя — предмет Фазы 2.
