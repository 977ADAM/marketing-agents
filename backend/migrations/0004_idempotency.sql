-- migrate:up
CREATE TABLE run_creation_keys (
 client_id VARCHAR(36) NOT NULL,
 run_kind VARCHAR(16) NOT NULL,
 request_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 input_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 run_id VARCHAR(36) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY(client_id,run_kind,request_key)
);

-- migrate:down
DROP TABLE run_creation_keys;
