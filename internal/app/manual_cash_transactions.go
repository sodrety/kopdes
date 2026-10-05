package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type CashTransactionCategory struct {
	ID            string `json:"id"`
	CategoryKey   string `json:"category_key"`
	AccountCode   string `json:"account_code,omitempty"`
	ParentID      string `json:"parent_id,omitempty"`
	ParentKey     string `json:"parent_key,omitempty"`
	ParentName    string `json:"parent_name,omitempty"`
	Direction     string `json:"direction"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name,omitempty"`
	NormalBalance string `json:"normal_balance,omitempty"`
	IsGroup       bool   `json:"is_group"`
	HasChildren   bool   `json:"has_children"`
	Depth         int    `json:"depth"`
	Active        bool   `json:"active"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type manualCashTransactionRequest struct {
	Direction   string                         `json:"direction" form:"direction"` // Ignored; derived from cash/bank COA lines.
	Source      string                         `json:"source" form:"source"`       // Legacy input; ignored.
	COACode     string                         `json:"coa_code" form:"coa_code"`   // Legacy input; ignored.
	CategoryID  string                         `json:"category_id" form:"category_id"`
	Description string                         `json:"description" form:"description"`
	Amount      int64                          `json:"amount" form:"amount"` // Derived from net cash/bank movement.
	RecordDate  string                         `json:"transaction_date" form:"transaction_date"`
	ReferenceNo string                         `json:"reference_no" form:"reference_no"`
	Note        string                         `json:"note" form:"note"`
	Lines       []manualCashJournalLineRequest `json:"lines" form:"-"`
}

type manualCashJournalLineRequest struct {
	COACode string `json:"coa_code"`
	Debit   int64  `json:"debit"`
	Credit  int64  `json:"credit"`
}

type ManualCashTransactionDraftView struct {
	ID              string
	EntryType       string
	CategoryID      string
	TransactionDate string
	ReferenceNo     string
	Description     string
	Note            string
	RecordedBy      string
	Lines           []manualCashJournalLineRequest
}

type cashTransactionCategoryRequest struct {
	Direction     string `json:"direction" form:"direction"`
	Name          string `json:"name" form:"name"`
	AccountCode   string `json:"account_code" form:"account_code"`
	ParentID      string `json:"parent_id" form:"parent_id"`
	NormalBalance string `json:"normal_balance" form:"normal_balance"`
}

type cashTransactionCategoryUpdateRequest struct {
	Name          string `json:"name" form:"name"`
	Active        *bool  `json:"active" form:"active"`
	AccountCode   string `json:"account_code" form:"account_code"`
	ParentID      string `json:"parent_id" form:"parent_id"`
	NormalBalance string `json:"normal_balance" form:"normal_balance"`
}

var (
	errInvalidManualCashTransaction           = errors.New("invalid manual cash transaction")
	errZeroManualCashMovement                 = errors.New("manual journal must have a non-zero cash/bank net movement")
	errFutureManualCashTransaction            = errors.New("manual cash transaction date is in the future")
	errCashTransactionCategoryNotFound        = errors.New("cash transaction category not found")
	errCashTransactionCategoryInactive        = errors.New("cash transaction category is inactive")
	errCashTransactionCategoryDirection       = errors.New("cash transaction category direction mismatch")
	errCashTransactionCategoryGroup           = errors.New("cash transaction category group cannot be used directly")
	errCashTransactionCategoryNameLocked      = errors.New("used cash transaction category names are immutable")
	errCashTransactionCategoryDirectionLocked = errors.New("used cash transaction category directions are immutable")
	errInvalidCashTransactionCategory         = errors.New("invalid cash transaction category")
	errCashTransactionCategoryParentNotFound  = errors.New("cash transaction category parent not found")
	errCashTransactionCategoryParentDirection = errors.New("cash transaction category parent direction mismatch")
	errCashTransactionCategoryCycle           = errors.New("cash transaction category parent cycle")
	errManualCashTransactionDraftNotFound     = errors.New("manual cash transaction draft not found")
	errManualCashTransactionDraftNotPending   = errors.New("manual cash transaction draft is no longer pending")
)

func normalizeCashTransactionCategoryName(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeCashTransactionCategoryKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeCashTransactionCategoryAccountCode(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validCashTransactionCategoryKey(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			if index == 0 && (character == '_' || character == '-') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func validCashTransactionNormalBalance(value string) bool {
	return value == "" || value == "D" || value == "C"
}

func validateManualCashTransactionRequest(req manualCashTransactionRequest) error {
	if normalizeCashTransactionCategoryName(req.Description) == "" || strings.TrimSpace(req.RecordDate) == "" || len(req.Lines) < 2 {
		return errInvalidManualCashTransaction
	}
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.RecordDate), jakartaLocation)
	if err != nil || date.Format("2006-01-02") != strings.TrimSpace(req.RecordDate) {
		return errInvalidManualCashTransaction
	}
	today := time.Now().In(jakartaLocation)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, jakartaLocation)
	if date.After(today) {
		return errFutureManualCashTransaction
	}
	return nil
}

func (s *Server) recordManualCashTransaction(c *gin.Context) {
	req, parseErr := manualCashTransactionRequestFromContext(c)
	if parseErr != nil {
		if errors.Is(parseErr, errInvalidRupiahAmountInput) {
			invalidRupiahAmountResponse(c)
		} else {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_manual_cash_transaction"))
		}
		return
	}
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	entryType := manualCashEntryTypeFromPath(c.Request.URL.Path)
	record, err := s.saveManualCashTransactionDraft(req, entryType, user.ID, "")
	respondManualCashTransactionResult(c, entryType, record, err)
}

var errInvalidRupiahAmountInput = errors.New("invalid rupiah amount input")

func manualCashTransactionRequestFromContext(c *gin.Context) (manualCashTransactionRequest, error) {
	var req manualCashTransactionRequest
	if isBrowserFormRequest(c) {
		if err := c.Request.ParseForm(); err == nil {
			req.CategoryID = c.PostForm("category_id")
			req.Description = c.PostForm("description")
			req.RecordDate = c.PostForm("transaction_date")
			req.ReferenceNo = c.PostForm("reference_no")
			req.Note = c.PostForm("note")
			codes := c.Request.PostForm["line_coa_code"]
			debits := c.Request.PostForm["debit_amount"]
			credits := c.Request.PostForm["credit_amount"]
			for index, code := range codes {
				debitRaw, creditRaw := "", ""
				if index < len(debits) {
					debitRaw = strings.TrimSpace(debits[index])
				}
				if index < len(credits) {
					creditRaw = strings.TrimSpace(credits[index])
				}
				if strings.TrimSpace(code) == "" && debitRaw == "" && creditRaw == "" {
					continue
				}
				line := manualCashJournalLineRequest{COACode: strings.TrimSpace(code)}
				if debitRaw != "" {
					amount, err := parseRupiahAmount(debitRaw)
					if err != nil {
						return req, errInvalidRupiahAmountInput
					}
					line.Debit = amount
				}
				if creditRaw != "" {
					amount, err := parseRupiahAmount(creditRaw)
					if err != nil {
						return req, errInvalidRupiahAmountInput
					}
					line.Credit = amount
				}
				req.Lines = append(req.Lines, line)
			}
		} else {
			return req, errInvalidManualCashTransaction
		}
	} else if err := c.ShouldBind(&req); err != nil {
		return req, errInvalidManualCashTransaction
	}
	return req, nil
}

func manualCashEntryTypeFromPath(path string) string {
	if strings.Contains(path, "/journals") {
		return "journal"
	}
	return "cash"
}

func (s *Server) saveManualCashTransactionDraft(req manualCashTransactionRequest, entryType, recordedBy, draftID string) (gin.H, error) {
	if entryType != "cash" && entryType != "journal" {
		return nil, errInvalidManualCashTransaction
	}
	req.CategoryID = strings.TrimSpace(req.CategoryID)
	req.Description = normalizeCashTransactionCategoryName(req.Description)
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	if err := validateManualCashTransactionRequest(req); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if draftID == "" {
		draftID = newID()
		if _, err := s.db.Exec(`INSERT INTO manual_cash_transaction_drafts (id,entry_type,payload,status,recorded_by) VALUES ($1,$2,$3,'pending',$4)`, draftID, entryType, string(payload), recordedBy); err != nil {
			return nil, err
		}
		return gin.H{"id": draftID, "entry_type": entryType, "status": "pending"}, nil
	}
	result, err := s.db.Exec(`UPDATE manual_cash_transaction_drafts SET payload=$1,status='pending',approved_by=NULL,transaction_id=NULL,approved_at=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND entry_type=$3 AND status='pending'`, string(payload), draftID, entryType)
	if err != nil {
		return nil, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		var status string
		err := s.db.QueryRow(`SELECT status FROM manual_cash_transaction_drafts WHERE id=$1 AND entry_type=$2`, draftID, entryType).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errManualCashTransactionDraftNotFound
		}
		if err != nil {
			return nil, err
		}
		return nil, errManualCashTransactionDraftNotPending
	}
	return gin.H{"id": draftID, "entry_type": entryType, "status": "pending"}, nil
}

func (s *Server) manualCashTransactionDraftsForAdmin(entryType string) ([]ManualCashTransactionDraftView, error) {
	rows, err := s.db.Query(`SELECT d.id,d.payload,COALESCE(NULLIF(u.full_name,''),u.email,'') FROM manual_cash_transaction_drafts d LEFT JOIN users u ON u.id=d.recorded_by WHERE d.entry_type=$1 AND d.status='pending' ORDER BY d.created_at,d.id`, entryType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drafts := make([]ManualCashTransactionDraftView, 0)
	for rows.Next() {
		var draft ManualCashTransactionDraftView
		var payload string
		if err := rows.Scan(&draft.ID, &payload, &draft.RecordedBy); err != nil {
			return nil, err
		}
		var req manualCashTransactionRequest
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return nil, err
		}
		draft.EntryType = entryType
		draft.CategoryID = req.CategoryID
		draft.TransactionDate = req.RecordDate
		draft.ReferenceNo = req.ReferenceNo
		draft.Description = req.Description
		draft.Note = req.Note
		draft.Lines = req.Lines
		drafts = append(drafts, draft)
	}
	return drafts, rows.Err()
}

func (s *Server) manualCashTransactionDraftForAdmin(draftID, entryType string) (ManualCashTransactionDraftView, error) {
	var draft ManualCashTransactionDraftView
	var payload string
	err := s.db.QueryRow(`SELECT d.id,d.payload,COALESCE(NULLIF(u.full_name,''),u.email,'') FROM manual_cash_transaction_drafts d LEFT JOIN users u ON u.id=d.recorded_by WHERE d.id=$1 AND d.entry_type=$2 AND d.status='pending'`, draftID, entryType).Scan(&draft.ID, &payload, &draft.RecordedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return ManualCashTransactionDraftView{}, errManualCashTransactionDraftNotFound
	}
	if err != nil {
		return ManualCashTransactionDraftView{}, err
	}
	var req manualCashTransactionRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return ManualCashTransactionDraftView{}, err
	}
	draft.EntryType = entryType
	draft.CategoryID = req.CategoryID
	draft.TransactionDate = req.RecordDate
	draft.ReferenceNo = req.ReferenceNo
	draft.Description = req.Description
	draft.Note = req.Note
	draft.Lines = req.Lines
	return draft, nil
}

func (s *Server) updateManualCashTransactionDraft(c *gin.Context) {
	entryType := manualCashEntryTypeFromPath(c.Request.URL.Path)
	draftID := strings.TrimSpace(c.Param("id"))
	req, parseErr := manualCashTransactionRequestFromContext(c)
	if parseErr != nil {
		if errors.Is(parseErr, errInvalidRupiahAmountInput) {
			invalidRupiahAmountResponse(c)
		} else {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_manual_cash_transaction"))
		}
		return
	}
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	_, err := s.saveManualCashTransactionDraft(req, entryType, user.ID, draftID)
	respondManualCashTransactionResult(c, entryType, gin.H{"id": draftID, "status": "pending"}, err)
}

func (s *Server) approveManualCashTransactionDraft(c *gin.Context) {
	entryType := manualCashEntryTypeFromPath(c.Request.URL.Path)
	draftID := strings.TrimSpace(c.Param("id"))
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer func() { _ = tx.Rollback() }()
	var payload, recordedBy, status string
	query := `SELECT payload,recorded_by,status FROM manual_cash_transaction_drafts WHERE id=$1 AND entry_type=$2` + rowLockClause(s.db)
	err = tx.QueryRow(query, draftID, entryType).Scan(&payload, &recordedBy, &status)
	if errors.Is(err, sql.ErrNoRows) {
		respondManualCashTransactionResult(c, entryType, nil, errManualCashTransactionDraftNotFound)
		return
	}
	if err != nil {
		respondManualCashTransactionResult(c, entryType, nil, err)
		return
	}
	if status != "pending" {
		respondManualCashTransactionResult(c, entryType, nil, errManualCashTransactionDraftNotPending)
		return
	}
	var req manualCashTransactionRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		respondManualCashTransactionResult(c, entryType, nil, err)
		return
	}
	posted, err := s.insertManualCashTransactionTx(tx, req, recordedBy)
	if err != nil {
		respondManualCashTransactionResult(c, entryType, nil, err)
		return
	}
	if _, err := tx.Exec(`UPDATE manual_cash_transaction_drafts SET status='approved',approved_by=$1,transaction_id=$2,approved_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending'`, user.ID, posted["id"], draftID); err != nil {
		respondManualCashTransactionResult(c, entryType, nil, err)
		return
	}
	if err := tx.Commit(); err != nil {
		respondManualCashTransactionResult(c, entryType, nil, err)
		return
	}
	respondManualCashTransactionResult(c, entryType, posted, nil)
}

