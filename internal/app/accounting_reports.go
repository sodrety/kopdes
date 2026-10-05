package app

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type FinancialJournalLineView struct {
	ID              string
	COACode         string
	COAName         string
	Component       string
	Side            string
	Amount          int64
	MappingOverride bool
}

type FinancialJournalEntryView struct {
	ID               string
	ReferenceNo      string
	TransactionType  string
	Date             string
	Description      string
	Category         string
	LoanType         string
	Status           string
	RecordedBy       string
	ReversalOf       string
	CorrectionReason string
	TotalDebit       int64
	TotalCredit      int64
	CanReverse       bool
	Lines            []FinancialJournalLineView
}

type FinancialLedgerLine struct {
	Date        string
	ReferenceNo string
	Description string
	Side        string
	Debit       int64
	Credit      int64
	Balance     int64
}

type FinancialTrialBalanceRow struct {
	Code, Name, AccountType, NormalBalance string
	Debit, Credit, Balance                 int64
}

type financialJournalReverseRequest struct {
	Reason string `json:"reason" form:"reason"`
	Date   string `json:"date" form:"date"`
}

var (
	errJournalNotPosted       = errors.New("journal entry is not posted")
	errJournalAlreadyReversed = errors.New("journal entry already reversed")
	errInvalidJournalReversal = errors.New("invalid journal reversal")
)

func (s *Server) adminJournalsPage(c *gin.Context) {
	if err := s.ensureFinancialJournalsBackfilled(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	view := strings.TrimSpace(c.Query("view"))
	if view == "" {
		view = "journal"
	}
	if view != "journal" && view != "ledger" && view != "trial_balance" {
		view = "journal"
	}
	dateFrom, dateTo := strings.TrimSpace(c.Query("date_from")), strings.TrimSpace(c.Query("date_to"))
	data := gin.H{"View": view, "DateFrom": dateFrom, "DateTo": dateTo, "DefaultDate": time.Now().In(jakartaLocation).Format("2006-01-02"), "InitialJournalLines": []int{0, 1}}
	accounts, err := s.coaPostingAccountsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	data["COAAccounts"] = accounts
	switch view {
	case "ledger":
		code := strings.TrimSpace(c.Query("account"))
		data["SelectedAccount"] = code
		ledger, opening, ending, account, err := s.financialLedgerForAdmin(code, dateFrom, dateTo)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		data["Ledger"] = ledger
		data["LedgerOpening"] = opening
		data["LedgerEnding"] = ending
		data["LedgerAccount"] = account
	case "trial_balance":
		rows, totals, err := s.financialTrialBalanceForAdmin(dateFrom, dateTo)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		data["TrialBalance"] = rows
		data["TrialDebit"] = totals[0]
		data["TrialCredit"] = totals[1]
	default:
		entries, err := s.financialJournalsForAdmin(dateFrom, dateTo)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		data["Entries"] = entries
	}
	current, _ := currentUser(c)
	data["CanRecord"] = hasPermission(current.Role, PermissionJournalsManage)
	data["CanApprove"] = hasPermission(current.Role, PermissionJournalsApprove)
	data["JournalFormLines"] = []manualCashJournalLineRequest{{}, {}}
	drafts, err := s.manualCashTransactionDraftsForAdmin("journal")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	data["JournalDrafts"] = drafts
	draftID := strings.TrimSpace(c.Query("draft"))
	if draftID != "" && view == "journal" {
		if !hasPermission(current.Role, PermissionJournalsManage) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_manual_cash_draft_not_found"))
			return
		}
		draft, err := s.manualCashTransactionDraftForAdmin(draftID, "journal")
		if err != nil {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_manual_cash_draft_not_found"))
			return
		}
		lines := draft.Lines
		if len(lines) < 2 {
			lines = append(lines, manualCashJournalLineRequest{})
		}
		data["JournalEditDraft"] = draft
		data["JournalDraftID"] = draft.ID
		data["JournalFormLines"] = lines
	}
	renderPage(c, "admin-journals", pageData(c, translate(languageFromRequest(c), "journals_page_title"), "journals", "journals", "journals_page_description", data))
}

