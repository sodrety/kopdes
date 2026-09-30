package app

import (
	"database/sql"
	"strings"
)

const initialJournalBackfillKey = "source_transactions_v1"

type historicalFinancialSource struct {
	id, transactionType, category, loanType string
	date, reference, source, counterpartCOA string
	description, recordedBy, note           string
	amount, principal                       int64
	direction                               string
}

// ensureFinancialJournalsBackfilled performs the one-time, idempotent import of
// existing operational records. The source records remain the system of record;
// this only creates journal entries and lines that are missing.
func (s *Server) ensureFinancialJournalsBackfilled() error {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.backfillFinancialJournalsIfNeededTx(tx); err != nil {
		return err
	}
	if err := s.resolvePendingFinancialJournalsTx(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) backfillFinancialJournalsIfNeededTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`INSERT INTO accounting_backfill_runs(name,completed_at) VALUES($1,NULL) ON CONFLICT(name) DO NOTHING`, initialJournalBackfillKey); err != nil {
		return err
	}
	var complete bool
	if err := tx.QueryRow(`SELECT completed_at IS NOT NULL FROM accounting_backfill_runs WHERE name=$1`+rowLockClause(s.db), initialJournalBackfillKey).Scan(&complete); err != nil {
		return err
	}
	if complete {
		return nil
	}
	if err := s.backfillFinancialJournalsTx(tx); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE accounting_backfill_runs SET completed_at=CURRENT_TIMESTAMP WHERE name=$1 AND completed_at IS NULL`, initialJournalBackfillKey)
	return err
}

func (s *Server) backfillFinancialJournalsTx(tx *sql.Tx) error {
	if err := s.backfillSavingJournalsTx(tx); err != nil {
		return err
	}
	if err := s.backfillLoanJournalsTx(tx); err != nil {
		return err
	}
	if err := s.backfillRepaymentJournalsTx(tx); err != nil {
		return err
	}
	return s.backfillManualCashJournalsTx(tx)
}

func (s *Server) backfillSavingJournalsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT sr.id,sr.type,sr.category,sr.amount,sr.record_date,sr.reference_no,sr.source,COALESCE(sr.coa_code,''),sr.note,sr.recorded_by,m.full_name
		FROM saving_records sr JOIN members m ON m.id=sr.member_id
		WHERE sr.type IN ('deposit','withdrawal') ORDER BY sr.record_date,sr.created_at,sr.id`)
	if err != nil {
		return err
	}
	var items []historicalFinancialSource
	for rows.Next() {
		var item historicalFinancialSource
		if err := rows.Scan(&item.id, &item.transactionType, &item.category, &item.amount, &item.date, &item.reference, &item.source, &item.counterpartCOA, &item.note, &item.recordedBy, &item.description); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		typeName := "savings"
		components := []accountingJournalComponent{
			{Component: "cash_bank", Side: accountingDirectionDebit, Amount: item.amount},
			{Component: "savings_liability", Side: accountingDirectionCredit, Amount: item.amount},
		}
		if item.transactionType == "withdrawal" {
			typeName = "withdrawal"
			components = []accountingJournalComponent{
				{Component: "savings_liability", Side: accountingDirectionDebit, Amount: item.amount},
				{Component: "cash_bank", Side: accountingDirectionCredit, Amount: item.amount},
			}
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: item.reference, TransactionID: item.id, TransactionType: typeName, TransactionDate: item.date,
			Source: item.source, Amount: item.amount, Category: item.category, Components: components,
			COAOverrides: map[string]string{"savings_liability": item.counterpartCOA},
			Description:  "Simpanan " + item.category + " — " + item.description, RecordedBy: item.recordedBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Historical loans are backfilled at disbursed principal only; legacy admin
// fees are intentionally not split into a separate income line.
func (s *Server) backfillLoanJournalsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT l.id,l.loan_request_id,l.loan_type,l.approved_amount,COALESCE(NULLIF(l.start_date,''),SUBSTR(CAST(l.approved_at AS TEXT),1,10),SUBSTR(CAST(l.created_at AS TEXT),1,10)),l.source,COALESCE(l.coa_code,''),l.approved_by,m.full_name
		FROM loans l JOIN members m ON m.id=l.member_id
		WHERE l.status <> 'cancelled' ORDER BY l.start_date,l.created_at,l.id`)
	if err != nil {
		return err
	}
	var items []historicalFinancialSource
	for rows.Next() {
		var item historicalFinancialSource
		if err := rows.Scan(&item.id, &item.reference, &item.loanType, &item.principal, &item.date, &item.source, &item.counterpartCOA, &item.recordedBy, &item.description); err != nil {
			rows.Close()
			return err
		}
		item.date = strings.TrimSpace(item.date)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if item.principal <= 0 {
			continue
		}
		components := []accountingJournalComponent{
			{Component: "loan_receivable", Side: accountingDirectionDebit, Amount: item.principal},
			{Component: "cash_bank", Side: accountingDirectionCredit, Amount: item.principal},
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: item.reference, TransactionID: item.id, TransactionType: "loan", TransactionDate: item.date,
			Source: item.source, Amount: item.principal, LoanType: item.loanType, Components: components,
			COAOverrides: map[string]string{"loan_receivable": item.counterpartCOA},
			Description:  "Pencairan pinjaman — " + item.description, RecordedBy: item.recordedBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) backfillRepaymentJournalsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT lr.id,l.loan_type,lr.amount,lr.record_date,lr.reference_no,lr.source,COALESCE(lr.coa_code,''),lr.recorded_by,m.full_name
		FROM loan_repayments lr JOIN loans l ON l.id=lr.loan_id JOIN members m ON m.id=lr.member_id
		WHERE l.status <> 'cancelled' ORDER BY lr.record_date,lr.created_at,lr.id`)
	if err != nil {
		return err
	}
	var items []historicalFinancialSource
	for rows.Next() {
		var item historicalFinancialSource
		if err := rows.Scan(&item.id, &item.loanType, &item.amount, &item.date, &item.reference, &item.source, &item.counterpartCOA, &item.recordedBy, &item.description); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: item.reference, TransactionID: item.id, TransactionType: "repayment", TransactionDate: item.date,
			Source: item.source, Amount: item.amount, LoanType: item.loanType,
			Components: []accountingJournalComponent{
				{Component: "cash_bank", Side: accountingDirectionDebit, Amount: item.amount},
				{Component: "loan_receivable", Side: accountingDirectionCredit, Amount: item.amount},
			},
			COAOverrides: map[string]string{"loan_receivable": item.counterpartCOA},
			Description:  "Angsuran pinjaman — " + item.description, RecordedBy: item.recordedBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) backfillManualCashJournalsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT mt.id,mt.transaction_date,mt.direction,mt.source,COALESCE(mt.coa_code,''),mt.amount,mt.reference_no,mt.description,mt.note,mt.recorded_by,COALESCE(NULLIF(c.category_key,''),mt.category_id,''),COALESCE(c.account_code,'')
		FROM manual_cash_transactions mt LEFT JOIN cash_transaction_categories c ON c.id=mt.category_id
		ORDER BY mt.transaction_date,mt.created_at,mt.id`)
	if err != nil {
		return err
	}
	var items []historicalFinancialSource
	for rows.Next() {
		var item historicalFinancialSource
		var categoryCOA string
		if err := rows.Scan(&item.id, &item.date, &item.direction, &item.source, &item.counterpartCOA, &item.amount, &item.reference, &item.description, &item.note, &item.recordedBy, &item.category, &categoryCOA); err != nil {
			rows.Close()
			return err
		}
		if item.counterpartCOA == "" {
			item.counterpartCOA = categoryCOA
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		cashSide, counterpartSide := accountingDirectionDebit, accountingDirectionCredit
		if item.direction == "cash_out" {
			cashSide, counterpartSide = accountingDirectionCredit, accountingDirectionDebit
		} else if item.direction != "cash_in" {
			continue
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: item.reference, TransactionID: item.id, TransactionType: "manual", TransactionDate: item.date,
			Source: item.source, Amount: item.amount, Category: item.category,
			Components: []accountingJournalComponent{
				{Component: "cash_bank", Side: cashSide, Amount: item.amount},
				{Component: "manual_counterpart", Side: counterpartSide, Amount: item.amount},
			},
			COAOverrides: map[string]string{"manual_counterpart": item.counterpartCOA},
			Description:  item.description, RecordedBy: item.recordedBy,
		}); err != nil {
			return err
		}
	}
	return nil
}
