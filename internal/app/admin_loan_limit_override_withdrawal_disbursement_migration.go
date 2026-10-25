package app

import (
	"database/sql"
	"fmt"
)

func addAdminLoanLimitOverrideAndWithdrawalDisbursement(tx *sql.Tx, isSQLite bool) error {
	var statements []string
	if isSQLite {
		statements = append(statements,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursement_date TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursed_by TEXT NULL REFERENCES users(id)`,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursed_at TIMESTAMP NULL`,
			`UPDATE withdrawal_requests SET disbursement_date=(SELECT record_date FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id),disbursed_by=(SELECT recorded_by FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id),disbursed_at=(SELECT created_at FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id) WHERE status='approved' AND saving_record_id IS NOT NULL AND EXISTS (SELECT 1 FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id)`,
		)
		statements = append(statements, sqliteAdminLoanLimitOverrideStatements()...)
		statements = append(statements, sqliteWithdrawalDisbursementGuardStatements()...)
	} else {
		searchPath, err := postgresMigrationSearchPath(tx)
		if err != nil {
			return err
		}
		statements = append(statements,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursement_date TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursed_by TEXT NULL REFERENCES users(id)`,
			`ALTER TABLE withdrawal_requests ADD COLUMN disbursed_at TIMESTAMP NULL`,
			`UPDATE withdrawal_requests SET disbursement_date=(SELECT record_date FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id),disbursed_by=(SELECT recorded_by FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id),disbursed_at=(SELECT created_at FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id) WHERE status='approved' AND saving_record_id IS NOT NULL AND EXISTS (SELECT 1 FROM saving_records WHERE saving_records.id=withdrawal_requests.saving_record_id)`,
		)
		statements = append(statements, postgresAdminLoanLimitOverrideStatements(searchPath)...)
		statements = append(statements, postgresWithdrawalDisbursementGuardStatements(searchPath)...)
	}
	for index, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("admin loan override and withdrawal disbursement statement %d: %w", index, err)
		}
	}
	return nil
}

func sqliteAdminLoanLimitOverrideStatements() []string {
	limit := `COALESCE((SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id),0)`
	adminLoan := `COALESCE(NEW.creation_source,'member')<>'admin'`
	loanRequestOrigin := `NOT EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND COALESCE(r.creation_source,'member')='admin')`
	return []string{
		`DROP TRIGGER IF EXISTS loan_requests_requested_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loan_requests_requested_amount_savings_limit_update`,
		`DROP TRIGGER IF EXISTS loan_requests_proposed_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loan_requests_proposed_amount_savings_limit_update`,
		`DROP TRIGGER IF EXISTS loans_approved_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loans_approved_amount_savings_limit_update`,
		fmt.Sprintf(`CREATE TRIGGER loan_requests_requested_amount_savings_limit_insert BEFORE INSERT ON loan_requests WHEN NEW.legacy_terms=0 AND %s AND NEW.requested_amount>%s BEGIN SELECT RAISE(ABORT,'loan amount exceeds the member-type limit'); END`, adminLoan, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_requested_amount_savings_limit_update BEFORE UPDATE OF requested_amount,member_id ON loan_requests WHEN NEW.legacy_terms=0 AND %s AND NEW.requested_amount>%s BEGIN SELECT RAISE(ABORT,'loan amount exceeds the member-type limit'); END`, adminLoan, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_proposed_amount_savings_limit_insert BEFORE INSERT ON loan_requests WHEN NEW.legacy_terms=0 AND %s AND NEW.proposed_approved_amount IS NOT NULL AND NEW.proposed_approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, adminLoan, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_proposed_amount_savings_limit_update BEFORE UPDATE OF proposed_approved_amount,member_id ON loan_requests WHEN NEW.legacy_terms=0 AND %s AND NEW.proposed_approved_amount IS NOT NULL AND NEW.proposed_approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, adminLoan, limit),
		fmt.Sprintf(`CREATE TRIGGER loans_approved_amount_savings_limit_insert BEFORE INSERT ON loans WHEN NEW.legacy_terms=0 AND %s AND NEW.approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, loanRequestOrigin, limit),
		fmt.Sprintf(`CREATE TRIGGER loans_approved_amount_savings_limit_update BEFORE UPDATE OF approved_amount,member_id ON loans WHEN NEW.legacy_terms=0 AND %s AND NEW.approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, loanRequestOrigin, limit),
	}
}

