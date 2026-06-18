package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type WithdrawalRequest struct {
	ID              string `json:"id"`
	MemberID        string `json:"member_id"`
	Amount          int    `json:"amount"`
	Note            string `json:"note"`
	Status          string `json:"status"`
	ReviewedAt      string `json:"reviewed_at,omitempty"`
	RejectionReason string `json:"rejection_reason,omitempty"`
	SavingRecordID  string `json:"saving_record_id,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type AdminWithdrawalRequest struct {
	WithdrawalRequest
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
}

type withdrawalRequestInput struct {
	Amount int    `json:"amount" form:"amount"`
	Note   string `json:"note" form:"note"`
}

type rejectWithdrawalInput struct {
	RejectionReason string `json:"rejection_reason" form:"rejection_reason"`
}

var (
	errInvalidWithdrawalRequest        = errors.New("invalid withdrawal request")
	errInactiveWithdrawalMember        = errors.New("inactive withdrawal member")
	errInsufficientSukarelaBalance     = errors.New("insufficient sukarela balance")
	errWithdrawalRequestNotFound       = errors.New("withdrawal request not found")
	errWithdrawalRequestNotPending     = errors.New("withdrawal request not pending")
	errInvalidWithdrawalRejection      = errors.New("invalid withdrawal rejection")
	errInvalidSavingWithdrawalCategory = errors.New("invalid saving withdrawal category")
)

func (s *Server) submitWithdrawalRequest(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	var req withdrawalRequestInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid withdrawal request")
		return
	}

	request, err := s.insertWithdrawalRequest(member, req)
	if errors.Is(err, errInvalidWithdrawalRequest) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Withdrawal amount must be greater than zero")
		return
	}
	if errors.Is(err, errInactiveWithdrawalMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only active members can request withdrawals")
		return
	}
	if errors.Is(err, errInsufficientSukarelaBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed Simpanan Sukarela balance")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondCreatedOrHXRedirect(c, "/member/withdrawal-requests", request)
}

func (s *Server) memberWithdrawalRequests(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	requests, err := s.withdrawalRequestsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal_requests": requests})
}

func (s *Server) adminWithdrawalRequests(c *gin.Context) {
	requests, err := s.withdrawalRequestsForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal_requests": requests})
}

func (s *Server) approveWithdrawalRequest(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	request, err := s.approveWithdrawalRequestByID(c.Param("id"), user.ID)
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending withdrawal requests can be approved")
		return
	}
	if errors.Is(err, errInactiveWithdrawalMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only active members can request withdrawals")
		return
	}
	if errors.Is(err, errInsufficientSukarelaBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed Simpanan Sukarela balance")
		return
	}
	if errors.Is(err, errWithdrawalRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Withdrawal request not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) rejectWithdrawalRequest(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req rejectWithdrawalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid withdrawal rejection")
		return
	}

	request, err := s.rejectWithdrawalRequestByID(c.Param("id"), user.ID, req)
	if errors.Is(err, errInvalidWithdrawalRejection) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Rejection reason is required")
		return
	}
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending withdrawal requests can be rejected")
		return
	}
	if errors.Is(err, errWithdrawalRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Withdrawal request not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondOKOrHXRedirect(c, "/admin/withdrawal-requests", request)
}

func (s *Server) insertWithdrawalRequest(member Member, req withdrawalRequestInput) (WithdrawalRequest, error) {
	if req.Amount <= 0 {
		return WithdrawalRequest{}, errInvalidWithdrawalRequest
	}
	if member.Status != "active" {
		return WithdrawalRequest{}, errInactiveWithdrawalMember
	}
	summary, err := s.savingSummary(member.ID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if req.Amount > summary.SukarelaBalance {
		return WithdrawalRequest{}, errInsufficientSukarelaBalance
	}

	request := WithdrawalRequest{
		ID:       newID(),
		MemberID: member.ID,
		Amount:   req.Amount,
		Note:     strings.TrimSpace(req.Note),
		Status:   "pending",
	}
	_, err = s.db.Exec(
		`INSERT INTO withdrawal_requests (id, member_id, amount, note, status) VALUES ($1, $2, $3, $4, 'pending')`,
		request.ID,
		request.MemberID,
		request.Amount,
		request.Note,
	)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	return request, nil
}

func (s *Server) withdrawalRequestsByMember(memberID string) ([]WithdrawalRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, amount, note, status, COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE member_id = $1
		ORDER BY created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []WithdrawalRequest
	for rows.Next() {
		var request WithdrawalRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Server) withdrawalRequestsForAdmin(status string) ([]AdminWithdrawalRequest, error) {
	status = strings.TrimSpace(status)
	query := `SELECT wr.id, wr.member_id, m.member_no, m.full_name, wr.amount, wr.note, wr.status, COALESCE(CAST(wr.reviewed_at AS TEXT), ''), wr.rejection_reason, COALESCE(wr.saving_record_id, ''), wr.created_at, wr.updated_at
		FROM withdrawal_requests wr
		INNER JOIN members m ON m.id = wr.member_id`
	args := []any{}
	if status != "" {
		query += ` WHERE wr.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY wr.created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []AdminWithdrawalRequest
	for rows.Next() {
		var request AdminWithdrawalRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.MemberNo, &request.FullName, &request.Amount, &request.Note, &request.Status, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Server) approveWithdrawalRequestByID(requestID, adminID string) (WithdrawalRequest, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var request WithdrawalRequest
	if err := tx.QueryRow(
		`SELECT id, member_id, amount, note, status, COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE id = $1`,
		requestID,
	).Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WithdrawalRequest{}, errWithdrawalRequestNotFound
		}
		return WithdrawalRequest{}, err
	}
	if request.Status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}

	result, err := tx.Exec(`UPDATE members SET updated_at = updated_at WHERE id = $1`, request.MemberID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if rowsAffected == 0 {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	}

	var memberStatus string
	if err := tx.QueryRow(`SELECT status FROM members WHERE id = $1`, request.MemberID).Scan(&memberStatus); err != nil {
		return WithdrawalRequest{}, err
	}
	if memberStatus != "active" {
		return WithdrawalRequest{}, errInactiveWithdrawalMember
	}

	summary, err := savingSummary(tx, request.MemberID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if request.Amount > summary.SukarelaBalance {
		return WithdrawalRequest{}, errInsufficientSukarelaBalance
	}

	recordID := newID()
	if _, err := tx.Exec(
		`INSERT INTO saving_records (id, member_id, type, category, amount, record_date, reference_no, note, recorded_by)
		VALUES ($1, $2, 'withdrawal', 'sukarela', $3, $4, '', $5, $6)`,
		recordID,
		request.MemberID,
		request.Amount,
		time.Now().Format("2006-01-02"),
		request.Note,
		adminID,
	); err != nil {
		return WithdrawalRequest{}, err
	}

	if _, err := tx.Exec(
		`UPDATE withdrawal_requests
		SET status = 'approved', reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, saving_record_id = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending'`,
		adminID,
		recordID,
		request.ID,
	); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRequest{}, err
	}
	return s.withdrawalRequestByID(request.ID)
}

func (s *Server) rejectWithdrawalRequestByID(requestID, adminID string, req rejectWithdrawalInput) (WithdrawalRequest, error) {
	reason := strings.TrimSpace(req.RejectionReason)
	if reason == "" {
		return WithdrawalRequest{}, errInvalidWithdrawalRejection
	}

	var status string
	err := s.db.QueryRow(`SELECT status FROM withdrawal_requests WHERE id = $1`, requestID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	}
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}

	if _, err := s.db.Exec(
		`UPDATE withdrawal_requests
		SET status = 'rejected', reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, rejection_reason = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending'`,
		adminID,
		reason,
		requestID,
	); err != nil {
		return WithdrawalRequest{}, err
	}
	return s.withdrawalRequestByID(requestID)
}

func (s *Server) withdrawalRequestByID(id string) (WithdrawalRequest, error) {
	var request WithdrawalRequest
	err := s.db.QueryRow(
		`SELECT id, member_id, amount, note, status, COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE id = $1`,
		id,
	).Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt)
	return request, err
}
