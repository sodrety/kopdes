package app

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	errInvalidMemberDeactivation              = errors.New("invalid member deactivation")
	errMemberDeactivationNotFound             = errors.New("member deactivation request not found")
	errMemberDeactivationAlreadyPending       = errors.New("member deactivation already pending")
	errMemberDeactivationNotPending           = errors.New("member deactivation not pending")
	errMemberDeactivationNotAllowed           = errors.New("member deactivation not allowed")
	errMemberDeactivationBankDetailsRequired  = errors.New("member deactivation bank details required")
	errMemberDeactivationPayoutSourceRequired = errors.New("member deactivation payout source required")
	errMemberDeactivationNegativeBalance      = errors.New("member deactivation has negative savings balance")
	errMemberDeactivationLastKetuaUtama       = errors.New("cannot deactivate the last active ketua utama")
	errMemberDeactivationBalanceChanged       = errors.New("member deactivation savings balance changed")
	errMemberDeactivationPending              = errors.New("member deactivation pending")
	errMemberDeactivationInactive             = errors.New("member is not active")
)

var memberDeactivationCategories = []string{"pokok", "wajib", "sukarela", "shu", "khusus"}

type MemberDeactivationBalance struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount"`
}

type MemberDeactivationRequest struct {
	ID                   string                      `json:"id"`
	MemberID             string                      `json:"member_id"`
	MemberNo             string                      `json:"member_no"`
	FullName             string                      `json:"full_name"`
	Reason               string                      `json:"reason"`
	Status               string                      `json:"status"`
	CurrentApprovalStage string                      `json:"current_approval_stage,omitempty"`
	PayoutSource         string                      `json:"payout_source,omitempty"`
	BankName             string                      `json:"-"`
	BankAccount          string                      `json:"-"`
	RequestedBy          string                      `json:"requested_by"`
	RequestedByName      string                      `json:"requested_by_name,omitempty"`
	CancelledBy          string                      `json:"cancelled_by,omitempty"`
	CancellationReason   string                      `json:"cancellation_reason,omitempty"`
	RejectionReason      string                      `json:"rejection_reason,omitempty"`
	CreatedAt            string                      `json:"created_at,omitempty"`
	ReviewedAt           string                      `json:"reviewed_at,omitempty"`
	Balances             []MemberDeactivationBalance `json:"balances"`
	TotalPayout          int64                       `json:"total_payout"`
	OutstandingLoan      int64                       `json:"outstanding_loan"`
	ApprovalHistory      []ApprovalDecision          `json:"approval_history"`
	CanDecide            bool                        `json:"can_decide"`
	CanCancel            bool                        `json:"can_cancel"`
}

type memberDeactivationInput struct {
	Reason string `json:"reason" form:"reason"`
}

type approveMemberDeactivationInput struct {
	PayoutSource string `json:"payout_source" form:"payout_source"`
	Note         string `json:"note" form:"note"`
}

func (s *Server) createMemberDeactivationRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var input memberDeactivationInput
	if err := c.ShouldBind(&input); err != nil || strings.TrimSpace(input.Reason) == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_member_deactivation_reason_required"))
		return
	}
	request, err := s.insertMemberDeactivationRequest(c.Param("id"), actor, input.Reason, languageFromRequest(c))
	if err != nil {
		respondMemberDeactivationError(c, err)
		return
	}
	respondCreatedOrHXRedirect(c, "/admin/members/"+request.MemberID, request)
}

func (s *Server) approveMemberDeactivationRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var input approveMemberDeactivationInput
	if err := c.ShouldBind(&input); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_member_deactivation_approval"))
		return
	}
	request, err := s.approveMemberDeactivationRequestByID(c.Param("id"), actor, input, languageFromRequest(c))
	if err != nil {
		respondMemberDeactivationError(c, err)
		return
	}
	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) rejectMemberDeactivationRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var input memberDeactivationInput
	if err := c.ShouldBind(&input); err != nil || strings.TrimSpace(input.Reason) == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_member_deactivation_rejection_required"))
		return
	}
	request, err := s.rejectMemberDeactivationRequestByID(c.Param("id"), actor, input.Reason)
	if err != nil {
		respondMemberDeactivationError(c, err)
		return
	}
	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) cancelMemberDeactivationRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	request, err := s.cancelMemberDeactivationRequestByID(c.Param("id"), actor)
	if err != nil {
		respondMemberDeactivationError(c, err)
		return
	}
	respondOKOrHXRedirect(c, "/admin/members/"+request.MemberID, request)
}

