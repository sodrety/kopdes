DROP TRIGGER IF EXISTS loan_requests_disbursement_guard ON loan_requests;
DROP FUNCTION IF EXISTS validate_loan_request_disbursement();
CREATE FUNCTION validate_loan_request_disbursement() RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
    IF OLD.legacy_terms=FALSE AND (
        OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
        OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL
        OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL
        OR NOT EXISTS (
            SELECT 1
            FROM users u
            JOIN officer_appointments oa ON oa.member_id=u.member_id
            WHERE u.id=NEW.disbursed_by
              AND u.active=TRUE
              AND u.historical_identity=FALSE
              AND oa.role='bendahara'
              AND oa.active=TRUE
        )
        OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)
    ) THEN RAISE EXCEPTION 'loan request is not ready for a treasurer disbursement'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER loan_requests_disbursement_guard
    BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at ON loan_requests
    FOR EACH ROW EXECUTE FUNCTION validate_loan_request_disbursement();
