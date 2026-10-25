BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE withdrawal_requests ADD COLUMN disbursement_date TEXT NOT NULL DEFAULT '';
ALTER TABLE withdrawal_requests ADD COLUMN disbursed_by TEXT NULL REFERENCES users(id);
ALTER TABLE withdrawal_requests ADD COLUMN disbursed_at TIMESTAMP NULL;

UPDATE withdrawal_requests wr
SET disbursement_date=sr.record_date,
    disbursed_by=sr.recorded_by,
    disbursed_at=sr.created_at
FROM saving_records sr
WHERE wr.saving_record_id=sr.id AND wr.status='approved';

CREATE OR REPLACE FUNCTION validate_loan_request_amount_against_savings() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE AND COALESCE(NEW.creation_source,'member')<>'admin' THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.requested_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN
            RAISE EXCEPTION 'loan amount exceeds the member-type limit';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION validate_loan_proposed_amount_against_savings() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE AND COALESCE(NEW.creation_source,'member')<>'admin' AND NEW.proposed_approved_amount IS NOT NULL THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.proposed_approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN
            RAISE EXCEPTION 'approved loan amount exceeds the member-type limit';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION validate_loan_approved_amount_against_savings() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE AND NOT EXISTS (
        SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND COALESCE(r.creation_source,'member')='admin'
    ) THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN
            RAISE EXCEPTION 'approved loan amount exceeds the member-type limit';
        END IF;
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS withdrawal_requests_disbursement_guard ON withdrawal_requests;
DROP FUNCTION IF EXISTS validate_withdrawal_request_disbursement();
CREATE FUNCTION validate_withdrawal_request_disbursement() RETURNS trigger
LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
    IF OLD.status<>'approved' OR NEW.status<>'approved'
        OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
        OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL OR OLD.saving_record_id IS NOT NULL
        OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL OR NEW.saving_record_id IS NULL
        OR NOT EXISTS (
            SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id
            WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE
        )
        OR NOT EXISTS (
            SELECT 1 FROM saving_records sr
            WHERE sr.id=NEW.saving_record_id AND sr.member_id=NEW.member_id AND sr.type='withdrawal'
              AND sr.category='sukarela' AND sr.amount=NEW.amount AND sr.record_date=NEW.disbursement_date
              AND sr.recorded_by=NEW.disbursed_by
        )
        OR NOT EXISTS (
            SELECT 1 FROM withdrawal_reservations r
            WHERE r.request_id=NEW.id AND r.member_id=NEW.member_id AND r.amount=NEW.amount AND r.status='active'
        )
    THEN
        RAISE EXCEPTION 'withdrawal request is not ready for a treasurer disbursement';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER withdrawal_requests_disbursement_guard
    BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at,saving_record_id ON withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION validate_withdrawal_request_disbursement();

COMMIT;
