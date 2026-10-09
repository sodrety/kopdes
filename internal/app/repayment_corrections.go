package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type repaymentCorrectionInput struct {
	Amount      int64  `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
	Reason      string `json:"reason" form:"reason"`
}

type repaymentRemovalInput struct {
	Reason string `json:"reason" form:"reason"`
}

type manualRepaymentInput struct {
	LoanID      string `json:"loan_id" form:"loan_id"`
	Amount      int64  `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
	Reason      string `json:"reason" form:"reason"`
}

type repaymentAuditSnapshot struct {
	Amount      int64  `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
	Source      string `json:"source"`
	COACode     string `json:"coa_code"`
}

type AdminRepaymentAudit struct {
	ID          string
	RepaymentID string
	LoanID      string
	MemberNo    string
	FullName    string
	Action      string
	Reason      string
	ActorName   string
	CreatedAt   string
	Before      repaymentAuditSnapshot
	After       repaymentAuditSnapshot
	HasAfter    bool
}

type repaymentLoanState struct {
	ID              string
	MemberID        string
	TotalObligation int64
	Status          string
	LoanType        string
}

type repaymentScheduleInstallment struct {
	ID              string
	ScheduledAmount int64
	PaidAmount      int64
	DueDate         string
}

type repaymentJournalLine struct {
	Side            string
	COACode         string
	Amount          int64
	Component       string
	MappingOverride bool
}

type repaymentJournalEntry struct {
	TransactionID   string
	ID              string
	ReferenceNo     string
	TransactionDate string
	Source          string
	Amount          int64
	Status          string
	BatchID         string
	Description     string
	Category        string
	LoanType        string
}

var (
	errInvalidRepaymentCorrection      = errors.New("invalid repayment correction")
	errRepaymentNotFound               = errors.New("repayment not found")
	errRepaymentCorrectionOtherActive  = errors.New("another active loan prevents reopening this loan")
	errRepaymentJournalAlreadyReversed = errors.New("repayment journal was already reversed")
	errRepaymentScheduleMissing        = errors.New("loan repayment schedule is missing")
)

func (s *Server) updateRepaymentBySuperAdmin(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}
	if !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_repayment_only"))
		return
	}

	var req repaymentCorrectionInput
	if err := bindRequestWithRupiahAmount(c, &req, "amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_repayment_correction"))
		return
	}
	if err := s.correctRepaymentByID(user, c.Param("id"), req); err != nil {
		respondRepaymentCorrectionError(c, err)
		return
	}
	respondRepaymentCorrectionSuccess(c, translate(lang, "toast_repayment_updated"), gin.H{"status": "updated"})
}

func (s *Server) removeRepaymentBySuperAdmin(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}
	if !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_repayment_only"))
		return
	}

	var req repaymentRemovalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_repayment_correction"))
		return
	}
	if !validRepaymentCorrectionReason(req.Reason) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_repayment_correction"))
		return
	}
	if err := s.removeRepaymentByID(user, c.Param("id"), strings.TrimSpace(req.Reason)); err != nil {
		respondRepaymentCorrectionError(c, err)
		return
	}
	respondRepaymentCorrectionSuccess(c, translate(lang, "toast_repayment_removed"), gin.H{"status": "removed"})
}

func (s *Server) addRepaymentBySuperAdmin(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}
	if !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_repayment_only"))
		return
	}

	var req manualRepaymentInput
	if err := bindRequestWithRupiahAmount(c, &req, "amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_manual_repayment"))
		return
	}
	req.LoanID = strings.TrimSpace(req.LoanID)
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.LoanID == "" || req.Amount <= 0 || !validRepaymentCorrectionDate(req.RecordDate) || !validRepaymentCorrectionReason(req.Reason) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_manual_repayment"))
		return
	}

	repayment, err := s.recordRepaymentWithAudit(req.LoanID, user.ID, repaymentInput{
		Amount: req.Amount, RecordDate: req.RecordDate, ReferenceNo: req.ReferenceNo, Note: req.Note, Role: user.Role,
	}, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, errRepaymentOverBalance):
			respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_repayment_over_balance"))
		case errors.Is(err, errLoanNotActive):
			respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_repayment_status"))
		case errors.Is(err, errLoanNotFound):
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_manual_repayment_loan_not_found"))
		default:
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		}
		return
	}
	respondRepaymentCorrectionSuccess(c, translate(lang, "toast_repayment_added"), gin.H{"repayment": repayment})
}

func insertRepaymentAddedAudit(tx *sql.Tx, auditID string, repayment LoanRepayment, reason, actorID string) error {
	afterState, err := json.Marshal(repaymentSnapshot(repayment))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO loan_repayment_audits(id,repayment_id,loan_id,member_id,action,reason,before_state,after_state,actor_id)
		VALUES($1,$2,$3,$4,'added',$5,'{}',$6,$7)`, auditID, repayment.ID, repayment.LoanID, repayment.MemberID, reason, string(afterState), actorID)
	return err
}

