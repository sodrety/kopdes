package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Loan struct {
	ID                 string `json:"id"`
	LoanRequestID      string `json:"loan_request_id"`
	MemberID           string `json:"member_id"`
	ApprovedAmount     int    `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int    `json:"monthly_installment"`
	RemainingBalance   int    `json:"remaining_balance"`
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
	MemberNo           string `json:"member_no"`
	FullName           string `json:"full_name"`
	ApprovedAmount     int    `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int    `json:"monthly_installment"`
	RemainingBalance   int    `json:"remaining_balance"`
	Status             string `json:"status"`
	ApprovedAt         string `json:"approved_at,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type approveLoanInput struct {
	ApprovedAmount int `json:"approved_amount" form:"approved_amount"`
	DurationMonths int `json:"duration_months" form:"duration_months"`
}

var (
	errInvalidLoanApproval           = errors.New("invalid loan approval")
	errApprovedAmountExceedsRequest  = errors.New("approved amount exceeds request")
	errLoanRequestNotPending         = errors.New("loan request not pending")
	errLoanRequestNotFound           = errors.New("loan request not found")
	errMemberAlreadyHasActiveLoan    = errors.New("member already has active loan")
	errInvalidLoanApprovalCalculated = errors.New("invalid calculated installment")
)

func (s *Server) approveLoanRequest(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req approveLoanInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid loan approval")
		return
	}

	loan, err := s.approveLoanRequestByID(c.Param("id"), user.ID, req)
	if errors.Is(err, errInvalidLoanApproval) || errors.Is(err, errInvalidLoanApprovalCalculated) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Approved amount and duration months must be greater than zero")
		return
	}
	if errors.Is(err, errApprovedAmountExceedsRequest) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Approved amount cannot exceed requested amount")
		return
	}
	if errors.Is(err, errLoanRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be approved")
		return
	}
	if errors.Is(err, errMemberAlreadyHasActiveLoan) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Member already has an active loan")
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

	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Redirect", "/admin/loans")
		c.Status(http.StatusNoContent)
		return
	}

	c.JSON(http.StatusCreated, loan)
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

func (s *Server) adminLoans(c *gin.Context) {
	loans, err := s.loansForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"loans": loans})
}

func (s *Server) approveLoanRequestByID(requestID, adminID string, req approveLoanInput) (Loan, error) {
	if req.ApprovedAmount <= 0 || req.DurationMonths <= 0 {
		return Loan{}, errInvalidLoanApproval
	}
	monthlyInstallment := req.ApprovedAmount / req.DurationMonths
	if monthlyInstallment <= 0 {
		return Loan{}, errInvalidLoanApprovalCalculated
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Loan{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var request struct {
		MemberID        string
		RequestedAmount int
		Status          string
	}
	err = tx.QueryRow(
		`SELECT member_id, requested_amount, status FROM loan_requests WHERE id = $1`,
		requestID,
	).Scan(&request.MemberID, &request.RequestedAmount, &request.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Loan{}, errLoanRequestNotFound
	}
	if err != nil {
		return Loan{}, err
	}
	if request.Status != "pending" {
		return Loan{}, errLoanRequestNotPending
	}
	if req.ApprovedAmount > request.RequestedAmount {
		return Loan{}, errApprovedAmountExceedsRequest
	}

	var activeLoanID string
	err = tx.QueryRow(`SELECT id FROM loans WHERE member_id = $1 AND status = 'active' LIMIT 1`, request.MemberID).Scan(&activeLoanID)
	if err == nil {
		return Loan{}, errMemberAlreadyHasActiveLoan
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Loan{}, err
	}

	loan := Loan{
		ID:                 newID(),
		LoanRequestID:      requestID,
		MemberID:           request.MemberID,
		ApprovedAmount:     req.ApprovedAmount,
		DurationMonths:     req.DurationMonths,
		MonthlyInstallment: monthlyInstallment,
		RemainingBalance:   req.ApprovedAmount,
		Status:             "active",
		ApprovedBy:         adminID,
	}

	if _, err := tx.Exec(
		`UPDATE loan_requests
		SET status = 'approved', reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND status = 'pending'`,
		adminID,
		requestID,
	); err != nil {
		return Loan{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8)`,
		loan.ID,
		loan.LoanRequestID,
		loan.MemberID,
		loan.ApprovedAmount,
		loan.DurationMonths,
		loan.MonthlyInstallment,
		loan.RemainingBalance,
		loan.ApprovedBy,
	); err != nil {
		return Loan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Loan{}, err
	}

	return s.loanByID(loan.ID)
}

func (s *Server) loanByID(id string) (Loan, error) {
	var loan Loan
	err := s.db.QueryRow(
		`SELECT id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at, created_at, updated_at
		FROM loans
		WHERE id = $1`,
		id,
	).Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.Status, &loan.ApprovedBy, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt)
	return loan, err
}

func (s *Server) activeLoanByMember(memberID string) (Loan, error) {
	var loan Loan
	err := s.db.QueryRow(
		`SELECT id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at, created_at, updated_at
		FROM loans
		WHERE member_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1`,
		memberID,
	).Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.Status, &loan.ApprovedBy, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt)
	return loan, err
}

func (s *Server) loansForAdmin(status string) ([]AdminLoan, error) {
	status = strings.TrimSpace(status)
	query := `SELECT l.id, l.loan_request_id, l.member_id, m.member_no, m.full_name, l.approved_amount, l.duration_months, l.monthly_installment, l.remaining_balance, l.status, l.approved_at, l.created_at, l.updated_at
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
		if err := rows.Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.MemberNo, &loan.FullName, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.Status, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt); err != nil {
			return nil, err
		}
		loans = append(loans, loan)
	}
	return loans, rows.Err()
}
