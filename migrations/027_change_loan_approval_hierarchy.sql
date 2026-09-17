BEGIN;

CREATE TEMPORARY TABLE migration_27_remapped_loan_requests ON COMMIT DROP AS
SELECT id
FROM loan_requests
WHERE status = 'pending'
  AND current_approval_stage = 'ketua_i'
  AND NOT EXISTS (
      SELECT 1
      FROM loan_request_approvals a
      WHERE a.request_id = loan_requests.id
        AND a.stage = 'ketua_ii'
        AND a.decision = 'approved'
  );

DROP TRIGGER IF EXISTS validate_loan_request_state_integrity ON loan_requests;
DROP FUNCTION IF EXISTS validate_loan_request_state_integrity();

UPDATE loan_requests
SET current_approval_stage = 'ketua_ii', updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM migration_27_remapped_loan_requests);

CREATE FUNCTION validate_loan_request_state_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    approved_current BOOLEAN;
    approved_manager BOOLEAN;
BEGIN
    IF OLD.legacy_terms = FALSE THEN
        IF NEW.legacy_terms <> FALSE THEN
            RAISE EXCEPTION 'legacy loan terms are migration-only';
        END IF;
        IF NEW.status = 'pending' AND (NEW.current_approval_stage IS NULL OR NEW.current_approval_stage NOT IN ('manager','ketua_i','ketua_ii','ketua_utama') OR (NEW.current_approval_stage <> 'manager' AND NEW.proposed_admin_fee_policy IS NULL)) THEN
            RAISE EXCEPTION 'invalid loan request approval state';
        END IF;
        IF NEW.status = 'approved' AND (NEW.current_approval_stage IS NOT NULL OR NEW.proposed_admin_fee_policy IS NULL) THEN
            RAISE EXCEPTION 'invalid approved loan request state';
        END IF;
        IF NEW.status IN ('rejected','cancelled') AND NEW.current_approval_stage IS NOT NULL THEN
            RAISE EXCEPTION 'invalid terminal loan request state';
        END IF;
        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = OLD.id AND a.stage = OLD.current_approval_stage AND a.decision = 'approved') INTO approved_current;
        IF NOT (
            (OLD.status = 'pending' AND NEW.status = 'pending' AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
            OR (OLD.status = 'pending' AND NEW.status = 'pending' AND ((OLD.current_approval_stage = 'manager' AND NEW.current_approval_stage = 'ketua_ii') OR (OLD.current_approval_stage = 'ketua_ii' AND NEW.current_approval_stage = 'ketua_i') OR (OLD.current_approval_stage = 'ketua_i' AND NEW.current_approval_stage = 'ketua_utama')) AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status = 'approved' AND OLD.current_approval_stage = 'ketua_utama' AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status = 'rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = OLD.id AND a.stage = OLD.current_approval_stage AND a.decision = 'rejected'))
            OR (OLD.status = 'pending' AND NEW.status = 'cancelled')
            OR (OLD.status <> 'pending' AND OLD.status = NEW.status AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
        ) THEN
            RAISE EXCEPTION 'invalid loan request state transition';
        END IF;
        SELECT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id = NEW.id AND a.stage = 'manager' AND a.decision = 'approved') INTO approved_manager;
        IF NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.current_approval_stage <> 'manager' OR NEW.status <> 'pending') AND NOT approved_manager THEN
            RAISE EXCEPTION 'Manager approval is required for snapshotted terms';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER validate_loan_request_state_integrity
BEFORE UPDATE OF status, current_approval_stage, legacy_terms, proposed_admin_fee_policy, proposed_monthly_admin_fee, proposed_total_admin_fee, proposed_total_obligation, proposed_approved_amount, proposed_duration_months, proposed_start_date
ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_loan_request_state_integrity();

UPDATE notifications n
SET resolved_at = CURRENT_TIMESTAMP
FROM notification_events e
WHERE n.event_id = e.id
  AND n.resolved_at IS NULL
  AND e.request_type = 'loan'
  AND e.request_id IN (SELECT id FROM migration_27_remapped_loan_requests);

INSERT INTO notification_events (id, event_type, request_type, request_id, payload)
SELECT 'migration-27-loan-stage-event-' || id,
       'approval_stage_ready',
       'loan',
       id,
       '{"stage":"ketua_ii"}'
FROM migration_27_remapped_loan_requests;

INSERT INTO notifications (id, event_id, user_id, title_key, body_key, link, audience)
SELECT 'migration-27-loan-stage-notification-' || r.id || '-' || u.id,
       'migration-27-loan-stage-event-' || r.id,
       u.id,
       'notification_approval_title',
       'notification_approval_body',
       '/admin/loan-requests',
       'officer'
FROM migration_27_remapped_loan_requests r
JOIN officer_appointments oa ON oa.role = 'ketua_ii' AND oa.active = TRUE
JOIN members m ON m.id = oa.member_id AND m.status = 'active'
JOIN users u ON u.member_id = m.id AND u.historical_identity = FALSE AND u.active = TRUE;

INSERT INTO schema_migrations (version, name)
VALUES (27, 'change_loan_approval_hierarchy');

COMMIT;
