BEGIN;

ALTER TABLE members ADD COLUMN IF NOT EXISTS old_npp TEXT;

INSERT INTO schema_migrations (version, name)
VALUES (38, 'add_old_npp_to_members');

COMMIT;
