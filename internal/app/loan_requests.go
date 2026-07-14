package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type LoanRequest struct {
	ID                      string            `json:"id"`
	MemberID                string            `json:"member_id"`
	RequestedAmount         int               `json:"requested_amount"`
	DurationMonths          int               `json:"duration_months"`
	Purpose                 string            `json:"purpose"`
	Status                  string            `json:"status"`
	CurrentApprovalStage    string            `json:"current_approval_stage,omitempty"`
	ProposedApprovedAmount  int               `json:"proposed_approved_amount,omitempty"`
	ProposedDurationMonths  int               `json:"proposed_duration_months,omitempty"`
	ProposedStartDate       string            `json:"proposed_start_date,omitempty"`
	ProposedInterestRateBPS int               `json:"proposed_interest_rate_bps,omitempty"`
	RejectionReason         string            `json:"rejection_reason,omitempty"`
	LatestDecision          *ApprovalDecision `json:"latest_decision,omitempty"`
	CreatedAt               string            `json:"created_at,omitempty"`
	UpdatedAt               string            `json:"updated_at,omitempty"`
}

type AdminLoanRequest struct {
	LoanRequest
	MemberNo        string             `json:"member_no"`
	FullName        string             `json:"full_name"`
	ApprovalHistory []ApprovalDecision `json:"approval_history"`
	CanDecide       bool               `json:"can_decide"`
}

type loanRequestInput struct {
	RequestedAmount int    `json:"requested_amount" form:"requested_amount"`
	DurationMonths  int    `json:"duration_months" form:"duration_months"`
	Purpose         string `json:"purpose" form:"purpose"`
}

type rejectLoanInput struct {
	RejectionReason string `json:"rejection_reason" form:"rejection_reason"`
}

func (s *Server) submitLoanRequest(c *gin.Context) {
	lang := languageFromRequest(c)
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	var req loanRequestInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_loan_request"))
		return
	}

	loanRequest, err := s.insertLoanRequest(member, req)
	if errors.Is(err, errInvalidLoanRequest) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_loan_request_fields"))
		return
	}
	if errors.Is(err, errInactiveLoanMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_inactive_loan_member"))
		return
	}
	if errors.Is(err, errPendingLoanRequestExists) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_pending_loan_request"))
		return
	}
	if errors.Is(err, errOutstandingLoanBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_outstanding_loan_request"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondCreatedOrHXRedirect(c, "/member/loan-requests", loanRequest)
}

func (s *Server) memberLoanRequests(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	requests, err := s.loanRequestsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"loan_requests": requests})
}

func (s *Server) adminLoanRequests(c *gin.Context) {
	user, _ := currentUser(c)
	requests, err := s.loanRequestsForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	for index := range requests {
		requests[index].CanDecide = requests[index].Status == "pending" && requests[index].CurrentApprovalStage == user.Role
	}
	c.JSON(http.StatusOK, gin.H{"loan_requests": requests})
}

func (s *Server) rejectLoanRequest(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req rejectLoanInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid loan rejection")
		return
	}

	loanRequest, err := s.rejectLoanRequestByID(c.Param("id"), user, req)
	if errors.Is(err, errInvalidLoanRejection) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Rejection reason is required")
		return
	}
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be rejected")
		return
	}
	if errors.Is(err, errWrongApprovalStage) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(languageFromRequest(c), "error_wrong_approval_stage"))
		return
	}
	if errors.Is(err, errLoanRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Loan request not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondOKOrHXRedirect(c, "/admin/loan-requests", loanRequest)
}

var (
	errInvalidLoanRequest       = errors.New("invalid loan request")
	errInactiveLoanMember       = errors.New("inactive loan member")
	errPendingLoanRequestExists = errors.New("pending loan request exists")
	errInvalidLoanRejection     = errors.New("invalid loan rejection")
	errOutstandingLoanBalance   = errors.New("outstanding loan balance")
)