func respondRepaymentCorrectionSuccess(c *gin.Context, message string, body any) {
	if isHTMXRequest(c) {
		if trigger, err := json.Marshal(gin.H{"kopdes:toast": gin.H{"type": "success", "message": message, "persist": true}}); err == nil {
			c.Header("HX-Trigger", string(trigger))
		}
	}
	redirect := "/admin/repayments"
	returnTo := c.PostForm("return_to")
	const loanDetailPrefix = "/admin/loans/"
	loanID := strings.TrimPrefix(returnTo, loanDetailPrefix)
	if strings.HasPrefix(returnTo, loanDetailPrefix) && loanID != "" && !strings.ContainsAny(loanID, "/\\?#%") && loanID != "." && loanID != ".." {
		redirect = returnTo
	}
	respondOKOrHXRedirect(c, redirect, body)
}

func respondRepaymentCorrectionError(c *gin.Context, err error) {
	lang := languageFromRequest(c)
	switch {
	case errors.Is(err, errInvalidRepaymentCorrection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_repayment_correction"))
	case errors.Is(err, errRepaymentOverBalance):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_repayment_correction_over_balance"))
	case errors.Is(err, errRepaymentNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_repayment_correction_not_found"))
	case errors.Is(err, errLoanNotActive):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_repayment_correction_status"))
	case errors.Is(err, errRepaymentScheduleMissing):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_repayment_schedule_unavailable"))
	case errors.Is(err, errRepaymentCorrectionOtherActive):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_repayment_correction_active_loan"))
	case errors.Is(err, errRepaymentJournalAlreadyReversed):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_repayment_journal_already_reversed"))
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
	}
}

func validRepaymentCorrectionReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return len(reason) > 0 && len(reason) <= 500
}

func validRepaymentCorrectionDate(value string) bool {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), jakartaLocation)
	return err == nil && parsed.Format("2006-01-02") == strings.TrimSpace(value)
}

func repaymentSnapshot(repayment LoanRepayment) repaymentAuditSnapshot {
	return repaymentAuditSnapshot{
		Amount: repayment.Amount, RecordDate: repayment.RecordDate,
		ReferenceNo: repayment.ReferenceNo, Note: repayment.Note,
		Source: repayment.Source, COACode: repayment.COACode,
	}
}

