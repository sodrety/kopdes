CREATE TABLE IF NOT EXISTS loan_requests (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL,
    requested_amount INTEGER NOT NULL CHECK (requested_amount > 0),
    duration_months INTEGER NOT NULL CHECK (duration_months > 0),
    purpose TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by TEXT NULL,
    reviewed_at TIMESTAMP NULL,
    rejection_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (member_id) REFERENCES members(id),
    FOREIGN KEY (reviewed_by) REFERENCES users(id)
);
