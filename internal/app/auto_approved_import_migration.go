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
	return refreshAutoApprovedImportedLoanGuards(tx, isSQLite)
}

func refreshAutoApprovedImportedLoanGuards(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		return allowAutoApprovedImportedLoansSQLite(tx)
	}
	return allowAutoApprovedImportedLoansPostgres(tx)
}

func allowAutoApprovedImportedLoansSQLite(tx *sql.Tx) error {
	base := sqliteLoanStateIntegrityStatements()
	origin, err := replaceMigrationSQL(base[0],
		`OR NEW.proposed_start_date<>'' BEGIN`,
		`OR NEW.proposed_start_date<>'' OR (NEW.auto_approved_import<>0 AND (COALESCE(NEW.creation_source,'member')<>'admin' OR NEW.batch_id IS NULL OR NEW.loan_type NOT IN ('voucher','goods_purchase_paylater'))) BEGIN`)
	if err != nil {
		return err
	}

	integrity := sqliteLoanApprovalDisbursementIntegrityStatements()
	state, err := replaceMigrationSQL(integrity[3],
		`WHEN OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM super_admin_overrides`,
		autoImportedSQLiteApprovalTransition()+`WHEN OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM super_admin_overrides`)
	if err != nil {
		return err
	}
	state, err = replaceMigrationSQL(state,
		`AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN`,
		`AND NEW.auto_approved_import=0 AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=NEW.id AND a.stage='manager' AND a.decision='approved') AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN`)
	if err != nil {
		return err
	}

	provenance, err := autoImportedSQLiteLoanProvenance(integrity[5])
	if err != nil {
		return err
	}
	disbursement, err := replaceSQLiteAutoImportedDisbursement(integrity[4])
	if err != nil {
		return err
	}

	for _, statement := range []string{
		`DROP TRIGGER IF EXISTS loan_requests_nonlegacy_origin`, origin,
		`DROP TRIGGER IF EXISTS loan_requests_state_integrity`, state,
		`DROP TRIGGER IF EXISTS loan_requests_disbursement_guard`, disbursement,
		`DROP TRIGGER IF EXISTS loans_request_provenance`, provenance,
		`DROP TRIGGER IF EXISTS loan_requests_auto_import_flag_immutable`,
		`CREATE TRIGGER loan_requests_auto_import_flag_immutable BEFORE UPDATE OF auto_approved_import ON loan_requests WHEN NEW.auto_approved_import<>OLD.auto_approved_import BEGIN SELECT RAISE(ABORT,'automatic import provenance is immutable'); END`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func autoImportedSQLiteApprovalTransition() string {
	return `WHEN OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='manager' AND OLD.auto_approved_import<>0 AND NEW.auto_approved_import<>0 AND NEW.current_approval_stage IS NULL AND NEW.reviewed_by IS NOT NULL AND NEW.reviewed_at IS NOT NULL AND COALESCE(NEW.creation_source,'member')='admin' AND NEW.batch_id IS NOT NULL AND NEW.loan_type IN ('voucher','goods_purchase_paylater') AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id) THEN NULL
			 `
}

func replaceSQLiteAutoImportedDisbursement(statement string) (string, error) {
	bendahara := `OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)`
	autoDisbursement := `OR (NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE) AND NOT (OLD.status='approved' AND OLD.auto_approved_import<>0 AND NEW.auto_approved_import<>0 AND COALESCE(OLD.creation_source,'member')='admin' AND OLD.batch_id IS NOT NULL AND OLD.loan_type IN ('voucher','goods_purchase_paylater') AND NEW.disbursed_by IS OLD.reviewed_by AND NEW.disbursed_by=NEW.reviewed_by AND NEW.disbursement_date=OLD.proposed_start_date))`
	return replaceMigrationSQL(statement, bendahara, autoDisbursement)
}

func autoImportedSQLiteLoanProvenance(statement string) (string, error) {
	allowedImport := `EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND r.auto_approved_import<>0 AND r.creation_source='admin' AND r.batch_id IS NOT NULL AND r.loan_type IN ('voucher','goods_purchase_paylater') AND r.reviewed_by=NEW.approved_by AND r.disbursement_date<>'' AND r.disbursed_by=NEW.approved_by AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=r.id))`
	regularApprovalGuard := `OR (NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved') AND NOT (`
	withImportException := `OR (NOT (` + allowedImport + `) AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved') AND NOT (`
	return replaceMigrationSQL(statement, regularApprovalGuard, withImportException)
}

func allowAutoApprovedImportedLoansPostgres(tx *sql.Tx) error {
	searchPath, err := postgresMigrationSearchPath(tx)
	if err != nil {
		return err
	}
	integrity := postgresLoanApprovalDisbursementIntegrityStatements(searchPath)

	base := postgresLoanStateIntegrityStatements()
	origin, err := replaceMigrationSQL(base[0],
		`IF NEW.legacy_terms IS DISTINCT FROM FALSE OR`,
		`IF (NEW.auto_approved_import IS TRUE AND (COALESCE(NEW.creation_source,'member')<>'admin' OR NEW.batch_id IS NULL OR NEW.loan_type NOT IN ('voucher','goods_purchase_paylater'))) OR NEW.legacy_terms IS DISTINCT FROM FALSE OR`)
	if err != nil {
		return err
	}
	origin = strings.Replace(origin, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1)

	state, err := replaceMigrationSQL(integrity[2],
		`OR (OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals`,
		autoImportedPostgresApprovalTransition()+`OR (OLD.status='pending' AND NEW.status='rejected' AND EXISTS (SELECT 1 FROM loan_request_approvals`)
	if err != nil {
		return err
	}
	state, err = replaceMigrationSQL(state,
		`AND NOT approved_manager AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN`,
		`AND NOT NEW.auto_approved_import AND NOT approved_manager AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.id AND o.decision='approved') THEN`)
	if err != nil {
		return err
	}

	disbursement, err := replacePostgresAutoImportedDisbursement(integrity[6])
	if err != nil {
		return err
	}
	provenance, err := autoImportedPostgresLoanProvenance(integrity[10])
	if err != nil {
		return err
	}
	flagGuard := fmt.Sprintf(`CREATE OR REPLACE FUNCTION protect_auto_import_flag() RETURNS trigger LANGUAGE plpgsql SET search_path = %s AS $$ BEGIN
		IF NEW.auto_approved_import IS DISTINCT FROM OLD.auto_approved_import THEN RAISE EXCEPTION 'automatic import provenance is immutable'; END IF;
		RETURN NEW;
	END $$`, searchPath)

	for _, statement := range []string{
		origin,
		integrity[0], integrity[1], state, integrity[3],
		integrity[4], integrity[5], disbursement, integrity[7],
		integrity[8], integrity[9], provenance, integrity[11],
		`DROP TRIGGER IF EXISTS protect_auto_import_flag ON loan_requests`, flagGuard,
		`CREATE TRIGGER protect_auto_import_flag BEFORE UPDATE OF auto_approved_import ON loan_requests FOR EACH ROW EXECUTE FUNCTION protect_auto_import_flag()`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func autoImportedPostgresApprovalTransition() string {
	return `OR (OLD.status='pending' AND NEW.status='approved' AND OLD.current_approval_stage='manager' AND OLD.auto_approved_import IS TRUE AND NEW.auto_approved_import IS TRUE AND NEW.current_approval_stage IS NULL AND NEW.reviewed_by IS NOT NULL AND NEW.reviewed_at IS NOT NULL AND COALESCE(NEW.creation_source,'member')='admin' AND NEW.batch_id IS NOT NULL AND NEW.loan_type IN ('voucher','goods_purchase_paylater') AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=OLD.id))
				`
}

func replacePostgresAutoImportedDisbursement(statement string) (string, error) {
	bendahara := `OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)`
	autoDisbursement := `OR (NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE) AND NOT (OLD.status='approved' AND OLD.auto_approved_import IS TRUE AND NEW.auto_approved_import IS TRUE AND COALESCE(OLD.creation_source,'member')='admin' AND OLD.batch_id IS NOT NULL AND OLD.loan_type IN ('voucher','goods_purchase_paylater') AND NEW.disbursed_by IS NOT DISTINCT FROM OLD.reviewed_by AND NEW.disbursed_by=NEW.reviewed_by AND NEW.disbursement_date=OLD.proposed_start_date))`
	statement, err := replaceMigrationSQL(statement, bendahara, autoDisbursement)
	if err != nil {
		return "", err
	}
	return strings.Replace(statement, `CREATE FUNCTION `, `CREATE OR REPLACE FUNCTION `, 1), nil
}

func autoImportedPostgresLoanProvenance(statement string) (string, error) {
	allowedImport := `EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND r.auto_approved_import IS TRUE AND r.creation_source='admin' AND r.batch_id IS NOT NULL AND r.loan_type IN ('voucher','goods_purchase_paylater') AND r.reviewed_by=NEW.approved_by AND r.disbursement_date<>'' AND r.disbursed_by=NEW.approved_by AND NOT EXISTS (SELECT 1 FROM loan_request_approvals a WHERE a.request_id=r.id))`
	regularApprovalGuard := `OR (NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved') AND NOT (`
	withImportException := `OR (NOT (` + allowedImport + `) AND NOT EXISTS (SELECT 1 FROM super_admin_overrides o WHERE o.request_type='loan' AND o.request_id=NEW.loan_request_id AND o.decision='approved') AND NOT (`
	return replaceMigrationSQL(statement, regularApprovalGuard, withImportException)
}

func replaceMigrationSQL(statement, old, replacement string) (string, error) {
	if !strings.Contains(statement, old) {
		return "", fmt.Errorf("migration SQL fragment not found: %s", old)
	}
	return strings.Replace(statement, old, replacement, 1), nil
}
