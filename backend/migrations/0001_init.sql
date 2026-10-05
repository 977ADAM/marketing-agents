-- 0001_init.sql — схема БД (SQLite).
--
-- Особенности диалекта: UUID/JSONB заменены на TEXT (UUID генерирует Go),
-- TIMESTAMPTZ — на DATETIME со значением по умолчанию в формате, который
-- драйвер modernc.org/sqlite разбирает в time.Time ('YYYY-MM-DD HH:MM:SS.mmm').

-- migrate:up
CREATE TABLE IF NOT EXISTS clients (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now'))
);

-- дефолтный клиент для кампаний без явного client_id
INSERT INTO clients (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'default')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS campaigns (
    id         TEXT PRIMARY KEY,
    client_id  TEXT NOT NULL REFERENCES clients(id),
    status     TEXT NOT NULL,
    brief      TEXT NOT NULL,
    strategy   TEXT,
    progress   TEXT,
    cost_usd   REAL,
    error      TEXT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now'))
);

CREATE TABLE IF NOT EXISTS deliverables (
    id          TEXT PRIMARY KEY,
    campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    topic       TEXT NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    cta         TEXT NOT NULL,
    review      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now'))
);

-- Проверка готовых текстов агентами: отдельная таблица от кампаний-генераций.
CREATE TABLE IF NOT EXISTS reviews (
    id         TEXT PRIMARY KEY,
    client_id  TEXT NOT NULL REFERENCES clients(id),
    status     TEXT NOT NULL,
    brief_text TEXT NOT NULL,
    result     TEXT,
    progress   TEXT,
    cost_usd   REAL,
    error      TEXT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now'))
);

-- Списки истории всегда сортируются по created_at DESC, поэтому индексы по ним.
CREATE INDEX IF NOT EXISTS idx_campaigns_created_at ON campaigns (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reviews_created_at   ON reviews (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deliverables_campaign ON deliverables (campaign_id, created_at);

-- migrate:down
--
-- Порядок обратный созданию: сначала индексы и дочерние таблицы, потом родители
-- (deliverables ссылается на campaigns, campaigns и reviews — на clients).

DROP INDEX IF EXISTS idx_deliverables_campaign;
DROP INDEX IF EXISTS idx_reviews_created_at;
DROP INDEX IF EXISTS idx_campaigns_created_at;

DROP TABLE IF EXISTS deliverables;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS campaigns;
DROP TABLE IF EXISTS clients;
