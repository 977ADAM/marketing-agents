-- 0003_reviews.sql
-- Проверка готовых текстов агентами: отдельная таблица от кампаний-генераций.
CREATE TABLE IF NOT EXISTS reviews (
    id         UUID PRIMARY KEY,
    client_id  UUID NOT NULL REFERENCES clients(id),
    status     TEXT NOT NULL,
    brief_text TEXT NOT NULL,
    result     JSONB,
    progress   JSONB,
    cost_usd   NUMERIC,
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
