-- PostgreSQL mirror of runtime migration 22 in internal/app/migrations.go.
BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.members
    DROP CONSTRAINT IF EXISTS members_member_type_check;

UPDATE public.members
SET member_type = 'customer'
WHERE member_type = 'self_employed';

ALTER TABLE public.members
    ADD CONSTRAINT members_member_type_check
    CHECK (member_type IN ('employee', 'contract_worker', 'daily_worker', 'customer'));

INSERT INTO public.schema_migrations (version, name)
VALUES (22, 'expand_member_types');

COMMIT;