func respondManualCashTransactionResult(c *gin.Context, entryType string, record gin.H, err error) {
	switch {
	case errors.Is(err, errInvalidManualCashTransaction):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_manual_cash_transaction"))
	case errors.Is(err, errFutureManualCashTransaction):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_manual_cash_transaction_future_date"))
	case errors.Is(err, errCashTransactionCategoryNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_cash_transaction_category_not_found"))
	case errors.Is(err, errCashTransactionCategoryInactive):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_inactive"))
	case errors.Is(err, errCashTransactionCategoryGroup):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_group"))
	case errors.Is(err, errCashTransactionCategoryDirection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_direction"))
	case errors.Is(err, errUnbalancedFinancialJournal):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_unbalanced_journal"))
	case errors.Is(err, errZeroManualCashMovement):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_journal_cash_movement_zero"))
	case errors.Is(err, errCOAAccountNotFound), errors.Is(err, errCOAAccountInactive), errors.Is(err, errCOAAccountGroup), errors.Is(err, errJournalAccountConflict):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_invalid_transaction_coa"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_reference_exists"))
	case errors.Is(err, errManualCashTransactionDraftNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_manual_cash_draft_not_found"))
	case errors.Is(err, errManualCashTransactionDraftNotPending):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_manual_cash_draft_not_pending"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		redirect := "/admin/transactions"
		if entryType == "journal" {
			redirect = "/admin/journals?view=journal"
		}
		respondCreatedOrHXRedirect(c, redirect, record)
	}
}

func (s *Server) insertManualCashTransaction(req manualCashTransactionRequest, recordedBy string) (gin.H, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := s.insertManualCashTransactionTx(tx, req, recordedBy)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Server) insertManualCashTransactionTx(tx *sql.Tx, req manualCashTransactionRequest, recordedBy string) (gin.H, error) {
	var err error
	req.CategoryID = strings.TrimSpace(req.CategoryID)
	req.Description = normalizeCashTransactionCategoryName(req.Description)
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	if req.CategoryID != "" && len(req.Lines) < 2 {
		var categoryActive, categoryIsGroup bool
		if err := s.db.QueryRow(`SELECT active,is_group FROM cash_transaction_categories WHERE id=$1`, req.CategoryID).Scan(&categoryActive, &categoryIsGroup); err == nil && categoryActive && categoryIsGroup {
			return nil, errCashTransactionCategoryGroup
		}
	}
	if err := validateManualCashTransactionRequest(req); err != nil {
		return nil, err
	}

	var totalDebits, totalCredits, cashBankDebits, cashBankCredits int64
	journalLines := make([]accountingJournalLineInput, 0, len(req.Lines))
	for _, line := range req.Lines {
		line.COACode = strings.TrimSpace(line.COACode)
		if line.COACode == "" || line.Debit < 0 || line.Credit < 0 || (line.Debit == 0) == (line.Credit == 0) {
			return nil, errInvalidManualCashTransaction
		}
		account, err := s.coaAccountForPostingTx(tx, line.COACode)
		if err != nil {
			return nil, err
		}
		isCashBank := account.AccountType == "asset" && (account.Subtype == "cash" || account.Subtype == "bank" || account.SystemKey == "CASH" || account.SystemKey == "BANK")
		if line.Debit > 0 {
			if line.Debit > math.MaxInt64-totalDebits || (isCashBank && line.Debit > math.MaxInt64-cashBankDebits) {
				return nil, errInvalidManualCashTransaction
			}
			totalDebits += line.Debit
			if isCashBank {
				cashBankDebits += line.Debit
			}
			journalLines = append(journalLines, accountingJournalLineInput{Component: "manual", Side: accountingDirectionDebit, COACode: line.COACode, Amount: line.Debit})
		} else {
			if line.Credit > math.MaxInt64-totalCredits || (isCashBank && line.Credit > math.MaxInt64-cashBankCredits) {
				return nil, errInvalidManualCashTransaction
			}
			totalCredits += line.Credit
			if isCashBank {
				cashBankCredits += line.Credit
			}
			journalLines = append(journalLines, accountingJournalLineInput{Component: "manual", Side: accountingDirectionCredit, COACode: line.COACode, Amount: line.Credit})
		}
	}
	if totalDebits <= 0 || totalDebits != totalCredits {
		return nil, errUnbalancedFinancialJournal
	}
	cashNet := cashBankDebits - cashBankCredits
	if cashNet == 0 {
		return nil, errZeroManualCashMovement
	}
	legacyDirection := "cash_in"
	accountingDirection := accountingDirectionDebit
	if cashNet < 0 {
		legacyDirection, accountingDirection = "cash_out", accountingDirectionCredit
	}
	amount := cashNet
	if amount < 0 {
		amount = -amount
	}
	source := transactionSourceBank // Legacy column only; account-level activity is held in journal lines.
	var categoryDirection, categoryName string
	var categoryActive, categoryIsGroup bool
	if req.CategoryID != "" {
		err = tx.QueryRow(`SELECT direction,name,active,is_group FROM cash_transaction_categories WHERE id=$1`, req.CategoryID).Scan(&categoryDirection, &categoryName, &categoryActive, &categoryIsGroup)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errCashTransactionCategoryNotFound
		}
		if err != nil {
			return nil, err
		}
		if !categoryActive {
			return nil, errCashTransactionCategoryInactive
		}
		if categoryIsGroup {
			return nil, errCashTransactionCategoryGroup
		}
		if categoryDirection != legacyDirection {
			return nil, errCashTransactionCategoryDirection
		}
	} else {
		categoryName = "Jurnal Umum"
	}
	if req.ReferenceNo == "" {
		req.ReferenceNo, err = s.nextManualCashReference(tx, req.RecordDate)
		if err != nil {
			return nil, err
		}
	}

	id := newID()
	if _, err := tx.Exec(`INSERT INTO manual_cash_transactions (id,transaction_date,direction,source,coa_code,accounting_direction,category_id,description,amount,reference_no,note,recorded_by) VALUES ($1,$2,$3,$4,NULL,$5,$6,$7,$8,$9,$10,$11)`, id, req.RecordDate, legacyDirection, source, accountingDirection, nullIfEmpty(req.CategoryID), req.Description, amount, req.ReferenceNo, req.Note, recordedBy); err != nil {
		return nil, err
	}
	if err := s.createFinancialJournalTx(tx, accountingJournalInput{
		ReferenceNo: req.ReferenceNo, TransactionID: id, TransactionType: "manual", TransactionDate: req.RecordDate,
		Source: source, Amount: totalDebits, Lines: journalLines,
		Description: req.Description, RecordedBy: recordedBy,
	}); err != nil {
		return nil, err
	}
	return gin.H{"id": id, "transaction_date": req.RecordDate, "direction": accountingDirection, "category_id": req.CategoryID, "category": categoryName, "description": req.Description, "amount": amount, "reference_no": req.ReferenceNo, "note": req.Note, "recorded_by": recordedBy}, nil
}

