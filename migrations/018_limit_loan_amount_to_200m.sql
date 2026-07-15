DROP INDEX IF EXISTS idx_loans_one_active_per_member;

ALTER TABLE loan_requests
    ADD CONSTRAINT loan_requests_requested_amount_max
    CHECK (requested_amount <= 200000000) NOT VALID;

ALTER TABLE loan_requests
    ADD CONSTRAINT loan_requests_proposed_approved_amount_max
    CHECK (proposed_approved_amount IS NULL OR proposed_approved_amount <= 200000000) NOT VALID;

ALTER TABLE loans
    ADD CONSTRAINT loans_approved_amount_max
    CHECK (approved_amount <= 200000000) NOT VALID;
