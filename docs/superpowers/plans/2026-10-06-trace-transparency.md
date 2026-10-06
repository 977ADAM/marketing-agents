# План: прозрачность агентов в трассе

Spec: `docs/superpowers/specs/2026-10-06-trace-transparency-design.md`.

## Global Constraints

- Сначала тест, который воспроизводит пробел (RED), затем минимальная реализация (GREEN).
- Язык комментариев и документации — русский, как в проекте; слои не нарушать:
  domain не знает про adapter/transport, core не импортирует features.
- Дефолты меняются только там, где это описано в spec; поведение при явно заданном
  `summary`/`off` сохраняется.
- Секреты и тела промптов не попадают в логи; в тестах внешние API не вызываются.

## Task 0: Подготовка

- [x] Живой вызов `deepseek-v4-pro` из `backend/.env` подтвердил `reasoning_content`
      и `reasoning_tokens` в обычном и JSON-режиме (квота: два коротких запроса).
- [ ] Прочитать spec, свериться с текущим состоянием трассы и вьюера.

## Task 1: Дефолт `full`

- [ ] Тесты: пустой `TRACE_MODE` → `full` (config), `ParseMode("")` → `full`; явные
      `summary`/`off`/`full` не меняются.
- [ ] Реализовать дефолт в `core/config` и `trace/domain`; обновить `.env.example`.
- [ ] Предупреждение в лог при старте: `full` без `BASIC_AUTH_USER`.

## Task 2: Размышления модели

- [ ] Тест адаптера DeepSeek (httptest): ответ с `reasoning_content`,
      `reasoning_tokens`, `finish_reason` → поля `corellm.Usage` заполнены; пустые
      поля не ломают клиент.
- [ ] Добавить `Reasoning`, `ReasoningTokens`, `FinishReason` в `corellm.Usage`
      (аддитивно; `Add` суммирует только токены).
- [ ] Заполнять поля в `adapters/llm/deepseek`.

## Task 3: Тело LLM-события

- [ ] Тесты декоратора: payload содержит `reasoning`, `model`, `finish_reason`,
      `reasoning_tokens`; длинное поле обрезано и перечислено в `truncated_fields`,
      короткое — нет; `summary` содержит счётчик размышлений.
- [ ] Перенести `TruncateText`/`TruncatedMark` в `trace/domain`, убрать мёртвый
      `truncate` из recorder.go, сохранить совместимость `export_test.go`.
- [ ] Обрезать тела по отдельности (`trace.MaxBodyBytes`) в декораторе; поднять
      дефолт `TRACE_MAX_PAYLOAD_BYTES` до 256 КиБ.

## Task 4: Читаемый вьюер

- [ ] vitest на `trace-payload.ts`: конверт `{data,truncated}`, `data`-строка
      (обрезанный конверт), извлечение секций, JSON-ответ как объект против текста.
- [ ] Реализовать `trace-payload.ts` и переписать вывод деталей в
      `TrajectoryPanel.svelte`: секции размышлений/промпта/запроса/ответа, метрики
      события, пометка обрезки, pretty-JSON для остальных видов.

## Task 5: Этапы в трассе

- [ ] Тесты: прогон кампании оставляет `phase`-события `researching`,
      `strategizing`, `producing`, `done`; падение — `failed`; проверка текстов —
      свои этапы.
- [ ] Эмитить `KindPhase` в workflow кампании и проверки рядом с обновлением
      прогресса.

## Task 6: Документация и проверка

- [ ] README: дефолт `full`, приватность, `reasoning_content`, обрезка по полям,
      новый дефолт `TRACE_MAX_PAYLOAD_BYTES`.
- [ ] `cd backend && go test ./...`, `go vet ./...`, `make test-race` для
      затронутых пакетов, `npm test`, `npm run check`, `npm run build`.
- [ ] Коммиты по задачам; в финале — `make verify` на живой MariaDB.