func (s *Server) nextManualCashReference(tx *sql.Tx, transactionDate string) (string, error) {
	prefix, err := manualCashReferencePrefix(transactionDate)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO manual_cash_transaction_sequences (transaction_date,next_sequence) VALUES ($1,1) ON CONFLICT(transaction_date) DO NOTHING`, transactionDate); err != nil {
		return "", err
	}
	query := `SELECT next_sequence FROM manual_cash_transaction_sequences WHERE transaction_date=$1` + rowLockClause(s.db)
	for {
		var sequence int
		if err := tx.QueryRow(query, transactionDate).Scan(&sequence); err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s%04d", prefix, sequence)
		if _, err := tx.Exec(`UPDATE manual_cash_transaction_sequences SET next_sequence=$1 WHERE transaction_date=$2`, sequence+1, transactionDate); err != nil {
			return "", err
		}
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE reference_no=$1`, candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}

func (s *Server) nextManualCashReferencePreview(transactionDate string) (string, error) {
	prefix, err := manualCashReferencePrefix(transactionDate)
	if err != nil {
		return "", err
	}
	rows, err := s.db.Query(`SELECT reference_no FROM manual_cash_transactions WHERE reference_no LIKE $1`, prefix+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	maximum := 0
	for rows.Next() {
		var reference string
		if err := rows.Scan(&reference); err != nil {
			return "", err
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(reference, prefix))
		if err == nil && sequence > maximum {
			maximum = sequence
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, maximum+1), nil
}

func manualCashReferencePrefix(transactionDate string) (string, error) {
	date, err := time.Parse("2006-01-02", transactionDate)
	if err != nil {
		return "", err
	}
	return "KAS-" + date.Format("20060102") + "-", nil
}

func (s *Server) manualCashNet() (int64, error) {
	var net int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN direction='cash_in' THEN amount ELSE -amount END),0) FROM manual_cash_transactions`).Scan(&net)
	return net, err
}

func (s *Server) manualCashTotals(dateFrom, dateTo string) (income, expense, incomeCount, expenseCount int64, err error) {
	err = s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN direction='cash_in' THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN direction='cash_out' THEN amount ELSE 0 END),0),
		COUNT(CASE WHEN direction='cash_in' THEN 1 END),
		COUNT(CASE WHEN direction='cash_out' THEN 1 END)
		FROM manual_cash_transactions
		WHERE transaction_date >= $1 AND transaction_date <= $2`, dateFrom, dateTo).Scan(&income, &expense, &incomeCount, &expenseCount)
	return
}

