package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type SavingRecord struct {
	ID          string `json:"id"`
	MemberID    string `json:"member_id"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Amount      int64  `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
	RecordedBy  string `json:"recorded_by,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type savingRequest struct {
	MemberID    string `json:"member_id" form:"member_id"`
	Type        string `json:"type" form:"type"`
	Category    string `json:"category" form:"category"`
	Amount      int64  `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
}

type SavingFilters struct {
	MemberID string `form:"member_id"`
	Type     string `form:"type"`
	Category string `form:"category"`
	DateFrom string `form:"date_from"`
	DateTo   string `form:"date_to"`
}

type AdminSavingRecord struct {
	SavingRecord
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	MemberType      string `json:"member_type"`
	MemberTypeLabel string `json:"member_type_label"`
}

func (s *Server) recordSaving(c *gin.Context) {
	var req savingRequest
	if err := bindRequestWithRupiahAmount(c, &req, "amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
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
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Member, category, amount, and record date are required")
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
	if errors.Is(err, errInvalidSavingWithdrawalCategory) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Withdrawals can only use Simpanan Sukarela")
		return
	}
	if errors.Is(err, errDirectWithdrawalNotAllowed) {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "Sukarela withdrawals must use the approval chain")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "Member not found")
		return
	}
	if isMonetaryAggregateCapacityError(err) {
		respondError(c, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", "error_monetary_aggregate_capacity")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}

	respondCreatedOrHXRedirect(c, "/admin/savings", record)
}

func (s *Server) adminSavings(c *gin.Context) {
	filters := savingFiltersFromQuery(c)
	records, err := s.savingsForAdmin(filters)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"savings": records})
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
	errInvalidSaving              = errors.New("invalid saving")
	errInactiveSavingMember       = errors.New("inactive saving member")
	errInsufficientSavingBalance  = errors.New("insufficient saving balance")
	errDirectWithdrawalNotAllowed = errors.New("direct withdrawal not allowed")
)