func (s *Server) financialJournalsForAdmin(dateFrom, dateTo string) ([]FinancialJournalEntryView, error) {
	query := `SELECT e.id,e.reference_no,e.transaction_type,e.transaction_date,e.description,e.category,e.loan_type,e.status,e.recorded_by,COALESCE(e.reversal_of,''),e.correction_reason,
		COALESCE(SUM(CASE WHEN l.side='debit' THEN l.amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN l.side='credit' THEN l.amount ELSE 0 END),0),
		(e.status='posted' AND NOT EXISTS(SELECT 1 FROM financial_journal_entries r WHERE r.reversal_of=e.id))
		FROM financial_journal_entries e LEFT JOIN financial_journal_lines l ON l.journal_id=e.id WHERE 1=1`
	args := []any{}
	if dateFrom != "" {
		args = append(args, dateFrom)
		query += ` AND e.transaction_date >= $` + strconv.Itoa(len(args))
	}
	if dateTo != "" {
		args = append(args, dateTo)
		query += ` AND e.transaction_date <= $` + strconv.Itoa(len(args))
	}
	query += ` GROUP BY e.id ORDER BY e.transaction_date DESC,e.created_at DESC,e.id DESC LIMIT 500`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]FinancialJournalEntryView, 0)
	for rows.Next() {
		var item FinancialJournalEntryView
		if err := rows.Scan(&item.ID, &item.ReferenceNo, &item.TransactionType, &item.Date, &item.Description, &item.Category, &item.LoanType, &item.Status, &item.RecordedBy, &item.ReversalOf, &item.CorrectionReason, &item.TotalDebit, &item.TotalCredit, &item.CanReverse); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range entries {
		lines, err := s.db.Query(`SELECT l.id,COALESCE(l.coa_code,''),COALESCE(a.name,''),l.component,l.side,l.amount,l.mapping_override
			FROM financial_journal_lines l LEFT JOIN coa_accounts a ON a.code=l.coa_code WHERE l.journal_id=$1 ORDER BY l.created_at,l.id`, entries[index].ID)
		if err != nil {
			return nil, err
		}
		for lines.Next() {
			var line FinancialJournalLineView
			if err := lines.Scan(&line.ID, &line.COACode, &line.COAName, &line.Component, &line.Side, &line.Amount, &line.MappingOverride); err != nil {
				lines.Close()
				return nil, err
			}
			entries[index].Lines = append(entries[index].Lines, line)
		}
		if err := lines.Err(); err != nil {
			lines.Close()
			return nil, err
		}
		if err := lines.Close(); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (s *Server) financialLedgerForAdmin(code, dateFrom, dateTo string) ([]FinancialLedgerLine, int64, int64, COAAccount, error) {
	var selected COAAccount
	if strings.TrimSpace(code) == "" {
		return nil, 0, 0, selected, sql.ErrNoRows
	}
	if err := s.db.QueryRow(`SELECT id,code,name,COALESCE(parent_code,''),account_type,COALESCE(subtype,''),normal_balance,is_group,active,COALESCE(system_key,'') FROM coa_accounts WHERE code=$1`, code).Scan(&selected.ID, &selected.Code, &selected.Name, &selected.ParentCode, &selected.AccountType, &selected.Subtype, &selected.NormalBalance, &selected.IsGroup, &selected.Active, &selected.SystemKey); err != nil {
		return nil, 0, 0, selected, err
	}
	if !selected.Active || selected.IsGroup {
		return nil, 0, 0, selected, sql.ErrNoRows
	}
	opening := int64(0)
	if dateFrom != "" {
		if err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN l.side='debit' THEN l.amount ELSE -l.amount END),0)
			FROM financial_journal_lines l JOIN financial_journal_entries e ON e.id=l.journal_id
			WHERE e.status='posted' AND l.coa_code=$1 AND e.transaction_date < $2`, code, dateFrom).Scan(&opening); err != nil {
			return nil, 0, 0, selected, err
		}
	}
	query := `SELECT e.transaction_date,e.reference_no,e.description,l.side,l.amount FROM financial_journal_lines l JOIN financial_journal_entries e ON e.id=l.journal_id WHERE e.status='posted' AND l.coa_code=$1`
	args := []any{code}
	if dateFrom != "" {
		args = append(args, dateFrom)
		query += ` AND e.transaction_date >= $` + strconv.Itoa(len(args))
	}
	if dateTo != "" {
		args = append(args, dateTo)
		query += ` AND e.transaction_date <= $` + strconv.Itoa(len(args))
	}
	query += ` ORDER BY e.transaction_date,e.created_at,e.id,l.created_at,l.id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, 0, selected, err
	}
	ledger := make([]FinancialLedgerLine, 0)
	balance := opening
	for rows.Next() {
		var line FinancialLedgerLine
		var amount int64
		if err := rows.Scan(&line.Date, &line.ReferenceNo, &line.Description, &line.Side, &amount); err != nil {
			rows.Close()
			return nil, 0, 0, selected, err
		}
		if line.Side == accountingDirectionDebit {
			line.Debit = amount
			balance += amount
		} else {
			line.Credit = amount
			balance -= amount
		}
		line.Balance = balance
		ledger = append(ledger, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, 0, selected, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, 0, selected, err
	}
	if selected.NormalBalance == "C" {
		opening = -opening
		balance = -balance
		for index := range ledger {
			ledger[index].Balance = -ledger[index].Balance
		}
	}
	return ledger, opening, balance, selected, nil
}

func (s *Server) financialTrialBalanceForAdmin(dateFrom, dateTo string) ([]FinancialTrialBalanceRow, [2]int64, error) {
	query := `SELECT a.code,a.name,a.account_type,a.normal_balance,
		COALESCE(SUM(CASE WHEN e.id IS NOT NULL AND l.side='debit' THEN l.amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN e.id IS NOT NULL AND l.side='credit' THEN l.amount ELSE 0 END),0)
		FROM coa_accounts a LEFT JOIN financial_journal_lines l ON l.coa_code=a.code
		LEFT JOIN financial_journal_entries e ON e.id=l.journal_id AND e.status='posted'`
	args := []any{}
	if dateFrom != "" {
		args = append(args, dateFrom)
		query += ` AND e.transaction_date >= $` + strconv.Itoa(len(args))
	}
	if dateTo != "" {
		args = append(args, dateTo)
		query += ` AND e.transaction_date <= $` + strconv.Itoa(len(args))
	}
	query += ` WHERE a.active=TRUE AND a.is_group=FALSE GROUP BY a.code,a.name,a.account_type,a.normal_balance ORDER BY a.code`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, [2]int64{}, err
	}
	result := make([]FinancialTrialBalanceRow, 0)
	var totals [2]int64
	for rows.Next() {
		var item FinancialTrialBalanceRow
		if err := rows.Scan(&item.Code, &item.Name, &item.AccountType, &item.NormalBalance, &item.Debit, &item.Credit); err != nil {
			rows.Close()
			return nil, totals, err
		}
		if item.NormalBalance == "C" {
			item.Balance = item.Credit - item.Debit
		} else {
			item.Balance = item.Debit - item.Credit
		}
		totals[0] += item.Debit
		totals[1] += item.Credit
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, totals, err
	}
	return result, totals, rows.Close()
}

