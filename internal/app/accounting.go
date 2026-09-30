package app

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	transactionSourceCash = "cash"
	transactionSourceBank = "bank"

	accountingDirectionDebit  = "debit"
	accountingDirectionCredit = "credit"
)

type COAAccount struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	ParentCode    string `json:"parent_code,omitempty"`
	AccountType   string `json:"account_type"`
	Subtype       string `json:"subtype,omitempty"`
	NormalBalance string `json:"normal_balance"`
	IsGroup       bool   `json:"is_group"`
	Active        bool   `json:"active"`
	SystemKey     string `json:"system_key,omitempty"`
}

type accountingJournalInput struct {
	ReferenceNo     string
	TransactionID   string
	TransactionType string
	TransactionDate string
	Source          string
	Amount          int64
	Direction       string
	COACode         string
	Category        string
	LoanType        string
	Components      []accountingJournalComponent
	Lines           []accountingJournalLineInput
	COAOverrides    map[string]string
	Description     string
	RecordedBy      string
	BatchID         string
}

type accountingJournalComponent struct {
	Component string
	Side      string
	Amount    int64
}

type accountingJournalLineInput struct {
	Component string
	Side      string
	COACode   string
	Amount    int64
}

var (
	errInvalidTransactionSource       = errors.New("invalid transaction source")
	errInvalidAccountingDirection     = errors.New("invalid accounting direction")
	errCOAAccountNotFound             = errors.New("coa account not found")
	errCOAAccountInactive             = errors.New("coa account inactive")
	errCOAAccountGroup                = errors.New("coa group account cannot be posted directly")
	errJournalAccountConflict         = errors.New("journal source and coa account must differ")
	errUnbalancedFinancialJournal     = errors.New("financial journal debits and credits must balance")
	errAccountingCOAOverrideForbidden = errors.New("per-transaction coa override is not permitted for this role")
)

func validTransactionSource(value string) bool {
	return value == transactionSourceCash || value == transactionSourceBank
}

func normalizeTransactionSource(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return transactionSourceBank
	}
	return value
}

func validAccountingDirection(value string) bool {
	return value == accountingDirectionDebit || value == accountingDirectionCredit
}

func legacyDirectionForAccountingDirection(value string) string {
	if value == accountingDirectionCredit {
		return "cash_out"
	}
	return "cash_in"
}

func accountingDirectionForLegacyDirection(value string) string {
	if value == "cash_out" {
		return accountingDirectionCredit
	}
	return accountingDirectionDebit
}

func sourceCOACode(source string) string {
	if source == transactionSourceCash {
		return "CASH"
	}
	return "BANK"
}

func (s *Server) coaAccountsForAdmin(includeInactive bool) ([]COAAccount, error) {
	query := `SELECT id,code,name,COALESCE(parent_code,''),account_type,COALESCE(subtype,''),normal_balance,is_group,active,COALESCE(system_key,'') FROM coa_accounts`
	if !includeInactive {
		query += ` WHERE active=TRUE`
	}
	query += ` ORDER BY code`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []COAAccount
	for rows.Next() {
		var account COAAccount
		if err := rows.Scan(&account.ID, &account.Code, &account.Name, &account.ParentCode, &account.AccountType, &account.Subtype, &account.NormalBalance, &account.IsGroup, &account.Active, &account.SystemKey); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Server) coaPostingAccountsForAdmin() ([]COAAccount, error) {
	accounts, err := s.coaAccountsForAdmin(false)
	if err != nil {
		return nil, err
	}
	posting := make([]COAAccount, 0, len(accounts))
	for _, account := range accounts {
		if !account.IsGroup {
			posting = append(posting, account)
		}
	}
	return posting, nil
}

func (s *Server) adminCOAAccounts(c *gin.Context) {
	accounts, err := s.coaAccountsForAdmin(c.Query("include_inactive") == "true")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"accounts": accounts})
}

func (s *Server) coaAccountForPostingTx(tx *sql.Tx, code string) (COAAccount, error) {
	var account COAAccount
	err := tx.QueryRow(`SELECT id,code,name,COALESCE(parent_code,''),account_type,COALESCE(subtype,''),normal_balance,is_group,active,COALESCE(system_key,'') FROM coa_accounts WHERE code=$1`, code).Scan(
		&account.ID, &account.Code, &account.Name, &account.ParentCode, &account.AccountType, &account.Subtype, &account.NormalBalance, &account.IsGroup, &account.Active, &account.SystemKey,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return COAAccount{}, errCOAAccountNotFound
	}
	if err != nil {
		return COAAccount{}, err
	}
	if !account.Active {
		return COAAccount{}, errCOAAccountInactive
	}
	if account.IsGroup {
		return COAAccount{}, errCOAAccountGroup
	}
	return account, nil
}