func (s *Server) correctRepaymentByID(actor User, repaymentID string, req repaymentCorrectionInput) error {
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	req.Reason = strings.TrimSpace(req.Reason)
	if !isSuperAdmin(actor) || req.Amount <= 0 || !validRepaymentCorrectionDate(req.RecordDate) || !validRepaymentCorrectionReason(req.Reason) {
		return errInvalidRepaymentCorrection
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	before, loan, err := repaymentAndLoanForUpdate(tx, s.db, repaymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return errRepaymentNotFound
	}
	if err != nil {
		return err
	}
	if loan.Status != "active" && loan.Status != "paid" && loan.Status != "adjustment_due" {
		return errLoanNotActive
	}
	after := before
	after.Amount = req.Amount
	after.RecordDate = req.RecordDate
	after.ReferenceNo = req.ReferenceNo
	after.Note = req.Note
	if repaymentSnapshot(before) == repaymentSnapshot(after) {
		return errInvalidRepaymentCorrection
	}

	if _, err := tx.Exec(`UPDATE loan_repayments SET amount=$1,record_date=$2,reference_no=$3,note=$4 WHERE id=$5`, after.Amount, after.RecordDate, after.ReferenceNo, after.Note, repaymentID); err != nil {
		return err
	}
	if err := s.refreshLoanRepaymentScheduleTx(tx, loan); err != nil {
		return err
	}
	auditID := newID()
	if err := s.syncRepaymentJournalTx(tx, auditID, actor.ID, req.Reason, after, loan.LoanType, false); err != nil {
		return err
	}
	if err := insertRepaymentCorrectionAudit(tx, auditID, repaymentID, before, &after, "edited", req.Reason, actor.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) removeRepaymentByID(actor User, repaymentID, reason string) error {
	reason = strings.TrimSpace(reason)
	if !isSuperAdmin(actor) || !validRepaymentCorrectionReason(reason) {
		return errInvalidRepaymentCorrection
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	before, loan, err := repaymentAndLoanForUpdate(tx, s.db, repaymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return errRepaymentNotFound
	}
	if err != nil {
		return err
	}
	if loan.Status != "active" && loan.Status != "paid" && loan.Status != "adjustment_due" {
		return errLoanNotActive
	}

	auditID := newID()
	if err := s.syncRepaymentJournalTx(tx, auditID, actor.ID, reason, before, loan.LoanType, true); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM loan_repayments WHERE id=$1`, repaymentID); err != nil {
		return err
	}
	if err := s.refreshLoanRepaymentScheduleTx(tx, loan); err != nil {
		return err
	}
	if err := insertRepaymentCorrectionAudit(tx, auditID, repaymentID, before, nil, "removed", reason, actor.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func repaymentAndLoanForUpdate(tx *sql.Tx, db *sql.DB, repaymentID string) (LoanRepayment, repaymentLoanState, error) {
	var repayment LoanRepayment
	var loan repaymentLoanState
	err := tx.QueryRow(`SELECT lr.id,lr.loan_id,lr.member_id,lr.amount,lr.source,COALESCE(lr.coa_code,''),lr.record_date,lr.reference_no,lr.note,lr.recorded_by,l.total_obligation,l.status,l.loan_type
		FROM loan_repayments lr JOIN loans l ON l.id=lr.loan_id WHERE lr.id=$1`+rowLockClause(db), repaymentID).
		Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.Amount, &repayment.Source, &repayment.COACode, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.RecordedBy, &loan.TotalObligation, &loan.Status, &loan.LoanType)
	loan.ID = repayment.LoanID
	loan.MemberID = repayment.MemberID
	return repayment, loan, err
}

func (s *Server) refreshLoanRepaymentScheduleTx(tx *sql.Tx, loan repaymentLoanState) error {
	rows, err := tx.Query(`SELECT id,scheduled_amount,paid_amount,due_date FROM loan_installments WHERE loan_id=$1 ORDER BY installment_no`+rowLockClause(s.db), loan.ID)
	if err != nil {
		return err
	}
	installments := make([]repaymentScheduleInstallment, 0)
	for rows.Next() {
		var item repaymentScheduleInstallment
		if err := rows.Scan(&item.ID, &item.ScheduledAmount, &item.PaidAmount, &item.DueDate); err != nil {
			_ = rows.Close()
			return err
		}
		item.PaidAmount = 0
		installments = append(installments, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(installments) == 0 {
		return errRepaymentScheduleMissing
	}

	rows, err = tx.Query(`SELECT id,amount FROM loan_repayments WHERE loan_id=$1 ORDER BY record_date,created_at,id`+rowLockClause(s.db), loan.ID)
	if err != nil {
		return err
	}
	type loanRepaymentAmount struct {
		ID     string
		Amount int64
	}
	payments := make([]loanRepaymentAmount, 0)
	for rows.Next() {
		var payment loanRepaymentAmount
		if err := rows.Scan(&payment.ID, &payment.Amount); err != nil {
			_ = rows.Close()
			return err
		}
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	var totalPaid int64
	for _, payment := range payments {
		if payment.Amount <= 0 || payment.Amount > math.MaxInt64-totalPaid {
			return errRepaymentOverBalance
		}
		totalPaid += payment.Amount
		remaining := payment.Amount
		for index := range installments {
			unpaid := installments[index].ScheduledAmount - installments[index].PaidAmount
			if unpaid <= 0 {
				continue
			}
			applied := remaining
			if applied > unpaid {
				applied = unpaid
			}
			installments[index].PaidAmount += applied
			remaining -= applied
			if remaining == 0 {
				break
			}
		}
		if remaining > 0 {
			return errRepaymentOverBalance
		}
	}
	if totalPaid > loan.TotalObligation {
		return errRepaymentOverBalance
	}

	for _, installment := range installments {
		if _, err := tx.Exec(`UPDATE loan_installments SET paid_amount=$1 WHERE id=$2`, installment.PaidAmount, installment.ID); err != nil {
			return err
		}
	}
	remainingBalance := loan.TotalObligation - totalPaid
	status := "active"
	if remainingBalance == 0 {
		status = "paid"
	} else if loan.Status == "adjustment_due" {
		status = "adjustment_due"
	} else {
		var anotherActive int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM loans WHERE member_id=$1 AND id<>$2 AND status='active'`, loan.MemberID, loan.ID).Scan(&anotherActive); err != nil {
			return err
		}
		if anotherActive > 0 {
			return errRepaymentCorrectionOtherActive
		}
	}
	nextDueDate := ""
	if remainingBalance > 0 {
		for _, installment := range installments {
			if installment.PaidAmount < installment.ScheduledAmount {
				nextDueDate = installment.DueDate
				break
			}
		}
		if nextDueDate == "" {
			return errRepaymentOverBalance
		}
	}
	if _, err := tx.Exec(`UPDATE loans SET remaining_balance=$1,status=$2,next_due_date=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, remainingBalance, status, nullIfEmpty(nextDueDate), loan.ID); err != nil {
		if status == "active" && isUniqueViolation(err) {
			return errRepaymentCorrectionOtherActive
		}
		return err
	}
	return nil
}

func insertRepaymentCorrectionAudit(tx *sql.Tx, auditID, repaymentID string, before LoanRepayment, after *LoanRepayment, action, reason, actorID string) error {
	beforeState, err := json.Marshal(repaymentSnapshot(before))
	if err != nil {
		return err
	}
	afterState := ""
	if after != nil {
		encoded, err := json.Marshal(repaymentSnapshot(*after))
		if err != nil {
			return err
		}
		afterState = string(encoded)
	}
	_, err = tx.Exec(`INSERT INTO loan_repayment_audits(id,repayment_id,loan_id,member_id,action,reason,before_state,after_state,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, auditID, repaymentID, before.LoanID, before.MemberID, action, reason, string(beforeState), afterState, actorID)
	return err
}

func (s *Server) repaymentAuditsForAdmin(filters RepaymentFilters) ([]AdminRepaymentAudit, error) {
	query := `SELECT a.id,a.repayment_id,a.loan_id,m.member_no,m.full_name,a.action,a.reason,COALESCE(NULLIF(u.full_name,''),u.email,''),CAST(a.created_at AS TEXT),a.before_state,a.after_state
		FROM loan_repayment_audits a JOIN members m ON m.id=a.member_id JOIN users u ON u.id=a.actor_id`
	args := []any{}
	if search := strings.TrimSpace(filters.Search); search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		query += ` WHERE LOWER(m.full_name) LIKE $1 OR LOWER(m.member_no) LIKE $1`
	}
	query += ` ORDER BY a.created_at DESC,a.id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	audits := make([]AdminRepaymentAudit, 0)
	for rows.Next() {
		var audit AdminRepaymentAudit
		var beforeState, afterState string
		if err := rows.Scan(&audit.ID, &audit.RepaymentID, &audit.LoanID, &audit.MemberNo, &audit.FullName, &audit.Action, &audit.Reason, &audit.ActorName, &audit.CreatedAt, &beforeState, &afterState); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(beforeState), &audit.Before); err != nil {
			return nil, err
		}
		if afterState != "" {
			if err := json.Unmarshal([]byte(afterState), &audit.After); err != nil {
				return nil, err
			}
			audit.HasAfter = true
		}
		audits = append(audits, audit)
	}
	return audits, rows.Err()
}

func (s *Server) syncRepaymentJournalTx(tx *sql.Tx, auditID, actorID, reason string, repayment LoanRepayment, loanType string, removing bool) error {
	var journal repaymentJournalEntry
	err := tx.QueryRow(`SELECT e.transaction_id,e.id,e.reference_no,e.transaction_date,e.source,e.amount,e.status,e.batch_id,e.description,e.category,e.loan_type
		FROM financial_journal_entries e
		WHERE e.transaction_id=$1 AND e.transaction_type='repayment' AND e.reversal_of IS NULL
		AND NOT EXISTS(SELECT 1 FROM financial_journal_entries r WHERE r.reversal_of=e.id)
		ORDER BY e.created_at DESC,e.id DESC LIMIT 1`+rowLockClause(s.db), repayment.ID).
		Scan(&journal.TransactionID, &journal.ID, &journal.ReferenceNo, &journal.TransactionDate, &journal.Source, &journal.Amount, &journal.Status, &journal.BatchID, &journal.Description, &journal.Category, &journal.LoanType)
	if errors.Is(err, sql.ErrNoRows) {
		var anyJournal bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='repayment')`, repayment.ID).Scan(&anyJournal); err != nil {
			return err
		}
		if anyJournal {
			return errRepaymentJournalAlreadyReversed
		}
		if removing {
			return nil
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: repayment.ReferenceNo, TransactionID: repayment.ID, TransactionType: "repayment",
			TransactionDate: repayment.RecordDate, Source: repayment.Source, Amount: repayment.Amount, LoanType: loanType,
			Components: []accountingJournalComponent{
				{Component: "cash_bank", Side: accountingDirectionDebit, Amount: repayment.Amount},
				{Component: "loan_receivable", Side: accountingDirectionCredit, Amount: repayment.Amount},
			},
			COAOverrides: map[string]string{"loan_receivable": repayment.COACode},
			Description:  "Angsuran pinjaman", RecordedBy: repayment.RecordedBy,
		}); err != nil {
			return err
		}
		var journalID string
		if err := tx.QueryRow(`SELECT id FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='repayment' ORDER BY created_at DESC,id DESC LIMIT 1`, repayment.ID).Scan(&journalID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE financial_journal_entries SET correction_reason=$1 WHERE id=$2`, reason, journalID)
		return err
	}
	if err != nil {
		return err
	}

	if journal.Status == "pending_mapping" {
		if removing {
			if _, err := tx.Exec(`DELETE FROM financial_journal_lines WHERE journal_id=$1`, journal.ID); err != nil {
				return err
			}
			_, err := tx.Exec(`DELETE FROM financial_journal_entries WHERE id=$1 AND status='pending_mapping'`, journal.ID)
			return err
		}
		if _, err := tx.Exec(`UPDATE financial_journal_entries SET reference_no=$1,transaction_date=$2,source=$3,amount=$4,correction_reason=$5 WHERE id=$6 AND status='pending_mapping'`, repayment.ReferenceNo, repayment.RecordDate, repayment.Source, repayment.Amount, reason, journal.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE financial_journal_lines SET amount=$1 WHERE journal_id=$2`, repayment.Amount, journal.ID)
		return err
	}
	if journal.Status != "posted" {
		return errJournalNotPosted
	}

	lines, err := repaymentJournalLinesTx(tx, journal.ID)
	if err != nil {
		return err
	}
	reversalID, err := insertRepaymentJournalReversalTx(tx, journal, lines, actorID, reason)
	if err != nil {
		return err
	}
	if err := recordFinancialTransactionAuditTx(tx, journal.ID, "repayment", actorID, "reversed_by", "", reversalID, ""); err != nil {
		return err
	}
	if removing {
		return nil
	}
	if len(lines) == 0 {
		return errInvalidJournalReversal
	}
	var debitTotal, creditTotal int64
	for _, line := range lines {
		if line.Amount != journal.Amount {
			return errInvalidJournalReversal
		}
		if line.Side == accountingDirectionDebit {
			debitTotal += line.Amount
		} else if line.Side == accountingDirectionCredit {
			creditTotal += line.Amount
		} else {
			return errInvalidJournalReversal
		}
	}
	if debitTotal == 0 || debitTotal != creditTotal || debitTotal != journal.Amount {
		return errInvalidJournalReversal
	}

	newJournalID := newID()
	description := journal.Description
	if description == "" {
		description = "Angsuran pinjaman"
	}
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type,correction_reason)
		VALUES($1,$2,$3,'repayment',$4,$5,$6,'posted','',$7,$8,$9,$10,$11)`, newJournalID, repayment.ReferenceNo, repayment.ID, repayment.RecordDate, repayment.Source, repayment.Amount, description, actorID, journal.Category, journal.LoanType, reason); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines(id,journal_id,side,coa_code,amount,component,mapping_override) VALUES($1,$2,$3,$4,$5,$6,$7)`, newID(), newJournalID, line.Side, line.COACode, repayment.Amount, line.Component, line.MappingOverride); err != nil {
			return err
		}
	}
	return nil
}

