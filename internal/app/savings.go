package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type SavingRecord struct {
	ID          string `json:"id"`
	MemberID    string `json:"member_id"`
	Type        string `json:"type"`
	Amount      int    `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
	RecordedBy  string `json:"recorded_by,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type savingRequest struct {
	MemberID    string `json:"member_id" form:"member_id"`
	Type        string `json:"type" form:"type"`
	Amount      int    `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
}

func (s *Server) recordSaving(c *gin.Context) {
	var req savingRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid saving request")
		return
	}

	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}

	record, err := s.insertSaving(req, user.ID)
	if errors.Is(err, errInvalidSaving) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Member, deposit amount, and record date are required")
		return
	}
	if errors.Is(err, errInactiveSavingMember) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Savings can only be recorded for active members")
		return
	}
	if errors.Is(err, errInsufficientSavingBalance) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed current saving balance")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondCreatedOrHXRedirect(c, "/admin/savings", record)
}

func (s *Server) memberSavings(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	records, err := s.savingsByMember(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"savings": records})
}

func (s *Server) memberSavingSummary(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}

	summary, err := s.savingSummary(member.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, summary)
}

var (
	errInvalidSaving             = errors.New("invalid saving")
	errInactiveSavingMember      = errors.New("inactive saving member")
	errInsufficientSavingBalance = errors.New("insufficient saving balance")
)

func (s *Server) insertSaving(req savingRequest, recordedBy string) (SavingRecord, error) {
	memberID := strings.TrimSpace(req.MemberID)
	recordType := strings.TrimSpace(req.Type)
	recordDate := strings.TrimSpace(req.RecordDate)
	if recordType == "" {
		recordType = "deposit"
	}
	if memberID == "" || !validSavingType(recordType) || req.Amount <= 0 || recordDate == "" {
		return SavingRecord{}, errInvalidSaving
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return SavingRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.Exec(`UPDATE members SET updated_at = updated_at WHERE id = $1`, memberID)
	if err != nil {
		return SavingRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return SavingRecord{}, err
	}
	if rowsAffected == 0 {
		return SavingRecord{}, sql.ErrNoRows
	}

	var member Member
	err = tx.QueryRow(
		`SELECT id, member_no, full_name, phone, address, join_date, status FROM members WHERE id = $1`,
		memberID,
	).Scan(&member.ID, &member.MemberNo, &member.FullName, &member.Phone, &member.Address, &member.JoinDate, &member.Status)
	if err != nil {
		return SavingRecord{}, err
	}
	if member.Status != "active" {
		return SavingRecord{}, errInactiveSavingMember
	}
	if recordType == "withdrawal" {
		summary, err := savingSummary(tx, member.ID)
		if err != nil {
			return SavingRecord{}, err
		}
		if req.Amount > summary.CurrentBalance {
			return SavingRecord{}, errInsufficientSavingBalance
		}
	}

	record := SavingRecord{
		ID:          newID(),
		MemberID:    member.ID,
		Type:        recordType,
		Amount:      req.Amount,
		RecordDate:  recordDate,
		ReferenceNo: strings.TrimSpace(req.ReferenceNo),
		Note:        strings.TrimSpace(req.Note),
		RecordedBy:  recordedBy,
	}
	_, err = tx.Exec(
		`INSERT INTO saving_records (id, member_id, type, amount, record_date, reference_no, note, recorded_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		record.ID,
		record.MemberID,
		record.Type,
		record.Amount,
		record.RecordDate,
		record.ReferenceNo,
		record.Note,
		record.RecordedBy,
	)
	if err != nil {
		return SavingRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return SavingRecord{}, err
	}
	return record, nil
}

func validSavingType(recordType string) bool {
	return recordType == "deposit" || recordType == "withdrawal"
}

func (s *Server) savingsByMember(memberID string) ([]SavingRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, type, amount, record_date, reference_no, note, recorded_by, created_at FROM saving_records WHERE member_id = $1 ORDER BY record_date DESC, created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SavingRecord
	for rows.Next() {
		var record SavingRecord
		if err := rows.Scan(&record.ID, &record.MemberID, &record.Type, &record.Amount, &record.RecordDate, &record.ReferenceNo, &record.Note, &record.RecordedBy, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Server) latestSavingsByMember(memberID string, limit int) ([]SavingRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, type, amount, record_date, reference_no, note, recorded_by, created_at
		FROM saving_records
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

	var records []SavingRecord
	for rows.Next() {
		var record SavingRecord
		if err := rows.Scan(&record.ID, &record.MemberID, &record.Type, &record.Amount, &record.RecordDate, &record.ReferenceNo, &record.Note, &record.RecordedBy, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type SavingSummary struct {
	TotalDeposit    int `json:"total_deposit"`
	TotalWithdrawal int `json:"total_withdrawal"`
	CurrentBalance  int `json:"current_balance"`
}

func (s *Server) savingSummary(memberID string) (SavingSummary, error) {
	return savingSummary(s.db, memberID)
}

type savingSummaryQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func savingSummary(q savingSummaryQuerier, memberID string) (SavingSummary, error) {
	var summary SavingSummary
	err := q.QueryRow(
		`SELECT
			COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'withdrawal' THEN amount ELSE 0 END), 0)
		FROM saving_records
		WHERE member_id = $1`,
		memberID,
	).Scan(&summary.TotalDeposit, &summary.TotalWithdrawal)
	if err != nil {
		return SavingSummary{}, err
	}
	summary.CurrentBalance = summary.TotalDeposit - summary.TotalWithdrawal
	return summary, nil
}