func (s *Server) createFinancialJournalTx(tx *sql.Tx, input accountingJournalInput) error {
	input.Source = normalizeTransactionSource(input.Source)
	input.Direction = strings.ToLower(strings.TrimSpace(input.Direction))
	input.COACode = strings.TrimSpace(input.COACode)
	if !validTransactionSource(input.Source) {
		input.Source = transactionSourceBank
	}
	if input.Amount <= 0 || strings.TrimSpace(input.TransactionID) == "" || strings.TrimSpace(input.TransactionType) == "" || strings.TrimSpace(input.TransactionDate) == "" || strings.TrimSpace(input.RecordedBy) == "" {
		return errInvalidManualCashTransaction
	}
	input.TransactionType = strings.ToLower(strings.TrimSpace(input.TransactionType))
	input.Category = strings.ToLower(strings.TrimSpace(input.Category))
	input.LoanType = strings.ToLower(strings.TrimSpace(input.LoanType))
	reference := strings.TrimSpace(input.ReferenceNo)
	if reference == "" {
		reference = "TXN-" + input.TransactionID
	}

	lines := append([]accountingJournalLineInput(nil), input.Lines...)
	if len(lines) == 0 {
		for _, component := range input.Components {
			lines = append(lines, accountingJournalLineInput{Component: component.Component, Side: component.Side, Amount: component.Amount})
		}
	}
	if len(lines) == 0 {
		if !validAccountingDirection(input.Direction) {
			return errInvalidAccountingDirection
		}
		sourceCode := sourceCOACode(input.Source)
		debitCode, creditCode := input.COACode, sourceCode
		if input.Direction == accountingDirectionDebit {
			debitCode, creditCode = sourceCode, input.COACode
		}
		lines = []accountingJournalLineInput{
			{Component: "source", Side: accountingDirectionDebit, COACode: debitCode, Amount: input.Amount},
			{Component: "counterpart", Side: accountingDirectionCredit, COACode: creditCode, Amount: input.Amount},
		}
	}

	var totalDebits, totalCredits int64
	for index := range lines {
		lines[index].Component = strings.ToLower(strings.TrimSpace(lines[index].Component))
		lines[index].Side = strings.ToLower(strings.TrimSpace(lines[index].Side))
		lines[index].COACode = strings.TrimSpace(lines[index].COACode)
		if lines[index].Component == "" || !validAccountingDirection(lines[index].Side) || lines[index].Amount <= 0 {
			return errInvalidManualCashTransaction
		}
		if lines[index].Side == accountingDirectionDebit {
			if lines[index].Amount > math.MaxInt64-totalDebits {
				return errUnbalancedFinancialJournal
			}
			totalDebits += lines[index].Amount
		} else {
			if lines[index].Amount > math.MaxInt64-totalCredits {
				return errUnbalancedFinancialJournal
			}
			totalCredits += lines[index].Amount
		}
	}
	if totalDebits <= 0 || totalDebits != totalCredits {
		return errUnbalancedFinancialJournal
	}

	var journalID, existingStatus string
	err := tx.QueryRow(`SELECT id,status FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type=$2 ORDER BY created_at,id LIMIT 1`+rowLockClause(s.db), input.TransactionID, input.TransactionType).Scan(&journalID, &existingStatus)
	if err == nil && existingStatus == "posted" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		journalID = newID()
		if _, err := tx.Exec(`INSERT INTO financial_journal_entries (id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending_mapping',$8,$9,$10,$11,$12)`, journalID, reference, input.TransactionID, input.TransactionType, input.TransactionDate, input.Source, totalDebits, strings.TrimSpace(input.BatchID), strings.TrimSpace(input.Description), input.RecordedBy, input.Category, input.LoanType); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`DELETE FROM financial_journal_lines WHERE journal_id=$1`, journalID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE financial_journal_entries SET reference_no=$1,transaction_date=$2,source=$3,amount=$4,batch_id=$5,description=$6,recorded_by=$7,category=$8,loan_type=$9 WHERE id=$10 AND status='pending_mapping'`, reference, input.TransactionDate, input.Source, totalDebits, strings.TrimSpace(input.BatchID), strings.TrimSpace(input.Description), input.RecordedBy, input.Category, input.LoanType, journalID); err != nil {
			return err
		}
	}

	allMapped := true
	manualLineSelections := len(input.Lines) > 0
	for _, line := range lines {
		coaCode := line.COACode
		isOverride := coaCode != "" && !manualLineSelections
		if coaCode == "" {
			coaCode = strings.TrimSpace(input.COAOverrides[line.Component])
			isOverride = coaCode != ""
		}
		if coaCode == "" {
			coaCode, err = s.accountingMappingCOACodeTx(tx, input.TransactionType, line.Component, input.Category, input.LoanType, input.TransactionDate)
			if err != nil {
				return err
			}
		}
		if coaCode == "" {
			allMapped = false
		} else if _, err := s.coaAccountForPostingTx(tx, coaCode); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines (id,journal_id,side,coa_code,amount,component,mapping_override) VALUES ($1,$2,$3,$4,$5,$6,$7)`, newID(), journalID, line.Side, coaCode, line.Amount, line.Component, isOverride); err != nil {
			return err
		}
	}
	if allMapped {
		_, err = tx.Exec(`UPDATE financial_journal_entries SET status='posted' WHERE id=$1 AND status='pending_mapping'`, journalID)
	}
	return err
}

