CREATE TABLE member_deactivation_requests (
    id TEXT PRIMARY KEY,
    member_id TEXT NOT NULL REFERENCES members(id),
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','cancelled')),
    current_approval_stage TEXT NULL CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager','ketua_i','ketua_ii','ketua_utama')),
    payout_source TEXT NULL CHECK (payout_source IS NULL OR payout_source IN ('cash','bank')),
    requested_by TEXT NOT NULL REFERENCES users(id),
    reviewed_by TEXT NULL REFERENCES users(id),
    reviewed_at TIMESTAMP NULL,
    cancelled_by TEXT NULL REFERENCES users(id),
    cancelled_at TIMESTAMP NULL,
    rejection_reason TEXT NOT NULL DEFAULT '',
    cancellation_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((status='pending' AND current_approval_stage IS NOT NULL AND reviewed_by IS NULL AND reviewed_at IS NULL) OR (status='approved' AND current_approval_stage IS NULL AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL) OR (status IN ('rejected','cancelled') AND current_approval_stage IS NULL))
);

CREATE UNIQUE INDEX idx_member_deactivation_one_pending ON member_deactivation_requests(member_id) WHERE status='pending';
CREATE INDEX idx_member_deactivation_requests_status ON member_deactivation_requests(status,current_approval_stage,created_at);

CREATE TABLE member_deactivation_request_balances (
    request_id TEXT NOT NULL REFERENCES member_deactivation_requests(id),
    category TEXT NOT NULL CHECK (category IN ('pokok','wajib','sukarela','shu','khusus')),
    amount BIGINT NOT NULL CHECK (amount >= 0),
    PRIMARY KEY (request_id,category)
);

CREATE TABLE member_deactivation_approvals (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL REFERENCES member_deactivation_requests(id),
    stage TEXT NOT NULL CHECK (stage IN ('manager','ketua_i','ketua_ii','ketua_utama')),
    decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
    officer_id TEXT NOT NULL REFERENCES users(id),
    officer_name TEXT NOT NULL,
    officer_role TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (request_id,stage)
);

CREATE INDEX idx_member_deactivation_approvals_request ON member_deactivation_approvals(request_id,created_at);

ALTER TABLE member_deactivation_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_deactivation_request_balances ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_deactivation_approvals ENABLE ROW LEVEL SECURITY;

DO $migration$
DECLARE
    function_definition TEXT;
    old_rule CONSTANT TEXT := $old$(OLD.status='pending' AND NEW.status='cancelled')$old$;
    new_rule CONSTANT TEXT := $new$(OLD.status='pending' AND NEW.status='cancelled') OR (OLD.status='approved' AND NEW.status='cancelled' AND OLD.disbursement_date='' AND NOT EXISTS (SELECT 1 FROM loans WHERE loans.loan_request_id=OLD.id))$new$;
BEGIN
    SELECT pg_get_functiondef('public.validate_loan_request_state_integrity()'::regprocedure) INTO function_definition;
    IF position(old_rule IN function_definition)=0 THEN
        RAISE EXCEPTION 'loan request state function does not contain expected cancellation rule';
    END IF;
    function_definition := replace(function_definition, old_rule, new_rule);
    EXECUTE function_definition;
END
$migration$;
