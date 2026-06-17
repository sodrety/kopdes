package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type LoanRequest struct {
	ID              string `json:"id"`
	MemberID        string `json:"member_id"`
	RequestedAmount int    `json:"requested_amount"`
	DurationMonths  int    `json:"duration_months"`
	Purpose         string `json:"purpose"`
	Status          string `json:"status"`
	RejectionReason string `json:"rejection_reason,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type AdminLoanRequest struct {
	ID              string `json:"id"`
	MemberID        string `json:"member_id"`
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	RequestedAmount int    `json:"requested_amount"`
	DurationMonths  int    `json:"duration_months"`
	Purpose         string `json:"purpose"`
	Status          string `json:"status"`
	RejectionReason string `json:"rejection_reason,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
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
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	var req loanRequestInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid loan request")
		return
	}

	loanRequest, err := s.insertLoanRequest(member, req)
	if errors.Is(err, errInvalidLoanRequest) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Requested amount and duration months must be greater than zero")
		return
	}
	if errors.Is(err, errInactiveLoanMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only active members can request loans")
		return
	}
	if errors.Is(err, errPendingLoanRequestExists) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Member already has a pending loan request")
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
	requests, err := s.loanRequestsForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
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

	loanRequest, err := s.rejectLoanRequestByID(c.Param("id"), user.ID, req)
	if errors.Is(err, errInvalidLoanRejection) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Rejection reason is required")
		return
	}
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be rejected")
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
)

func (s *Server) insertLoanRequest(member Member, req loanRequestInput) (LoanRequest, error) {
	if req.RequestedAmount <= 0 || req.DurationMonths <= 0 {
		return LoanRequest{}, errInvalidLoanRequest
	}
	if member.Status != "active" {
		return LoanRequest{}, errInactiveLoanMember
	}

	var pendingID string
	err := s.db.QueryRow(`SELECT id FROM loan_requests WHERE member_id = $1 AND status = 'pending' LIMIT 1`, member.ID).Scan(&pendingID)
	if err == nil {
		return LoanRequest{}, errPendingLoanRequestExists
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, err
	}

	loanRequest := LoanRequest{
		ID:              newID(),
		MemberID:        member.ID,
		RequestedAmount: req.RequestedAmount,
		DurationMonths:  req.DurationMonths,
		Purpose:         strings.TrimSpace(req.Purpose),
		Status:          "pending",
	}
	_, err = s.db.Exec(
		`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, purpose, status) VALUES ($1, $2, $3, $4, $5, 'pending')`,
		loanRequest.ID,
		loanRequest.MemberID,
		loanRequest.RequestedAmount,
		loanRequest.DurationMonths,
		loanRequest.Purpose,
	)
	if err != nil {
		return LoanRequest{}, err
	}
	return loanRequest, nil
}

func (s *Server) loanRequestsByMember(memberID string) ([]LoanRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, requested_amount, duration_months, purpose, status, rejection_reason, created_at, updated_at
		FROM loan_requests
		WHERE member_id = $1
		ORDER BY created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []LoanRequest
	for rows.Next() {
		var request LoanRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Server) loanRequestsForAdmin(status string) ([]AdminLoanRequest, error) {
	status = strings.TrimSpace(status)
	query := `SELECT lr.id, lr.member_id, m.member_no, m.full_name, lr.requested_amount, lr.duration_months, lr.purpose, lr.status, lr.rejection_reason, lr.created_at, lr.updated_at
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
	defer rows.Close()

	var requests []AdminLoanRequest
	for rows.Next() {
		var request AdminLoanRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.MemberNo, &request.FullName, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Server) rejectLoanRequestByID(requestID, adminID string, req rejectLoanInput) (LoanRequest, error) {
	reason := strings.TrimSpace(req.RejectionReason)
	if reason == "" {
		return LoanRequest{}, errInvalidLoanRejection
	}

	var status string
	err := s.db.QueryRow(`SELECT status FROM loan_requests WHERE id = $1`, requestID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanRequest{}, errLoanRequestNotFound
	}
	if err != nil {
		return LoanRequest{}, err
	}
	if status != "pending" {
		return LoanRequest{}, errLoanRequestNotPending
	}

	if _, err := s.db.Exec(
		`UPDATE loan_requests
		SET status = 'rejected', reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, rejection_reason = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending'`,
		adminID,
		reason,
		requestID,
	); err != nil {
		return LoanRequest{}, err
	}

	return s.loanRequestByID(requestID)
}

func (s *Server) loanRequestByID(id string) (LoanRequest, error) {
	var request LoanRequest
	err := s.db.QueryRow(
		`SELECT id, member_id, requested_amount, duration_months, purpose, status, rejection_reason, created_at, updated_at
		FROM loan_requests
		WHERE id = $1`,
		id,
	).Scan(&request.ID, &request.MemberID, &request.RequestedAmount, &request.DurationMonths, &request.Purpose, &request.Status, &request.RejectionReason, &request.CreatedAt, &request.UpdatedAt)
	return request, err
}
