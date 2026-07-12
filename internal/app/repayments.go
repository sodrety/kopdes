package app

import (
	"database/sql"
	"encoding/json"
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

type RepaymentFilters struct {
	Search string `form:"search"`
}

var (
	errInvalidRepayment     = errors.New("invalid repayment")
	errRepaymentOverBalance = errors.New("repayment over balance")
	errLoanNotFound         = errors.New("loan not found")
	errLoanNotActive        = errors.New("loan not active")
)

func (s *Server) recordLoanRepayment(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req repaymentInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_loan_repayment"))
		return
	}

	repayment, err := s.recordRepayment(c.Param("id"), user.ID, req)
	if errors.Is(err, errInvalidRepayment) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_loan_repayment_fields"))
		return
	}
	if errors.Is(err, errRepaymentOverBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_repayment_over_balance"))
		return
	}
	if errors.Is(err, errLoanNotActive) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(lang, "error_loan_repayment_status"))
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

	if isHTMXRequest(c) {
		trigger, err := json.Marshal(gin.H{
			"kopdes:toast": gin.H{
				"type":    "success",
				"message": translate(languageFromRequest(c), "toast_repayment_recorded"),
				"persist": true,
			},
		})
		if err == nil {
			c.Header("HX-Trigger", string(trigger))
		}
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
		`SELECT member_id, remaining_balance, status FROM loans WHERE id = $1`+rowLockClause(s.db),
		loanID,
	).Scan(&loan.MemberID, &loan.RemainingBalance, &loan.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return LoanRepayment{}, errLoanNotFound
	}
	if err != nil {
		return LoanRepayment{}, err
	}
	if loan.Status != "active" && loan.Status != "adjustment_due" {
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

	remaining := req.Amount
	rows, err := tx.Query(`SELECT id,scheduled_amount,paid_amount FROM loan_installments WHERE loan_id=$1 AND paid_amount < scheduled_amount ORDER BY installment_no`, loanID)
	if err != nil {
		return LoanRepayment{}, err
	}
	type unpaidInstallment struct {
		id              string
		scheduled, paid int
	}
	var installments []unpaidInstallment
	for rows.Next() {
		var i unpaidInstallment
		if err = rows.Scan(&i.id, &i.scheduled, &i.paid); err != nil {
			rows.Close()
			return LoanRepayment{}, err
		}
		installments = append(installments, i)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return LoanRepayment{}, err
	}
	if err = rows.Close(); err != nil {
		return LoanRepayment{}, err
	}
	for _, i := range installments {
		if remaining == 0 {
			break
		}
		applied := i.scheduled - i.paid
		if applied > remaining {
			applied = remaining
		}
		if _, err = tx.Exec(`UPDATE loan_installments SET paid_amount=paid_amount+$1 WHERE id=$2`, applied, i.id); err != nil {
			return LoanRepayment{}, err
		}
		remaining -= applied
	}
	if remaining != 0 {
		return LoanRepayment{}, errRepaymentOverBalance
	}
	newBalance := loan.RemainingBalance - req.Amount
	newStatus := loan.Status
	if newBalance == 0 {
		newStatus = "paid"
	}
	var nextDue string
	if newBalance > 0 {
		if err = tx.QueryRow(`SELECT due_date FROM loan_installments WHERE loan_id=$1 AND paid_amount < scheduled_amount ORDER BY installment_no LIMIT 1`, loanID).Scan(&nextDue); err != nil {
			return LoanRepayment{}, err
		}
	}
	result, err := tx.Exec(
		`UPDATE loans SET remaining_balance=$1,status=$2,next_due_date=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4 AND status=$5 AND remaining_balance=$6`,
		newBalance, newStatus, nextDue, loanID, loan.Status, loan.RemainingBalance,
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

func (s *Server) repaymentsForAdmin(filters RepaymentFilters) ([]AdminLoanRepayment, error) {
	search := strings.TrimSpace(filters.Search)
	query := `SELECT lr.id, lr.loan_id, lr.member_id, m.member_no, m.full_name, lr.amount, lr.record_date, lr.reference_no, lr.note, lr.created_at
		FROM loan_repayments lr
		INNER JOIN members m ON m.id = lr.member_id`
	args := []any{}
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		query += `
		WHERE LOWER(m.full_name) LIKE $1 OR LOWER(m.member_no) LIKE $1`
	}
	query += `
		ORDER BY lr.record_date DESC, lr.created_at DESC`

	rows, err := s.db.Query(query, args...)
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

func repaymentFiltersFromQuery(c *gin.Context) RepaymentFilters {
	return RepaymentFilters{
		Search: strings.TrimSpace(c.Query("search")),
	}
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
