-- 0002_run_events.sql — журнал событий прогона (трасса).
--
-- Append-only: события только добавляются, ретенция чистит их по времени.
-- Внешнего ключа на кампанию нет намеренно: события переживают удаление прогона и
-- не мешают ему, а вид прогона (кампания или проверка) известен из эндпоинта.
--
-- payload заполняется только в режиме full (тела промптов и ответов), поэтому
-- колонка nullable; summary есть всегда — по ней строится лента.

-- migrate:up
CREATE TABLE IF NOT EXISTS run_events (
    id                TEXT PRIMARY KEY,
    run_id            TEXT NOT NULL,
    seq               INTEGER NOT NULL,
    at                DATETIME NOT NULL,
    kind              TEXT NOT NULL,
    name              TEXT NOT NULL,
    status            TEXT NOT NULL,
    duration_ms       INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    summary           TEXT NOT NULL,
    payload           TEXT,
    error             TEXT
);

-- Лента читается по прогону в порядке появления событий.
CREATE INDEX IF NOT EXISTS idx_run_events_run ON run_events (run_id, seq);
-- Ретенция удаляет по времени.
CREATE INDEX IF NOT EXISTS idx_run_events_at ON run_events (at);

-- migrate:down
DROP INDEX IF EXISTS idx_run_events_at;
DROP INDEX IF EXISTS idx_run_events_run;
DROP TABLE IF EXISTS run_events;
