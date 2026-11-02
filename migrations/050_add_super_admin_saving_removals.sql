CREATE TABLE IF NOT EXISTS saving_record_removals (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES members(id),
    type TEXT NOT NULL CHECK (type IN ('deposit','withdrawal')),
    category TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('cash','bank')),
    coa_code TEXT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    record_date TEXT NOT NULL,
    reference_no TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    recorded_by TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL,
    removed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    removed_by TEXT NOT NULL REFERENCES users(id),
    removal_reason TEXT NOT NULL,
    source_journal_id TEXT NOT NULL DEFAULT '',
    reversal_journal_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_saving_record_removals_member_date
    ON saving_record_removals(member_id, record_date, created_at);

ALTER TABLE saving_record_removals ENABLE ROW LEVEL SECURITY;
