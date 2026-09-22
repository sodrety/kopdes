package app

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func isSuperAdmin(user User) bool {
	return user.Role == "super_admin" && !user.MemberID.Valid
}

func insertSuperAdminOverride(tx *sql.Tx, requestType, requestID string, actor User, previousStage, decision, reason string) error {
	if !isSuperAdmin(actor) {
		return errors.New("super admin actor is required")
	}
	_, err := tx.Exec(`INSERT INTO super_admin_overrides (id,request_type,request_id,actor_id,previous_stage,decision,reason) VALUES ($1,$2,$3,$4,$5,$6,$7)`, newID(), requestType, requestID, actor.ID, strings.TrimSpace(previousStage), decision, strings.TrimSpace(reason))
	return err
}

func approvalHistoryWithOverrides(db *sql.DB, table, requestType, requestID string, includeOfficer bool) ([]ApprovalDecision, error) {
	history, err := approvalHistory(db, table, requestID, includeOfficer)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT o.id,o.previous_stage,o.decision,o.actor_id,COALESCE(u.full_name,''),u.email,o.reason,o.created_at FROM super_admin_overrides o JOIN users u ON u.id=o.actor_id WHERE o.request_type=$1 AND o.request_id=$2`, requestType, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var decision ApprovalDecision
		var actorName, actorEmail string
		if err := rows.Scan(&decision.ID, &decision.Stage, &decision.Decision, &decision.OfficerID, &actorName, &actorEmail, &decision.Reason, &decision.CreatedAt); err != nil {
			return nil, err
		}
		decision.OfficerRole = "super_admin"
		decision.OfficerName = strings.TrimSpace(actorName)
		if decision.OfficerName == "" {
			decision.OfficerName = actorEmail
		}
		if !includeOfficer {
			decision.OfficerID = ""
			decision.OfficerName = ""
		}
		history = append(history, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(history, func(i, j int) bool {
		if history[i].CreatedAt == history[j].CreatedAt {
			return history[i].ID < history[j].ID
		}
		return history[i].CreatedAt < history[j].CreatedAt
	})
	return history, nil
}

func latestDecisionWithOverrides(db *sql.DB, table, requestType, requestID string) (*ApprovalDecision, error) {
	history, err := approvalHistoryWithOverrides(db, table, requestType, requestID, false)
	if err != nil || len(history) == 0 {
		return nil, err
	}
	return &history[len(history)-1], nil
}

func (s *Server) overrideLoanRequest(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok || !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_only"))
		return
	}
	var req approveLoanInput
	if err := bindRequestWithRupiahAmount(c, &req, "approved_amount"); err != nil && !errors.Is(err, errInvalidRupiahAmount) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_approval"))
		return
	} else if errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	}
	result, err := s.overrideLoanRequestByID(c.Param("id"), user, req)
	if errors.Is(err, errInvalidLoanApproval) || errors.Is(err, errInvalidLoanApprovalCalculated) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_loan_approval_fields"))
		return
	}
	if errors.Is(err, errLoanAmountLimitExceeded) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_loan_amount_limit"))
		return
	}
	if errors.Is(err, errInvalidLoanStartDate) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_loan_start_date_range"))
		return
	}
	if errors.Is(err, errInactiveLoanMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_inactive_loan_member"))
		return
	}
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_request_not_pending"))
		return
	}
	if errors.Is(err, errLoanRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_loan_request_not_found"))
		return
	}
	if errors.Is(err, errInvalidTransactionSource) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_transaction_source"))
		return
	}
	if errors.Is(err, errCOAAccountNotFound) || errors.Is(err, errCOAAccountInactive) || errors.Is(err, errCOAAccountGroup) || errors.Is(err, errJournalAccountConflict) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_transaction_coa"))
		return
	}
	if err != nil {
		slog.Error("super admin loan override failed", "request_id", requestIDFromContext(c), "loan_request_id", c.Param("id"), "error", err)
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/loans", result)
}

func (s *Server) overrideLoanRejection(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok || !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_only"))
		return
	}
	var req rejectLoanInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_rejection"))
		return
	}
	request, err := s.rejectLoanRequestBySuperAdmin(c.Param("id"), user, req)
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_request_not_pending"))
		return
	}
	if errors.Is(err, errLoanRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_loan_request_not_found"))
		return
	}
	if err != nil {
		slog.Error("super admin loan rejection failed", "request_id", requestIDFromContext(c), "loan_request_id", c.Param("id"), "error", err)
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/loan-requests", request)
}

func (s *Server) overrideWithdrawalRequest(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok || !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_only"))
		return
	}
	var req approveWithdrawalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_approval"))
		return
	}
	request, err := s.approveWithdrawalRequestBySuperAdmin(c.Param("id"), user, req)
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_withdrawal_request_not_pending"))
		return
	}
	if errors.Is(err, errInactiveWithdrawalMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_inactive_withdrawal_member"))
		return
	}
	if errors.Is(err, errInsufficientSukarelaBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_withdrawal_amount_over_available"))
		return
	}
	if errors.Is(err, errWithdrawalRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_withdrawal_request_not_found"))
		return
	}
	if errors.Is(err, errInvalidTransactionSource) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_transaction_source"))
		return
	}
	if errors.Is(err, errCOAAccountNotFound) || errors.Is(err, errCOAAccountInactive) || errors.Is(err, errCOAAccountGroup) || errors.Is(err, errJournalAccountConflict) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_transaction_coa"))
		return
	}
	if isMonetaryAggregateCapacityError(err) {
		respondError(c, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", translate(lang, "error_monetary_aggregate_capacity"))
		return
	}
	if err != nil {
		slog.Error("super admin withdrawal override failed", "request_id", requestIDFromContext(c), "withdrawal_request_id", c.Param("id"), "error", err)
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) overrideWithdrawalRejection(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok || !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_only"))
		return
	}
	var req rejectWithdrawalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_rejection"))
		return
	}
	request, err := s.rejectWithdrawalRequestBySuperAdmin(c.Param("id"), user, req)
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_withdrawal_request_not_pending"))
		return
	}
	if errors.Is(err, errWithdrawalRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_withdrawal_request_not_found"))
		return
	}
	if err != nil {
		slog.Error("super admin withdrawal rejection failed", "request_id", requestIDFromContext(c), "withdrawal_request_id", c.Param("id"), "error", err)
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) overrideLoanRequestByID(requestID string, admin User, req approveLoanInput) (LoanApprovalResult, error) {
	source := normalizeTransactionSource(req.Source)
	if !validTransactionSource(source) {
		return LoanApprovalResult{}, errInvalidTransactionSource
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return LoanApprovalResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var request struct {
		MemberID                string
		LoanType                string
		RequestedAmount         int64
		RequestedDurationMonths int
		Status                  string
		Stage                   string
		CreatedAt               string
		ProposedApprovedAmount  int64
		ProposedDurationMonths  int
		ProposedStartDate       string
		ProposedAdminFeePolicy  string
		ProposedMonthlyAdminFee *int64
		ProposedTotalAdminFee   int64
		ProposedTotalObligation int64
	}
	err = tx.QueryRow(`SELECT member_id,loan_type,requested_amount,duration_months,status,COALESCE(current_approval_stage,''),created_at,COALESCE(proposed_approved_amount,0),COALESCE(proposed_duration_months,0),proposed_start_date,COALESCE(proposed_admin_fee_policy,''),proposed_monthly_admin_fee,COALESCE(proposed_total_admin_fee,0),COALESCE(proposed_total_obligation,0) FROM loan_requests WHERE id=$1`+rowLockClause(s.db), requestID).Scan(&request.MemberID, &request.LoanType, &request.RequestedAmount, &request.RequestedDurationMonths, &request.Status, &request.Stage, &request.CreatedAt, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedAdminFeePolicy, &request.ProposedMonthlyAdminFee, &request.ProposedTotalAdminFee, &request.ProposedTotalObligation)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanApprovalResult{}, errLoanRequestNotFound
	}
	if err != nil {
		return LoanApprovalResult{}, err
	}
	if request.Status != "pending" {
		return LoanApprovalResult{}, errLoanRequestNotPending
	}
	if err := insertSuperAdminOverride(tx, "loan", requestID, admin, request.Stage, "approved", req.Note); err != nil {
		return LoanApprovalResult{}, err
	}

	amount := req.ApprovedAmount
	if amount <= 0 {
		amount = request.ProposedApprovedAmount
	}
	if amount <= 0 {
		amount = request.RequestedAmount
	}
	duration := req.DurationMonths
	if duration <= 0 {
		duration = request.ProposedDurationMonths
	}
	if duration <= 0 {
		duration = request.RequestedDurationMonths
	}
	startDate := strings.TrimSpace(req.StartDate)
	if startDate == "" {
		startDate = strings.TrimSpace(request.ProposedStartDate)
	}
	if startDate == "" {
		startDate = time.Now().In(jakartaLocation).Format("2006-01-02")
	}
	if amount <= 0 || duration <= 0 {
		return LoanApprovalResult{}, errInvalidLoanApproval
	}

	var memberStatus string
	if err := tx.QueryRow(`SELECT status FROM members WHERE id=$1`+rowLockClause(s.db), request.MemberID).Scan(&memberStatus); err != nil {
		return LoanApprovalResult{}, err
	}
	if memberStatus != "active" {
		return LoanApprovalResult{}, errInactiveLoanMember
	}
	summary, err := savingSummary(tx, request.MemberID)
	if err != nil {
		return LoanApprovalResult{}, err
	}
	if amount > maxLoanAmountForSavingBalance(summary.CurrentBalance) {
		return LoanApprovalResult{}, errLoanAmountLimitExceeded
	}
	start, parseErr := parseLoanDate(startDate)
	if parseErr != nil || start.After(time.Now().In(jakartaLocation)) {
		return LoanApprovalResult{}, errInvalidLoanStartDate
	}
	requestTime, parseErr := parseDatabaseTime(request.CreatedAt)
	if parseErr != nil || start.Before(time.Date(requestTime.In(jakartaLocation).Year(), requestTime.In(jakartaLocation).Month(), requestTime.In(jakartaLocation).Day(), 0, 0, 0, 0, jakartaLocation)) {
		return LoanApprovalResult{}, errInvalidLoanStartDate
	}

	var calc LoanScheduleCalculation
	adminFeePolicy := request.ProposedAdminFeePolicy
	if adminFeePolicy == "" {
		var snapshotMonthlyAdminFee *int64
		switch request.LoanType {
		case "regular":
			calc, err = calculateRegularLoanSchedule(amount, duration, startDate)
			adminFeePolicy = regularLoanAdminFeePolicy
		case "secondary_goods":
			calc, err = calculateSecondaryGoodsLoanSchedule(amount, duration, startDate)
			adminFeePolicy = secondaryGoodsAdminFeePolicy
		case "goods_purchase_paylater":
			calc, err = calculatePaylaterLoanSchedule(amount, duration, startDate)
			adminFeePolicy = paylaterAdminFeePolicy
		default:
			return LoanApprovalResult{}, errInvalidLoanApproval
		}
		if err != nil {
			return LoanApprovalResult{}, errInvalidLoanApprovalCalculated
		}
		request.ProposedApprovedAmount = amount
		request.ProposedDurationMonths = duration
		request.ProposedStartDate = startDate
		request.ProposedAdminFeePolicy = adminFeePolicy
		if adminFeePolicy == regularLoanAdminFeePolicy {
			monthlyAdminFee := calc.MonthlyAdminFee
			snapshotMonthlyAdminFee = &monthlyAdminFee
		}
		request.ProposedMonthlyAdminFee = snapshotMonthlyAdminFee
		if adminFeePolicy == secondaryGoodsAdminFeePolicy || adminFeePolicy == paylaterAdminFeePolicy {
			request.ProposedMonthlyAdminFee = nil
		}
		request.ProposedTotalAdminFee = calc.TotalAdminFee
		request.ProposedTotalObligation = calc.TotalObligation
		if _, err := tx.Exec(`UPDATE loan_requests SET proposed_approved_amount=$1,proposed_duration_months=$2,proposed_start_date=$3,proposed_admin_fee_policy=$4,proposed_monthly_admin_fee=$5,proposed_total_admin_fee=$6,proposed_total_obligation=$7,updated_at=CURRENT_TIMESTAMP WHERE id=$8 AND status='pending'`, amount, duration, startDate, adminFeePolicy, request.ProposedMonthlyAdminFee, request.ProposedTotalAdminFee, request.ProposedTotalObligation, requestID); err != nil {
			return LoanApprovalResult{}, err
		}
	} else {
		if (req.ApprovedAmount > 0 && req.ApprovedAmount != request.ProposedApprovedAmount) || (req.DurationMonths > 0 && req.DurationMonths != request.ProposedDurationMonths) || (strings.TrimSpace(req.StartDate) != "" && strings.TrimSpace(req.StartDate) != request.ProposedStartDate) {
			return LoanApprovalResult{}, errInvalidLoanApproval
		}
	}

	if request.ProposedApprovedAmount <= 0 || request.ProposedDurationMonths <= 0 || request.ProposedStartDate == "" || request.ProposedAdminFeePolicy == "" || request.ProposedTotalObligation <= 0 {
		return LoanApprovalResult{}, errInvalidLoanApproval
	}
	if err := validateLoanFeeSnapshot(request.ProposedAdminFeePolicy, request.ProposedApprovedAmount, request.ProposedDurationMonths, request.ProposedMonthlyAdminFee, request.ProposedTotalAdminFee, request.ProposedTotalObligation); err != nil {
		return LoanApprovalResult{}, errInvalidLoanApprovalCalculated
	}
	maxDuration := maxRegularLoanDurationMonths
	if request.ProposedAdminFeePolicy == secondaryGoodsAdminFeePolicy {
		maxDuration = maxSecondaryGoodsDuration
	}
	if request.ProposedAdminFeePolicy == paylaterAdminFeePolicy {
		maxDuration = 1
	}
	calc, err = buildLoanScheduleFromObligation(request.ProposedTotalObligation, request.ProposedDurationMonths, request.ProposedStartDate, maxDuration)
	if err != nil {
		return LoanApprovalResult{}, errInvalidLoanApprovalCalculated
	}
	loan := Loan{ID: newID(), LoanRequestID: requestID, MemberID: request.MemberID, LoanType: request.LoanType, ApprovedAmount: request.ProposedApprovedAmount, DurationMonths: request.ProposedDurationMonths, MonthlyInstallment: calc.Installments[0].ScheduledAmount, RemainingBalance: request.ProposedTotalObligation, StartDate: request.ProposedStartDate, AdminFeePolicy: request.ProposedAdminFeePolicy, MonthlyAdminFee: request.ProposedMonthlyAdminFee, TotalAdminFee: request.ProposedTotalAdminFee, TotalObligation: request.ProposedTotalObligation, NextDueDate: calc.Installments[0].DueDate, FinalDueDate: calc.Installments[len(calc.Installments)-1].DueDate, Status: "active", Source: source, COACode: strings.TrimSpace(req.COACode), ApprovedBy: admin.ID}
	if _, err := tx.Exec(`UPDATE loan_requests SET status='approved',current_approval_stage=NULL,reviewed_by=$1,reviewed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND status='pending'`, admin.ID, requestID); err != nil {
		return LoanApprovalResult{}, err
	}
	if _, err := tx.Exec(`INSERT INTO loans (id,loan_request_id,member_id,loan_type,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,source,coa_code,start_date,admin_fee_policy,monthly_admin_fee,total_admin_fee,total_obligation,next_due_date,final_due_date) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, loan.ID, loan.LoanRequestID, loan.MemberID, loan.LoanType, loan.ApprovedAmount, loan.DurationMonths, loan.MonthlyInstallment, loan.RemainingBalance, loan.ApprovedBy, loan.Source, nullIfEmpty(loan.COACode), loan.StartDate, loan.AdminFeePolicy, loan.MonthlyAdminFee, loan.TotalAdminFee, loan.TotalObligation, loan.NextDueDate, loan.FinalDueDate); err != nil {
		return LoanApprovalResult{}, err
	}
	for _, installment := range calc.Installments {
		if _, err := tx.Exec(`INSERT INTO loan_installments (id,loan_id,installment_no,due_date,scheduled_amount,paid_amount) VALUES ($1,$2,$3,$4,$5,0)`, newID(), loan.ID, installment.Number, installment.DueDate, installment.ScheduledAmount); err != nil {
			return LoanApprovalResult{}, err
		}
	}
	if err := s.createFinancialJournalTx(tx, accountingJournalInput{ReferenceNo: requestID, TransactionID: loan.ID, TransactionType: "loan", TransactionDate: loan.StartDate, Source: loan.Source, Amount: loan.ApprovedAmount, Direction: accountingDirectionCredit, COACode: loan.COACode, Description: "Pencairan pinjaman", RecordedBy: admin.ID}); err != nil {
		return LoanApprovalResult{}, err
	}
	if err := resolveRequestNotifications(tx, "loan", requestID); err != nil {
		return LoanApprovalResult{}, err
	}
	if err := createMemberOutcomeNotification(tx, "loan", requestID, request.MemberID, "approved", "/member/loan-requests"); err != nil {
		return LoanApprovalResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoanApprovalResult{}, err
	}
	createdLoan, err := s.loanByID(loan.ID)
	if err != nil {
		return LoanApprovalResult{}, err
	}
	updated, err := s.loanRequestByID(requestID)
	if err != nil {
		return LoanApprovalResult{}, err
	}
	return LoanApprovalResult{Request: updated, Loan: &createdLoan}, nil
}

