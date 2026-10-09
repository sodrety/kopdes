BEGIN;

CREATE TABLE loan_repayment_audits (
    id TEXT PRIMARY KEY,
    repayment_id TEXT NOT NULL,
    loan_id TEXT NOT NULL REFERENCES loans(id),
    member_id TEXT NOT NULL REFERENCES members(id),
    action TEXT NOT NULL CHECK (action IN ('edited','removed')),
    reason TEXT NOT NULL,
    before_state TEXT NOT NULL,
    after_state TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_loan_repayment_audits_created
    ON loan_repayment_audits(created_at,id);
CREATE INDEX idx_loan_repayment_audits_repayment
    ON loan_repayment_audits(repayment_id,created_at,id);

ALTER TABLE loan_repayment_audits ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION protect_loan_repayment_audit_rows()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN
        RAISE EXCEPTION 'loan repayment audit rows are append-only';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM public.users
        WHERE id=NEW.actor_id
          AND role='super_admin'
          AND active=TRUE
          AND historical_identity=FALSE
    ) THEN
        RAISE EXCEPTION 'active super admin actor is required';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER loan_repayment_audits_append_only
BEFORE INSERT OR UPDATE OR DELETE ON loan_repayment_audits
FOR EACH ROW EXECUTE FUNCTION protect_loan_repayment_audit_rows();

INSERT INTO schema_migrations (version,name)
VALUES (40,'audit_super_admin_repayment_corrections');

COMMIT;
