-- PostgreSQL mirror of runtime migration 21 in internal/app/migrations.go.
-- Historical adjustment and penangguhan rows stay outside loan_repayments so
-- operational repayment totals remain unchanged.

CREATE TABLE IF NOT EXISTS public.loan_repayment_events (
    id TEXT PRIMARY KEY,
    loan_id TEXT NOT NULL REFERENCES public.loans(id),
    member_id TEXT NOT NULL REFERENCES public.members(id),
    event_type TEXT NOT NULL CHECK (event_type IN ('adjustment', 'suspension')),
    amount BIGINT NOT NULL CHECK (
        (event_type = 'adjustment' AND amount < 0)
        OR (event_type = 'suspension' AND amount = 0)
    ),
    record_date TEXT NOT NULL DEFAULT '',
    reference_no TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    recorded_by TEXT NOT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source_key TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_loan_repayment_events_member_date
    ON public.loan_repayment_events(member_id, record_date, created_at);

CREATE INDEX IF NOT EXISTS idx_loan_repayment_events_loan_date
    ON public.loan_repayment_events(loan_id, record_date, created_at);

INSERT INTO public.schema_migrations (version, name)
VALUES (21, 'create_historical_loan_repayment_events')
ON CONFLICT (version) DO NOTHING;
