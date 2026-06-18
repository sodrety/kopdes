CREATE TABLE IF NOT EXISTS withdrawal_requests (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL,
    amount INTEGER NOT NULL CHECK (amount > 0),
    note TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by TEXT NULL,
    reviewed_at TIMESTAMP NULL,
    rejection_reason TEXT NOT NULL DEFAULT '',
    saving_record_id TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id),
    FOREIGN KEY (reviewed_by) REFERENCES users(id),
    FOREIGN KEY (saving_record_id) REFERENCES saving_records(id)
);
