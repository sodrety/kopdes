BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE loan_requests ADD COLUMN disbursement_date TEXT NOT NULL DEFAULT '';
ALTER TABLE loan_requests ADD COLUMN disbursed_by TEXT NULL REFERENCES users(id);
ALTER TABLE loan_requests ADD COLUMN disbursed_at TIMESTAMP NULL;
UPDATE loan_requests
SET disbursement_date=(SELECT start_date FROM loans WHERE loans.loan_request_id=loan_requests.id),
    disbursed_by=(SELECT approved_by FROM loans WHERE loans.loan_request_id=loan_requests.id),
    disbursed_at=(SELECT approved_at FROM loans WHERE loans.loan_request_id=loan_requests.id)
WHERE EXISTS (SELECT 1 FROM loans WHERE loans.loan_request_id=loan_requests.id);
DROP INDEX IF EXISTS idx_loan_requests_one_pending_per_member;
CREATE UNIQUE INDEX idx_loan_requests_one_pending_per_member
    ON loan_requests(member_id)
    WHERE status='pending' OR (status='approved' AND disbursement_date='');

CREATE OR REPLACE VIEW member_loan_amount_limits AS
    SELECT m.id AS member_id,
        (CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
            * GREATEST(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
            + GREATEST(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0) AS loan_limit
    FROM members m LEFT JOIN saving_records sr ON sr.member_id=m.id
    GROUP BY m.id,m.member_type;

CREATE OR REPLACE FUNCTION validate_loan_request_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.requested_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'loan amount exceeds the member-type limit'; END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION validate_loan_proposed_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE AND NEW.proposed_approved_amount IS NOT NULL THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.proposed_approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION validate_loan_approved_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE member_loan_limit NUMERIC;
BEGIN
    IF NEW.legacy_terms=FALSE THEN
        SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
        IF NEW.approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS validate_loan_request_state_integrity ON loan_requests;
DROP FUNCTION IF EXISTS validate_loan_request_state_integrity();
CREATE FUNCTION validate_loan_request_state_integrity() RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
DECLARE
    approved_current BOOLEAN;
    approved_manager BOOLEAN;
    override_decision BOOLEAN;
BEGIN
    IF OLD.legacy_terms=FALSE THEN
        IF NEW.legacy_terms<>FALSE THEN RAISE EXCEPTION 'legacy loan terms are migration-only'; END IF;
        IF NEW.status='pending' AND (NEW.current_approval_stage IS NULL OR NEW.current_approval_stage NOT IN ('manager','ketua_i','ketua_ii','ketua_utama') OR (NEW.current_approval_stage<>'manager' AND NEW.proposed_admin_fee_policy IS NULL)) THEN RAISE EXCEPTION 'invalid loan request approval state'; END IF;
        IF NEW.status='approved' AND (NEW.current_approval_stage IS NOT NULL OR NEW.proposed_admin_fee_policy IS NULL) THEN RAISE EXCEPTION 'invalid approved loan request state'; END IF;
        IF NEW.status IN ('rejected','cancelled') AND NEW.current_approval_stage IS NOT NULL THEN RAISE EXCEPTION 'invalid terminal loan request state'; END IF;

        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage=OLD.current_approval_stage AND a.decision='approved') INTO approved_current;
        SELECT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=OLD.id AND o.decision=NEW.status) INTO override_decision;
        IF NOT (
            (OLD.status='pending' AND NEW.status='pending' AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
            OR (OLD.status='pending' AND NEW.status='pending' AND ((OLD.current_approval_stage='manager' AND NEW.current_approval_stage='ketua_ii') OR (OLD.current_approval_stage='ketua_ii' AND NEW.current_approval_stage='ketua_i') OR (OLD.current_approval_stage='ketua_i' AND NEW.current_approval_stage='ketua_utama')) AND approved_current)
            OR (OLD.status='pending' AND NEW.status IN ('approved','rejected') AND override_decision)
            OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_i' AND NEW.proposed_approved_amount<20000000 AND approved_current)
            OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND NEW.proposed_approved_amount>=20000000 AND approved_current)
            OR (OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage=OLD.current_approval_stage AND a.decision='rejected'))
            OR (OLD.status='pending' AND NEW.status='cancelled')
            OR (OLD.status<>'pending' AND OLD.status=NEW.status AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
        ) THEN RAISE EXCEPTION 'invalid loan request state transition'; END IF;

        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') INTO approved_manager;
        IF NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.current_approval_stage<>'manager' OR NEW.status<>'pending') AND NOT approved_manager AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN RAISE EXCEPTION 'Manager approval is required for snapshotted terms'; END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_loan_request_state_integrity
    BEFORE UPDATE OF status,current_approval_stage,legacy_terms,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation,proposed_approved_amount,proposed_duration_months,proposed_start_date
    ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_loan_request_state_integrity();

