CREATE TABLE IF NOT EXISTS loans (
    id TEXT PRIMARY KEY,
    loan_request_id TEXT NOT NULL UNIQUE,
    member_id TEXT NOT NULL,
    approved_amount INTEGER NOT NULL CHECK (approved_amount > 0),
    duration_months INTEGER NOT NULL CHECK (duration_months > 0),
    monthly_installment INTEGER NOT NULL CHECK (monthly_installment > 0),
    remaining_balance INTEGER NOT NULL CHECK (remaining_balance >= 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paid', 'cancelled')),
    approved_by TEXT NOT NULL,
    approved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (loan_request_id) REFERENCES loan_requests(id),
    FOREIGN KEY (member_id) REFERENCES members(id),
    FOREIGN KEY (approved_by) REFERENCES users(id)
);
