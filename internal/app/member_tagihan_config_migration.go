package app

import (
	"database/sql"
)

func addAdminRoleAndMemberTagihanConfig(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		officerAppointmentsExists, err := sqliteTableExists(tx, "officer_appointments")
		if err != nil {
			return err
		}
		loanRequestsExists, err := sqliteTableExists(tx, "loan_requests")
		if err != nil {
			return err
		}
		loanApprovalsExists, err := sqliteTableExists(tx, "loan_request_approvals")
		if err != nil {
			return err
		}
		withdrawalApprovalsExists, err := sqliteTableExists(tx, "withdrawal_request_approvals")
		if err != nil {
			return err
		}
		superAdminOverridesExists, err := sqliteTableExists(tx, "super_admin_overrides")
		if err != nil {
			return err
		}
		if officerAppointmentsExists {
			if err := rebuildSQLiteOfficerAppointmentsForAdmin(tx); err != nil {
				return err
			}
		}
		if loanRequestsExists && loanApprovalsExists && superAdminOverridesExists {
			for _, statement := range []string{
				`DROP TRIGGER IF EXISTS loan_requests_state_integrity`,
				`DROP TRIGGER IF EXISTS loans_request_provenance`,
			} {
				if _, err := tx.Exec(statement); err != nil {
					return err
				}
			}
		}
		if loanApprovalsExists {
			if err := rebuildSQLiteApprovalTableForAdmin(tx, "loan_request_approvals", "loan_request_approvals_v29", "loan_requests"); err != nil {
				return err
			}
		}
		if withdrawalApprovalsExists {
			if err := rebuildSQLiteApprovalTableForAdmin(tx, "withdrawal_request_approvals", "withdrawal_request_approvals_v29", "withdrawal_requests"); err != nil {
				return err
			}
		}
		if loanRequestsExists && loanApprovalsExists && superAdminOverridesExists {
			for _, statement := range superAdminSQLiteApprovalIntegrityStatements() {
				if _, err := tx.Exec(statement); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(`CREATE TABLE member_tagihan_configs (
			member_id TEXT PRIMARY KEY,
			simpanan_wajib INTEGER NOT NULL DEFAULT 0 CHECK (simpanan_wajib >= 0),
			simpanan_manasuka INTEGER NOT NULL DEFAULT 0 CHECK (simpanan_manasuka >= 0),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (member_id) REFERENCES members(id)
		)`)
		if err != nil {
			return err
		}
		savingRecordsExists, err := sqliteTableExists(tx, "saving_records")
		if err != nil {
			return err
		}
		seedSQL := `INSERT INTO member_tagihan_configs (member_id,simpanan_wajib,simpanan_manasuka) SELECT id,0,0 FROM members WHERE id IS NOT NULL ON CONFLICT (member_id) DO NOTHING`
		if savingRecordsExists {
			seedSQL = memberTagihanConfigSeedSQL
		}
		_, err = tx.Exec(seedSQL)
		return err
	}

	for _, statement := range []string{
		`ALTER TABLE officer_appointments DROP CONSTRAINT IF EXISTS officer_appointments_role_check`,
		`ALTER TABLE officer_appointments ADD CONSTRAINT officer_appointments_role_check CHECK (role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'))`,
		`ALTER TABLE loan_request_approvals DROP CONSTRAINT IF EXISTS loan_request_approvals_officer_role_check`,
		`ALTER TABLE loan_request_approvals ADD CONSTRAINT loan_request_approvals_officer_role_check CHECK (officer_role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'))`,
		`ALTER TABLE withdrawal_request_approvals DROP CONSTRAINT IF EXISTS withdrawal_request_approvals_officer_role_check`,
		`ALTER TABLE withdrawal_request_approvals ADD CONSTRAINT withdrawal_request_approvals_officer_role_check CHECK (officer_role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin'))`,
		`CREATE TABLE IF NOT EXISTS member_tagihan_configs (
			member_id TEXT PRIMARY KEY REFERENCES members(id),
			simpanan_wajib BIGINT NOT NULL DEFAULT 0 CHECK (simpanan_wajib >= 0),
			simpanan_manasuka BIGINT NOT NULL DEFAULT 0 CHECK (simpanan_manasuka >= 0),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		memberTagihanConfigSeedSQL,
		`ALTER TABLE member_tagihan_configs ENABLE ROW LEVEL SECURITY`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func sqliteTableExists(tx *sql.Tx, tableName string) (bool, error) {
	var exists bool
	err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name=$1)`, tableName).Scan(&exists)
	return exists, err
}

func superAdminSQLiteApprovalIntegrityStatements() []string {
	return []string{
		`DROP TRIGGER IF EXISTS loan_requests_admin_fee_policy_first_update`,
		`DROP TRIGGER IF EXISTS loan_requests_state_integrity`,
		`DROP TRIGGER IF EXISTS loans_request_provenance`,
		`CREATE TRIGGER loan_requests_admin_fee_policy_first_update BEFORE UPDATE OF proposed_admin_fee_policy ON loan_requests WHEN OLD.proposed_admin_fee_policy IS NULL AND NEW.proposed_admin_fee_policy IS NOT NULL AND (OLD.status<>'pending' OR OLD.current_approval_stage<>'manager' OR NOT ((NEW.loan_type='regular' AND NEW.legacy_terms=0 AND NEW.proposed_admin_fee_policy='regular_tiered_monthly_v1') OR (NEW.loan_type='secondary_goods' AND NEW.legacy_terms=0 AND NEW.proposed_admin_fee_policy='secondary_goods_one_time_v1') OR (NEW.loan_type='goods_purchase_paylater' AND NEW.legacy_terms=0 AND NEW.proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR (NEW.loan_type='regular' AND NEW.legacy_terms=1 AND NEW.proposed_admin_fee_policy='legacy_flat_monthly')) AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=OLD.id AND o.decision='approved')) BEGIN SELECT RAISE(ABORT,'proposed loan admin fee snapshot must be assigned at Manager stage'); END`,
		`CREATE TRIGGER loan_requests_state_integrity BEFORE UPDATE OF status,current_approval_stage,legacy_terms,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation,proposed_approved_amount,proposed_duration_months,proposed_start_date ON loan_requests WHEN OLD.legacy_terms=0 BEGIN
			SELECT CASE WHEN NEW.legacy_terms<>0 THEN RAISE(ABORT,'legacy loan terms are migration-only')
				WHEN NEW.status='pending' AND (NEW.current_approval_stage IS NULL OR NEW.current_approval_stage NOT IN ('manager','ketua_i','ketua_ii','ketua_utama') OR (NEW.current_approval_stage<>'manager' AND NEW.proposed_admin_fee_policy IS NULL)) THEN RAISE(ABORT,'invalid loan request approval state')
				WHEN NEW.status='approved' AND (NEW.current_approval_stage IS NOT NULL OR NEW.proposed_admin_fee_policy IS NULL) THEN RAISE(ABORT,'invalid approved loan request state')
				WHEN NEW.status IN ('rejected','cancelled') AND NEW.current_approval_stage IS NOT NULL THEN RAISE(ABORT,'invalid terminal loan request state') END;
			SELECT CASE WHEN OLD.status='pending' AND NEW.status='pending' AND OLD.current_approval_stage=NEW.current_approval_stage THEN NULL
				WHEN OLD.status='pending' AND NEW.status='pending' AND ((OLD.current_approval_stage='manager' AND NEW.current_approval_stage='ketua_ii') OR (OLD.current_approval_stage='ketua_ii' AND NEW.current_approval_stage='ketua_i') OR (OLD.current_approval_stage='ketua_i' AND NEW.current_approval_stage='ketua_utama')) AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage=OLD.current_approval_stage AND a.decision='approved') THEN NULL
				WHEN OLD.status='pending' AND NEW.status IN ('approved','rejected') AND EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=OLD.id AND o.decision=NEW.status) THEN NULL
				WHEN OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage='ketua_utama' AND a.decision='approved') THEN NULL
				WHEN OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage=OLD.current_approval_stage AND a.decision='rejected') THEN NULL
				WHEN OLD.status='pending' AND NEW.status='cancelled' THEN NULL
				WHEN OLD.status<>'pending' AND OLD.status=NEW.status AND OLD.current_approval_stage IS NEW.current_approval_stage THEN NULL
				ELSE RAISE(ABORT,'invalid loan request state transition') END;
			SELECT CASE WHEN NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.current_approval_stage<>'manager' OR NEW.status<>'pending') AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN RAISE(ABORT,'Manager approval is required for snapshotted terms') END;
		END`,
		`CREATE TRIGGER loans_request_provenance BEFORE INSERT ON loans WHEN NEW.legacy_terms=0 BEGIN SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved') AND ((SELECT COUNT(*) FROM loan_requests r WHERE r.id=NEW.loan_request_id AND r.status='approved' AND r.current_approval_stage IS NULL AND r.legacy_terms=0 AND r.member_id=NEW.member_id AND r.loan_type=NEW.loan_type AND r.proposed_approved_amount=NEW.approved_amount AND r.proposed_duration_months=NEW.duration_months AND r.proposed_start_date=NEW.start_date AND r.proposed_admin_fee_policy=NEW.admin_fee_policy AND r.proposed_monthly_admin_fee IS NEW.monthly_admin_fee AND r.proposed_total_admin_fee=NEW.total_admin_fee AND r.proposed_total_obligation=NEW.total_obligation)=0 OR NEW.remaining_balance<>NEW.total_obligation OR (SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.decision='approved' AND a.stage IN ('manager','ketua_i','ketua_ii','ketua_utama'))<>4 OR NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_utama' AND a.decision='approved' AND a.officer_id=NEW.approved_by)) THEN RAISE(ABORT,'loan must match a fully approved loan request') END; END`,
	}
}

const memberTagihanConfigSeedSQL = `
	INSERT INTO member_tagihan_configs (member_id,simpanan_wajib,simpanan_manasuka)
	SELECT m.id,
		COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
			FROM saving_records sr
			WHERE sr.member_id=m.id AND sr.category='wajib'
			ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0),
		COALESCE((SELECT CASE WHEN sr.type='deposit' THEN sr.amount ELSE 0 END
			FROM saving_records sr
			WHERE sr.member_id=m.id AND sr.category='sukarela'
			ORDER BY sr.record_date DESC,sr.created_at DESC,sr.id DESC LIMIT 1),0)
	FROM members m
	WHERE m.id IS NOT NULL
	ON CONFLICT (member_id) DO NOTHING`

func rebuildSQLiteOfficerAppointmentsForAdmin(tx *sql.Tx) error {
	statements := []string{
		`DROP TRIGGER IF EXISTS protect_last_ketua_utama_member_deactivation`,
		`DROP TRIGGER IF EXISTS suspend_officer_on_member_deactivation`,
		`DROP TRIGGER IF EXISTS protect_last_ketua_utama_appointment`,
		`CREATE TABLE officer_appointments_v29 (
			id TEXT PRIMARY KEY,
			member_id TEXT NOT NULL UNIQUE,
			role TEXT NOT NULL CHECK (role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin')),
			active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (member_id) REFERENCES members(id)
		)`,
		`INSERT INTO officer_appointments_v29 (id,member_id,role,active,created_at,updated_at)
			SELECT id,member_id,role,active,created_at,updated_at FROM officer_appointments`,
		`DROP TABLE officer_appointments`,
		`ALTER TABLE officer_appointments_v29 RENAME TO officer_appointments`,
		`CREATE INDEX idx_officer_appointments_role_active ON officer_appointments(role,active)`,
		`CREATE TRIGGER protect_last_ketua_utama_member_deactivation
			 BEFORE UPDATE OF status ON members
			 WHEN OLD.status='active' AND NEW.status<>'active'
			  AND EXISTS (SELECT 1 FROM officer_appointments WHERE member_id=OLD.id AND role='ketua_utama' AND active=TRUE)
			  AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			 BEGIN SELECT RAISE(ABORT, 'at least one active Ketua Utama is required'); END`,
		`CREATE TRIGGER suspend_officer_on_member_deactivation
			 AFTER UPDATE OF status ON members
			 WHEN OLD.status='active' AND NEW.status<>'active'
			 BEGIN UPDATE officer_appointments SET active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE member_id=NEW.id AND active=TRUE; END`,
		`CREATE TRIGGER protect_last_ketua_utama_appointment
			 BEFORE UPDATE OF role,active ON officer_appointments
			 WHEN OLD.role='ketua_utama' AND OLD.active=TRUE AND (NEW.role<>'ketua_utama' OR NEW.active=FALSE)
			  AND EXISTS (SELECT 1 FROM members WHERE id=OLD.member_id AND status='active')
			  AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			 BEGIN SELECT RAISE(ABORT, 'at least one active Ketua Utama is required'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSQLiteApprovalTableForAdmin(tx *sql.Tx, table, replacement, requestTable string) error {
	statements := []string{
		`CREATE TABLE ` + replacement + ` (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			stage TEXT NOT NULL CHECK (stage IN ('manager','ketua_i','ketua_ii','ketua_utama')),
			decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
			officer_id TEXT NOT NULL,
			officer_member_id TEXT NOT NULL DEFAULT '',
			officer_member_no TEXT NOT NULL DEFAULT '',
			officer_name TEXT NOT NULL,
			officer_role TEXT NOT NULL CHECK (officer_role IN ('manager','bendahara','ketua_i','ketua_ii','ketua_utama','admin')),
			note TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (request_id,stage),
			FOREIGN KEY (request_id) REFERENCES ` + requestTable + `(id),
			FOREIGN KEY (officer_id) REFERENCES users(id)
		)`,
		`INSERT INTO ` + replacement + ` (id,request_id,stage,decision,officer_id,officer_member_id,officer_member_no,officer_name,officer_role,note,reason,created_at)
			SELECT id,request_id,stage,decision,officer_id,officer_member_id,officer_member_no,officer_name,officer_role,note,reason,created_at FROM ` + table,
		`DROP TABLE ` + table,
		`ALTER TABLE ` + replacement + ` RENAME TO ` + table,
		`CREATE INDEX idx_` + table + `_request ON ` + table + `(request_id,created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
