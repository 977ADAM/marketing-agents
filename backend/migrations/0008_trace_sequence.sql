-- migrate:up
CREATE TABLE run_event_sequences (
 run_id VARCHAR(64) NOT NULL PRIMARY KEY,
 last_seq BIGINT NOT NULL DEFAULT 0
);
INSERT INTO run_event_sequences (run_id,last_seq) SELECT run_id,MAX(seq) FROM run_events GROUP BY run_id;

-- migrate:down
DROP TABLE run_event_sequences;
