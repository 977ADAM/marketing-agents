-- 0002_run_events.down.sql — откат журнала событий прогона.

DROP INDEX IF EXISTS idx_run_events_at;
DROP INDEX IF EXISTS idx_run_events_run;
DROP TABLE IF EXISTS run_events;
