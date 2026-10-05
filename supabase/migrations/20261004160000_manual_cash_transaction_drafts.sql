BEGIN;
SET LOCAL statement_timeout = '30s';

CREATE TABLE public.manual_cash_transaction_drafts (
    id TEXT PRIMARY KEY,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('cash','journal')),
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','approved')),
    recorded_by TEXT NOT NULL REFERENCES public.users(id),
    approved_by TEXT NULL REFERENCES public.users(id),
    transaction_id TEXT NULL UNIQUE REFERENCES public.manual_cash_transactions(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_at TIMESTAMP NULL,
    CHECK ((status='pending' AND approved_by IS NULL AND transaction_id IS NULL AND approved_at IS NULL) OR (status='approved' AND approved_by IS NOT NULL AND transaction_id IS NOT NULL AND approved_at IS NOT NULL))
);

CREATE INDEX idx_manual_cash_transaction_drafts_pending
    ON public.manual_cash_transaction_drafts(entry_type,status,created_at);

ALTER TABLE public.manual_cash_transaction_drafts ENABLE ROW LEVEL SECURITY;

INSERT INTO public.schema_migrations (version, name)
VALUES (36, 'manual_cash_transaction_drafts');

COMMIT;
