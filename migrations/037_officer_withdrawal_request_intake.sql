BEGIN;

ALTER TABLE withdrawal_requests ADD COLUMN created_by TEXT NULL REFERENCES users(id);
ALTER TABLE withdrawal_requests ADD COLUMN creation_source TEXT NOT NULL DEFAULT 'member' CHECK (creation_source IN ('member','officer'));

INSERT INTO schema_migrations (version, name)
VALUES (37, 'officer_withdrawal_request_intake');

COMMIT;