func respondMemberDeactivationError(c *gin.Context, err error) {
	lang := languageFromRequest(c)
	switch {
	case errors.Is(err, errInvalidMemberDeactivation):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_member_deactivation_reason_required"))
	case errors.Is(err, errMemberDeactivationNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_member_deactivation_not_found"))
	case errors.Is(err, errMemberDeactivationAlreadyPending):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_already_pending"))
	case errors.Is(err, errMemberDeactivationNotPending):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_not_pending"))
	case errors.Is(err, errMemberDeactivationNotAllowed):
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_member_deactivation_not_allowed"))
	case errors.Is(err, errWrongApprovalStage):
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_wrong_approval_stage"))
	case errors.Is(err, errMemberDeactivationBankDetailsRequired):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_bank_details_required"))
	case errors.Is(err, errMemberDeactivationPayoutSourceRequired):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_member_deactivation_payout_source_required"))
	case errors.Is(err, errMemberDeactivationNegativeBalance):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_negative_balance"))
	case errors.Is(err, errMemberDeactivationLastKetuaUtama):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_last_ketua_utama"))
	case errors.Is(err, errMemberDeactivationBalanceChanged):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_balance_changed"))
	case errors.Is(err, errInvalidTransactionSource):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_member_deactivation_payout_source_required"))
	case errors.Is(err, errInactiveWithdrawalMember):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_inactive_withdrawal_member"))
	case errors.Is(err, errMemberDeactivationInactive):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_member_deactivation_member_inactive"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
	}
}

func (s *Server) insertMemberDeactivationRequest(memberID string, actor User, reason, lang string) (MemberDeactivationRequest, error) {
	memberID = strings.TrimSpace(memberID)
	reason = strings.TrimSpace(reason)
	if memberID == "" || reason == "" {
		return MemberDeactivationRequest{}, errInvalidMemberDeactivation
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var member Member
	err = tx.QueryRow(`SELECT id,status,COALESCE(bank_name,''),COALESCE(bank_account,'') FROM members WHERE id=$1`+rowLockClause(s.db), memberID).Scan(&member.ID, &member.Status, &member.BankName, &member.BankAccount)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, errMemberDeactivationNotFound
	}
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if member.Status != "active" {
		return MemberDeactivationRequest{}, errMemberDeactivationInactive
	}
	var pendingID string
	if err := tx.QueryRow(`SELECT id FROM member_deactivation_requests WHERE member_id=$1 AND status='pending'`, memberID).Scan(&pendingID); err == nil {
		return MemberDeactivationRequest{}, errMemberDeactivationAlreadyPending
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, err
	}
	var isKetuaUtama bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM officer_appointments WHERE member_id=$1 AND role='ketua_utama' AND active=TRUE)`, memberID).Scan(&isKetuaUtama); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if isKetuaUtama {
		var activeKetuaUtama int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active'`).Scan(&activeKetuaUtama); err != nil {
			return MemberDeactivationRequest{}, err
		}
		if activeKetuaUtama <= 1 {
			return MemberDeactivationRequest{}, errMemberDeactivationLastKetuaUtama
		}
	}
	balances, total, err := memberDeactivationBalancesTx(tx, memberID)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	request := MemberDeactivationRequest{ID: newID(), MemberID: memberID, Reason: reason, Status: "pending", CurrentApprovalStage: approvalStageManager, RequestedBy: actor.ID, Balances: balances, TotalPayout: total}
	if _, err := tx.Exec(`INSERT INTO member_deactivation_requests (id,member_id,reason,status,current_approval_stage,requested_by) VALUES ($1,$2,$3,'pending','manager',$4)`, request.ID, memberID, reason, actor.ID); err != nil {
		if isUniqueViolation(err) {
			return MemberDeactivationRequest{}, errMemberDeactivationAlreadyPending
		}
		return MemberDeactivationRequest{}, err
	}
	for _, balance := range balances {
		if _, err := tx.Exec(`INSERT INTO member_deactivation_request_balances (request_id,category,amount) VALUES ($1,$2,$3)`, request.ID, balance.Category, balance.Amount); err != nil {
			return MemberDeactivationRequest{}, err
		}
	}
	if err := cancelMemberPendingWithdrawalsTx(tx, memberID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := cancelMemberUndisbursedLoansTx(tx, memberID, actor.ID, translate(lang, "loan_request_cancelled_by_deactivation")); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := createStageNotification(tx, "member_deactivation", request.ID, approvalStageManager, "/admin/withdrawal-requests"); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return request, nil
}

