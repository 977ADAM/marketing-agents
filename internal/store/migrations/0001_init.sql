-- 0001_init.sql
CREATE TABLE IF NOT EXISTS clients (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- дефолтный клиент для кампаний без явного client_id
INSERT INTO clients (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'default')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS campaigns (
    id         UUID PRIMARY KEY,
    client_id  UUID NOT NULL REFERENCES clients(id),
    status     TEXT NOT NULL,
    brief      JSONB NOT NULL,
    strategy   JSONB,
    cost_usd   NUMERIC,
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deliverables (
    id          UUID PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    topic       TEXT NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    cta         TEXT NOT NULL,
    review      JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