func (s *Server) reverseFinancialJournal(c *gin.Context) {
	var req financialJournalReverseRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_journal_reversal"))
		return
	}
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	if len(strings.TrimSpace(req.Reason)) == 0 || len(strings.TrimSpace(req.Reason)) > 500 {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_journal_reversal"))
		return
	}
	date := strings.TrimSpace(req.Date)
	if date == "" {
		date = time.Now().In(jakartaLocation).Format("2006-01-02")
	}
	if parsed, err := time.ParseInLocation("2006-01-02", date, jakartaLocation); err != nil || parsed.Format("2006-01-02") != date {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_journal_reversal"))
		return
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	err := s.reverseFinancialJournalTx(c.Param("id"), user.ID, date, strings.TrimSpace(req.Reason))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_journal_not_found"))
	case errors.Is(err, errJournalNotPosted):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_journal_not_posted"))
	case errors.Is(err, errJournalAlreadyReversed):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_journal_already_reversed"))
	case errors.Is(err, errInvalidJournalReversal):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_journal_reversal"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondOKOrHXRedirect(c, "/admin/journals", gin.H{"status": "reversed"})
	}
}

func (s *Server) reverseFinancialJournalTx(journalID, actorID, reversalDate, reason string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var entry FinancialJournalEntryView
	err = tx.QueryRow(`SELECT id,reference_no,transaction_type,transaction_date,description,category,loan_type,status,recorded_by FROM financial_journal_entries WHERE id=$1`+rowLockClause(s.db), journalID).
		Scan(&entry.ID, &entry.ReferenceNo, &entry.TransactionType, &entry.Date, &entry.Description, &entry.Category, &entry.LoanType, &entry.Status, &entry.RecordedBy)
	if err != nil {
		return err
	}
	if entry.Status != "posted" {
		return errJournalNotPosted
	}
	var reversed bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM financial_journal_entries WHERE reversal_of=$1)`, journalID).Scan(&reversed); err != nil {
		return err
	}
	if reversed {
		return errJournalAlreadyReversed
	}
	rows, err := tx.Query(`SELECT l.side,l.coa_code,l.amount,l.component,a.account_type,COALESCE(a.subtype,''),COALESCE(a.system_key,'')
		FROM financial_journal_lines l JOIN coa_accounts a ON a.code=l.coa_code WHERE l.journal_id=$1 ORDER BY l.created_at,l.id`, journalID)
	if err != nil {
		return err
	}
	type reverseLine struct {
		side, code, component, accountType, subtype, systemKey string
		amount                                                 int64
	}
	var lines []reverseLine
	var debits, credits, cashNet int64
	for rows.Next() {
		var line reverseLine
		if err := rows.Scan(&line.side, &line.code, &line.amount, &line.component, &line.accountType, &line.subtype, &line.systemKey); err != nil {
			rows.Close()
			return err
		}
		if line.amount <= 0 {
			rows.Close()
			return errInvalidJournalReversal
		}
		if line.side == accountingDirectionDebit {
			if line.amount > math.MaxInt64-debits {
				rows.Close()
				return errInvalidJournalReversal
			}
			debits += line.amount
			if isCashBankAccount(line.accountType, line.subtype, line.systemKey) {
				cashNet += line.amount
			}
		} else if line.side == accountingDirectionCredit {
			if line.amount > math.MaxInt64-credits {
				rows.Close()
				return errInvalidJournalReversal
			}
			credits += line.amount
			if isCashBankAccount(line.accountType, line.subtype, line.systemKey) {
				cashNet -= line.amount
			}
		} else {
			rows.Close()
			return errInvalidJournalReversal
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(lines) == 0 || debits == 0 || debits != credits {
		return errInvalidJournalReversal
	}
	transactionID := newID()
	reference, description := "REV-"+entry.ReferenceNo, "Reversal: "+entry.Description
	legacyDirection, accountingDirection := "cash_in", accountingDirectionDebit
	cashAmount := cashNet
	if cashNet < 0 {
		legacyDirection, accountingDirection = "cash_out", accountingDirectionCredit
		cashAmount = -cashNet
	}
	if cashNet != 0 {
		reference, err = s.nextManualCashReference(tx, reversalDate)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manual_cash_transactions(id,transaction_date,direction,source,coa_code,accounting_direction,category_id,description,amount,reference_no,note,recorded_by) VALUES($1,$2,$3,'bank',NULL,$4,NULL,$5,$6,$7,$8,$9)`, transactionID, reversalDate, legacyDirection, accountingDirection, description, cashAmount, reference, reason, actorID); err != nil {
			return err
		}
	}
	newJournalID := newID()
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type,reversal_of,correction_reason) VALUES($1,$2,$3,'manual',$4,'bank',$5,'posted','',$6,$7,$8,$9,$10,$11)`, newJournalID, reference, transactionID, reversalDate, debits, description, actorID, entry.Category, entry.LoanType, journalID, reason); err != nil {
		return err
	}
	for _, line := range lines {
		side := accountingDirectionCredit
		if line.side == accountingDirectionCredit {
			side = accountingDirectionDebit
		}
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines(id,journal_id,side,coa_code,amount,component,mapping_override) VALUES($1,$2,$3,$4,$5,$6,TRUE)`, newID(), newJournalID, side, line.code, line.amount, line.component); err != nil {
			return err
		}
	}
	if err := recordFinancialTransactionAuditTx(tx, journalID, entry.TransactionType, actorID, "reversed_by", "", newJournalID, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func isCashBankAccount(accountType, subtype, systemKey string) bool {
	return accountType == "asset" && (subtype == "cash" || subtype == "bank" || systemKey == "CASH" || systemKey == "BANK")
}

func (s *Server) coaCashBankPostingAccountsForAdmin() ([]COAAccount, error) {
	accounts, err := s.coaPostingAccountsForAdmin()
	if err != nil {
		return nil, err
	}
	result := make([]COAAccount, 0)
	for _, account := range accounts {
		if isCashBankAccount(account.AccountType, account.Subtype, account.SystemKey) {
			result = append(result, account)
		}
	}
	return result, nil
}