func (s *Server) rejectLoanRequestBySuperAdmin(requestID string, admin User, req rejectLoanInput) (LoanRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return LoanRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var stage, memberID, status string
	if err := tx.QueryRow(`SELECT COALESCE(current_approval_stage,''),member_id,status FROM loan_requests WHERE id=$1`+rowLockClause(s.db), requestID).Scan(&stage, &memberID, &status); errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, errLoanRequestNotFound
	} else if err != nil {
		return LoanRequest{}, err
	}
	if status != "pending" {
		return LoanRequest{}, errLoanRequestNotPending
	}
	if err := insertSuperAdminOverride(tx, "loan", requestID, admin, stage, "rejected", req.RejectionReason); err != nil {
		return LoanRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE loan_requests SET status='rejected',current_approval_stage=NULL,reviewed_by=$1,reviewed_at=CURRENT_TIMESTAMP,rejection_reason=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending'`, admin.ID, strings.TrimSpace(req.RejectionReason), requestID); err != nil {
		return LoanRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "loan", requestID); err != nil {
		return LoanRequest{}, err
	}
	if err := createMemberOutcomeNotification(tx, "loan", requestID, memberID, "rejected", "/member/loan-requests"); err != nil {
		return LoanRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoanRequest{}, err
	}
	return s.loanRequestByID(requestID)
}