func repaymentJournalLinesTx(tx *sql.Tx, journalID string) ([]repaymentJournalLine, error) {
	rows, err := tx.Query(`SELECT side,coa_code,amount,component,mapping_override FROM financial_journal_lines WHERE journal_id=$1 ORDER BY created_at,id`, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := make([]repaymentJournalLine, 0)
	for rows.Next() {
		var line repaymentJournalLine
		if err := rows.Scan(&line.Side, &line.COACode, &line.Amount, &line.Component, &line.MappingOverride); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func insertRepaymentJournalReversalTx(tx *sql.Tx, entry repaymentJournalEntry, lines []repaymentJournalLine, actorID, reason string) (string, error) {
	if len(lines) == 0 || entry.Amount <= 0 {
		return "", errInvalidJournalReversal
	}
	var debits, credits int64
	for _, line := range lines {
		if line.Amount <= 0 || line.COACode == "" {
			return "", errInvalidJournalReversal
		}
		switch line.Side {
		case accountingDirectionDebit:
			if line.Amount > math.MaxInt64-debits {
				return "", errInvalidJournalReversal
			}
			debits += line.Amount
		case accountingDirectionCredit:
			if line.Amount > math.MaxInt64-credits {
				return "", errInvalidJournalReversal
			}
			credits += line.Amount
		default:
			return "", errInvalidJournalReversal
		}
	}
	if debits == 0 || debits != credits || debits != entry.Amount {
		return "", errInvalidJournalReversal
	}
	reference := "REV-" + entry.ReferenceNo
	if entry.ReferenceNo == "" {
		reference = "REV-" + entry.ID
	}
	description := "Reversal: " + entry.Description
	reversalID := newID()
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type,reversal_of,correction_reason)
		VALUES($1,$2,$3,'repayment',$4,$5,$6,'posted',$7,$8,$9,$10,$11,$12,$13)`, reversalID, reference, entry.TransactionID, time.Now().In(jakartaLocation).Format("2006-01-02"), entry.Source, debits, entry.BatchID, description, actorID, entry.Category, entry.LoanType, entry.ID, reason); err != nil {
		return "", err
	}
	for _, line := range lines {
		side := accountingDirectionCredit
		if line.Side == accountingDirectionCredit {
			side = accountingDirectionDebit
		}
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines(id,journal_id,side,coa_code,amount,component,mapping_override) VALUES($1,$2,$3,$4,$5,$6,TRUE)`, newID(), reversalID, side, line.COACode, line.Amount, line.Component); err != nil {
			return "", err
		}
	}
	return reversalID, nil
}