func (s *Server) accountingMappingCOACodeTx(tx *sql.Tx, transactionType, component, category, loanType, transactionDate string) (string, error) {
	var code string
	err := tx.QueryRow(`SELECT COALESCE(coa_code,'') FROM accounting_mappings
		WHERE transaction_type=$1 AND component=$2 AND active=TRUE
		  AND (category=$3 OR category='') AND (loan_type=$4 OR loan_type='')
		  AND (effective_from IS NULL OR effective_from <= $5)
		  AND (effective_to IS NULL OR effective_to >= $5)
		ORDER BY CASE WHEN category=$3 THEN 0 ELSE 1 END,
		         CASE WHEN loan_type=$4 THEN 0 ELSE 1 END,
		         COALESCE(effective_from,'0000-00-00') DESC,id
		LIMIT 1`, transactionType, component, category, loanType, transactionDate).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}

func (s *Server) resolvePendingFinancialJournalsTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,transaction_type,category,loan_type,transaction_date FROM financial_journal_entries WHERE status='pending_mapping' ORDER BY transaction_date,id`)
	if err != nil {
		return err
	}
	type pendingJournal struct{ id, transactionType, category, loanType, date string }
	var journals []pendingJournal
	for rows.Next() {
		var item pendingJournal
		if err := rows.Scan(&item.id, &item.transactionType, &item.category, &item.loanType, &item.date); err != nil {
			rows.Close()
			return err
		}
		journals = append(journals, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, journal := range journals {
		var totalLines int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM financial_journal_lines WHERE journal_id=$1`, journal.id).Scan(&totalLines); err != nil {
			return err
		}
		if totalLines == 0 {
			// Backfill populates the template lines before resolving mappings. Never
			// promote an empty journal just because it has no unresolved lines.
			continue
		}
		lines, err := tx.Query(`SELECT id,component FROM financial_journal_lines WHERE journal_id=$1 AND coa_code=''`, journal.id)
		if err != nil {
			return err
		}
		type pendingLine struct{ id, component string }
		var pending []pendingLine
		for lines.Next() {
			var line pendingLine
			if err := lines.Scan(&line.id, &line.component); err != nil {
				lines.Close()
				return err
			}
			pending = append(pending, line)
		}
		if err := lines.Err(); err != nil {
			lines.Close()
			return err
		}
		if err := lines.Close(); err != nil {
			return err
		}
		allMapped := len(pending) == 0 && totalLines > 0
		for _, line := range pending {
			code, err := s.accountingMappingCOACodeTx(tx, journal.transactionType, line.component, journal.category, journal.loanType, journal.date)
			if err != nil {
				return err
			}
			if code == "" {
				allMapped = false
				continue
			}
			if _, err := s.coaAccountForPostingTx(tx, code); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE financial_journal_lines SET coa_code=$1 WHERE id=$2 AND coa_code=''`, code, line.id); err != nil {
				return err
			}
		}
		if allMapped {
			if _, err := tx.Exec(`UPDATE financial_journal_entries SET status='posted' WHERE id=$1 AND status='pending_mapping'`, journal.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func recordFinancialTransactionAuditTx(tx *sql.Tx, transactionID, transactionType, actorID, fieldName, oldValue, newValue, batchID string) error {
	_, err := tx.Exec(`INSERT INTO financial_transaction_audits (id,transaction_id,transaction_type,actor_id,field_name,old_value,new_value,batch_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, newID(), transactionID, transactionType, actorID, fieldName, oldValue, newValue, batchID)
	return err
}

