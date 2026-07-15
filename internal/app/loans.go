package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Loan struct {
	ID                 string `json:"id"`
	LoanRequestID      string `json:"loan_request_id"`
	MemberID           string `json:"member_id"`
	LoanType           string `json:"loan_type"`
	LegacyTerms        bool   `json:"legacy_terms"`
	ApprovedAmount     int64  `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int64  `json:"monthly_installment"`
	RemainingBalance   int64  `json:"remaining_balance"`
	StartDate          string `json:"start_date"`
	AdminFeePolicy     string `json:"admin_fee_policy"`
	MonthlyAdminFee    *int64 `json:"monthly_admin_fee,omitempty"`
	TotalAdminFee      int64  `json:"total_admin_fee"`
	TotalObligation    int64  `json:"total_obligation"`
	NextDueDate        string `json:"next_due_date"`
	FinalDueDate       string `json:"final_due_date"`
	IsOverdue          bool   `json:"is_overdue"`
	Status             string `json:"status"`
	ApprovedBy         string `json:"approved_by,omitempty"`
	ApprovedAt         string `json:"approved_at,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type AdminLoan struct {
	ID                 string `json:"id"`
	LoanRequestID      string `json:"loan_request_id"`
	MemberID           string `json:"member_id"`
	LoanType           string `json:"loan_type"`
	LegacyTerms        bool   `json:"legacy_terms"`
	MemberNo           string `json:"member_no"`
	FullName           string `json:"full_name"`
	MemberType         string `json:"member_type"`
	MemberTypeLabel    string `json:"member_type_label"`
	ApprovedAmount     int64  `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int64  `json:"monthly_installment"`
	RemainingBalance   int64  `json:"remaining_balance"`
	StartDate          string `json:"start_date"`
	AdminFeePolicy     string `json:"admin_fee_policy"`
	MonthlyAdminFee    *int64 `json:"monthly_admin_fee,omitempty"`
	TotalAdminFee      int64  `json:"total_admin_fee"`
	TotalObligation    int64  `json:"total_obligation"`
	NextDueDate        string `json:"next_due_date"`
	FinalDueDate       string `json:"final_due_date"`
	IsOverdue          bool   `json:"is_overdue"`
	Status             string `json:"status"`
	ApprovedAt         string `json:"approved_at,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type approveLoanInput struct {
	ApprovedAmount int64  `json:"approved_amount" form:"approved_amount"`
	DurationMonths int    `json:"duration_months" form:"duration_months"`
	StartDate      string `json:"start_date" form:"start_date"`
	Note           string `json:"note" form:"note"`
}

type LoanApprovalResult struct {
	Request LoanRequest `json:"loan_request"`
	Loan    *Loan       `json:"loan,omitempty"`
}

type correctLoanStartDateInput struct {
	StartDate string `json:"start_date" form:"start_date"`
}

type LoanInstallment struct {
	ID              string `json:"id"`
	LoanID          string `json:"loan_id"`
	InstallmentNo   int    `json:"installment_no"`
	DueDate         string `json:"due_date"`
	ScheduledAmount int64  `json:"scheduled_amount"`
	PaidAmount      int64  `json:"paid_amount"`
}

var (
	errInvalidLoanApproval           = errors.New("invalid loan approval")
	errLoanRequestNotPending         = errors.New("loan request not pending")
	errLoanRequestNotFound           = errors.New("loan request not found")
	errInvalidLoanApprovalCalculated = errors.New("invalid calculated installment")
	errInvalidLoanStartDate          = errors.New("invalid loan start date")
	errLoanStartDateLocked           = errors.New("loan start date locked")
	errLoanStartDateStatus           = errors.New("loan start date status ineligible")
)

func loanOverdue(nextDueDate string, remainingBalance int64) bool {
	if remainingBalance <= 0 || nextDueDate == "" {
		return false
	}
	due, err := parseLoanDate(nextDueDate)
	if err != nil {
		return false
	}
	now := time.Now().In(jakartaLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jakartaLocation)
	return due.Before(today)
}