func addLoanRepaymentAuditProtection(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		for _, statement := range []string{
			`CREATE TRIGGER loan_repayment_audits_actor_guard BEFORE INSERT ON loan_repayment_audits WHEN NOT EXISTS (SELECT 1 FROM users WHERE id=NEW.actor_id AND role='super_admin' AND active=TRUE AND historical_identity=FALSE) BEGIN SELECT RAISE(ABORT,'active super admin actor is required'); END`,
			`CREATE TRIGGER loan_repayment_audits_append_only_update BEFORE UPDATE ON loan_repayment_audits BEGIN SELECT RAISE(ABORT,'loan repayment audit rows are append-only'); END`,
			`CREATE TRIGGER loan_repayment_audits_append_only_delete BEFORE DELETE ON loan_repayment_audits BEGIN SELECT RAISE(ABORT,'loan repayment audit rows are append-only'); END`,
		} {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		return nil
	}
	var qualifiedSchema string
	if err := tx.QueryRow(`SELECT quote_ident(current_schema())`).Scan(&qualifiedSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE loan_repayment_audits ENABLE ROW LEVEL SECURITY`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE OR REPLACE FUNCTION protect_loan_repayment_audit_rows() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$ BEGIN
		IF TG_OP IN ('UPDATE','DELETE') THEN RAISE EXCEPTION 'loan repayment audit rows are append-only'; END IF;
		IF NOT EXISTS (SELECT 1 FROM ` + qualifiedSchema + `.users WHERE id=NEW.actor_id AND role='super_admin' AND active=TRUE AND historical_identity=FALSE) THEN RAISE EXCEPTION 'active super admin actor is required'; END IF;
		RETURN NEW;
	END $$`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE TRIGGER loan_repayment_audits_append_only BEFORE INSERT OR UPDATE OR DELETE ON loan_repayment_audits FOR EACH ROW EXECUTE FUNCTION protect_loan_repayment_audit_rows()`)
	return err
}

func allowAddedRepaymentAuditActions(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		for _, statement := range []string{
			`DROP TRIGGER IF EXISTS loan_repayment_audits_actor_guard`,
			`DROP TRIGGER IF EXISTS loan_repayment_audits_append_only_update`,
			`DROP TRIGGER IF EXISTS loan_repayment_audits_append_only_delete`,
			`CREATE TABLE loan_repayment_audits_v41 (
				id TEXT PRIMARY KEY,
				repayment_id TEXT NOT NULL,
				loan_id TEXT NOT NULL REFERENCES loans(id),
				member_id TEXT NOT NULL REFERENCES members(id),
				action TEXT NOT NULL CHECK (action IN ('added','edited','removed')),
				reason TEXT NOT NULL,
				before_state TEXT NOT NULL,
				after_state TEXT NOT NULL DEFAULT '',
				actor_id TEXT NOT NULL REFERENCES users(id),
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`INSERT INTO loan_repayment_audits_v41 SELECT id,repayment_id,loan_id,member_id,action,reason,before_state,after_state,actor_id,created_at FROM loan_repayment_audits`,
			`DROP TABLE loan_repayment_audits`,
			`ALTER TABLE loan_repayment_audits_v41 RENAME TO loan_repayment_audits`,
			`CREATE INDEX idx_loan_repayment_audits_created ON loan_repayment_audits(created_at,id)`,
			`CREATE INDEX idx_loan_repayment_audits_repayment ON loan_repayment_audits(repayment_id,created_at,id)`,
		} {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		return addLoanRepaymentAuditProtection(tx, true)
	}
	if _, err := tx.Exec(`ALTER TABLE loan_repayment_audits DROP CONSTRAINT IF EXISTS loan_repayment_audits_action_check`); err != nil {
		return err
	}
	_, err := tx.Exec(`ALTER TABLE loan_repayment_audits ADD CONSTRAINT loan_repayment_audits_action_check CHECK (action IN ('added','edited','removed'))`)
	return err
}
