package app

import (
	"database/sql"
	"errors"
	"fmt"
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
	Description     string
	RecordedBy      string
	BatchID         string
}

var (
	errInvalidTransactionSource   = errors.New("invalid transaction source")
	errInvalidAccountingDirection = errors.New("invalid accounting direction")
	errCOAAccountNotFound         = errors.New("coa account not found")
	errCOAAccountInactive         = errors.New("coa account inactive")
	errCOAAccountGroup            = errors.New("coa group account cannot be posted directly")
	errJournalAccountConflict     = errors.New("journal source and coa account must differ")
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
		return errInvalidTransactionSource
	}
	if !validAccountingDirection(input.Direction) {
		return errInvalidAccountingDirection
	}
	if input.Amount <= 0 || strings.TrimSpace(input.TransactionID) == "" || strings.TrimSpace(input.TransactionType) == "" || strings.TrimSpace(input.TransactionDate) == "" || strings.TrimSpace(input.RecordedBy) == "" {
		return errInvalidManualCashTransaction
	}
	reference := strings.TrimSpace(input.ReferenceNo)
	if reference == "" {
		reference = "TXN-" + input.TransactionID
	}
	status := "pending_mapping"
	sourceCode := sourceCOACode(input.Source)
	if input.COACode != "" {
		if sourceCode == input.COACode {
			return errJournalAccountConflict
		}
		if _, err := s.coaAccountForPostingTx(tx, input.COACode); err != nil {
			return err
		}
		if _, err := s.coaAccountForPostingTx(tx, sourceCode); err != nil {
			return err
		}
		status = "posted"
	}
	journalID := newID()
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries (id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, journalID, reference, input.TransactionID, input.TransactionType, input.TransactionDate, input.Source, input.Amount, status, strings.TrimSpace(input.BatchID), strings.TrimSpace(input.Description), input.RecordedBy); err != nil {
		return err
	}
	if status != "posted" {
		return nil
	}
	debitCode, creditCode := input.COACode, sourceCode
	if input.Direction == accountingDirectionDebit {
		debitCode, creditCode = sourceCode, input.COACode
	}
	for _, line := range []struct {
		side, code, component string
	}{
		{accountingDirectionDebit, debitCode, "source"},
		{accountingDirectionCredit, creditCode, "counterpart"},
	} {
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines (id,journal_id,side,coa_code,amount,component) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), journalID, line.side, line.code, input.Amount, line.component); err != nil {
			return err
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
