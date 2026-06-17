# marketing-agents

Мультиагентный сервис генерации маркетинговых статей: бриф → стратег →
копирайтеры (по темам, параллельно) → критик → пакет статей. Go + DeepSeek.

## Запуск

```bash
cp .env.example .env   # указать DEEPSEEK_API_KEY
docker compose up -d --build
curl localhost:8080/healthz   # ok
```

## API

- `POST /campaigns` — `{product, goal, audience, tone, client_id?}` → `202 {id, status}`
- `GET /campaigns/{id}` — статус и результат (когда `done`)
- `GET /healthz`

## Пример

```bash
curl -XPOST localhost:8080/campaigns -H 'Content-Type: application/json' \
  -d '{"product":"Эко-бутылка","goal":"рост продаж","audience":"ЗОЖ 25-40","tone":"дружелюбный"}'
```

## Тесты

```bash
go test ./...                                   # unit
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
