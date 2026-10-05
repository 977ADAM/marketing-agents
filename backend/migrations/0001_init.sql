-- 0001_init.sql — схема БД (MariaDB 11.4, InnoDB, utf8mb4).
--
-- Особенности диалекта: id — UUID, который генерирует Go, поэтому VARCHAR(36);
-- JSON-структуры (бриф, стратегия, прогресс, ревью) хранятся текстом, потому что
-- разбирает и собирает их Go, а не БД; время — DATETIME(3) в UTC (точность
-- миллисекунд, как раньше), поэтому и приложение, и сессия работают в UTC
-- (см. time_zone в DSN и --default-time-zone у сервера).
--
-- Текст: MEDIUMTEXT (16 МБ) там, где объём не наш (стратегия со всеми
-- кандидатами, черновики статей, JSON ревью), TEXT (64 КБ) — для полей, которые
-- не индексируются.

-- migrate:up
CREATE TABLE IF NOT EXISTS clients (
    id         VARCHAR(36) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- дефолтный клиент для кампаний без явного client_id
INSERT IGNORE INTO clients (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'default');

CREATE TABLE IF NOT EXISTS campaigns (
    id         VARCHAR(36) NOT NULL,
    -- seq — порядок вставки: тайбрейкер для записей с одинаковым created_at
    -- (точность — миллисекунды), иначе «новые сверху» в истории недетерминирован.
    seq        BIGINT NOT NULL AUTO_INCREMENT,
    client_id  VARCHAR(36) NOT NULL,
    status     VARCHAR(32) NOT NULL,
    brief      MEDIUMTEXT NOT NULL,
    strategy   MEDIUMTEXT NULL,
    progress   MEDIUMTEXT NULL,
    cost_usd   DOUBLE NULL,
    error      MEDIUMTEXT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_campaigns_seq (seq),
    -- Списки истории всегда сортируются по created_at DESC, поэтому индекс по ней.
    KEY idx_campaigns_created_at (created_at),
    CONSTRAINT fk_campaigns_client FOREIGN KEY (client_id) REFERENCES clients (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS deliverables (
    id          VARCHAR(36) NOT NULL,
    campaign_id VARCHAR(36) NOT NULL,
    -- position — порядок статей в медиаплане: created_at у них общий (вставка
    -- одной транзакцией), а порядок тем важен для выдачи.
    position    INT NOT NULL DEFAULT 0,
    topic       TEXT NOT NULL,
    title       TEXT NOT NULL,
    body        MEDIUMTEXT NOT NULL,
    cta         TEXT NOT NULL,
    review      MEDIUMTEXT NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_deliverables_campaign (campaign_id, position),
    CONSTRAINT fk_deliverables_campaign FOREIGN KEY (campaign_id)
        REFERENCES campaigns (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Проверка готовых текстов агентами: отдельная таблица от кампаний-генераций.
CREATE TABLE IF NOT EXISTS reviews (
    id         VARCHAR(36) NOT NULL,
    -- seq — порядок вставки, тайбрейкер для одинакового created_at (см. campaigns).
    seq        BIGINT NOT NULL AUTO_INCREMENT,
    client_id  VARCHAR(36) NOT NULL,
    status     VARCHAR(32) NOT NULL,
    brief_text MEDIUMTEXT NOT NULL,
    result     MEDIUMTEXT NULL,
    progress   MEDIUMTEXT NULL,
    cost_usd   DOUBLE NULL,
    error      MEDIUMTEXT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_reviews_seq (seq),
    KEY idx_reviews_created_at (created_at),
    CONSTRAINT fk_reviews_client FOREIGN KEY (client_id) REFERENCES clients (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- migrate:down
--
-- Порядок обратный созданию: сначала дочерние таблицы, потом родители
-- (deliverables ссылается на campaigns, campaigns и reviews — на clients).
-- Индексы уходят вместе с таблицами.

DROP TABLE IF EXISTS deliverables;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS campaigns;
DROP TABLE IF EXISTS clients;
