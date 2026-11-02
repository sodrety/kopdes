BEGIN;
SET LOCAL statement_timeout = '30s';
SET LOCAL search_path = public, pg_temp;

CREATE TABLE IF NOT EXISTS public.saving_record_removals (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES public.members(id),
    type TEXT NOT NULL CHECK (type IN ('deposit','withdrawal')),
    category TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('cash','bank')),
    coa_code TEXT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    record_date TEXT NOT NULL,
    reference_no TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    recorded_by TEXT NOT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL,
    removed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    removed_by TEXT NOT NULL REFERENCES public.users(id),
    removal_reason TEXT NOT NULL,
    source_journal_id TEXT NOT NULL DEFAULT '',
    reversal_journal_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_saving_record_removals_member_date
    ON public.saving_record_removals(member_id, record_date, created_at);

ALTER TABLE public.saving_record_removals ENABLE ROW LEVEL SECURITY;

INSERT INTO public.schema_migrations (version, name)
VALUES (50, 'add_super_admin_saving_removals')
ON CONFLICT (version) DO NOTHING;

COMMIT;
