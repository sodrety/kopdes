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
	ID                   string            `json:"id"`
	MemberID             string            `json:"member_id"`
	Amount               int               `json:"amount"`
	Note                 string            `json:"note"`
	Status               string            `json:"status"`
	CurrentApprovalStage string            `json:"current_approval_stage,omitempty"`
	ReviewedAt           string            `json:"reviewed_at,omitempty"`
	RejectionReason      string            `json:"rejection_reason,omitempty"`
	SavingRecordID       string            `json:"saving_record_id,omitempty"`
	LatestDecision       *ApprovalDecision `json:"latest_decision,omitempty"`
	CreatedAt            string            `json:"created_at,omitempty"`
	UpdatedAt            string            `json:"updated_at,omitempty"`
}

type AdminWithdrawalRequest struct {
	WithdrawalRequest
	MemberNo        string             `json:"member_no"`
	FullName        string             `json:"full_name"`
	ApprovalHistory []ApprovalDecision `json:"approval_history"`
	CanDecide       bool               `json:"can_decide"`
}

type withdrawalRequestInput struct {
	Amount int    `json:"amount" form:"amount"`
	Note   string `json:"note" form:"note"`
}

type rejectWithdrawalInput struct {
	RejectionReason string `json:"rejection_reason" form:"rejection_reason"`
}

type approveWithdrawalInput struct {
	Note string `json:"note" form:"note"`
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
	user, _ := currentUser(c)
	requests, err := s.withdrawalRequestsForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	for index := range requests {
		requests[index].CanDecide = requests[index].Status == "pending" && requests[index].CurrentApprovalStage == user.Role
	}
	c.JSON(http.StatusOK, gin.H{"withdrawal_requests": requests})
}

