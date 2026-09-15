-- PostgreSQL migration mirrored by runtime migration 22 in internal/app/migrations.go.
BEGIN;

ALTER TABLE members
    DROP CONSTRAINT IF EXISTS members_member_type_check;

UPDATE members
SET member_type = 'customer'
WHERE member_type = 'self_employed';

ALTER TABLE members
    ADD CONSTRAINT members_member_type_check
    CHECK (member_type IN ('employee', 'contract_worker', 'daily_worker', 'customer'));

INSERT INTO schema_migrations (version, name)
VALUES (22, 'expand_member_types');

COMMIT;
