BEGIN;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('member','manager','ketua_i','ketua_ii','ketua_utama','super_admin'));
CREATE UNIQUE INDEX idx_users_one_active_super_admin ON users(role)
WHERE role = 'super_admin' AND active = TRUE AND historical_identity = FALSE;

CREATE TABLE super_admin_overrides (
    id TEXT PRIMARY KEY,
    request_type TEXT NOT NULL CHECK (request_type IN ('loan','withdrawal')),
    request_id TEXT NOT NULL,
    actor_id TEXT NOT NULL REFERENCES users(id),
    previous_stage TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (request_type, request_id)
);

CREATE INDEX idx_super_admin_overrides_request
    ON super_admin_overrides(request_type, request_id, created_at);

CREATE TABLE admin_audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES users(id),
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status INTEGER NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_admin_audit_events_actor_created
    ON admin_audit_events(actor_id, created_at, id);

ALTER TABLE super_admin_overrides ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_audit_events ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION protect_super_admin_audit_tables()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        RAISE EXCEPTION 'super admin audit records are append-only';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM public.users
        WHERE id = NEW.actor_id
          AND role = 'super_admin'
          AND active = TRUE
          AND historical_identity = FALSE
    ) THEN
        RAISE EXCEPTION 'super admin actor is required';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER super_admin_overrides_append_only
BEFORE INSERT OR UPDATE OR DELETE ON super_admin_overrides
FOR EACH ROW EXECUTE FUNCTION protect_super_admin_audit_tables();

CREATE TRIGGER admin_audit_events_append_only
BEFORE INSERT OR UPDATE OR DELETE ON admin_audit_events
FOR EACH ROW EXECUTE FUNCTION protect_super_admin_audit_tables();

DROP TRIGGER IF EXISTS protect_proposed_loan_terms_identity ON loan_requests;
DROP FUNCTION IF EXISTS protect_proposed_loan_terms_identity();

CREATE FUNCTION protect_proposed_loan_terms_identity()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    IF OLD.proposed_admin_fee_policy IS NOT NULL
       AND (OLD.loan_type IS DISTINCT FROM NEW.loan_type
            OR OLD.legacy_terms IS DISTINCT FROM NEW.legacy_terms) THEN
        RAISE EXCEPTION 'loan terms identity is immutable after snapshot';
    END IF;
    IF OLD.proposed_admin_fee_policy IS NULL
       AND NEW.proposed_admin_fee_policy IS NOT NULL
       AND (OLD.status <> 'pending' OR OLD.current_approval_stage <> 'manager')
       AND NOT EXISTS (
           SELECT 1
           FROM public.super_admin_overrides o
           WHERE o.request_type = 'loan'
             AND o.request_id = OLD.id
             AND o.decision = 'approved'
       ) THEN
        RAISE EXCEPTION 'proposed loan admin fee snapshot must be assigned at Manager stage';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER protect_proposed_loan_terms_identity
BEFORE UPDATE OF loan_type, legacy_terms, proposed_admin_fee_policy ON loan_requests
FOR EACH ROW EXECUTE FUNCTION protect_proposed_loan_terms_identity();

DROP TRIGGER IF EXISTS validate_loan_request_state_integrity ON loan_requests;
DROP FUNCTION IF EXISTS validate_loan_request_state_integrity();

CREATE FUNCTION validate_loan_request_state_integrity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    approved_current BOOLEAN;
    approved_manager BOOLEAN;
    override_decision BOOLEAN;