func (s *Server) cashTransactionCategoriesForAdmin(includeInactive bool) ([]CashTransactionCategory, error) {
	query := `SELECT c.id,COALESCE(NULLIF(c.category_key,''),c.id),COALESCE(c.account_code,''),COALESCE(c.parent_id,''),COALESCE(p.category_key,''),c.direction,c.name,COALESCE(c.normal_balance,''),c.is_group,c.active,CAST(c.created_at AS TEXT),CAST(c.updated_at AS TEXT),EXISTS (SELECT 1 FROM cash_transaction_categories child WHERE child.parent_id=c.id) FROM cash_transaction_categories c LEFT JOIN cash_transaction_categories p ON p.id=c.parent_id`
	if !includeInactive {
		query += ` WHERE c.active=TRUE`
	}
	query += ` ORDER BY c.direction,COALESCE(NULLIF(c.account_code,''),'999999999'),c.name,c.id`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var categories []CashTransactionCategory
	for rows.Next() {
		var category CashTransactionCategory
		if err := rows.Scan(&category.ID, &category.CategoryKey, &category.AccountCode, &category.ParentID, &category.ParentKey, &category.Direction, &category.Name, &category.NormalBalance, &category.IsGroup, &category.Active, &category.CreatedAt, &category.UpdatedAt, &category.HasChildren); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return flattenCashTransactionCategories(categories), nil
}

func (s *Server) createCashTransactionCategory(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req cashTransactionCategoryRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	category, err := s.insertCashTransactionCategory(actor.ID, req)
	switch {
	case errors.Is(err, errInvalidCashTransactionCategory):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
	case errors.Is(err, errCashTransactionCategoryParentNotFound):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_parent_not_found"))
	case errors.Is(err, errCashTransactionCategoryParentDirection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_parent_direction"))
	case errors.Is(err, errCashTransactionCategoryCycle):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_cycle"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_category_exists"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondCreatedOrHXRedirect(c, "/admin/transactions/categories", category)
	}
}

func (s *Server) insertCashTransactionCategory(actorID string, req cashTransactionCategoryRequest) (CashTransactionCategory, error) {
	req.Direction = strings.TrimSpace(req.Direction)
	req.Name = normalizeCashTransactionCategoryName(req.Name)
	req.AccountCode = normalizeCashTransactionCategoryAccountCode(req.AccountCode)
	req.ParentID = strings.TrimSpace(req.ParentID)
	req.NormalBalance = strings.ToUpper(strings.TrimSpace(req.NormalBalance))
	if !validCashTransactionDirection(req.Direction) || req.Name == "" || len(req.Name) > 100 || !validCashTransactionNormalBalance(req.NormalBalance) {
		return CashTransactionCategory{}, errInvalidCashTransactionCategory
	}
	category := CashTransactionCategory{ID: newID(), CategoryKey: cashTransactionCategoryKey(req.Direction, req.AccountCode, req.Name), AccountCode: req.AccountCode, ParentID: req.ParentID, Direction: req.Direction, Name: req.Name, NormalBalance: req.NormalBalance, Active: true}
	if !validCashTransactionCategoryKey(category.CategoryKey) {
		return CashTransactionCategory{}, errInvalidCashTransactionCategory
	}
	tx, err := s.db.Begin()
	if err != nil {
		return CashTransactionCategory{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateCashTransactionCategoryParentTx(tx, category.ID, category.ParentID, category.Direction); err != nil {
		return CashTransactionCategory{}, err
	}
	if _, err := tx.Exec(`INSERT INTO cash_transaction_categories (id,category_key,account_code,parent_id,direction,name,normal_balance,is_group,active,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,FALSE,TRUE,$8)`, category.ID, category.CategoryKey, category.AccountCode, nullIfEmpty(category.ParentID), category.Direction, category.Name, nullIfEmpty(category.NormalBalance), actorID); err != nil {
		return CashTransactionCategory{}, err
	}
	if category.ParentID != "" {
		if _, err := tx.Exec(`UPDATE cash_transaction_categories SET is_group=TRUE WHERE id=$1`, category.ParentID); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,new_name) VALUES ($1,$2,$3,'created',$4)`, newID(), category.ID, actorID, category.Name); err != nil {
		return CashTransactionCategory{}, err
	}
	if err := tx.Commit(); err != nil {
		return CashTransactionCategory{}, err
	}
	return category, nil
}

func (s *Server) updateCashTransactionCategory(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req cashTransactionCategoryUpdateRequest
	if err := c.ShouldBind(&req); err != nil || req.Active == nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	req.Name = normalizeCashTransactionCategoryName(req.Name)
	if req.Name == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	req.AccountCode = normalizeCashTransactionCategoryAccountCode(req.AccountCode)
	req.ParentID = strings.TrimSpace(req.ParentID)
	req.NormalBalance = strings.ToUpper(strings.TrimSpace(req.NormalBalance))
	if !validCashTransactionNormalBalance(req.NormalBalance) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	category, err := s.updateCashTransactionCategoryByID(actor.ID, c.Param("id"), req)
	switch {
	case errors.Is(err, errCashTransactionCategoryNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_cash_transaction_category_not_found"))
	case errors.Is(err, errCashTransactionCategoryNameLocked):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_name_locked"))
	case errors.Is(err, errCashTransactionCategoryDirectionLocked):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_direction_locked"))
	case errors.Is(err, errCashTransactionCategoryParentNotFound):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_parent_not_found"))
	case errors.Is(err, errCashTransactionCategoryParentDirection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_parent_direction"))
	case errors.Is(err, errCashTransactionCategoryCycle):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_cycle"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_category_exists"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondOKOrHXRedirect(c, "/admin/transactions/categories", category)
	}
}

func (s *Server) updateCashTransactionCategoryByID(actorID, id string, req cashTransactionCategoryUpdateRequest) (CashTransactionCategory, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return CashTransactionCategory{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current CashTransactionCategory
	err = tx.QueryRow(`SELECT id,COALESCE(NULLIF(category_key,''),id),COALESCE(account_code,''),COALESCE(parent_id,''),direction,name,COALESCE(normal_balance,''),is_group,active,CAST(created_at AS TEXT),CAST(updated_at AS TEXT) FROM cash_transaction_categories WHERE id=$1`, id).Scan(&current.ID, &current.CategoryKey, &current.AccountCode, &current.ParentID, &current.Direction, &current.Name, &current.NormalBalance, &current.IsGroup, &current.Active, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CashTransactionCategory{}, errCashTransactionCategoryNotFound
	}
	if err != nil {
		return CashTransactionCategory{}, err
	}
	if current.Name != req.Name {
		var used int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE category_id=$1`, id).Scan(&used); err != nil {
			return CashTransactionCategory{}, err
		}
		if used > 0 {
			return CashTransactionCategory{}, errCashTransactionCategoryNameLocked
		}
	}
	if err := validateCashTransactionCategoryParentTx(tx, id, req.ParentID, current.Direction); err != nil {
		return CashTransactionCategory{}, err
	}
	if _, err := tx.Exec(`UPDATE cash_transaction_categories SET name=$1,account_code=$2,parent_id=$3,normal_balance=$4,active=$5,updated_at=CURRENT_TIMESTAMP WHERE id=$6`, req.Name, nullIfEmpty(req.AccountCode), nullIfEmpty(req.ParentID), nullIfEmpty(req.NormalBalance), *req.Active, id); err != nil {
		return CashTransactionCategory{}, err
	}
	if req.ParentID != "" {
		if _, err := tx.Exec(`UPDATE cash_transaction_categories SET is_group=TRUE WHERE id=$1`, req.ParentID); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if current.Name != req.Name {
		if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,'renamed',$4,$5)`, newID(), id, actorID, current.Name, req.Name); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if current.Active != *req.Active {
		action := "deactivated"
		if *req.Active {
			action = "reactivated"
		}
		if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), id, actorID, action, current.Name, current.Name); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CashTransactionCategory{}, err
	}
	current.Name, current.AccountCode, current.ParentID, current.NormalBalance, current.Active = req.Name, req.AccountCode, req.ParentID, req.NormalBalance, *req.Active
	return current, nil
}
