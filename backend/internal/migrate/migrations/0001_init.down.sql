-- 0001_init.down.sql — откат начальной схемы.
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