func postgresAdminLoanLimitOverrideStatements(searchPath string) []string {
	return []string{
		fmt.Sprintf(`CREATE OR REPLACE FUNCTION validate_loan_request_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = %s AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE AND COALESCE(NEW.creation_source,'member')<>'admin' THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.requested_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`, searchPath),
		fmt.Sprintf(`CREATE OR REPLACE FUNCTION validate_loan_proposed_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = %s AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE AND COALESCE(NEW.creation_source,'member')<>'admin' AND NEW.proposed_approved_amount IS NOT NULL THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.proposed_approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`, searchPath),
		fmt.Sprintf(`CREATE OR REPLACE FUNCTION validate_loan_approved_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = %s AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE AND NOT EXISTS (SELECT 1 FROM loan_requests r WHERE r.id=NEW.loan_request_id AND COALESCE(r.creation_source,'member')='admin') THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`, searchPath),
	}
}

func sqliteWithdrawalDisbursementGuardStatements() []string {
	return []string{
		`DROP TRIGGER IF EXISTS withdrawal_requests_disbursement_guard`,
		`CREATE TRIGGER withdrawal_requests_disbursement_guard BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at,saving_record_id ON withdrawal_requests BEGIN
			SELECT CASE WHEN OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
				OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL OR OLD.saving_record_id IS NOT NULL
				OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL OR NEW.saving_record_id IS NULL
				OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=1 AND u.historical_identity=0 AND oa.role='bendahara' AND oa.active=1)
				OR NOT EXISTS (SELECT 1 FROM saving_records sr WHERE sr.id=NEW.saving_record_id AND sr.member_id=NEW.member_id AND sr.type='withdrawal' AND sr.category='sukarela' AND sr.amount=NEW.amount AND sr.record_date=NEW.disbursement_date AND sr.recorded_by=NEW.disbursed_by)
				OR NOT EXISTS (SELECT 1 FROM withdrawal_reservations r WHERE r.request_id=NEW.id AND r.member_id=NEW.member_id AND r.amount=NEW.amount AND r.status='active')
				THEN RAISE(ABORT,'withdrawal request is not ready for a treasurer disbursement') END;
		END`,
	}
}

func postgresWithdrawalDisbursementGuardStatements(searchPath string) []string {
	return []string{
		`DROP TRIGGER IF EXISTS withdrawal_requests_disbursement_guard ON withdrawal_requests`,
		`DROP FUNCTION IF EXISTS validate_withdrawal_request_disbursement()`,
		fmt.Sprintf(`CREATE FUNCTION validate_withdrawal_request_disbursement() RETURNS trigger LANGUAGE plpgsql SET search_path = %s AS $$ BEGIN
			IF OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
				OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL OR OLD.saving_record_id IS NOT NULL
				OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL OR NEW.saving_record_id IS NULL
				OR NOT EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
				OR NOT EXISTS (SELECT 1 FROM saving_records sr WHERE sr.id=NEW.saving_record_id AND sr.member_id=NEW.member_id AND sr.type='withdrawal' AND sr.category='sukarela' AND sr.amount=NEW.amount AND sr.record_date=NEW.disbursement_date AND sr.recorded_by=NEW.disbursed_by)
				OR NOT EXISTS (SELECT 1 FROM withdrawal_reservations r WHERE r.request_id=NEW.id AND r.member_id=NEW.member_id AND r.amount=NEW.amount AND r.status='active')
			THEN RAISE EXCEPTION 'withdrawal request is not ready for a treasurer disbursement'; END IF;
			RETURN NEW;
		END $$`, searchPath),
		`CREATE TRIGGER withdrawal_requests_disbursement_guard BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at,saving_record_id ON withdrawal_requests FOR EACH ROW EXECUTE FUNCTION validate_withdrawal_request_disbursement()`,
	}
}
