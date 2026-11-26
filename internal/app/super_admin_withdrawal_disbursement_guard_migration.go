package app

import (
	"database/sql"
	"fmt"
)

func allowSuperAdminWithdrawalDisbursementInGuard(tx *sql.Tx, isSQLite bool) error {
	var statements []string
	if isSQLite {
		statements = []string{
			`DROP TRIGGER IF EXISTS withdrawal_requests_disbursement_guard`,
			`CREATE TRIGGER withdrawal_requests_disbursement_guard BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at,saving_record_id ON withdrawal_requests BEGIN
				SELECT CASE WHEN OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
					OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL OR OLD.saving_record_id IS NOT NULL
					OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL OR NEW.saving_record_id IS NULL
					OR NOT (
						EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=1 AND u.historical_identity=0 AND oa.role='bendahara' AND oa.active=1)
						OR EXISTS (SELECT 1 FROM withdrawal_disbursement_overrides o JOIN users u ON u.id=o.actor_id WHERE o.request_id=NEW.id AND o.actor_id=NEW.disbursed_by AND o.disbursement_date=NEW.disbursement_date AND u.role='super_admin' AND u.active=1 AND u.historical_identity=0)
					)
					OR NOT EXISTS (SELECT 1 FROM saving_records sr WHERE sr.id=NEW.saving_record_id AND sr.member_id=NEW.member_id AND sr.type='withdrawal' AND sr.category='sukarela' AND sr.amount=NEW.amount AND sr.record_date=NEW.disbursement_date AND sr.recorded_by=NEW.disbursed_by)
					OR NOT EXISTS (SELECT 1 FROM withdrawal_reservations r WHERE r.request_id=NEW.id AND r.member_id=NEW.member_id AND r.amount=NEW.amount AND r.status='active')
					THEN RAISE(ABORT,'withdrawal request is not ready for a treasurer disbursement') END;
			END`,
		}
	} else {
		searchPath, err := postgresMigrationSearchPath(tx)
		if err != nil {
			return err
		}
		statements = []string{
			`DROP TRIGGER IF EXISTS withdrawal_requests_disbursement_guard ON withdrawal_requests`,
			`DROP FUNCTION IF EXISTS validate_withdrawal_request_disbursement()`,
			fmt.Sprintf(`CREATE FUNCTION validate_withdrawal_request_disbursement() RETURNS trigger LANGUAGE plpgsql SET search_path = %s AS $$ BEGIN
				IF OLD.status<>'approved' OR NEW.status<>'approved' OR OLD.current_approval_stage IS NOT NULL OR NEW.current_approval_stage IS NOT NULL
					OR OLD.disbursement_date<>'' OR OLD.disbursed_by IS NOT NULL OR OLD.disbursed_at IS NOT NULL OR OLD.saving_record_id IS NOT NULL
					OR NEW.disbursement_date='' OR NEW.disbursed_by IS NULL OR NEW.disbursed_at IS NULL OR NEW.saving_record_id IS NULL
					OR NOT (
						EXISTS (SELECT 1 FROM users u JOIN officer_appointments oa ON oa.member_id=u.member_id WHERE u.id=NEW.disbursed_by AND u.active=TRUE AND u.historical_identity=FALSE AND oa.role='bendahara' AND oa.active=TRUE)
						OR EXISTS (SELECT 1 FROM withdrawal_disbursement_overrides o JOIN users u ON u.id=o.actor_id WHERE o.request_id=NEW.id AND o.actor_id=NEW.disbursed_by AND o.disbursement_date=NEW.disbursement_date AND u.role='super_admin' AND u.active=TRUE AND u.historical_identity=FALSE)
					)
					OR NOT EXISTS (SELECT 1 FROM saving_records sr WHERE sr.id=NEW.saving_record_id AND sr.member_id=NEW.member_id AND sr.type='withdrawal' AND sr.category='sukarela' AND sr.amount=NEW.amount AND sr.record_date=NEW.disbursement_date AND sr.recorded_by=NEW.disbursed_by)
					OR NOT EXISTS (SELECT 1 FROM withdrawal_reservations r WHERE r.request_id=NEW.id AND r.member_id=NEW.member_id AND r.amount=NEW.amount AND r.status='active')
				THEN RAISE EXCEPTION 'withdrawal request is not ready for a treasurer disbursement'; END IF;
				RETURN NEW;
			END $$`, searchPath),
			`CREATE TRIGGER withdrawal_requests_disbursement_guard BEFORE UPDATE OF disbursement_date,disbursed_by,disbursed_at,saving_record_id ON withdrawal_requests FOR EACH ROW EXECUTE FUNCTION validate_withdrawal_request_disbursement()`,
		}
	}
	for index, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("super admin withdrawal disbursement guard statement %d: %w", index, err)
		}
	}
	return nil
}