func (s *Server) insertLoanRequest(member Member, req loanRequestInput) (LoanRequest, error) {
	if req.RequestedAmount <= 0 || req.DurationMonths <= 0 || req.DurationMonths > maxLoanDurationMonths {
		return LoanRequest{}, errInvalidLoanRequest
	}
	if member.Status != "active" {
		return LoanRequest{}, errInactiveLoanMember
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return LoanRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// PostgreSQL serializes all outstanding/pending checks for this member.
	// SQLite serializes writers and the in-process mutex avoids upgrade races.
	var lockedMemberID string
	if err := tx.QueryRow(`SELECT id FROM members WHERE id = $1`+rowLockClause(s.db), member.ID).Scan(&lockedMemberID); err != nil {
		return LoanRequest{}, err
	}
	var outstanding int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(remaining_balance),0) FROM loans WHERE member_id=$1 AND status <> 'cancelled' AND remaining_balance > 0`, member.ID).Scan(&outstanding); err != nil {
		return LoanRequest{}, err
	}
	if outstanding > 0 {
		return LoanRequest{}, errOutstandingLoanBalance
	}

	var pendingID string
	err = tx.QueryRow(`SELECT id FROM loan_requests WHERE member_id = $1 AND status = 'pending' LIMIT 1`, member.ID).Scan(&pendingID)
	if err == nil {
		return LoanRequest{}, errPendingLoanRequestExists
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, err
	}

	loanRequest := LoanRequest{
		ID:                   newID(),
		MemberID:             member.ID,
		RequestedAmount:      req.RequestedAmount,
		DurationMonths:       req.DurationMonths,
		Purpose:              strings.TrimSpace(req.Purpose),
		Status:               "pending",
		CurrentApprovalStage: approvalStageManager,
	}
	_, err = tx.Exec(
		`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, purpose, status, current_approval_stage) VALUES ($1, $2, $3, $4, $5, 'pending', 'manager')`,
		loanRequest.ID,
		loanRequest.MemberID,
		loanRequest.RequestedAmount,
		loanRequest.DurationMonths,
		loanRequest.Purpose,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return LoanRequest{}, errPendingLoanRequestExists
		}
		return LoanRequest{}, err
	}
	if err := createStageNotification(tx, "loan", loanRequest.ID, approvalStageManager, "/admin/loan-requests"); err != nil {
		return LoanRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			return LoanRequest{}, errPendingLoanRequestExists
		}
		return LoanRequest{}, err
	}
	return loanRequest, nil
}

func (s *Server) loanRequestsByMember(memberID string) ([]LoanRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, requested_amount, duration_months, purpose, status, COALESCE(current_approval_stage,''), COALESCE(proposed_approved_amount,0), COALESCE(proposed_duration_months,0), proposed_start_date, COALESCE(proposed_interest_rate_bps,0), rejection_reason, created_at, updated_at
		FROM loan_requests
		WHERE member_id = $1
		ORDER BY created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	var requests []LoanRequest
	for rows.Next() {
		var request LoanRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.CurrentApprovalStage, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedInterestRateBPS, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range requests {
		requests[index].LatestDecision, err = latestApprovalDecision(s.db, "loan_request_approvals", requests[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *Server) loanRequestsForAdmin(status string) ([]AdminLoanRequest, error) {
	status = strings.TrimSpace(status)
	query := `SELECT lr.id, lr.member_id, m.member_no, m.full_name, lr.requested_amount, lr.duration_months, lr.purpose, lr.status, COALESCE(lr.current_approval_stage,''), COALESCE(lr.proposed_approved_amount,0), COALESCE(lr.proposed_duration_months,0), lr.proposed_start_date, COALESCE(lr.proposed_interest_rate_bps,0), lr.rejection_reason, lr.created_at, lr.updated_at
		FROM loan_requests lr
		INNER JOIN members m ON m.id = lr.member_id`
	args := []any{}
	if status != "" {
		query += ` WHERE lr.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY lr.created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var requests []AdminLoanRequest
	for rows.Next() {
		var request AdminLoanRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.MemberNo, &request.FullName, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.CurrentApprovalStage, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedInterestRateBPS, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range requests {
		requests[index].ApprovalHistory, err = approvalHistory(s.db, "loan_request_approvals", requests[index].ID, true)
		if err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *Server) rejectLoanRequestByID(requestID string, officer User, req rejectLoanInput) (LoanRequest, error) {
	reason := strings.TrimSpace(req.RejectionReason)
	if reason == "" {
		return LoanRequest{}, errInvalidLoanRejection
	}
	tx, err := s.db.Begin()
	if err != nil {
		return LoanRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var status, stage, memberID string
	err = tx.QueryRow(`SELECT status,COALESCE(current_approval_stage,''),member_id FROM loan_requests WHERE id = $1`+rowLockClause(s.db), requestID).Scan(&status, &stage, &memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, errLoanRequestNotFound
	}
	if err != nil {
		return LoanRequest{}, err
	}
	if status != "pending" {
		return LoanRequest{}, errLoanRequestNotPending
	}
	if stage != officer.Role {
		return LoanRequest{}, errWrongApprovalStage
	}
	if err := insertApprovalDecision(tx, "loan_request_approvals", requestID, officer, "rejected", "", reason); err != nil {
		return LoanRequest{}, err
	}
	result, err := tx.Exec(
		`UPDATE loan_requests
		SET status = 'rejected', current_approval_stage=NULL, reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, rejection_reason = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending' AND current_approval_stage=$4`,
		officer.ID,
		reason,
		requestID,
		officer.Role,
	)
	if err != nil {
		return LoanRequest{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return LoanRequest{}, errLoanRequestNotPending
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

func (s *Server) cancelLoanRequest(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	request, err := s.cancelLoanRequestByID(c.Param("id"), member.ID)
	if errors.Is(err, errLoanRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_loan_request_not_found"))
		return
	}
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_loan_request_not_pending"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondOKOrHXRedirect(c, "/member/loan-requests", request)
}

func (s *Server) cancelLoanRequestByID(requestID, memberID string) (LoanRequest, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return LoanRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	err = tx.QueryRow(`SELECT status FROM loan_requests WHERE id=$1 AND member_id=$2`+rowLockClause(s.db), requestID, memberID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, errLoanRequestNotFound
	}
	if err != nil {
		return LoanRequest{}, err
	}
	if status != "pending" {
		return LoanRequest{}, errLoanRequestNotPending
	}
	result, err := tx.Exec(`UPDATE loan_requests SET status='cancelled',current_approval_stage=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND member_id=$2 AND status='pending'`, requestID, memberID)
	if err != nil {
		return LoanRequest{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return LoanRequest{}, errLoanRequestNotPending
	}
	if err := resolveRequestNotifications(tx, "loan", requestID); err != nil {
		return LoanRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoanRequest{}, err
	}
	return s.loanRequestByID(requestID)
}

func (s *Server) loanRequestByID(id string) (LoanRequest, error) {
	var request LoanRequest
	err := s.db.QueryRow(
		`SELECT id, member_id, requested_amount, duration_months, purpose, status, COALESCE(current_approval_stage,''), COALESCE(proposed_approved_amount,0), COALESCE(proposed_duration_months,0), proposed_start_date, COALESCE(proposed_interest_rate_bps,0), rejection_reason, created_at, updated_at
		FROM loan_requests
		WHERE id = $1`,
		id,
	).Scan(&request.ID, &request.MemberID, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.CurrentApprovalStage, &request.ProposedApprovedAmount, &request.ProposedDurationMonths, &request.ProposedStartDate, &request.ProposedInterestRateBPS, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt)
	if err == nil {
		request.LatestDecision, err = latestApprovalDecision(s.db, "loan_request_approvals", request.ID)
	}
	return request, err
}