func memberDeactivationBalancesTx(tx *sql.Tx, memberID string) ([]MemberDeactivationBalance, int64, error) {
	balances, total, err := memberDeactivationBalances(tx, memberID)
	if err != nil {
		return nil, 0, err
	}
	for _, balance := range balances {
		if balance.Amount < 0 {
			return nil, 0, errMemberDeactivationNegativeBalance
		}
	}
	return balances, total, nil
}

func memberDeactivationBalances(queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, memberID string) ([]MemberDeactivationBalance, int64, error) {
	rows, err := queryer.Query(`SELECT category,COALESCE(SUM(CASE WHEN type='deposit' THEN amount ELSE -amount END),0) FROM saving_records WHERE member_id=$1 GROUP BY category`, memberID)
	if err != nil {
		return nil, 0, err
	}
	amounts := make(map[string]int64, len(memberDeactivationCategories))
	for rows.Next() {
		var category string
		var amount int64
		if err := rows.Scan(&category, &amount); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		amounts[category] = amount
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	balances := make([]MemberDeactivationBalance, 0, len(memberDeactivationCategories))
	var total int64
	for _, category := range memberDeactivationCategories {
		amount := amounts[category]
		if (amount > 0 && total > math.MaxInt64-amount) || (amount < 0 && total < math.MinInt64-amount) {
			return nil, 0, fmt.Errorf("member deactivation total overflow")
		}
		total += amount
		balances = append(balances, MemberDeactivationBalance{Category: category, Amount: amount})
	}
	return balances, total, nil
}

func hasPendingMemberDeactivation(tx *sql.Tx, memberID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM member_deactivation_requests WHERE member_id=$1 AND status='pending')`, memberID).Scan(&exists)
	return exists, err
}

func cancelMemberPendingWithdrawalsTx(tx *sql.Tx, memberID string) error {
	rows, err := tx.Query(`SELECT id FROM withdrawal_requests WHERE member_id=$1 AND status='pending'`, memberID)
	if err != nil {
		return err
	}
	var requestIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		requestIDs = append(requestIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range requestIDs {
		if _, err := tx.Exec(`UPDATE withdrawal_requests SET status='cancelled',current_approval_stage=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='pending'`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE withdrawal_reservations SET status='released',updated_at=CURRENT_TIMESTAMP WHERE request_id=$1 AND status='active'`, id); err != nil {
			return err
		}
		if err := resolveRequestNotifications(tx, "withdrawal", id); err != nil {
			return err
		}
	}
	return nil
}

