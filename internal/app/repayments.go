package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type LoanRepayment struct {
	ID          string `json:"id"`
	LoanID      string `json:"loan_id"`
	MemberID    string `json:"member_id"`
	Amount      int    `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
	RecordedBy  string `json:"recorded_by,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type AdminLoanRepayment struct {
	ID          string `json:"id"`
	LoanID      string `json:"loan_id"`
	MemberID    string `json:"member_id"`
	MemberNo    string `json:"member_no"`
	FullName    string `json:"full_name"`
	Amount      int    `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type repaymentInput struct {
	Amount      int    `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
}

var (
	errInvalidRepayment     = errors.New("invalid repayment")
	errRepaymentOverBalance = errors.New("repayment over balance")
	errLoanNotFound         = errors.New("loan not found")
	errLoanNotActive        = errors.New("loan not active")
)

func (s *Server) recordLoanRepayment(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req repaymentInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid repayment")
		return
	}

	repayment, err := s.recordRepayment(c.Param("id"), user.ID, req)
	if errors.Is(err, errInvalidRepayment) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Repayment amount and record date are required")
		return
	}
	if errors.Is(err, errRepaymentOverBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Repayment amount cannot exceed remaining loan balance")
		return
	}
	if errors.Is(err, errLoanNotActive) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only active loans can receive repayments")
		return
	}
	if errors.Is(err, errLoanNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Loan not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondCreatedOrHXRedirect(c, "/admin/loans", repayment)
}

func (s *Server) memberRepayments(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	repayments, err := s.repaymentsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"repayments": repayments})
}

func (s *Server) recordRepayment(loanID, adminID string, req repaymentInput) (LoanRepayment, error) {
	recordDate := strings.TrimSpace(req.RecordDate)
	if req.Amount <= 0 || recordDate == "" {
		return LoanRepayment{}, errInvalidRepayment
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return LoanRepayment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var loan struct {
		MemberID         string
		RemainingBalance int
		Status           string
	}
	err = tx.QueryRow(
		`SELECT member_id, remaining_balance, status FROM loans WHERE id = $1`,
		loanID,
	).Scan(&loan.MemberID, &loan.RemainingBalance, &loan.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanRepayment{}, errLoanNotFound
	}
	if err != nil {
		return LoanRepayment{}, err
	}
	if loan.Status != "active" {
		return LoanRepayment{}, errLoanNotActive
	}
	if req.Amount > loan.RemainingBalance {
		return LoanRepayment{}, errRepaymentOverBalance
	}

	repayment := LoanRepayment{
		ID:          newID(),
		LoanID:      loanID,
		MemberID:    loan.MemberID,
		Amount:      req.Amount,
		RecordDate:  recordDate,
		ReferenceNo: strings.TrimSpace(req.ReferenceNo),
		Note:        strings.TrimSpace(req.Note),
		RecordedBy:  adminID,
	}

	result, err := tx.Exec(
		`UPDATE loans
		SET remaining_balance = remaining_balance - $1,
			status = CASE WHEN remaining_balance - $1 = 0 THEN 'paid' ELSE 'active' END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND status = 'active' AND remaining_balance >= $1`,
		req.Amount,
		loanID,
	)
	if err != nil {
		return LoanRepayment{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return LoanRepayment{}, err
	}
	if rowsAffected == 0 {
		return LoanRepayment{}, errRepaymentOverBalance
	}

	if _, err := tx.Exec(
		`INSERT INTO loan_repayments (id, loan_id, member_id, amount, record_date, reference_no, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		repayment.ID,
		repayment.LoanID,
		repayment.MemberID,
		repayment.Amount,
		repayment.RecordDate,
		repayment.ReferenceNo,
		repayment.Note,
		repayment.RecordedBy,
	); err != nil {
		return LoanRepayment{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoanRepayment{}, err
	}

	return s.repaymentByID(repayment.ID)
}

func (s *Server) repaymentByID(id string) (LoanRepayment, error) {
	var repayment LoanRepayment
	err := s.db.QueryRow(
		`SELECT id, loan_id, member_id, amount, record_date, reference_no, note, recorded_by, created_at
		FROM loan_repayments
		WHERE id = $1`,
		id,
	).Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.Amount, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.RecordedBy, &repayment.CreatedAt)
	return repayment, err
}

func (s *Server) repaymentsByMember(memberID string) ([]LoanRepayment, error) {
	rows, err := s.db.Query(
		`SELECT id, loan_id, member_id, amount, record_date, reference_no, note, recorded_by, created_at
		FROM loan_repayments
		WHERE member_id = $1
		ORDER BY record_date DESC, created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repayments []LoanRepayment
	for rows.Next() {
		var repayment LoanRepayment
		if err := rows.Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.Amount, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.RecordedBy, &repayment.CreatedAt); err != nil {
			return nil, err
		}
		repayments = append(repayments, repayment)
	}
	return repayments, rows.Err()
}

func (s *Server) repaymentsForAdmin() ([]AdminLoanRepayment, error) {
	rows, err := s.db.Query(
		`SELECT lr.id, lr.loan_id, lr.member_id, m.member_no, m.full_name, lr.amount, lr.record_date, lr.reference_no, lr.note, lr.created_at
		FROM loan_repayments lr
		INNER JOIN members m ON m.id = lr.member_id
		ORDER BY lr.record_date DESC, lr.created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repayments []AdminLoanRepayment
	for rows.Next() {
		var repayment AdminLoanRepayment
		if err := rows.Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.MemberNo, &repayment.FullName, &repayment.Amount, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.CreatedAt); err != nil {
			return nil, err
		}
		repayments = append(repayments, repayment)
	}
	return repayments, rows.Err()
}

func (s *Server) latestRepaymentsByMember(memberID string, limit int) ([]LoanRepayment, error) {
	rows, err := s.db.Query(
		`SELECT id, loan_id, member_id, amount, record_date, reference_no, note, recorded_by, created_at
		FROM loan_repayments
		WHERE member_id = $1
		ORDER BY record_date DESC, created_at DESC
		LIMIT $2`,
		memberID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repayments []LoanRepayment
	for rows.Next() {
		var repayment LoanRepayment
		if err := rows.Scan(&repayment.ID, &repayment.LoanID, &repayment.MemberID, &repayment.Amount, &repayment.RecordDate, &repayment.ReferenceNo, &repayment.Note, &repayment.RecordedBy, &repayment.CreatedAt); err != nil {
			return nil, err
		}
		repayments = append(repayments, repayment)
	}
	return repayments, rows.Err()
}
