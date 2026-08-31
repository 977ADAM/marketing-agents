# marketing-agents

Мультиагентный сервис маркетингового контента: Go + DeepSeek. Два режима:

1. **Генерация кампании**: бриф → стратег → копирайтеры (по темам, параллельно) →
   критик → пакет статей.
2. **Проверка готовых текстов**: бриф + готовые статьи прогоняются через двух агентов —
   «соответствие брифу» (продукт/УТП/аудитория/запреты/слоган/возражения) и
   «корректность текста» (факты, орфография, стиль, противоречия) — с итоговым отчётом.

Встроенный веб-интерфейс (React SPA): форма брифа, наблюдение за прогоном кампании,
проверка текстов (вставка или загрузка .docx) и история — всё в одном бинаре.

## Запуск

```bash
cp .env.example .env   # указать DEEPSEEK_API_KEY (+ BASIC_AUTH_USER/PASS для UI)
docker compose up -d --build
curl localhost:8080/healthz   # ok
```

Веб-интерфейс открывается на `http://localhost:8080/` (за basic-auth).

## API

### Кампании (генерация)
- `POST /api/campaigns` — `{product, goal, audience, tone, client_id?}` → `202 {id, status}`
- `GET /api/campaigns/{id}` — статус и результат (когда `done`)
- `GET /api/campaigns/{id}/events` — SSE-поток живого прогресса прогона (по-темный)
- `GET /api/campaigns` — список всех кампаний

### Проверка готовых текстов
- `POST /api/reviews` — `{brief, texts: [{title, body}, ...]}` → `202 {id, status}`
- `GET /api/reviews/{id}` — статус и отчёт: по каждому тексту `compliance`/`quality`
  (оценки 0–100 + замечания), `overall = min(...)`, `verdict: pass|fix`
- `GET /api/reviews/{id}/events` — SSE-поток прогресса проверки
- `GET /api/reviews` — список проверок
- `POST /api/reviews/extract` — multipart `file` (.docx) → `{title, text}` (текст из
  `word/document.xml`, включая таблицы; первая строка — заголовок)

### Общее
- `GET /healthz`

## Пример

```bash
curl -XPOST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"product":"Эко-бутылка","goal":"рост продаж","audience":"ЗОЖ 25-40","tone":"дружелюбный"}'

# Проверка готового текста по брифу
curl -XPOST localhost:8080/api/reviews -H 'Content-Type: application/json' \
  -d '{"brief":"Продукт: шины Ikon. Запрет: не упоминать другие бренды.","texts":[{"title":"Статья","body":"..."}]}'
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

Все настройки — через env, см. `.env.example`. Модели разнесены по ролям:
`MODEL_DEFAULT` (`deepseek-v4-pro`) — стратег и критик, `MODEL_FAST`
(`deepseek-v4-flash`) — копирайтеры. Привязка ролей к моделям — через
`SetRoleModel` в LLM-клиенте (см. `cmd/server/main.go`).

## Известное ограничение

Если процесс упадёт во время прогона, кампания останется в статусе `running`,
а работа LLM потеряется. Восстановление после сбоя — предмет Фазы 2.