func cancelMemberUndisbursedLoansTx(tx *sql.Tx, memberID, actorID, reason string) error {
	rows, err := tx.Query(`SELECT id FROM loan_requests WHERE member_id=$1 AND (status='pending' OR (status='approved' AND disbursement_date='')) AND NOT EXISTS (SELECT 1 FROM loans WHERE loans.loan_request_id=loan_requests.id)`, memberID)
	if err != nil {
		return err
	}
	var requestIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		requestIDs = append(requestIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range requestIDs {
		if _, err := tx.Exec(`UPDATE loan_requests SET status='cancelled',current_approval_stage=NULL,cancellation_reason=$1,cancelled_by=$2,cancelled_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND (status='pending' OR (status='approved' AND disbursement_date=''))`, reason, actorID, id); err != nil {
			return err
		}
		if err := resolveRequestNotifications(tx, "loan", id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) approveMemberDeactivationRequestByID(requestID string, actor User, input approveMemberDeactivationInput, lang string) (MemberDeactivationRequest, error) {
	source := strings.TrimSpace(input.PayoutSource)
	stage := approvalStageForOfficer(actor.Role)
	if source != "" && source != transactionSourceCash && source != transactionSourceBank {
		return MemberDeactivationRequest{}, errInvalidTransactionSource
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var memberID string
	if err := tx.QueryRow(`SELECT member_id FROM member_deactivation_requests WHERE id=$1`, requestID).Scan(&memberID); errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, errMemberDeactivationNotFound
	} else if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE members SET updated_at=updated_at WHERE id=$1`, memberID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	request, err := memberDeactivationRequestForUpdate(tx, s.db, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, errMemberDeactivationNotFound
	}
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if request.Status != "pending" {
		return MemberDeactivationRequest{}, errMemberDeactivationNotPending
	}
	if request.CurrentApprovalStage != stage {
		return MemberDeactivationRequest{}, errWrongApprovalStage
	}
	if stage == approvalStageManager {
		if source == "" {
			return MemberDeactivationRequest{}, errMemberDeactivationPayoutSourceRequired
		}
		if source == transactionSourceBank && (strings.TrimSpace(request.BankName) == "" || strings.TrimSpace(request.BankAccount) == "") {
			return MemberDeactivationRequest{}, errMemberDeactivationBankDetailsRequired
		}
		request.PayoutSource = source
	}
	if err := insertMemberDeactivationDecisionTx(tx, requestID, actor, stage, "approved", input.Note, ""); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "member_deactivation", requestID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	next := nextApprovalStage(stage)
	if next != "" {
		if _, err := tx.Exec(`UPDATE member_deactivation_requests SET current_approval_stage=$1,payout_source=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending' AND current_approval_stage=$4`, next, request.PayoutSource, requestID, stage); err != nil {
			return MemberDeactivationRequest{}, err
		}
		if err := createStageNotification(tx, "member_deactivation", requestID, next, "/admin/withdrawal-requests"); err != nil {
			return MemberDeactivationRequest{}, err
		}
		if err := tx.Commit(); err != nil {
			return MemberDeactivationRequest{}, err
		}
		return s.memberDeactivationRequestByID(requestID)
	}
	if request.PayoutSource != transactionSourceCash && request.PayoutSource != transactionSourceBank {
		return MemberDeactivationRequest{}, errMemberDeactivationPayoutSourceRequired
	}
	current, total, err := memberDeactivationBalancesTx(tx, request.MemberID)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if total != request.TotalPayout || !sameDeactivationBalances(current, request.Balances) {
		return MemberDeactivationRequest{}, errMemberDeactivationBalanceChanged
	}
	if request.PayoutSource == transactionSourceBank {
		if strings.TrimSpace(request.BankName) == "" || strings.TrimSpace(request.BankAccount) == "" {
			return MemberDeactivationRequest{}, errMemberDeactivationBankDetailsRequired
		}
	}
	var memberIsKetuaUtama bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM officer_appointments WHERE member_id=$1 AND role='ketua_utama' AND active=TRUE)`, request.MemberID).Scan(&memberIsKetuaUtama); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if memberIsKetuaUtama {
		var activeKetuaUtama int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active'`).Scan(&activeKetuaUtama); err != nil {
			return MemberDeactivationRequest{}, err
		}
		if activeKetuaUtama <= 1 {
			return MemberDeactivationRequest{}, errMemberDeactivationLastKetuaUtama
		}
	}
	date := time.Now().In(jakartaLocation).Format("2006-01-02")
	reference := "DEAK-" + request.ID
	payoutNote := translate(lang, "member_deactivation_payout_note") + " " + request.ID + ": " + request.Reason
	for _, balance := range request.Balances {
		if balance.Amount == 0 {
			continue
		}
		recordID := newID()
		if _, err := tx.Exec(`INSERT INTO saving_records (id,member_id,type,category,source,coa_code,amount,record_date,reference_no,note,recorded_by) VALUES ($1,$2,'withdrawal',$3,$4,NULL,$5,$6,$7,$8,$9)`, recordID, request.MemberID, balance.Category, request.PayoutSource, balance.Amount, date, reference, payoutNote, actor.ID); err != nil {
			return MemberDeactivationRequest{}, err
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: reference, TransactionID: recordID, TransactionType: "withdrawal", TransactionDate: date,
			Source: request.PayoutSource, Amount: balance.Amount, Category: balance.Category,
			Components: []accountingJournalComponent{
				{Component: "savings_liability", Side: accountingDirectionDebit, Amount: balance.Amount},
				{Component: "cash_bank", Side: accountingDirectionCredit, Amount: balance.Amount},
			},
			Description: translate(lang, "member_deactivation_journal_description") + " " + request.MemberNo + " · " + translate(lang, "simpanan_"+balance.Category), RecordedBy: actor.ID, BatchID: request.ID,
		}); err != nil {
			return MemberDeactivationRequest{}, err
		}
	}
	memberResult, err := tx.Exec(`UPDATE members SET status='inactive',updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='active'`, request.MemberID)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if affected, err := memberResult.RowsAffected(); err != nil {
		return MemberDeactivationRequest{}, err
	} else if affected != 1 {
		return MemberDeactivationRequest{}, errMemberDeactivationNotPending
	}
	requestResult, err := tx.Exec(`UPDATE member_deactivation_requests SET status='approved',current_approval_stage=NULL,reviewed_by=$1,reviewed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND status='pending' AND current_approval_stage=$3`, actor.ID, requestID, stage)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if affected, err := requestResult.RowsAffected(); err != nil {
		return MemberDeactivationRequest{}, err
	} else if affected != 1 {
		return MemberDeactivationRequest{}, errMemberDeactivationNotPending
	}
	if err := createMemberOutcomeNotification(tx, "member_deactivation", requestID, request.MemberID, "approved", "/admin/members/"+request.MemberID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return s.memberDeactivationRequestByID(requestID)
}