func (s *Server) approveWithdrawalRequestBySuperAdmin(requestID string, admin User, req approveWithdrawalInput) (WithdrawalRequest, error) {
	source := normalizeTransactionSource(req.Source)
	if !validTransactionSource(source) {
		return WithdrawalRequest{}, errInvalidTransactionSource
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var request WithdrawalRequest
	if err := tx.QueryRow(`SELECT id,member_id,amount,note,status,COALESCE(current_approval_stage,''),COALESCE(CAST(reviewed_at AS TEXT),''),rejection_reason,COALESCE(saving_record_id,''),created_at,updated_at FROM withdrawal_requests WHERE id=$1`+rowLockClause(s.db), requestID).Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.CurrentApprovalStage, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	} else if err != nil {
		return WithdrawalRequest{}, err
	}
	if request.Status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if err := insertSuperAdminOverride(tx, "withdrawal", requestID, admin, request.CurrentApprovalStage, "approved", req.Note); err != nil {
		return WithdrawalRequest{}, err
	}
	var memberStatus string
	if err := tx.QueryRow(`SELECT status FROM members WHERE id=$1`+rowLockClause(s.db), request.MemberID).Scan(&memberStatus); err != nil {
		return WithdrawalRequest{}, err
	}
	if memberStatus != "active" {
		return WithdrawalRequest{}, errInactiveWithdrawalMember
	}
	summary, err := savingSummary(tx, request.MemberID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if request.Amount > maxWithdrawalAmountForSukarelaBalance(summary.SukarelaBalance) {
		return WithdrawalRequest{}, errInsufficientSukarelaBalance
	}
	var reservationAmount int64
	var reservationStatus string
	if err := tx.QueryRow(`SELECT amount,status FROM withdrawal_reservations WHERE request_id=$1`+rowLockClause(s.db), requestID).Scan(&reservationAmount, &reservationStatus); err != nil {
		return WithdrawalRequest{}, err
	}
	if reservationStatus != "active" || reservationAmount != request.Amount {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	recordID := newID()
	note := strings.TrimSpace(request.Note)
	if strings.TrimSpace(req.Note) != "" {
		note = strings.TrimSpace(req.Note)
	}
	recordDate := time.Now().In(jakartaLocation).Format("2006-01-02")
	if _, err := tx.Exec(`INSERT INTO saving_records (id,member_id,type,category,source,coa_code,amount,record_date,reference_no,note,recorded_by) VALUES ($1,$2,'withdrawal','sukarela',$3,$4,$5,$6,'',$7,$8)`, recordID, request.MemberID, source, nullIfEmpty(strings.TrimSpace(req.COACode)), request.Amount, recordDate, note, admin.ID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := s.createFinancialJournalTx(tx, accountingJournalInput{TransactionID: recordID, TransactionType: "withdrawal", TransactionDate: recordDate, Source: source, Amount: request.Amount, Direction: accountingDirectionCredit, COACode: strings.TrimSpace(req.COACode), Description: "Penarikan sukarela", RecordedBy: admin.ID}); err != nil {
		return WithdrawalRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE withdrawal_requests SET status='approved',current_approval_stage=NULL,reviewed_by=$1,reviewed_at=CURRENT_TIMESTAMP,saving_record_id=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending'`, admin.ID, recordID, requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE withdrawal_reservations SET status='consumed',updated_at=CURRENT_TIMESTAMP WHERE request_id=$1 AND status='active'`, requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "withdrawal", requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := createMemberOutcomeNotification(tx, "withdrawal", requestID, request.MemberID, "approved", "/member/withdrawal-requests"); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRequest{}, err
	}
	return s.withdrawalRequestByID(request.ID)
}

func (s *Server) rejectWithdrawalRequestBySuperAdmin(requestID string, admin User, req rejectWithdrawalInput) (WithdrawalRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var stage, memberID, status string
	if err := tx.QueryRow(`SELECT COALESCE(current_approval_stage,''),member_id,status FROM withdrawal_requests WHERE id=$1`+rowLockClause(s.db), requestID).Scan(&stage, &memberID, &status); errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	} else if err != nil {
		return WithdrawalRequest{}, err
	}
	if status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if err := insertSuperAdminOverride(tx, "withdrawal", requestID, admin, stage, "rejected", req.RejectionReason); err != nil {
		return WithdrawalRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE withdrawal_requests SET status='rejected',current_approval_stage=NULL,reviewed_by=$1,reviewed_at=CURRENT_TIMESTAMP,rejection_reason=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending'`, admin.ID, strings.TrimSpace(req.RejectionReason), requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if _, err := tx.Exec(`UPDATE withdrawal_reservations SET status='released',updated_at=CURRENT_TIMESTAMP WHERE request_id=$1 AND status='active'`, requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "withdrawal", requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := createMemberOutcomeNotification(tx, "withdrawal", requestID, memberID, "rejected", "/member/withdrawal-requests"); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRequest{}, err
	}
	return s.withdrawalRequestByID(requestID)
}
