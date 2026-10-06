# План: прозрачность агентов в трассе

Spec: `docs/superpowers/specs/2026-10-06-trace-transparency-design.md`.
Статус: выполнен (2026-10-06). Коммиты: `docs: plan trace transparency work`,
`feat: make trace full by default`, `feat: capture model reasoning in the run trace`,
`feat: show agent reasoning in the trace viewer`.

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
- [x] Прочитать spec, свериться с текущим состоянием трассы и вьюера.

## Task 1: Дефолт `full`

- [x] Тесты: пустой `TRACE_MODE` → `full` (config), `ParseMode("")` → `DefaultMode`.
- [x] Дефолт в `core/config` и `trace/domain` (`DefaultMode`), обновлён `.env.example`.
- [x] Предупреждение в лог при старте: `full` без `BASIC_AUTH_USER`.

## Task 2: Размышления модели

- [x] Тест адаптера DeepSeek: `reasoning_content`, `reasoning_tokens`, `finish_reason`
      → поля `corellm.Usage`; модель без размышлений не ломает клиент.
- [x] `Reasoning`, `ReasoningTokens`, `FinishReason` в `corellm.Usage` (аддитивно;
      `Add` суммирует только токены, как и раньше с `Response`).
- [x] Заполнение в `adapters/llm/deepseek`; `accounting` не теряет поля на ошибке записи.

## Task 3: Тело LLM-события

- [x] Тесты декоратора: payload содержит `reasoning`, `model`, `finish_reason`,
      `reasoning_tokens`; длинное поле обрезано и перечислено в `truncated_fields`.
- [x] `TruncateText`/`TruncatedMark`/`MaxBodyBytes` в `trace/domain`, мёртвый
      `truncate` из recorder.go убран, `export_test.go` берёт пометку из domain.
- [x] Обрезка тел по отдельности; дефолт `TRACE_MAX_PAYLOAD_BYTES` — 256 КиБ.

## Task 4: Читаемый вьюер

- [x] vitest на `trace-payload.ts` (9 тестов): конверт `{data,truncated}`, `data`-строка,
      секции, JSON-ответ против текста, пустое тело.
- [x] `trace-payload.ts` + переписанный вывод деталей в `TrajectoryPanel.svelte`
      (секции, метаданные вызова, пометка обрезки); стили секций в `components.css`.

## Task 5: Этапы в трассе

- [x] Тесты: `researching`, `strategizing`, `producing` (по статье), `failed`;
      этап проверки текстов `reviewing`. Тест-двойники `captureTrace` и `sinkSpy`
      получили мьютексы — события пишут параллельные статьи (было скрытое гонками
      место, всплыло на `go test -race`).
- [x] `KindPhase` эмитится в workflow кампании и проверки. Отличие от spec: этап
      `done` отдельным событием не пишется — успешный итог и так помечен
      `result`-событием, дублировать его незачем.

## Task 6: Документация и проверка

- [x] README: дефолт `full`, приватность и предупреждение о basic-auth,
      `reasoning_content`, обрезка по полям, новый дефолт `TRACE_MAX_PAYLOAD_BYTES`,
      форма тела события в API.
- [x] `make verify` (build + vet + check + Go-тесты на MariaDB + 53 фронтовых теста),
      `go test -race -count=3 ./tests/workflow/`.
- [x] Демонстрационный прогон без расходов: поддельный OpenAI-совместимый сервер
      (`.superpowers/fake-llm.py`) → кампания через реальный API → панель трассы
      со секцией «Размышления модели»; скриншот `.superpowers/trace.png`.
