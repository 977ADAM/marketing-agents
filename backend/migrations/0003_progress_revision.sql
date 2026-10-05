-- migrate:up
ALTER TABLE campaigns ADD COLUMN progress_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE reviews ADD COLUMN progress_revision BIGINT NOT NULL DEFAULT 0;

-- migrate:down
ALTER TABLE reviews DROP COLUMN progress_revision;
ALTER TABLE campaigns DROP COLUMN progress_revision;
