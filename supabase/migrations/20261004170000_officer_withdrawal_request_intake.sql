BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.withdrawal_requests ADD COLUMN created_by TEXT NULL REFERENCES public.users(id);
ALTER TABLE public.withdrawal_requests ADD COLUMN creation_source TEXT NOT NULL DEFAULT 'member' CHECK (creation_source IN ('member','officer'));

INSERT INTO public.schema_migrations (version, name)
VALUES (37, 'officer_withdrawal_request_intake');

COMMIT;