func (s *Server) updateCashTransactionSource(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req struct {
		Source string `json:"source" form:"source"`
	}
	if err := c.ShouldBind(&req); err != nil || !validTransactionSource(strings.ToLower(strings.TrimSpace(req.Source))) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_transaction_source"))
		return
	}
	if err := s.changeCashTransactionSource(c.Param("id"), strings.ToLower(strings.TrimSpace(req.Source)), user.ID); errors.Is(err, errInvalidTransactionSource) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_transaction_source"))
		return
	} else if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_transaction_not_found"))
		return
	} else if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/transactions", gin.H{"id": c.Param("id"), "source": req.Source})
}

func (s *Server) updateCashTransactionSources(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req struct {
		Source string   `json:"source" form:"source"`
		IDs    []string `json:"ids" form:"ids"`
	}
	if err := c.ShouldBind(&req); err != nil || !validTransactionSource(strings.ToLower(strings.TrimSpace(req.Source))) || len(req.IDs) == 0 {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_transaction_source"))
		return
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer func() { _ = tx.Rollback() }()
	batchID := newID()
	for _, id := range req.IDs {
		if err := s.changeCashTransactionSourceTx(tx, strings.TrimSpace(id), source, user.ID, batchID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_transaction_not_found"))
			} else {
				respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_transaction_source"))
			}
			return
		}
	}
	if err := recordFinancialTransactionAuditTx(tx, batchID, "source_batch", user.ID, "batch_source", "", source, batchID); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/transactions", gin.H{"batch_id": batchID, "source": source, "updated": len(req.IDs)})
}

