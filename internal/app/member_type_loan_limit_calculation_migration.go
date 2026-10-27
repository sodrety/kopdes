package app

import (
	"database/sql"
	"fmt"
)

func updateMemberTypeLoanLimitCalculation(tx *sql.Tx, isSQLite bool) error {
	statements := []string{
		`CREATE OR REPLACE VIEW member_loan_amount_limits AS
			SELECT m.id AS member_id,
				(CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
					* (
						GREATEST(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
						+ GREATEST(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount::NUMERIC WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount::NUMERIC ELSE 0 END),0),0)
					) AS loan_limit
			FROM members m LEFT JOIN saving_records sr ON sr.member_id=m.id
			GROUP BY m.id,m.member_type`,
	}
	if isSQLite {
		statements = []string{
			`DROP VIEW IF EXISTS member_loan_amount_limits`,
			`CREATE VIEW member_loan_amount_limits AS
				SELECT m.id AS member_id,
					(CASE m.member_type WHEN 'contract_worker' THEN 2 WHEN 'employee' THEN 4 WHEN 'daily_worker' THEN 1 WHEN 'customer' THEN 1 ELSE 0 END)
						* (
							MAX(COALESCE(SUM(CASE WHEN sr.category='wajib' AND sr.type='deposit' THEN sr.amount WHEN sr.category='wajib' AND sr.type='withdrawal' THEN -sr.amount ELSE 0 END),0),0)
							+ MAX(COALESCE(SUM(CASE WHEN sr.category='sukarela' AND sr.type='deposit' THEN sr.amount WHEN sr.category='sukarela' AND sr.type='withdrawal' THEN -sr.amount ELSE 0 END),0),0)
						) AS loan_limit
				FROM members m LEFT JOIN saving_records sr ON sr.member_id=m.id
				GROUP BY m.id,m.member_type`,
		}
	}
	for index, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("member-type loan limit calculation statement %d: %w", index, err)
		}
	}
	return nil
}
