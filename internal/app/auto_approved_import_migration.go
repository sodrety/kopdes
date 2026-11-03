package app

import (
	"database/sql"
	"fmt"
	"strings"
)

func allowAutoApprovedImportedLoans(tx *sql.Tx, isSQLite bool) error {
	if _, err := tx.Exec(`ALTER TABLE loan_requests ADD COLUMN auto_approved_import BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	if isSQLite {
		return allowAutoApprovedImportedLoansSQLite(tx)
	}
	return allowAutoApprovedImportedLoansPostgres(tx)
}

func allowAutoApprovedImportedLoansSQLite(tx *sql.Tx) error {
	statements := sqliteLoanStateIntegrityStatements()
	origin, err := replaceMigrationSQL(statements[0],
		`OR NEW.proposed_start_date<>'' BEGIN`,
		`OR NEW.proposed_start_date<>'' OR (NEW.auto_approved_import<>0 AND (COALESCE(NEW.creation_source,'member')<>'admin' OR NEW.batch_id IS NULL OR NEW.loan_type NOT IN ('voucher','goods_purchase_paylater'))) BEGIN`)
	if err != nil {
		return err
	}
	state, err := replaceMigrationSQL(statements[3],
		`WHEN OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage='ketua_utama' AND a.decision='approved') THEN NULL`,
		`WHEN OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id AND a.stage='ketua_utama' AND a.decision='approved') THEN NULL
			 WHEN OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='manager' AND OLD.auto_approved_import<>0 AND NEW.auto_approved_import<>0 AND NEW.current_approval_stage IS NULL AND NEW.reviewed_by IS NOT NULL AND NEW.reviewed_at IS NOT NULL AND COALESCE(NEW.creation_source,'member')='admin' AND NEW.batch_id IS NOT NULL AND NEW.loan_type IN ('voucher','goods_purchase_paylater') AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id) THEN NULL`)
	if err != nil {
		return err
	}
	state, err = replaceMigrationSQL(state,
		`AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') THEN RAISE(ABORT,'Manager approval is required for snapshotted terms') END;`,
		`AND NEW.auto_approved_import=0 AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') THEN RAISE(ABORT,'Manager approval is required for snapshotted terms') END;`)
	if err != nil {
		return err
	}
	provenance, err := autoImportedSQLiteLoanProvenance(statements[4])
	if err != nil {
		return err
	}
	disbursement := sqliteBendaharaLoanDisbursementGuardStatements()[1]
	disbursement, err = replaceMigrationSQL(disbursement,
		`OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
				OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)`,
		`OR (NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
					AND NOT (OLD.status='approved' AND OLD.auto_approved_import<>0 AND NEW.auto_approved_import<>0 AND COALESCE(OLD.creation_source,'member')='admin' AND OLD.batch_id IS NOT NULL AND OLD.loan_type IN ('voucher','goods_purchase_paylater') AND NEW.disbursed_by IS OLD.reviewed_by AND NEW.disbursed_by=NEW.reviewed_by AND NEW.disbursement_date=OLD.proposed_start_date))
				OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)`)
	if err != nil {
		return err
	}

	for _, statement := range []string{
		`DROP TRIGGER IF EXISTS loan_requests_nonlegacy_origin`, origin,
		`DROP TRIGGER IF EXISTS loan_requests_state_integrity`, state,
		`DROP TRIGGER IF EXISTS loans_request_provenance`, provenance,
		sqliteBendaharaLoanDisbursementGuardStatements()[0], disbursement,
		`CREATE TRIGGER loan_requests_auto_import_flag_immutable BEFORE UPDATE OF auto_approved_import ON loan_requests WHEN NEW.auto_approved_import<>OLD.auto_approved_import BEGIN SELECT RAISE(ABORT,'automatic import provenance is immutable'); END`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func autoImportedSQLiteLoanProvenance(statement string) (string, error) {
	countCondition := `(SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.decision='approved' AND a.stage IN ('manager','ketua_i','ketua_ii','ketua_utama'))<>4`
	allowedImport := `EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND r.auto_approved_import<>0 AND r.creation_source='admin' AND r.batch_id IS NOT NULL AND r.loan_type IN ('voucher','goods_purchase_paylater') AND r.reviewed_by=NEW.approved_by AND r.disbursement_date<>'' AND r.disbursed_by=NEW.approved_by AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=r.id))`
	statement, err := replaceMigrationSQL(statement, `OR `+countCondition, `OR (`+countCondition+` AND NOT `+allowedImport+`)`)
	if err != nil {
		return "", err
	}
	finalApproval := `NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_utama' AND a.decision='approved' AND a.officer_id=NEW.approved_by)`
	statement, err = replaceMigrationSQL(statement, `OR `+finalApproval, `OR (`+finalApproval+` AND NOT `+allowedImport+`)`)
	if err != nil {
		return "", err
	}
	return statement, nil
}

func allowAutoApprovedImportedLoansPostgres(tx *sql.Tx) error {
	statements := postgresLoanStateIntegrityStatements()
	origin, err := replaceMigrationSQL(statements[0],
		`IF NEW.legacy_terms IS DISTINCT FROM FALSE OR`,
		`IF (NEW.auto_approved_import IS TRUE AND (COALESCE(NEW.creation_source,'member')<>'admin' OR NEW.batch_id IS NULL OR NEW.loan_type NOT IN ('voucher','goods_purchase_paylater'))) OR NEW.legacy_terms IS DISTINCT FROM FALSE OR`)
	if err != nil {
		return err
	}
	origin = strings.Replace(origin, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1)
	state, err := replaceMigrationSQL(statements[6],
		`OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND approved_current)`,
		`OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='ketua_utama' AND approved_current)
					OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='manager' AND OLD.auto_approved_import IS TRUE AND NEW.auto_approved_import IS TRUE AND NEW.current_approval_stage IS NULL AND NEW.reviewed_by IS NOT NULL AND NEW.reviewed_at IS NOT NULL AND COALESCE(NEW.creation_source,'member')='admin' AND NEW.batch_id IS NOT NULL AND NEW.loan_type IN ('voucher','goods_purchase_paylater') AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id))`)
	if err != nil {
		return err
	}
	state, err = replaceMigrationSQL(state,
		`AND NOT approved_manager THEN RAISE EXCEPTION 'Manager approval is required for snapshotted terms';`,
		`AND NOT NEW.auto_approved_import AND NOT approved_manager THEN RAISE EXCEPTION 'Manager approval is required for snapshotted terms';`)
	if err != nil {
		return err
	}
	state = strings.Replace(state, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1)
	provenance, err := autoImportedPostgresLoanProvenance(statements[8])
	if err != nil {
		return err
	}
	provenance = strings.Replace(provenance, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1)

	searchPath, err := postgresMigrationSearchPath(tx)
	if err != nil {
		return err
	}
	disbursementStatements := postgresBendaharaLoanDisbursementGuardStatements(searchPath)
	disbursement, err := replaceMigrationSQL(disbursementStatements[2],
		`OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
				OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)`,
		`OR (NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
					AND NOT (OLD.status='approved' AND OLD.auto_approved_import IS TRUE AND NEW.auto_approved_import IS TRUE AND COALESCE(OLD.creation_source,'member')='admin' AND OLD.batch_id IS NOT NULL AND OLD.loan_type IN ('voucher','goods_purchase_paylater') AND NEW.disbursed_by IS NOT DISTINCT FROM OLD.reviewed_by AND NEW.disbursed_by=NEW.reviewed_by AND NEW.disbursement_date=OLD.proposed_start_date))
				OR EXISTS (SELECT 1 FROM loans l WHERE l.loan_request_id=OLD.id)`)
	if err != nil {
		return err
	}
	disbursement = strings.Replace(disbursement, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1)

	flagGuard := fmt.Sprintf(`CREATE OR REPLACE FUNCTION protect_auto_import_flag() RETURNS trigger LANGUAGE plpgsql SET search_path = %s AS $$ BEGIN
		IF NEW.auto_approved_import IS DISTINCT FROM OLD.auto_approved_import THEN RAISE EXCEPTION 'automatic import provenance is immutable'; END IF;
		RETURN NEW;
	END $$`, searchPath)
	for _, statement := range []string{origin, state, provenance, disbursement, flagGuard, `CREATE TRIGGER protect_auto_import_flag BEFORE UPDATE OF auto_approved_import ON loan_requests FOR EACH ROW EXECUTE FUNCTION protect_auto_import_flag()`} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func autoImportedPostgresLoanProvenance(statement string) (string, error) {
	countCondition := `(SELECT COUNT(*) FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.decision='approved' AND a.stage IN ('manager','ketua_i','ketua_ii','ketua_utama'))<>4`
	allowedImport := `EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND r.auto_approved_import IS TRUE AND r.creation_source='admin' AND r.batch_id IS NOT NULL AND r.loan_type IN ('voucher','goods_purchase_paylater') AND r.reviewed_by=NEW.approved_by AND r.disbursement_date<>'' AND r.disbursed_by=NEW.approved_by AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=r.id))`
	statement, err := replaceMigrationSQL(statement, `OR `+countCondition, `OR (`+countCondition+` AND NOT `+allowedImport+`)`)
	if err != nil {
		return "", err
	}
	finalApproval := `NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.loan_request_id AND a.stage='ketua_utama' AND a.decision='approved' AND a.officer_id=NEW.approved_by)`
	statement, err = replaceMigrationSQL(statement, `OR `+finalApproval, `OR (`+finalApproval+` AND NOT `+allowedImport+`)`)
	if err != nil {
		return "", err
	}
	return statement, nil
}

func replaceMigrationSQL(statement, old, replacement string) (string, error) {
	if !strings.Contains(statement, old) {
		return "", fmt.Errorf("migration SQL fragment not found: %s", old)
	}
	return strings.Replace(statement, old, replacement, 1), nil
}
