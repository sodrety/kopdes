ALTER TABLE members
    ADD COLUMN bank_name TEXT NOT NULL DEFAULT '';

ALTER TABLE members
    ADD COLUMN bank_account TEXT NOT NULL DEFAULT '';

ALTER TABLE loan_requests
    DROP CONSTRAINT IF EXISTS loan_requests_requested_amount_max;

ALTER TABLE loan_requests
    DROP CONSTRAINT IF EXISTS loan_requests_proposed_approved_amount_max;

ALTER TABLE loans
    DROP CONSTRAINT IF EXISTS loans_approved_amount_max;

CREATE OR REPLACE FUNCTION validate_loan_request_amount_against_savings()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    saving_balance NUMERIC;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount::NUMERIC ELSE -amount::NUMERIC END), 0)
    INTO saving_balance
    FROM saving_records
    WHERE member_id = NEW.member_id;

    IF NEW.requested_amount::NUMERIC > saving_balance * 4 THEN
        RAISE EXCEPTION 'loan amount exceeds four times saving balance';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS loan_requests_requested_amount_savings_limit ON loan_requests;

CREATE TRIGGER loan_requests_requested_amount_savings_limit
BEFORE INSERT OR UPDATE OF requested_amount, member_id ON loan_requests
FOR EACH ROW EXECUTE FUNCTION validate_loan_request_amount_against_savings();

CREATE OR REPLACE FUNCTION validate_loan_proposed_amount_against_savings()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    saving_balance NUMERIC;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount::NUMERIC ELSE -amount::NUMERIC END), 0)
    INTO saving_balance
    FROM saving_records
    WHERE member_id = NEW.member_id;

    IF NEW.proposed_approved_amount IS NOT NULL AND NEW.proposed_approved_amount::NUMERIC > saving_balance * 4 THEN
        RAISE EXCEPTION 'approved loan amount exceeds four times saving balance';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS loan_requests_proposed_amount_savings_limit ON loan_requests;

CREATE TRIGGER loan_requests_proposed_amount_savings_limit
BEFORE INSERT OR UPDATE OF proposed_approved_amount, member_id ON loan_requests
FOR EACH ROW EXECUTE FUNCTION validate_loan_proposed_amount_against_savings();

CREATE OR REPLACE FUNCTION validate_loan_approved_amount_against_savings()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    saving_balance NUMERIC;
BEGIN
    SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount::NUMERIC ELSE -amount::NUMERIC END), 0)
    INTO saving_balance
    FROM saving_records
    WHERE member_id = NEW.member_id;

    IF NEW.approved_amount::NUMERIC > saving_balance * 4 THEN
        RAISE EXCEPTION 'approved loan amount exceeds four times saving balance';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS loans_approved_amount_savings_limit ON loans;

CREATE TRIGGER loans_approved_amount_savings_limit
BEFORE INSERT OR UPDATE OF approved_amount, member_id ON loans
FOR EACH ROW EXECUTE FUNCTION validate_loan_approved_amount_against_savings();