func (s *Server) approveLoanRequest(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}

	var req approveLoanInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_approval"))
		return
	}
	result, err := s.approveLoanRequestByID(c.Param("id"), user, req)
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
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_request_not_pending"))
		return
	}
	if errors.Is(err, errWrongApprovalStage) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_wrong_approval_stage"))
		return
	}
	if errors.Is(err, errLoanRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_loan_request_not_found"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}

	redirect := "/admin/loan-requests"
	if result.Loan != nil {
		redirect = "/admin/loans"
	}
	respondOKOrHXRedirect(c, redirect, result)
}

func (s *Server) adminLoanDetail(c *gin.Context) {
	loan, err := s.loanByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, 404, "NOT_FOUND", "Loan not found")
		return
	}
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	rows, err := s.db.Query(`SELECT id, loan_id, installment_no, due_date, scheduled_amount, paid_amount FROM loan_installments WHERE loan_id = $1 ORDER BY installment_no`, loan.ID)
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	defer rows.Close()
	items := []LoanInstallment{}
	for rows.Next() {
		var i LoanInstallment
		if rows.Scan(&i.ID, &i.LoanID, &i.InstallmentNo, &i.DueDate, &i.ScheduledAmount, &i.PaidAmount) != nil {
			respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
			return
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if err := rows.Close(); err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(200, gin.H{"loan": loan, "installments": items})
}

func (s *Server) correctLoanStartDate(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, 401, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	var req correctLoanStartDateInput
	if c.ShouldBind(&req) != nil {
		respondError(c, 400, "VALIDATION_ERROR", translate(lang, "error_invalid_loan_start_date"))
		return
	}
	loan, err := s.correctLoanStartDateByID(c.Param("id"), user.ID, req.StartDate)
	if errors.Is(err, errInvalidLoanStartDate) {
		respondError(c, 400, "VALIDATION_ERROR", translate(lang, "error_loan_start_date_range"))
		return
	}
	if errors.Is(err, errLoanStartDateLocked) {
		respondError(c, 400, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_start_date_locked"))
		return
	}
	if errors.Is(err, errLoanStartDateStatus) {
		respondError(c, 400, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_start_date_status"))
		return
	}
	if errors.Is(err, errLoanNotFound) {
		respondError(c, 404, "NOT_FOUND", "Loan not found")
		return
	}
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondOKOrHXRedirect(c, "/admin/loans/"+loan.ID, loan)
}

func (s *Server) memberActiveLoan(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	loan, err := s.activeLoanByMember(member.ID)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Active loan not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, loan)
}

func (s *Server) memberOutstandingLoans(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	loans, err := s.outstandingLoansByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	var total int64
	for _, loan := range loans {
		total += loan.RemainingBalance
	}
	c.JSON(http.StatusOK, gin.H{"loans": loans, "total_outstanding": total})
}

func (s *Server) adminLoans(c *gin.Context) {
	loans, err := s.loansForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"loans": loans})
}

