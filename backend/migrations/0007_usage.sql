-- migrate:up
ALTER TABLE campaigns ADD COLUMN cost_known BOOLEAN NULL;
ALTER TABLE reviews ADD COLUMN cost_known BOOLEAN NULL;
CREATE TABLE run_usage (
 id VARCHAR(36) NOT NULL PRIMARY KEY,
 run_kind VARCHAR(16) NOT NULL,
 run_id VARCHAR(36) NOT NULL,
 attempt BIGINT NOT NULL,
 model VARCHAR(255) NOT NULL,
 role VARCHAR(64) NOT NULL,
 prompt_tokens BIGINT NOT NULL,
 completion_tokens BIGINT NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_run_usage (run_kind,run_id)
);

-- migrate:down
ALTER TABLE reviews DROP COLUMN cost_known;
ALTER TABLE campaigns DROP COLUMN cost_known;
DROP TABLE run_usage;