func (s *Server) approveWithdrawalRequest(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	var req approveWithdrawalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid approval")
		return
	}
	request, err := s.approveWithdrawalRequestByID(c.Param("id"), user, req)
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending withdrawal requests can be approved")
		return
	}
	if errors.Is(err, errWrongApprovalStage) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "This request is waiting for another Officer Role")
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

	request, err := s.rejectWithdrawalRequestByID(c.Param("id"), user, req)
	if errors.Is(err, errInvalidWithdrawalRejection) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Rejection reason is required")
		return
	}
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending withdrawal requests can be rejected")
		return
	}
	if errors.Is(err, errWrongApprovalStage) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "This request is waiting for another Officer Role")
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
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var lockedMemberID string
	if err := tx.QueryRow(`SELECT id FROM members WHERE id=$1`+rowLockClause(s.db), member.ID).Scan(&lockedMemberID); err != nil {
		return WithdrawalRequest{}, err
	}
	summary, err := savingSummary(tx, member.ID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	var reserved int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM withdrawal_reservations WHERE member_id=$1 AND status='active'`, member.ID).Scan(&reserved); err != nil {
		return WithdrawalRequest{}, err
	}
	if req.Amount > summary.SukarelaBalance-reserved {
		return WithdrawalRequest{}, errInsufficientSukarelaBalance
	}

	request := WithdrawalRequest{
		ID:                   newID(),
		MemberID:             member.ID,
		Amount:               req.Amount,
		Note:                 strings.TrimSpace(req.Note),
		Status:               "pending",
		CurrentApprovalStage: approvalStageManager,
	}
	_, err = tx.Exec(
		`INSERT INTO withdrawal_requests (id, member_id, amount, note, status, current_approval_stage) VALUES ($1, $2, $3, $4, 'pending', 'manager')`,
		request.ID,
		request.MemberID,
		request.Amount,
		request.Note,
	)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if _, err := tx.Exec(`INSERT INTO withdrawal_reservations (id,request_id,member_id,amount,status) VALUES ($1,$2,$3,$4,'active')`, newID(), request.ID, request.MemberID, request.Amount); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := createStageNotification(tx, "withdrawal", request.ID, approvalStageManager, "/admin/withdrawal-requests"); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRequest{}, err
	}
	return request, nil
}

func (s *Server) withdrawalRequestsByMember(memberID string) ([]WithdrawalRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, amount, note, status, COALESCE(current_approval_stage,''), COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE member_id = $1
		ORDER BY created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	var requests []WithdrawalRequest
	for rows.Next() {
		var request WithdrawalRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.CurrentApprovalStage, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
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
		requests[index].LatestDecision, err = latestApprovalDecision(s.db, "withdrawal_request_approvals", requests[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *Server) withdrawalRequestsForAdmin(status string) ([]AdminWithdrawalRequest, error) {
	status = strings.TrimSpace(status)
	query := `SELECT wr.id, wr.member_id, m.member_no, m.full_name, wr.amount, wr.note, wr.status, COALESCE(wr.current_approval_stage,''), COALESCE(CAST(wr.reviewed_at AS TEXT), ''), wr.rejection_reason, COALESCE(wr.saving_record_id, ''), wr.created_at, wr.updated_at
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
	var requests []AdminWithdrawalRequest
	for rows.Next() {
		var request AdminWithdrawalRequest
		if err := rows.Scan(&request.ID, &request.MemberID, &request.MemberNo, &request.FullName, &request.Amount, &request.Note, &request.Status, &request.CurrentApprovalStage, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
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
		requests[index].ApprovalHistory, err = approvalHistory(s.db, "withdrawal_request_approvals", requests[index].ID, true)
		if err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *Server) approveWithdrawalRequestByID(requestID string, officer User, req approveWithdrawalInput) (WithdrawalRequest, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var request WithdrawalRequest
	if err := tx.QueryRow(
		`SELECT id, member_id, amount, note, status, COALESCE(current_approval_stage,''), COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE id = $1`+rowLockClause(s.db),
		requestID,
	).Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.CurrentApprovalStage, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WithdrawalRequest{}, errWithdrawalRequestNotFound
		}
		return WithdrawalRequest{}, err
	}
	if request.Status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if request.CurrentApprovalStage != officer.Role {
		return WithdrawalRequest{}, errWrongApprovalStage
	}
	if err := insertApprovalDecision(tx, "withdrawal_request_approvals", requestID, officer, "approved", req.Note, ""); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "withdrawal", requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	nextStage := nextApprovalStage(officer.Role)
	if nextStage != "" {
		result, err := tx.Exec(`UPDATE withdrawal_requests SET current_approval_stage=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2 AND status='pending' AND current_approval_stage=$3`, nextStage, requestID, officer.Role)
		if err != nil {
			return WithdrawalRequest{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return WithdrawalRequest{}, errWithdrawalRequestNotPending
		}
		if err := createStageNotification(tx, "withdrawal", requestID, nextStage, "/admin/withdrawal-requests"); err != nil {
			return WithdrawalRequest{}, err
		}
		if err := tx.Commit(); err != nil {
			return WithdrawalRequest{}, err
		}
		return s.withdrawalRequestByID(requestID)
	}

	var memberStatus string
	if err := tx.QueryRow(`SELECT status FROM members WHERE id = $1`+rowLockClause(s.db), request.MemberID).Scan(&memberStatus); err != nil {
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
	var reservationAmount int
	var reservationStatus string
	if err := tx.QueryRow(`SELECT amount,status FROM withdrawal_reservations WHERE request_id=$1`+rowLockClause(s.db), requestID).Scan(&reservationAmount, &reservationStatus); err != nil {
		return WithdrawalRequest{}, err
	}
	if reservationStatus != "active" || reservationAmount != request.Amount {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}

	recordID := newID()
	if _, err := tx.Exec(
		`INSERT INTO saving_records (id, member_id, type, category, amount, record_date, reference_no, note, recorded_by)
		VALUES ($1, $2, 'withdrawal', 'sukarela', $3, $4, '', $5, $6)`,
		recordID,
		request.MemberID,
		request.Amount,
		time.Now().In(jakartaLocation).Format("2006-01-02"),
		request.Note,
		officer.ID,
	); err != nil {
		return WithdrawalRequest{}, err
	}

	result, err := tx.Exec(
		`UPDATE withdrawal_requests
		SET status = 'approved', current_approval_stage=NULL, reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, saving_record_id = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending' AND current_approval_stage='ketua_utama'`,
		officer.ID,
		recordID,
		request.ID,
	)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if _, err := tx.Exec(`UPDATE withdrawal_reservations SET status='consumed',updated_at=CURRENT_TIMESTAMP WHERE request_id=$1 AND status='active'`, requestID); err != nil {
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

func (s *Server) rejectWithdrawalRequestByID(requestID string, officer User, req rejectWithdrawalInput) (WithdrawalRequest, error) {
	reason := strings.TrimSpace(req.RejectionReason)
	if reason == "" {
		return WithdrawalRequest{}, errInvalidWithdrawalRejection
	}

	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var status, stage, memberID string
	err = tx.QueryRow(`SELECT status,COALESCE(current_approval_stage,''),member_id FROM withdrawal_requests WHERE id=$1`+rowLockClause(s.db), requestID).Scan(&status, &stage, &memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	}
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if stage != officer.Role {
		return WithdrawalRequest{}, errWrongApprovalStage
	}
	if err := insertApprovalDecision(tx, "withdrawal_request_approvals", requestID, officer, "rejected", "", reason); err != nil {
		return WithdrawalRequest{}, err
	}
	result, err := tx.Exec(
		`UPDATE withdrawal_requests
		SET status = 'rejected', current_approval_stage=NULL, reviewed_by = $1, reviewed_at = CURRENT_TIMESTAMP, rejection_reason = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = 'pending' AND current_approval_stage=$4`,
		officer.ID,
		reason,
		requestID,
		officer.Role,
	)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
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

func (s *Server) cancelWithdrawalRequest(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	request, err := s.cancelWithdrawalRequestByID(c.Param("id"), member.ID)
	if errors.Is(err, errWithdrawalRequestNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Withdrawal request not found")
		return
	}
	if errors.Is(err, errWithdrawalRequestNotPending) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Only pending withdrawal requests can be changed")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	respondOKOrHXRedirect(c, "/member/withdrawal-requests", request)
}

func (s *Server) cancelWithdrawalRequestByID(requestID, memberID string) (WithdrawalRequest, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return WithdrawalRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	err = tx.QueryRow(`SELECT status FROM withdrawal_requests WHERE id=$1 AND member_id=$2`+rowLockClause(s.db), requestID, memberID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRequest{}, errWithdrawalRequestNotFound
	}
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if status != "pending" {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	result, err := tx.Exec(`UPDATE withdrawal_requests SET status='cancelled',current_approval_stage=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND member_id=$2 AND status='pending'`, requestID, memberID)
	if err != nil {
		return WithdrawalRequest{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return WithdrawalRequest{}, errWithdrawalRequestNotPending
	}
	if _, err := tx.Exec(`UPDATE withdrawal_reservations SET status='released',updated_at=CURRENT_TIMESTAMP WHERE request_id=$1 AND status='active'`, requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := resolveRequestNotifications(tx, "withdrawal", requestID); err != nil {
		return WithdrawalRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRequest{}, err
	}
	return s.withdrawalRequestByID(requestID)
}

func (s *Server) withdrawalRequestByID(id string) (WithdrawalRequest, error) {
	var request WithdrawalRequest
	err := s.db.QueryRow(
		`SELECT id, member_id, amount, note, status, COALESCE(current_approval_stage,''), COALESCE(CAST(reviewed_at AS TEXT), ''), rejection_reason, COALESCE(saving_record_id, ''), created_at, updated_at
		FROM withdrawal_requests
		WHERE id = $1`,
		id,
	).Scan(&request.ID, &request.MemberID, &request.Amount, &request.Note, &request.Status, &request.CurrentApprovalStage, &request.ReviewedAt, &request.RejectionReason, &request.SavingRecordID, &request.CreatedAt, &request.UpdatedAt)
	if err == nil {
		request.LatestDecision, err = latestApprovalDecision(s.db, "withdrawal_request_approvals", request.ID)
	}
	return request, err
}