func sameDeactivationBalances(left, right []MemberDeactivationBalance) bool {
	if len(left) != len(right) {
		return false
	}
	amounts := make(map[string]int64, len(left))
	for _, balance := range left {
		amounts[balance.Category] = balance.Amount
	}
	for _, balance := range right {
		if amounts[balance.Category] != balance.Amount {
			return false
		}
	}
	return true
}

func (s *Server) rejectMemberDeactivationRequestByID(requestID string, actor User, reason string) (MemberDeactivationRequest, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return MemberDeactivationRequest{}, errInvalidMemberDeactivation
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	request, err := memberDeactivationRequestForUpdate(tx, s.db, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, errMemberDeactivationNotFound
	}
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if request.Status != "pending" {
		return MemberDeactivationRequest{}, errMemberDeactivationNotPending
	}
	stage := approvalStageForOfficer(actor.Role)
	if request.CurrentApprovalStage != stage {
		return MemberDeactivationRequest{}, errWrongApprovalStage
	}
	if err := insertMemberDeactivationDecisionTx(tx, requestID, actor, stage, "rejected", "", reason); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE member_deactivation_requests SET status='rejected',current_approval_stage=NULL,rejection_reason=$1,reviewed_by=$2,reviewed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending' AND current_approval_stage=$4`, reason, actor.ID, requestID, stage); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "member_deactivation", requestID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := createMemberOutcomeNotification(tx, "member_deactivation", requestID, request.MemberID, "rejected", "/admin/members/"+request.MemberID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return s.memberDeactivationRequestByID(requestID)
}