func (s *Server) insertSaving(req savingRequest, recordedBy string) (SavingRecord, error) {
	memberID := strings.TrimSpace(req.MemberID)
	recordType := strings.TrimSpace(req.Type)
	category := strings.TrimSpace(req.Category)
	recordDate := strings.TrimSpace(req.RecordDate)
	if recordType == "" {
		recordType = "deposit"
	}
	if recordType == "withdrawal" {
		return SavingRecord{}, errDirectWithdrawalNotAllowed
	}
	if memberID == "" || !validSavingType(recordType) || !validSavingCategory(category) || req.Amount <= 0 || recordDate == "" {
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
		if category != "sukarela" {
			return SavingRecord{}, errInvalidSavingWithdrawalCategory
		}
		summary, err := savingSummary(tx, member.ID)
		if err != nil {
			return SavingRecord{}, err
		}
		if req.Amount > summary.BalanceForCategory(category) {
			return SavingRecord{}, errInsufficientSavingBalance
		}
	}

	record := SavingRecord{
		ID:          newID(),
		MemberID:    member.ID,
		Type:        recordType,
		Category:    category,
		Amount:      req.Amount,
		RecordDate:  recordDate,
		ReferenceNo: strings.TrimSpace(req.ReferenceNo),
		Note:        strings.TrimSpace(req.Note),
		RecordedBy:  recordedBy,
	}
	_, err = tx.Exec(
		`INSERT INTO saving_records (id, member_id, type, category, amount, record_date, reference_no, note, recorded_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		record.ID,
		record.MemberID,
		record.Type,
		record.Category,
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
	return recordType == "deposit"
}

func validSavingCategory(category string) bool {
	return category == "pokok" || category == "wajib" || category == "sukarela"
}

func savingFiltersFromQuery(c *gin.Context) SavingFilters {
	return SavingFilters{
		MemberID: strings.TrimSpace(c.Query("member_id")),
		Type:     strings.TrimSpace(c.Query("type")),
		Category: strings.TrimSpace(c.Query("category")),
		DateFrom: strings.TrimSpace(c.Query("date_from")),
		DateTo:   strings.TrimSpace(c.Query("date_to")),
	}
}

func (s *Server) savingsForAdmin(filters SavingFilters) ([]AdminSavingRecord, error) {
	query := strings.Builder{}
	query.WriteString(`SELECT sr.id, sr.member_id, sr.type, sr.category, sr.amount, sr.record_date, sr.reference_no, sr.note, sr.recorded_by, sr.created_at, m.member_no, m.full_name, m.member_type
		FROM saving_records sr
		JOIN members m ON m.id = sr.member_id
		WHERE 1 = 1`)

	var args []any
	addFilter := func(condition string, value any) {
		args = append(args, value)
		query.WriteString(fmt.Sprintf(" AND %s $%d", condition, len(args)))
	}
	if filters.MemberID != "" {
		addFilter("sr.member_id =", filters.MemberID)
	}
	if filters.Type != "" && validSavingType(filters.Type) {
		addFilter("sr.type =", filters.Type)
	}
	if filters.Category != "" && validSavingCategory(filters.Category) {
		addFilter("sr.category =", filters.Category)
	}
	if filters.DateFrom != "" {
		addFilter("sr.record_date >=", filters.DateFrom)
	}
	if filters.DateTo != "" {
		addFilter("sr.record_date <=", filters.DateTo)
	}
	query.WriteString(" ORDER BY sr.record_date DESC, sr.created_at DESC")

	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []AdminSavingRecord
	for rows.Next() {
		var record AdminSavingRecord
		if err := rows.Scan(&record.ID, &record.MemberID, &record.Type, &record.Category, &record.Amount, &record.RecordDate, &record.ReferenceNo, &record.Note, &record.RecordedBy, &record.CreatedAt, &record.MemberNo, &record.FullName, &record.MemberType); err != nil {
			return nil, err
		}
		record.MemberTypeLabel = memberTypeLabel(record.MemberType)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Server) savingsByMember(memberID string) ([]SavingRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, type, category, amount, record_date, reference_no, note, recorded_by, created_at FROM saving_records WHERE member_id = $1 ORDER BY record_date DESC, created_at DESC`,
		memberID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SavingRecord
	for rows.Next() {
		var record SavingRecord
		if err := rows.Scan(&record.ID, &record.MemberID, &record.Type, &record.Category, &record.Amount, &record.RecordDate, &record.ReferenceNo, &record.Note, &record.RecordedBy, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Server) latestSavingsByMember(memberID string, limit int) ([]SavingRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, member_id, type, category, amount, record_date, reference_no, note, recorded_by, created_at
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
		if err := rows.Scan(&record.ID, &record.MemberID, &record.Type, &record.Category, &record.Amount, &record.RecordDate, &record.ReferenceNo, &record.Note, &record.RecordedBy, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type SavingSummary struct {
	TotalDeposit               int64 `json:"total_deposit"`
	TotalWithdrawal            int64 `json:"total_withdrawal"`
	CurrentBalance             int64 `json:"current_balance"`
	PokokBalance               int64 `json:"pokok_balance"`
	WajibBalance               int64 `json:"wajib_balance"`
	SukarelaBalance            int64 `json:"sukarela_balance"`
	AvailableWithdrawalBalance int64 `json:"available_withdrawal_balance"`
}

func (s SavingSummary) BalanceForCategory(category string) int64 {
	switch category {
	case "pokok":
		return s.PokokBalance
	case "wajib":
		return s.WajibBalance
	case "sukarela":
		return s.SukarelaBalance
	default:
		return 0
	}
}

func (s *Server) savingSummary(memberID string) (SavingSummary, error) {
	summary, err := savingSummary(s.db, memberID)
	if err != nil {
		return SavingSummary{}, err
	}
	var reserved int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM withdrawal_reservations WHERE member_id=$1 AND status='active'`, memberID).Scan(&reserved); err != nil {
		return SavingSummary{}, err
	}
	summary.AvailableWithdrawalBalance = summary.SukarelaBalance - reserved
	return summary, nil
}

type savingSummaryQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func savingSummary(q savingSummaryQuerier, memberID string) (SavingSummary, error) {
	var summary SavingSummary
	err := q.QueryRow(
		`SELECT
			COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'withdrawal' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN category = 'pokok' AND type = 'deposit' THEN amount WHEN category = 'pokok' AND type = 'withdrawal' THEN -amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN category = 'wajib' AND type = 'deposit' THEN amount WHEN category = 'wajib' AND type = 'withdrawal' THEN -amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN category = 'sukarela' AND type = 'deposit' THEN amount WHEN category = 'sukarela' AND type = 'withdrawal' THEN -amount ELSE 0 END), 0)
		FROM saving_records
		WHERE member_id = $1`,
		memberID,
	).Scan(&summary.TotalDeposit, &summary.TotalWithdrawal, &summary.PokokBalance, &summary.WajibBalance, &summary.SukarelaBalance)
	if err != nil {
		return SavingSummary{}, err
	}
	summary.CurrentBalance = summary.TotalDeposit - summary.TotalWithdrawal
	summary.AvailableWithdrawalBalance = summary.SukarelaBalance
	return summary, nil
}
