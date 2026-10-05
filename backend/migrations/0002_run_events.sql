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
    id                VARCHAR(36) NOT NULL,
    run_id            VARCHAR(64) NOT NULL,
    seq               BIGINT NOT NULL,
    at                DATETIME(3) NOT NULL,
    kind              VARCHAR(32) NOT NULL,
    name              VARCHAR(64) NOT NULL,
    status            VARCHAR(32) NOT NULL,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    prompt_tokens     BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    summary           MEDIUMTEXT NOT NULL,
    payload           MEDIUMTEXT NULL,
    error             MEDIUMTEXT NULL,
    PRIMARY KEY (id),
    -- Лента читается по прогону в порядке появления событий.
    KEY idx_run_events_run (run_id, seq),
    -- Ретенция удаляет по времени.
    KEY idx_run_events_at (at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- migrate:down
DROP TABLE IF EXISTS run_events;
