-- migrate:up
ALTER TABLE campaigns ADD COLUMN lease_owner VARCHAR(36) NULL, ADD COLUMN lease_attempt BIGINT NOT NULL DEFAULT 0, ADD COLUMN lease_until DATETIME(3) NULL;
ALTER TABLE reviews ADD COLUMN lease_owner VARCHAR(36) NULL, ADD COLUMN lease_attempt BIGINT NOT NULL DEFAULT 0, ADD COLUMN lease_until DATETIME(3) NULL;

-- migrate:down
ALTER TABLE reviews DROP COLUMN lease_until, DROP COLUMN lease_attempt, DROP COLUMN lease_owner;
ALTER TABLE campaigns DROP COLUMN lease_until, DROP COLUMN lease_attempt, DROP COLUMN lease_owner;