DROP TRIGGER IF EXISTS loan_requests_disbursement_guard ON loan_requests;
DROP FUNCTION IF EXISTS validate_loan_request_disbursement();
CREATE FUNCTION validate_loan_request_disbursement() RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
    IF OLD.legacy_terms=FALSE AND (
        OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
        OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL
        OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL
        OR NOT EXISTS (SELECT 1 FROM users u WHERE u.id=NEW.disbursed_by AND u.role='bendahara' AND u.active=TRUE AND u.historical_identity=FALSE)
        OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)
    ) THEN RAISE EXCEPTION 'loan request is not ready for a treasurer disbursement'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER loan_requests_disbursement_guard
    BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at ON loan_requests
    FOR EACH ROW EXECUTE FUNCTION validate_loan_request_disbursement();

DROP TRIGGER IF EXISTS validate_loan_request_provenance ON loans;
DROP FUNCTION IF EXISTS validate_loan_request_provenance();
CREATE FUNCTION validate_loan_request_provenance() RETURNS trigger LANGUAGE plpgsql SET search_path = public, pg_temp AS $$
BEGIN
    IF NEW.legacy_terms=FALSE AND (
        NOT EXISTS (
            SELECT 1 FROM loan_requests r
            WHERE r.id=NEW.loan_request_id AND r.status='approved' AND r.current_approval_stage IS NULL AND r.legacy_terms=FALSE
              AND r.member_id=NEW.member_id AND r.loan_type=NEW.loan_type
              AND r.proposed_approved_amount=NEW.approved_amount AND r.proposed_duration_months=NEW.duration_months
              AND r.disbursement_date=NEW.start_date AND r.disbursed_by IS NOT NULL AND r.disbursed_at IS NOT NULL
              AND r.reviewed_by=NEW.approved_by AND r.proposed_admin_fee_policy=NEW.admin_fee_policy
              AND r.proposed_monthly_admin_fee IS NOT DISTINCT FROM NEW.monthly_admin_fee
              AND r.proposed_total_admin_fee=NEW.total_admin_fee AND r.proposed_total_obligation=NEW.total_obligation
        )
        OR NEW.remaining_balance<>NEW.total_obligation
        OR (
            NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved')
            AND NOT (
                (NEW.approved_amount<20000000
                    AND (SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.decision='approved')=3
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='manager' AND a.decision='approved')
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_ii' AND a.decision='approved')
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a JOIN loan_requests r ON r.id=a.request_id WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_i' AND a.decision='approved' AND a.officer_id=r.reviewed_by)
                    AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_utama'))
                OR (NEW.approved_amount>=20000000
                    AND (SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.decision='approved')=4
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='manager' AND a.decision='approved')
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_ii' AND a.decision='approved')
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_i' AND a.decision='approved')
                    AND EXISTS (SELECT 1 FROM loan_request_approvals a JOIN loan_requests r ON r.id=a.request_id WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_utama' AND a.decision='approved' AND a.officer_id=r.reviewed_by))
            )
        )
    ) THEN RAISE EXCEPTION 'loan must match a fully approved and disbursed loan request'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER validate_loan_request_provenance
    BEFORE INSERT ON loans FOR EACH ROW EXECUTE FUNCTION validate_loan_request_provenance();

INSERT INTO schema_migrations (version,name)
VALUES (34,'member_type_loan_limits_and_disbursement');

COMMIT;
