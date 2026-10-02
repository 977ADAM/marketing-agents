# frontend — веб-интерфейс marketing-agents

SvelteKit 3 (Svelte 5, runes) в режиме SPA (`ssr = false`), адаптер
`@sveltejs/adapter-node`. Общая документация проекта — в [../README.md](../README.md).

## Роль в стеке

Фронт — единственная опубликованная точка входа: он отдаёт приложение и сам
проксирует `/api/*` и `/healthz` в Go-API:

- `src/routes/api/[...path]/+server.ts` — потоковый прокси на `BACKEND_URL`.
  Тело не буферизуется (иначе не работает SSE), хоп-бай-хоп заголовки не
  пересылаются. Загрузка `.docx` приходит сырыми байтами
  (`application/octet-stream`), а multipart для Go-API собирает прокси — так
  запрос не попадает под CSRF-защиту SvelteKit для form-запросов;
- `src/routes/healthz/+server.ts` — healthcheck фронта (проверяет доступность API).

## Команды

```bash
npm ci
npm run dev        # dev-сервер на :5173, /api и /healthz уходят на Go-API :8080
npm run build      # сборка в build/ (Node-сервер)
npm run preview    # локальный просмотр сборки
npm run check      # svelte-check: типы и Svelte-диагностики
npm test           # юнит-тесты (vitest), один прогон
npm run test:watch # юнит-тесты в watch-режиме
```

Для dev-режима нужен запущенный Go-API на `localhost:8080`
(`cd ../backend && go run ./cmd/server`): `/api` и `/healthz` проксирует Vite
(`server.proxy` в `vite.config.ts`).

Локальный запуск собранного приложения без Docker требует явного адреса API —
дефолт `BACKEND_URL` рассчитан на docker-compose (имя сервиса `backend`):

```bash
BACKEND_URL=http://localhost:8080 node build
```

## Структура

```
src/routes/         страницы: / (новая кампания), /campaigns/[id], /reviews, /reviews/[id]
src/routes/api/     прокси /api/* на Go-API
src/lib/api/        запросы к API и wire-типы (единственный источник правды по контракту)
src/lib/stores/     состояние: история сайдбара, прогон (run), живой прогресс (SSE), тосты
src/lib/components/ карточки статей/отчётов, панель прогресса, скелетоны, тостер
src/lib/styles/     токены, база и стили компонентов (обычный CSS)
src/test/           хелперы юнит-тестов (заглушка $app/paths, подмена EventSource)
```

## Правила, важные при доработке

- ссылки и переходы — только через `resolve()` из `$app/paths`;
- модули внутри `src/lib` импортируются как `#lib/...` с расширением
  (`#lib/api/client.js`) — алиас `$lib` в SvelteKit 3 удалён;
- бизнес-логики и валидации во фронте нет: всё, что можно посчитать (оценки,
  градации, сводки, разбор `.docx`), считает API, а ошибки приходят как
  `400 {error:{code,message}}` и показываются в форме.

## Тесты

Юнит-тесты (`vitest`, `environment: node`) покрывают модули без DOM:
`src/lib/api/client.ts`, `src/lib/stores/{progress,run,history}.ts`,
`src/lib/{labels,format}.ts`. Виртуальный модуль `$app/paths` подменяется
заглушкой `src/test/app-paths.stub.ts` (см. `vitest.config.ts`), сеть и
`EventSource` — моками. Компоненты Svelte юнит-тестами не покрыты.

`npm test` сначала выполняет `svelte-kit sync`: без сгенерированного
`node_modules/$app/tsconfig.json` трансформер не соберёт TypeScript.

## Публикация в подпуть

Сборка с `PUBLIC_BASE=/marketing/` (build-arg образа) — от неё зависят ссылки,
ассеты и префикс `/api`. Nginx при этом **не должен** срезать префикс: запросы
`/marketing/...` приходят в контейнер как есть, а префикс для API снимает SvelteKit.