func (s *Server) approveLoanRequestByID(requestID string, officer User, req approveLoanInput) (LoanApprovalResult, error) {
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
	err = tx.QueryRow(
		`SELECT member_id,loan_type,requested_amount,duration_months,status,COALESCE(current_approval_stage,''),created_at,COALESCE(proposed_approved_amount,0),COALESCE(proposed_duration_months,0),proposed_start_date,COALESCE(proposed_admin_fee_policy,''),proposed_monthly_admin_fee,COALESCE(proposed_total_admin_fee,0),COALESCE(proposed_total_obligation,0) FROM loan_requests WHERE id = $1`+rowLockClause(s.db),
		requestID,
	).Scan(&request.MemberID, &request.LoanType, &request.RequestedAmount, &request.RequestedDurationMonths, &request.Status, &request.Stage, &request.CreatedAt, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedAdminFeePolicy, &request.ProposedMonthlyAdminFee, &request.ProposedTotalAdminFee, &request.ProposedTotalObligation)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanApprovalResult{}, errLoanRequestNotFound
	}
	if err != nil {
		return LoanApprovalResult{}, err
	}
	if request.Status != "pending" {
		return LoanApprovalResult{}, errLoanRequestNotPending
	}
	if request.Stage != officer.Role {
		return LoanApprovalResult{}, errWrongApprovalStage
	}

	if officer.Role == approvalStageManager {
		startDate := strings.TrimSpace(req.StartDate)
		if req.ApprovedAmount <= 0 || req.DurationMonths <= 0 || startDate == "" {
			return LoanApprovalResult{}, errInvalidLoanApproval
		}
		if req.ApprovedAmount > maxLoanPrincipalAmount {
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
		var adminFeePolicy string
		switch request.LoanType {
		case "regular":
			calc, err = calculateRegularLoanSchedule(req.ApprovedAmount, req.DurationMonths, startDate)
			adminFeePolicy = regularLoanAdminFeePolicy
		case "secondary_goods":
			calc, err = calculateSecondaryGoodsLoanSchedule(req.ApprovedAmount, req.DurationMonths, startDate)
			adminFeePolicy = secondaryGoodsAdminFeePolicy
		case "goods_purchase_paylater":
			calc, err = calculatePaylaterLoanSchedule(req.ApprovedAmount, req.DurationMonths, startDate)
			adminFeePolicy = paylaterAdminFeePolicy
		default:
			return LoanApprovalResult{}, errInvalidLoanApproval
		}
		if err != nil {
			return LoanApprovalResult{}, errInvalidLoanApprovalCalculated
		}
		request.ProposedApprovedAmount = req.ApprovedAmount
		request.ProposedDurationMonths = req.DurationMonths
		request.ProposedStartDate = startDate
		request.ProposedAdminFeePolicy = adminFeePolicy
		request.ProposedMonthlyAdminFee = &calc.MonthlyAdminFee
		if adminFeePolicy == secondaryGoodsAdminFeePolicy || adminFeePolicy == paylaterAdminFeePolicy {
			request.ProposedMonthlyAdminFee = nil
		}
		request.ProposedTotalAdminFee = calc.TotalAdminFee
		request.ProposedTotalObligation = calc.TotalObligation
		if _, err := tx.Exec(`UPDATE loan_requests SET proposed_approved_amount=$1,proposed_duration_months=$2,proposed_start_date=$3,proposed_admin_fee_policy=$4,proposed_monthly_admin_fee=$5,proposed_total_admin_fee=$6,proposed_total_obligation=$7,updated_at=CURRENT_TIMESTAMP WHERE id=$8 AND status='pending' AND current_approval_stage='manager'`, req.ApprovedAmount, req.DurationMonths, startDate, adminFeePolicy, request.ProposedMonthlyAdminFee, calc.TotalAdminFee, calc.TotalObligation, requestID); err != nil {
			return LoanApprovalResult{}, err
		}
	}
	if request.ProposedApprovedAmount <= 0 || request.ProposedDurationMonths <= 0 || request.ProposedStartDate == "" || request.ProposedAdminFeePolicy == "" || request.ProposedTotalObligation <= 0 {
		return LoanApprovalResult{}, errInvalidLoanApproval
	}
	if err := insertApprovalDecision(tx, "loan_request_approvals", requestID, officer, "approved", req.Note, ""); err != nil {
		return LoanApprovalResult{}, err
	}
	if err := resolveRequestNotifications(tx, "loan", requestID); err != nil {
		return LoanApprovalResult{}, err
	}
	if officer.Role == approvalStageManager && (request.ProposedApprovedAmount != request.RequestedAmount || request.ProposedDurationMonths != request.RequestedDurationMonths) {
		if err := createMemberLoanTermsChangedNotification(tx, requestID, request.MemberID); err != nil {
			return LoanApprovalResult{}, err
		}
	}

	nextStage := nextApprovalStage(officer.Role)
	if nextStage != "" {
		result, err := tx.Exec(`UPDATE loan_requests SET current_approval_stage=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND status='pending' AND current_approval_stage=$3`, nextStage, requestID, officer.Role)
		if err != nil {
			return LoanApprovalResult{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return LoanApprovalResult{}, errLoanRequestNotPending
		}
		if err := createStageNotification(tx, "loan", requestID, nextStage, "/admin/loan-requests"); err != nil {
			return LoanApprovalResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return LoanApprovalResult{}, err
		}
		updated, err := s.loanRequestByID(requestID)
		return LoanApprovalResult{Request: updated}, err
	}

	var lockedMemberID string
	if err = tx.QueryRow(`SELECT id FROM members WHERE id = $1`+rowLockClause(s.db), request.MemberID).Scan(&lockedMemberID); err != nil {
		return LoanApprovalResult{}, err
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
	calc, err := buildLoanScheduleFromObligation(request.ProposedTotalObligation, request.ProposedDurationMonths, request.ProposedStartDate, maxDuration)
	if err != nil {
		return LoanApprovalResult{}, errInvalidLoanApprovalCalculated
	}
	monthlyInstallment := calc.Installments[0].ScheduledAmount

	loan := Loan{
		ID:                 newID(),
		LoanRequestID:      requestID,
		MemberID:           request.MemberID,
		LoanType:           request.LoanType,
		ApprovedAmount:     request.ProposedApprovedAmount,
		DurationMonths:     request.ProposedDurationMonths,
		MonthlyInstallment: monthlyInstallment,
		RemainingBalance:   request.ProposedTotalObligation,
		StartDate:          request.ProposedStartDate,
		AdminFeePolicy:     request.ProposedAdminFeePolicy,
		MonthlyAdminFee:    request.ProposedMonthlyAdminFee,
		TotalAdminFee:      request.ProposedTotalAdminFee,
		TotalObligation:    request.ProposedTotalObligation,
		NextDueDate:        calc.Installments[0].DueDate,
		FinalDueDate:       calc.Installments[len(calc.Installments)-1].DueDate,
		Status:             "active",
		ApprovedBy:         officer.ID,
	}

	result, err := tx.Exec(
		`UPDATE loan_requests
		SET status = 'approved', current_approval_stage=NULL, reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND status = 'pending' AND current_approval_stage='ketua_utama'`,
		officer.ID,
		requestID,
	)
	if err != nil {
		return LoanApprovalResult{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return LoanApprovalResult{}, errLoanRequestNotPending
	}
	if _, err := tx.Exec(
		`INSERT INTO loans (id, loan_request_id, member_id, loan_type, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, start_date, admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, next_due_date, final_due_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9, $10, $11, $12, $13, $14, $15, $16)`,
		loan.ID,
		loan.LoanRequestID,
		loan.MemberID,
		loan.LoanType,
		loan.ApprovedAmount,
		loan.DurationMonths,
		loan.MonthlyInstallment,
		loan.RemainingBalance,
		loan.ApprovedBy,
		loan.StartDate, loan.AdminFeePolicy, loan.MonthlyAdminFee, loan.TotalAdminFee, loan.TotalObligation, loan.NextDueDate, loan.FinalDueDate,
	); err != nil {
		return LoanApprovalResult{}, err
	}
	for _, installment := range calc.Installments {
		if _, err := tx.Exec(`INSERT INTO loan_installments (id,loan_id,installment_no,due_date,scheduled_amount,paid_amount) VALUES ($1,$2,$3,$4,$5,0)`, newID(), loan.ID, installment.Number, installment.DueDate, installment.ScheduledAmount); err != nil {
			return LoanApprovalResult{}, err
		}
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

func (s *Server) loanByID(id string) (Loan, error) {
	var loan Loan
	err := s.db.QueryRow(
		`SELECT id, loan_request_id, member_id, loan_type, legacy_terms, approved_amount, duration_months, monthly_installment, remaining_balance, start_date, admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, next_due_date, final_due_date, status, approved_by, approved_at, created_at, updated_at
		FROM loans
		WHERE id = $1`,
		id,
	).Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.LoanType, &loan.LegacyTerms, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.StartDate, &loan.AdminFeePolicy, &loan.MonthlyAdminFee, &loan.TotalAdminFee, &loan.TotalObligation, &loan.NextDueDate, &loan.FinalDueDate, &loan.Status, &loan.ApprovedBy, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt)
	loan.IsOverdue = loanOverdue(loan.NextDueDate, loan.RemainingBalance)
	return loan, err
}

func (s *Server) activeLoanByMember(memberID string) (Loan, error) {
	var loan Loan
	err := s.db.QueryRow(
		`SELECT id, loan_request_id, member_id, loan_type, legacy_terms, approved_amount, duration_months, monthly_installment, remaining_balance, start_date, admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, next_due_date, final_due_date, status, approved_by, approved_at, created_at, updated_at
		FROM loans
		WHERE member_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1`,
		memberID,
	).Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.LoanType, &loan.LegacyTerms, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.StartDate, &loan.AdminFeePolicy, &loan.MonthlyAdminFee, &loan.TotalAdminFee, &loan.TotalObligation, &loan.NextDueDate, &loan.FinalDueDate, &loan.Status, &loan.ApprovedBy, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt)
	loan.IsOverdue = loanOverdue(loan.NextDueDate, loan.RemainingBalance)
	return loan, err
}

func (s *Server) loansForAdmin(status string) ([]AdminLoan, error) {
	status = strings.TrimSpace(status)
	query := `SELECT l.id, l.loan_request_id, l.member_id, l.loan_type, l.legacy_terms, m.member_no, m.full_name, m.member_type, l.approved_amount, l.duration_months, l.monthly_installment, l.remaining_balance, l.start_date,l.admin_fee_policy,l.monthly_admin_fee,l.total_admin_fee,l.total_obligation,l.next_due_date,l.final_due_date,l.status, l.approved_at, l.created_at, l.updated_at
		FROM loans l
		INNER JOIN members m ON m.id = l.member_id`
	args := []any{}
	if status != "" {
		query += ` WHERE l.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY l.created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []AdminLoan
	for rows.Next() {
		var loan AdminLoan
		if err := rows.Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.LoanType, &loan.LegacyTerms, &loan.MemberNo, &loan.FullName, &loan.MemberType, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.StartDate, &loan.AdminFeePolicy, &loan.MonthlyAdminFee, &loan.TotalAdminFee, &loan.TotalObligation, &loan.NextDueDate, &loan.FinalDueDate, &loan.Status, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt); err != nil {
			return nil, err
		}
		loan.MemberTypeLabel = memberTypeLabel(loan.MemberType)
		loan.IsOverdue = loanOverdue(loan.NextDueDate, loan.RemainingBalance)
		loans = append(loans, loan)
	}
	return loans, rows.Err()
}

func (s *Server) outstandingLoansByMember(memberID string) ([]Loan, error) {
	rows, err := s.db.Query(`SELECT id,loan_request_id,member_id,loan_type,legacy_terms,approved_amount,duration_months,monthly_installment,remaining_balance,start_date,admin_fee_policy,monthly_admin_fee,total_admin_fee,total_obligation,next_due_date,final_due_date,status,approved_by,approved_at,created_at,updated_at FROM loans WHERE member_id=$1 AND status <> 'cancelled' AND remaining_balance>0 ORDER BY next_due_date,created_at`, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Loan{}
	for rows.Next() {
		var l Loan
		if err = rows.Scan(&l.ID, &l.LoanRequestID, &l.MemberID, &l.LoanType, &l.LegacyTerms, &l.ApprovedAmount, &l.DurationMonths, &l.MonthlyInstallment, &l.RemainingBalance, &l.StartDate, &l.AdminFeePolicy, &l.MonthlyAdminFee, &l.TotalAdminFee, &l.TotalObligation, &l.NextDueDate, &l.FinalDueDate, &l.Status, &l.ApprovedBy, &l.ApprovedAt, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		l.IsOverdue = loanOverdue(l.NextDueDate, l.RemainingBalance)
		items = append(items, l)
	}
	return items, rows.Err()
}

func (s *Server) totalOutstandingByMember(memberID string) (int64, error) {
	var total int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(remaining_balance),0) FROM loans WHERE member_id=$1 AND status <> 'cancelled' AND remaining_balance>0`, memberID).Scan(&total)
	return total, err
}

func parseDatabaseTime(value string) (time.Time, error) {
	// Schema timestamps without an offset are UTC. Offset-bearing values retain
	// their explicit zone before conversion to the Jakarta business date.
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00", time.RFC3339Nano} {
		if t, e := time.ParseInLocation(layout, value, time.UTC); e == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid database time")
}

func (s *Server) correctLoanStartDateByID(loanID, adminID, value string) (Loan, error) {
	value = strings.TrimSpace(value)
	start, err := parseLoanDate(value)
	if err != nil || start.After(time.Now().In(jakartaLocation)) {
		return Loan{}, errInvalidLoanStartDate
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Loan{}, err
	}
	defer tx.Rollback()
	var old, requestCreated, status, loanType, adminFeePolicy string
	var legacyTerms bool
	var principal, totalAdminFee, totalObligation int64
	var duration int
	var monthlyAdminFee *int64
	err = tx.QueryRow(`SELECT l.start_date,l.approved_amount,l.duration_months,lr.created_at,l.status,l.loan_type,l.legacy_terms,l.admin_fee_policy,l.monthly_admin_fee,l.total_admin_fee,l.total_obligation FROM loans l JOIN loan_requests lr ON lr.id=l.loan_request_id WHERE l.id=$1`+rowLockClause(s.db), loanID).Scan(&old, &principal, &duration, &requestCreated, &status, &loanType, &legacyTerms, &adminFeePolicy, &monthlyAdminFee, &totalAdminFee, &totalObligation)
	if errors.Is(err, sql.ErrNoRows) {
		return Loan{}, errLoanNotFound
	}
	if err != nil {
		return Loan{}, err
	}
	if status != "active" && status != "adjustment_due" {
		return Loan{}, errLoanStartDateStatus
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE loan_id=$1`, loanID).Scan(&count); err != nil {
		return Loan{}, err
	}
	if count > 0 {
		return Loan{}, errLoanStartDateLocked
	}
	rt, e := parseDatabaseTime(requestCreated)
	if e != nil || start.Before(time.Date(rt.In(jakartaLocation).Year(), rt.In(jakartaLocation).Month(), rt.In(jakartaLocation).Day(), 0, 0, 0, 0, jakartaLocation)) {
		return Loan{}, errInvalidLoanStartDate
	}
	if e = validateLoanFeeSnapshot(adminFeePolicy, principal, duration, monthlyAdminFee, totalAdminFee, totalObligation); e != nil {
		return Loan{}, errInvalidLoanStartDate
	}
	maxDuration := maxRegularLoanDurationMonths
	if adminFeePolicy == secondaryGoodsAdminFeePolicy {
		maxDuration = maxSecondaryGoodsDuration
	}
	if adminFeePolicy == paylaterAdminFeePolicy {
		maxDuration = 1
	}
	if legacyTerms || adminFeePolicy == "legacy_flat_monthly" {
		maxDuration = int(^uint(0) >> 1)
	}
	calc, e := buildLoanScheduleFromObligation(totalObligation, duration, value, maxDuration)
	if e != nil {
		return Loan{}, errInvalidLoanStartDate
	}
	if _, e = tx.Exec(`DELETE FROM loan_installments WHERE loan_id=$1`, loanID); e != nil {
		return Loan{}, e
	}
	for _, i := range calc.Installments {
		if _, e = tx.Exec(`INSERT INTO loan_installments(id,loan_id,installment_no,due_date,scheduled_amount,paid_amount) VALUES($1,$2,$3,$4,$5,0)`, newID(), loanID, i.Number, i.DueDate, i.ScheduledAmount); e != nil {
			return Loan{}, e
		}
	}
	if _, e = tx.Exec(`UPDATE loans SET start_date=$1,monthly_installment=$2,next_due_date=$3,final_due_date=$4,updated_at=CURRENT_TIMESTAMP WHERE id=$5`, value, calc.Installments[0].ScheduledAmount, calc.Installments[0].DueDate, calc.Installments[len(calc.Installments)-1].DueDate, loanID); e != nil {
		return Loan{}, e
	}
	if _, e = tx.Exec(`INSERT INTO loan_start_date_audits(id,loan_id,old_start_date,new_start_date,changed_by) VALUES($1,$2,$3,$4,$5)`, newID(), loanID, old, value, adminID); e != nil {
		return Loan{}, e
	}
	if e = tx.Commit(); e != nil {
		return Loan{}, e
	}
	return s.loanByID(loanID)
}
