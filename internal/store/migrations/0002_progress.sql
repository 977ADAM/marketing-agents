-- 0002_progress.sql
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS progress JSONB;