func (s *Server) changeCashTransactionSource(transactionID, source, actorID string) error {
	transactionID = strings.TrimSpace(transactionID)
	source = normalizeTransactionSource(source)
	if transactionID == "" || !validTransactionSource(source) {
		return errInvalidTransactionSource
	}
	parts := strings.SplitN(transactionID, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return sql.ErrNoRows
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.changeCashTransactionSourceTx(tx, transactionID, source, actorID, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) changeCashTransactionSourceTx(tx *sql.Tx, transactionID, source, actorID, batchID string) error {
	transactionID = strings.TrimSpace(transactionID)
	source = normalizeTransactionSource(source)
	if transactionID == "" || !validTransactionSource(source) {
		return errInvalidTransactionSource
	}
	parts := strings.SplitN(transactionID, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return sql.ErrNoRows
	}
	kind, id := parts[0], parts[1]
	var oldSource string
	var transactionType string
	var table string
	switch kind {
	case "saving":
		table, transactionType = "saving_records", "savings"
	case "withdrawal":
		table, transactionType = "saving_records", "withdrawal"
	case "loan":
		table, transactionType = "loans", "loan"
	case "repayment":
		table, transactionType = "loan_repayments", "repayment"
	case "manual":
		table, transactionType = "manual_cash_transactions", "manual"
	default:
		return sql.ErrNoRows
	}
	if err := tx.QueryRow(`SELECT source FROM `+table+` WHERE id=$1`+rowLockClause(s.db), id).Scan(&oldSource); err != nil {
		return err
	}
	oldSource = normalizeTransactionSource(oldSource)
	if oldSource == source {
		return nil
	}
	if _, err := tx.Exec(`UPDATE `+table+` SET source=$1 WHERE id=$2`, source, id); err != nil {
		return err
	}
	if err := recordFinancialTransactionAuditTx(tx, id, transactionType, actorID, "source", oldSource, source, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE financial_journal_entries SET source=$1 WHERE transaction_id=$2 AND transaction_type=$3`, source, id, transactionType); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE financial_journal_lines SET coa_code=$1 WHERE component='source' AND journal_id IN (SELECT id FROM financial_journal_entries WHERE transaction_id=$2 AND transaction_type=$3) AND coa_code=$4`, sourceCOACode(source), id, transactionType, sourceCOACode(oldSource)); err != nil {
		return err
	}
	return nil
}

func relaxManualCashTransactionSourceProtection(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		var tableExists bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='manual_cash_transactions')`).Scan(&tableExists); err != nil {
			return err
		}
		if !tableExists {
			return nil
		}
		for _, statement := range []string{
			`DROP TRIGGER IF EXISTS protect_manual_cash_transactions_immutable`,
			`DROP TRIGGER IF EXISTS protect_manual_cash_transactions_delete`,
			`DROP TRIGGER IF EXISTS protect_cash_transaction_category_name`,
			`DROP INDEX IF EXISTS idx_manual_cash_transactions_date`,
			`DROP INDEX IF EXISTS idx_manual_cash_transactions_direction_category`,
			`CREATE TABLE manual_cash_transactions_v31 (
				id TEXT PRIMARY KEY,
				transaction_date TEXT NOT NULL,
				direction TEXT NOT NULL CHECK (direction IN ('cash_in','cash_out')),
				category_id TEXT NULL,
				description TEXT NOT NULL,
				amount BIGINT NOT NULL CHECK (amount > 0),
				reference_no TEXT NOT NULL UNIQUE,
				note TEXT NOT NULL DEFAULT '',
				recorded_by TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				source TEXT NOT NULL DEFAULT 'bank' CHECK (source IN ('cash','bank')),
				coa_code TEXT,
				accounting_direction TEXT CHECK (accounting_direction IN ('debit','credit')),
				FOREIGN KEY (category_id) REFERENCES cash_transaction_categories(id),
				FOREIGN KEY (recorded_by) REFERENCES users(id)
			)`,
			`INSERT INTO manual_cash_transactions_v31 (id,transaction_date,direction,category_id,description,amount,reference_no,note,recorded_by,created_at,source,coa_code,accounting_direction)
				SELECT id,transaction_date,direction,category_id,description,amount,reference_no,note,recorded_by,created_at,source,coa_code,accounting_direction FROM manual_cash_transactions`,
			`DROP TABLE manual_cash_transactions`,
			`ALTER TABLE manual_cash_transactions_v31 RENAME TO manual_cash_transactions`,
			`CREATE INDEX idx_manual_cash_transactions_date ON manual_cash_transactions(transaction_date, created_at)`,
			`CREATE INDEX idx_manual_cash_transactions_direction_category ON manual_cash_transactions(direction, category_id)`,
		} {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("relax manual cash transaction category: %w", err)
			}
		}
		for _, statement := range []string{
			`CREATE TRIGGER protect_manual_cash_transactions_immutable
				 BEFORE UPDATE ON manual_cash_transactions
				 WHEN NEW.transaction_date IS NOT OLD.transaction_date
				   OR NEW.direction IS NOT OLD.direction
				   OR NEW.category_id IS NOT OLD.category_id
				   OR NEW.description IS NOT OLD.description
				   OR NEW.amount IS NOT OLD.amount
				   OR NEW.reference_no IS NOT OLD.reference_no
				   OR NEW.note IS NOT OLD.note
				   OR NEW.recorded_by IS NOT OLD.recorded_by
				   OR NEW.created_at IS NOT OLD.created_at
				 BEGIN SELECT RAISE(ABORT, 'manual cash transaction fields are immutable'); END`,
			`CREATE TRIGGER protect_manual_cash_transactions_delete
				 BEFORE DELETE ON manual_cash_transactions
				 BEGIN SELECT RAISE(ABORT, 'manual cash transactions are immutable'); END`,
			`CREATE TRIGGER protect_cash_transaction_category_name
				 BEFORE UPDATE OF name ON cash_transaction_categories
				 WHEN EXISTS (SELECT 1 FROM manual_cash_transactions WHERE category_id=OLD.id)
				 BEGIN SELECT RAISE(ABORT, 'used cash transaction category names are immutable'); END`,
		} {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		return nil
	}
	for _, statement := range []string{
		`ALTER TABLE manual_cash_transactions ALTER COLUMN category_id DROP NOT NULL`,
		`DROP TRIGGER IF EXISTS protect_manual_cash_transactions_immutable ON manual_cash_transactions`,
		`DROP FUNCTION IF EXISTS protect_manual_cash_transactions_immutable()`,
		`CREATE FUNCTION protect_manual_cash_transactions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF TG_OP = 'DELETE' THEN
				RAISE EXCEPTION 'manual cash transactions are immutable';
			END IF;
			IF NEW.transaction_date IS DISTINCT FROM OLD.transaction_date
			   OR NEW.direction IS DISTINCT FROM OLD.direction
			   OR NEW.category_id IS DISTINCT FROM OLD.category_id
			   OR NEW.description IS DISTINCT FROM OLD.description
			   OR NEW.amount IS DISTINCT FROM OLD.amount
			   OR NEW.reference_no IS DISTINCT FROM OLD.reference_no
			   OR NEW.note IS DISTINCT FROM OLD.note
			   OR NEW.recorded_by IS DISTINCT FROM OLD.recorded_by
			   OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
				RAISE EXCEPTION 'manual cash transaction fields are immutable';
			END IF;
			RETURN NEW;
		END $$`,
		`CREATE TRIGGER protect_manual_cash_transactions_immutable BEFORE UPDATE OR DELETE ON manual_cash_transactions FOR EACH ROW EXECUTE FUNCTION protect_manual_cash_transactions_immutable()`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
