-- PostgreSQL mirror of runtime migration 12 in internal/app/migrations.go.
BEGIN;
SET LOCAL statement_timeout = '30s';

ALTER TABLE users ADD COLUMN full_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
UPDATE users SET role = 'manager', full_name = CASE WHEN full_name = '' THEN email ELSE full_name END WHERE role = 'admin';
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('member', 'manager', 'ketua_i', 'ketua_ii', 'ketua_utama'));

ALTER TABLE loan_requests ADD COLUMN current_approval_stage TEXT NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_approved_amount INTEGER NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_duration_months INTEGER NULL;
ALTER TABLE loan_requests ADD COLUMN proposed_start_date TEXT NOT NULL DEFAULT '';
ALTER TABLE loan_requests ADD COLUMN proposed_interest_rate_bps INTEGER NULL;
ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_status_check;
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'));
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_approval_stage_check CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama'));
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_amount_check CHECK (proposed_approved_amount IS NULL OR proposed_approved_amount > 0);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_duration_check CHECK (proposed_duration_months IS NULL OR proposed_duration_months BETWEEN 1 AND 120);
ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_rate_check CHECK (proposed_interest_rate_bps IS NULL OR proposed_interest_rate_bps BETWEEN 0 AND 1000);
UPDATE loan_requests SET current_approval_stage = CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END;

ALTER TABLE withdrawal_requests ADD COLUMN current_approval_stage TEXT NULL;
ALTER TABLE withdrawal_requests DROP CONSTRAINT IF EXISTS withdrawal_requests_status_check;
ALTER TABLE withdrawal_requests ADD CONSTRAINT withdrawal_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'));
ALTER TABLE withdrawal_requests ADD CONSTRAINT withdrawal_requests_approval_stage_check CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama'));
UPDATE withdrawal_requests SET current_approval_stage = CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END;

CREATE TABLE loan_request_approvals (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL REFERENCES loan_requests(id),
    stage TEXT NOT NULL CHECK (stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    officer_id TEXT NOT NULL REFERENCES users(id),
    officer_name TEXT NOT NULL,
    officer_role TEXT NOT NULL CHECK (officer_role IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
    note TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (request_id, stage)
);
CREATE INDEX idx_loan_request_approvals_request ON loan_request_approvals(request_id, created_at);

CREATE TABLE withdrawal_request_approvals (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL REFERENCES withdrawal_requests(id),
    stage TEXT NOT NULL CHECK (stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    officer_id TEXT NOT NULL REFERENCES users(id),
    officer_name TEXT NOT NULL,
    officer_role TEXT NOT NULL CHECK (officer_role IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
    note TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (request_id, stage)
);
CREATE INDEX idx_withdrawal_request_approvals_request ON withdrawal_request_approvals(request_id, created_at);

CREATE TABLE withdrawal_reservations (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE REFERENCES withdrawal_requests(id),
    member_id TEXT NOT NULL REFERENCES members(id),
    amount INTEGER NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'consumed')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_withdrawal_reservations_member_status ON withdrawal_reservations(member_id, status);

CREATE TABLE officer_audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES users(id),
    actor_name TEXT NOT NULL,
    target_id TEXT NOT NULL REFERENCES users(id),
    target_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created', 'role_changed', 'activated', 'deactivated', 'password_reset')),
    old_role TEXT NOT NULL DEFAULT '',
    new_role TEXT NOT NULL DEFAULT '',
    old_active BOOLEAN NULL,
    new_active BOOLEAN NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_officer_audit_events_target ON officer_audit_events(target_id, created_at);

CREATE TABLE notification_events (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    request_type TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES notification_events(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    title_key TEXT NOT NULL,
    body_key TEXT NOT NULL,
    link TEXT NOT NULL DEFAULT '',
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_notifications_user_state ON notifications(user_id, resolved_at, is_read, created_at);

INSERT INTO withdrawal_reservations (id, request_id, member_id, amount, status)
SELECT 'migration-' || id, id, member_id, amount, 'active'
FROM withdrawal_requests
WHERE status = 'pending';

-- These tables live in Supabase's exposed public schema but are only accessed
-- by the server-side database role. No Data API policies are intentionally added.
ALTER TABLE loan_request_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE withdrawal_request_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE withdrawal_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE officer_audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;

INSERT INTO schema_migrations (version, name)
VALUES (12, 'add_officer_hierarchy_and_approvals');

COMMIT;
