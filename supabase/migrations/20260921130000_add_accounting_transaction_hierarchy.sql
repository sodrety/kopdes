BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.saving_records ADD COLUMN source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank'));
ALTER TABLE public.saving_records ADD COLUMN coa_code TEXT;
ALTER TABLE public.loan_repayments ADD COLUMN source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank'));
ALTER TABLE public.loan_repayments ADD COLUMN coa_code TEXT;
ALTER TABLE public.loan_repayment_events ADD COLUMN source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank'));
ALTER TABLE public.loans ADD COLUMN source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank'));
ALTER TABLE public.loans ADD COLUMN coa_code TEXT;
ALTER TABLE public.manual_cash_transactions ADD COLUMN source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank'));
ALTER TABLE public.manual_cash_transactions ADD COLUMN coa_code TEXT;
ALTER TABLE public.manual_cash_transactions ADD COLUMN accounting_direction TEXT CHECK (accounting_direction IN ('debit','credit'));
ALTER TABLE public.manual_cash_transactions ALTER COLUMN category_id DROP NOT NULL;
UPDATE public.manual_cash_transactions SET accounting_direction = CASE direction WHEN 'cash_in' THEN 'debit' WHEN 'cash_out' THEN 'credit' END WHERE accounting_direction IS NULL;

CREATE TABLE public.coa_accounts (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    parent_code TEXT NULL REFERENCES public.coa_accounts(code),
    account_type TEXT NOT NULL CHECK (account_type IN ('asset','liability','equity','revenue','expense')),
    subtype TEXT NOT NULL DEFAULT '',
    normal_balance TEXT NOT NULL CHECK (normal_balance IN ('D','C')),
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    system_key TEXT NULL UNIQUE,
    created_by TEXT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_coa_accounts_parent ON public.coa_accounts(parent_code);
CREATE INDEX idx_coa_accounts_active_posting ON public.coa_accounts(active,is_group,code);
INSERT INTO public.coa_accounts (id,code,name,account_type,subtype,normal_balance,is_group,active,system_key)
VALUES ('coa-system-cash','CASH','Kas','asset','cash','D',FALSE,TRUE,'CASH')
ON CONFLICT (code) DO NOTHING;
INSERT INTO public.coa_accounts (id,code,name,account_type,subtype,normal_balance,is_group,active,system_key)
VALUES ('coa-system-bank','BANK','Bank','asset','bank','D',FALSE,TRUE,'BANK')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE public.accounting_mappings (
    id TEXT PRIMARY KEY,
    mapping_key TEXT NOT NULL UNIQUE,
    transaction_type TEXT NOT NULL,
    component TEXT NOT NULL,
    loan_type TEXT NOT NULL DEFAULT '',
    coa_code TEXT NULL,
    effective_from TEXT NULL,
    effective_to TEXT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_accounting_mappings_lookup ON public.accounting_mappings(transaction_type,component,loan_type,effective_from,effective_to,active);

CREATE TABLE public.coa_import_batches (
    id TEXT PRIMARY KEY,
    file_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('draft','activated','rejected')),
    uploaded_by TEXT NOT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at TIMESTAMP NULL
);
CREATE TABLE public.coa_import_rows (
    id TEXT PRIMARY KEY,
    batch_id TEXT NOT NULL REFERENCES public.coa_import_batches(id),
    excel_row INTEGER NOT NULL,
    code TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    parent_code TEXT NOT NULL DEFAULT '',
    account_type TEXT NOT NULL DEFAULT '',
    subtype TEXT NOT NULL DEFAULT '',
    normal_balance TEXT NOT NULL DEFAULT '',
    is_group BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL CHECK (status IN ('valid','invalid')),
    error_message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_coa_import_rows_batch ON public.coa_import_rows(batch_id,excel_row);

CREATE TABLE public.financial_journal_entries (
    id TEXT PRIMARY KEY,
    reference_no TEXT NOT NULL,
    transaction_id TEXT NOT NULL,
    transaction_type TEXT NOT NULL,
    transaction_date TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('cash','bank')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('pending_mapping','posted')),
    batch_id TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    recorded_by TEXT NOT NULL REFERENCES public.users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_financial_journal_entries_date ON public.financial_journal_entries(transaction_date,created_at);
CREATE INDEX idx_financial_journal_entries_transaction ON public.financial_journal_entries(transaction_id,transaction_type);

CREATE TABLE public.financial_journal_lines (
    id TEXT PRIMARY KEY,
    journal_id TEXT NOT NULL REFERENCES public.financial_journal_entries(id),
    side TEXT NOT NULL CHECK (side IN ('debit','credit')),
    coa_code TEXT NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    component TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_financial_journal_lines_journal ON public.financial_journal_lines(journal_id,side,coa_code);

CREATE TABLE public.financial_transaction_audits (
    id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL,
    transaction_type TEXT NOT NULL,
    actor_id TEXT NOT NULL REFERENCES public.users(id),
    field_name TEXT NOT NULL,
    old_value TEXT NOT NULL DEFAULT '',
    new_value TEXT NOT NULL DEFAULT '',
    batch_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_financial_transaction_audits_transaction ON public.financial_transaction_audits(transaction_id,created_at);

ALTER TABLE public.coa_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.accounting_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.financial_journal_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.financial_journal_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.financial_transaction_audits ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.coa_import_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.coa_import_rows ENABLE ROW LEVEL SECURITY;

INSERT INTO public.schema_migrations (version, name)
VALUES (31, 'add_accounting_transaction_hierarchy');

COMMIT;
