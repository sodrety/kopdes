package app

import (
	"database/sql"
	"fmt"
)

func addMemberTypeLoanLimitIntegrity(tx *sql.Tx, isSQLite bool) error {
	statements := postgresMemberTypeLoanLimitStatements()
	if isSQLite {
		statements = sqliteMemberTypeLoanLimitStatements()
	}
	for index, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("member-type loan limit statement %d: %w", index, err)
		}
	}
	return nil
}

func sqliteMemberTypeLoanLimitStatements() []string {
	limit := `COALESCE((SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id),0)`
	return []string{
		`DROP TRIGGER IF EXISTS loan_requests_requested_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loan_requests_requested_amount_savings_limit_update`,
		`DROP TRIGGER IF EXISTS loan_requests_proposed_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loan_requests_proposed_amount_savings_limit_update`,
		`DROP TRIGGER IF EXISTS loans_approved_amount_savings_limit_insert`,
		`DROP TRIGGER IF EXISTS loans_approved_amount_savings_limit_update`,
		`DROP VIEW IF EXISTS member_loan_amount_limits`,
		`CREATE VIEW member_loan_amount_limits AS
			SELECT m.id AS member_id,
				(CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
					* MAX(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount ELSE 0 END),0),0)
					+ MAX(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount ELSE 0 END),0),0) AS loan_limit
			FROM members m LEFT JOIN saving_records sr ON sr.member_id=m.id
			GROUP BY m.id,m.member_type`,
		fmt.Sprintf(`CREATE TRIGGER loan_requests_requested_amount_savings_limit_insert BEFORE INSERT ON loan_requests WHEN NEW.legacy_terms=0 AND NEW.requested_amount>%s BEGIN SELECT RAISE(ABORT,'loan amount exceeds the member-type limit'); END`, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_requested_amount_savings_limit_update BEFORE UPDATE OF requested_amount,member_id ON loan_requests WHEN NEW.legacy_terms=0 AND NEW.requested_amount>%s BEGIN SELECT RAISE(ABORT,'loan amount exceeds the member-type limit'); END`, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_proposed_amount_savings_limit_insert BEFORE INSERT ON loan_requests WHEN NEW.legacy_terms=0 AND NEW.proposed_approved_amount IS NOT NULL AND NEW.proposed_approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, limit),
		fmt.Sprintf(`CREATE TRIGGER loan_requests_proposed_amount_savings_limit_update BEFORE UPDATE OF proposed_approved_amount,member_id ON loan_requests WHEN NEW.legacy_terms=0 AND NEW.proposed_approved_amount IS NOT NULL AND NEW.proposed_approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, limit),
		fmt.Sprintf(`CREATE TRIGGER loans_approved_amount_savings_limit_insert BEFORE INSERT ON loans WHEN NEW.legacy_terms=0 AND NEW.approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, limit),
		fmt.Sprintf(`CREATE TRIGGER loans_approved_amount_savings_limit_update BEFORE UPDATE OF approved_amount,member_id ON loans WHEN NEW.legacy_terms=0 AND NEW.approved_amount>%s BEGIN SELECT RAISE(ABORT,'approved loan amount exceeds the member-type limit'); END`, limit),
	}
}

func postgresMemberTypeLoanLimitStatements() []string {
	return []string{
		`CREATE OR REPLACE VIEW member_loan_amount_limits AS
			SELECT m.id AS member_id,
				(CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
					* GREATEST(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
					+ GREATEST(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0) AS loan_limit
			FROM members m LEFT JOIN saving_records sr ON sr.member_id=m.id
			GROUP BY m.id,m.member_type`,
		`CREATE OR REPLACE FUNCTION validate_loan_request_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.requested_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE OR REPLACE FUNCTION validate_loan_proposed_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE AND NEW.proposed_approved_amount IS NOT NULL THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.proposed_approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE OR REPLACE FUNCTION validate_loan_approved_amount_against_savings() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$ DECLARE member_loan_limit NUMERIC; BEGIN
			IF NEW.legacy_terms=FALSE THEN
				SELECT loan_limit INTO member_loan_limit FROM member_loan_amount_limits WHERE member_id=NEW.member_id;
				IF NEW.approved_amount::NUMERIC>COALESCE(member_loan_limit,0) THEN RAISE EXCEPTION 'approved loan amount exceeds the member-type limit'; END IF;
			END IF;
			RETURN NEW;
		END $$`,
	}
}
