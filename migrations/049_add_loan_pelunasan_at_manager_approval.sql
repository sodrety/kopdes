ALTER TABLE loan_requests
    ADD COLUMN settle_outstanding_loans BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN loan_payoff_amount BIGINT NOT NULL DEFAULT 0 CHECK (loan_payoff_amount >= 0);