func (s *Server) cancelMemberDeactivationRequestByID(requestID string, actor User) (MemberDeactivationRequest, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	request, err := memberDeactivationRequestForUpdate(tx, s.db, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, errMemberDeactivationNotFound
	}
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	if request.Status != "pending" {
		return MemberDeactivationRequest{}, errMemberDeactivationNotPending
	}
	if request.RequestedBy != actor.ID {
		return MemberDeactivationRequest{}, errMemberDeactivationNotAllowed
	}
	if _, err := tx.Exec(`UPDATE member_deactivation_requests SET status='cancelled',current_approval_stage=NULL,cancelled_by=$1,cancelled_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND status='pending'`, actor.ID, requestID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "member_deactivation", requestID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return s.memberDeactivationRequestByID(requestID)
}

func insertMemberDeactivationDecisionTx(tx *sql.Tx, requestID string, actor User, stage, decision, note, reason string) error {
	name := strings.TrimSpace(actor.FullName)
	if name == "" {
		name = actor.Email
	}
	_, err := tx.Exec(`INSERT INTO member_deactivation_approvals (id,request_id,stage,decision,officer_id,officer_name,officer_role,note,reason) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, newID(), requestID, stage, decision, actor.ID, name, actor.Role, strings.TrimSpace(note), strings.TrimSpace(reason))
	return err
}

func (s *Server) memberDeactivationRequestByID(id string) (MemberDeactivationRequest, error) {
	request, err := loadMemberDeactivationRequest(s.db, id)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	request.ApprovalHistory, err = memberDeactivationApprovalHistory(s.db, id)
	return request, err
}

func loadMemberDeactivationRequest(tx interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}, id string) (MemberDeactivationRequest, error) {
	var request MemberDeactivationRequest
	err := tx.QueryRow(`SELECT r.id,r.member_id,m.member_no,m.full_name,r.reason,r.status,COALESCE(r.current_approval_stage,''),COALESCE(r.payout_source,''),COALESCE(m.bank_name,''),COALESCE(m.bank_account,''),r.requested_by,COALESCE(u.full_name,''),COALESCE(r.cancelled_by,''),COALESCE(r.cancellation_reason,''),COALESCE(r.rejection_reason,''),COALESCE(CAST(r.created_at AS TEXT),''),COALESCE(CAST(r.reviewed_at AS TEXT),'') FROM member_deactivation_requests r JOIN members m ON m.id=r.member_id LEFT JOIN users u ON u.id=r.requested_by WHERE r.id=$1`, id).Scan(&request.ID, &request.MemberID, &request.MemberNo, &request.FullName, &request.Reason, &request.Status, &request.CurrentApprovalStage, &request.PayoutSource, &request.BankName, &request.BankAccount, &request.RequestedBy, &request.RequestedByName, &request.CancelledBy, &request.CancellationReason, &request.RejectionReason, &request.CreatedAt, &request.ReviewedAt)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	balances, total, err := memberDeactivationBalancesForRequest(tx, request.ID)
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	request.Balances, request.TotalPayout = balances, total
	if err := tx.QueryRow(`SELECT COALESCE(SUM(remaining_balance),0) FROM loans WHERE member_id=$1 AND status IN ('active','adjustment_due') AND remaining_balance>0`, request.MemberID).Scan(&request.OutstandingLoan); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return request, nil
}

func memberDeactivationRequestForUpdate(tx *sql.Tx, db *sql.DB, id string) (MemberDeactivationRequest, error) {
	var lockedID string
	if err := tx.QueryRow(`SELECT id FROM member_deactivation_requests WHERE id=$1`+rowLockClause(db), id).Scan(&lockedID); err != nil {
		return MemberDeactivationRequest{}, err
	}
	return loadMemberDeactivationRequest(tx, id)
}

func memberDeactivationBalancesForRequest(tx interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, requestID string) ([]MemberDeactivationBalance, int64, error) {
	rows, err := tx.Query(`SELECT category,amount FROM member_deactivation_request_balances WHERE request_id=$1 ORDER BY category`, requestID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	byCategory := map[string]int64{}
	for rows.Next() {
		var category string
		var amount int64
		if err := rows.Scan(&category, &amount); err != nil {
			return nil, 0, err
		}
		byCategory[category] = amount
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	balances := make([]MemberDeactivationBalance, 0, len(memberDeactivationCategories))
	var total int64
	for _, category := range memberDeactivationCategories {
		amount := byCategory[category]
		if amount > math.MaxInt64-total {
			return nil, 0, fmt.Errorf("member deactivation total overflow")
		}
		total += amount
		balances = append(balances, MemberDeactivationBalance{Category: category, Amount: amount})
	}
	return balances, total, nil
}

func memberDeactivationApprovalHistory(db *sql.DB, requestID string) ([]ApprovalDecision, error) {
	rows, err := db.Query(`SELECT id,stage,decision,officer_id,officer_name,officer_role,note,reason,created_at FROM member_deactivation_approvals WHERE request_id=$1 ORDER BY created_at,id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []ApprovalDecision
	for rows.Next() {
		var item ApprovalDecision
		if err := rows.Scan(&item.ID, &item.Stage, &item.Decision, &item.OfficerID, &item.OfficerName, &item.OfficerRole, &item.Note, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, item)
	}
	return history, rows.Err()
}

func (s *Server) memberDeactivationRequestsForAdmin() ([]MemberDeactivationRequest, error) {
	rows, err := s.db.Query(`SELECT id FROM member_deactivation_requests WHERE status='pending' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	requests := make([]MemberDeactivationRequest, 0, len(ids))
	for _, id := range ids {
		request, err := s.memberDeactivationRequestByID(id)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (s *Server) latestMemberDeactivationRequest(memberID string) (MemberDeactivationRequest, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM member_deactivation_requests WHERE member_id=$1 ORDER BY CASE WHEN status='pending' THEN 0 ELSE 1 END,created_at DESC,id DESC LIMIT 1`, memberID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberDeactivationRequest{}, nil
	}
	if err != nil {
		return MemberDeactivationRequest{}, err
	}
	return s.memberDeactivationRequestByID(id)
}

func allowUndisbursedLoanRequestCancellation(tx *sql.Tx, isSQLite bool) error {
	const old = `(OLD.status='pending' AND NEW.status='cancelled')`
	const added = `(OLD.status='pending' AND NEW.status='cancelled') OR (OLD.status='approved' AND NEW.status='cancelled' AND OLD.disbursement_date='' AND NOT EXISTS (SELECT 1 FROM loans WHERE loans.loan_request_id=OLD.id))`
	if isSQLite {
		const sqliteOld = `WHEN OLD.status='pending' AND NEW.status='cancelled' THEN NULL`
		const sqliteAdded = `WHEN OLD.status='pending' AND NEW.status='cancelled' THEN NULL
			 WHEN OLD.status='approved' AND NEW.status='cancelled' AND OLD.disbursement_date='' AND NOT EXISTS (SELECT 1 FROM loans WHERE loans.loan_request_id=OLD.id) THEN NULL`
		var triggerSQL string
		if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name='loan_requests_state_integrity'`).Scan(&triggerSQL); err != nil {
			return err
		}
		if !strings.Contains(triggerSQL, sqliteOld) {
			return fmt.Errorf("loan request state trigger does not contain expected cancellation rule")
		}
		triggerSQL = strings.Replace(triggerSQL, sqliteOld, sqliteAdded, 1)
		if _, err := tx.Exec(`DROP TRIGGER loan_requests_state_integrity`); err != nil {
			return err
		}
		_, err := tx.Exec(triggerSQL)
		return err
	}
	var functionDef string
	if err := tx.QueryRow(`SELECT pg_get_functiondef('validate_loan_request_state_integrity()'::regprocedure)`).Scan(&functionDef); err != nil {
		return err
	}
	if !strings.Contains(functionDef, old) {
		return fmt.Errorf("loan request state function does not contain expected cancellation rule")
	}
	functionDef = strings.Replace(functionDef, old, added, 1)
	_, err := tx.Exec(functionDef)
	return err
}