BEGIN
    IF OLD.legacy_terms = FALSE THEN
        IF NEW.legacy_terms <> FALSE THEN
            RAISE EXCEPTION 'legacy loan terms are migration-only';
        END IF;
        IF NEW.status = 'pending'
           AND (NEW.current_approval_stage IS NULL
                OR NEW.current_approval_stage NOT IN ('manager','ketua_i','ketua_ii','ketua_utama')
                OR (NEW.current_approval_stage <> 'manager' AND NEW.proposed_admin_fee_policy IS NULL)) THEN
            RAISE EXCEPTION 'invalid loan request approval state';
        END IF;
        IF NEW.status = 'approved'
           AND (NEW.current_approval_stage IS NOT NULL OR NEW.proposed_admin_fee_policy IS NULL) THEN
            RAISE EXCEPTION 'invalid approved loan request state';
        END IF;
        IF NEW.status IN ('rejected','cancelled') AND NEW.current_approval_stage IS NOT NULL THEN
            RAISE EXCEPTION 'invalid terminal loan request state';
        END IF;

        SELECT EXISTS (
            SELECT 1 FROM loan_request_approvals a
            WHERE a.request_id = OLD.id
              AND a.stage = OLD.current_approval_stage
              AND a.decision = 'approved'
        ) INTO approved_current;
        SELECT EXISTS (
            SELECT 1 FROM super_admin_overrides o
            WHERE o.request_type = 'loan'
              AND o.request_id = OLD.id
              AND o.decision = NEW.status
        ) INTO override_decision;

        IF NOT (
            (OLD.status = 'pending' AND NEW.status = 'pending'
                AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
            OR (OLD.status = 'pending' AND NEW.status = 'pending'
                AND ((OLD.current_approval_stage = 'manager' AND NEW.current_approval_stage = 'ketua_ii')
                    OR (OLD.current_approval_stage = 'ketua_ii' AND NEW.current_approval_stage = 'ketua_i')
                    OR (OLD.current_approval_stage = 'ketua_i' AND NEW.current_approval_stage = 'ketua_utama'))
                AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status IN ('approved','rejected') AND override_decision)
            OR (OLD.status = 'pending' AND NEW.status = 'approved'
                AND OLD.current_approval_stage = 'ketua_utama' AND approved_current)
            OR (OLD.status = 'pending' AND NEW.status = 'rejected'
                AND EXISTS (
                    SELECT 1 FROM loan_request_approvals a
                    WHERE a.request_id = OLD.id
                      AND a.stage = OLD.current_approval_stage
                      AND a.decision = 'rejected'
                ))
            OR (OLD.status = 'pending' AND NEW.status = 'cancelled')
            OR (OLD.status <> 'pending' AND OLD.status = NEW.status
                AND OLD.current_approval_stage IS NOT DISTINCT FROM NEW.current_approval_stage)
        ) THEN
            RAISE EXCEPTION 'invalid loan request state transition';
        END IF;

        SELECT EXISTS (
            SELECT 1 FROM loan_request_approvals a
            WHERE a.request_id = NEW.id
              AND a.stage = 'manager'
              AND a.decision = 'approved'
        ) INTO approved_manager;
        IF NEW.proposed_admin_fee_policy IS NOT NULL
           AND (NEW.current_approval_stage <> 'manager' OR NEW.status <> 'pending')
           AND NOT approved_manager
           AND NOT EXISTS (
               SELECT 1 FROM super_admin_overrides o
               WHERE o.request_type = 'loan'
                 AND o.request_id = NEW.id
                 AND o.decision = 'approved'
           ) THEN
            RAISE EXCEPTION 'Manager approval is required for snapshotted terms';
        END IF;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER validate_loan_request_state_integrity
BEFORE UPDATE OF status, current_approval_stage, legacy_terms,
    proposed_admin_fee_policy, proposed_monthly_admin_fee,
    proposed_total_admin_fee, proposed_total_obligation,
    proposed_approved_amount, proposed_duration_months, proposed_start_date
ON loan_requests FOR EACH ROW EXECUTE FUNCTION validate_loan_request_state_integrity();

DROP TRIGGER IF EXISTS validate_loan_request_provenance ON loans;
DROP FUNCTION IF EXISTS validate_loan_request_provenance();

CREATE FUNCTION validate_loan_request_provenance()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    override_approved BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM super_admin_overrides o
        WHERE o.request_type = 'loan'
          AND o.request_id = NEW.loan_request_id
          AND o.decision = 'approved'
    ) INTO override_approved;

    IF NEW.legacy_terms = FALSE
       AND NOT override_approved
       AND (
           NOT EXISTS (
               SELECT 1 FROM loan_requests r
               WHERE r.id = NEW.loan_request_id
                 AND r.status = 'approved'
                 AND r.current_approval_stage IS NULL
                 AND r.legacy_terms = FALSE
                 AND r.member_id = NEW.member_id
                 AND r.loan_type = NEW.loan_type
                 AND r.proposed_approved_amount = NEW.approved_amount
                 AND r.proposed_duration_months = NEW.duration_months
                 AND r.proposed_start_date = NEW.start_date
                 AND r.proposed_admin_fee_policy = NEW.admin_fee_policy
                 AND r.proposed_monthly_admin_fee IS NOT DISTINCT FROM NEW.monthly_admin_fee
                 AND r.proposed_total_admin_fee = NEW.total_admin_fee
                 AND r.proposed_total_obligation = NEW.total_obligation
           )
           OR NEW.remaining_balance <> NEW.total_obligation
           OR (SELECT COUNT(*) FROM loan_request_approvals a
               WHERE a.request_id = NEW.loan_request_id
                 AND a.decision = 'approved'
                 AND a.stage IN ('manager','ketua_i','ketua_ii','ketua_utama')) <> 4
           OR NOT EXISTS (
               SELECT 1 FROM loan_request_approvals a
               WHERE a.request_id = NEW.loan_request_id
                 AND a.stage = 'ketua_utama'
                 AND a.decision = 'approved'
                 AND a.officer_id = NEW.approved_by
           )
       ) THEN
        RAISE EXCEPTION 'loan must match a fully approved loan request';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER validate_loan_request_provenance
BEFORE INSERT ON loans
FOR EACH ROW EXECUTE FUNCTION validate_loan_request_provenance();

INSERT INTO schema_migrations (version, name)
VALUES (28, 'add_super_admin_support');

COMMIT;
