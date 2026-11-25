package app

import (
	"database/sql"
	"fmt"
)

func addSuperAdminWithdrawalDisbursementOverrideAudit(tx *sql.Tx, isSQLite bool) error {
	statements := []string{
		`CREATE TABLE withdrawal_disbursement_overrides (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL UNIQUE REFERENCES withdrawal_requests(id),
			actor_id TEXT NOT NULL REFERENCES users(id),
			disbursement_date TEXT NOT NULL,
			frequency_limit_bypassed BOOLEAN NOT NULL,
			reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX idx_withdrawal_disbursement_overrides_actor_created ON withdrawal_disbursement_overrides(actor_id,created_at,id)`,
	}
	if isSQLite {
		statements = append(statements,
			`CREATE TRIGGER withdrawal_disbursement_overrides_actor_guard BEFORE INSERT ON withdrawal_disbursement_overrides WHEN NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND role='super_admin' AND active=TRUE AND historical_identity=FALSE) BEGIN SELECT RAISE(ABORT,'super admin actor is required'); END`,
			`CREATE TRIGGER withdrawal_disbursement_overrides_append_only_update BEFORE UPDATE ON withdrawal_disbursement_overrides BEGIN SELECT RAISE(ABORT,'super admin audit records are append-only'); END`,
			`CREATE TRIGGER withdrawal_disbursement_overrides_append_only_delete BEFORE DELETE ON withdrawal_disbursement_overrides BEGIN SELECT RAISE(ABORT,'super admin audit records are append-only'); END`,
		)
	} else {
		statements = append(statements,
			`ALTER TABLE withdrawal_disbursement_overrides ENABLE ROW LEVEL SECURITY`,
			`CREATE TRIGGER withdrawal_disbursement_overrides_append_only BEFORE INSERT OR UPDATE OR DELETE ON withdrawal_disbursement_overrides FOR EACH ROW EXECUTE FUNCTION protect_super_admin_audit_tables()`,
		)
	}
	for index, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("withdrawal disbursement override audit statement %d: %w", index, err)
		}
	}
	return nil
}
