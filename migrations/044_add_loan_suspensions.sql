BEGIN;
CREATE TABLE loan_suspensions (
    id TEXT PRIMARY KEY,
    loan_id TEXT NOT NULL REFERENCES loans(id),
    member_id TEXT NOT NULL REFERENCES members(id),
    statement_month TEXT NOT NULL,
    fee_amount BIGINT NOT NULL CHECK (fee_amount > 0),
    record_date TEXT NOT NULL,
    reason TEXT NOT NULL,
    recorded_by TEXT NOT NULL REFERENCES users(id),
    before_schedule TEXT NOT NULL,
    before_final_due_date TEXT NOT NULL,
    after_final_due_date TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (loan_id, statement_month)
);
ALTER TABLE loan_suspensions ENABLE ROW LEVEL SECURITY;
COMMIT;
