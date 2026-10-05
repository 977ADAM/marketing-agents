-- migrate:up
CREATE TABLE run_checkpoints (
 run_kind VARCHAR(16) NOT NULL,
 run_id VARCHAR(36) NOT NULL,
 stage VARCHAR(32) NOT NULL,
 position INT NOT NULL,
 input_hash VARCHAR(64) NOT NULL,
 payload MEDIUMTEXT NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY(run_kind,run_id,stage,position)
);

-- migrate:down
DROP TABLE run_checkpoints;
