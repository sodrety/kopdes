package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type CashTransactionCategory struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type manualCashTransactionRequest struct {
	Direction   string `json:"direction" form:"direction"`
	CategoryID  string `json:"category_id" form:"category_id"`
	Description string `json:"description" form:"description"`
	Amount      int64  `json:"amount" form:"amount"`
	RecordDate  string `json:"transaction_date" form:"transaction_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
}

type cashTransactionCategoryRequest struct {
	Direction string `json:"direction" form:"direction"`
	Name      string `json:"name" form:"name"`
}

type cashTransactionCategoryUpdateRequest struct {
	Name   string `json:"name" form:"name"`
	Active *bool  `json:"active" form:"active"`
}

var (
	errInvalidManualCashTransaction      = errors.New("invalid manual cash transaction")
	errFutureManualCashTransaction       = errors.New("manual cash transaction date is in the future")
	errCashTransactionCategoryNotFound   = errors.New("cash transaction category not found")
	errCashTransactionCategoryInactive   = errors.New("cash transaction category is inactive")
	errCashTransactionCategoryDirection  = errors.New("cash transaction category direction mismatch")
	errCashTransactionCategoryNameLocked = errors.New("used cash transaction category names are immutable")
	errInvalidCashTransactionCategory    = errors.New("invalid cash transaction category")
)

func normalizeCashTransactionCategoryName(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validateManualCashTransactionRequest(req manualCashTransactionRequest) error {
	if !validCashTransactionDirection(strings.TrimSpace(req.Direction)) ||
		strings.TrimSpace(req.CategoryID) == "" ||
		normalizeCashTransactionCategoryName(req.Description) == "" ||
		req.Amount <= 0 || strings.TrimSpace(req.RecordDate) == "" {
		return errInvalidManualCashTransaction
	}
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.RecordDate), jakartaLocation)
	if err != nil || date.Format("2006-01-02") != strings.TrimSpace(req.RecordDate) {
		return errInvalidManualCashTransaction
	}
	today := time.Now().In(jakartaLocation)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, jakartaLocation)
	if date.After(today) {
		return errFutureManualCashTransaction
	}
	return nil
}

func (s *Server) recordManualCashTransaction(c *gin.Context) {
	var req manualCashTransactionRequest
	if err := bindRequestWithRupiahAmount(c, &req, "amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_manual_cash_transaction"))
		return
	}
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	record, err := s.insertManualCashTransaction(req, user.ID)
	switch {
	case errors.Is(err, errInvalidManualCashTransaction):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_manual_cash_transaction"))
	case errors.Is(err, errFutureManualCashTransaction):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_manual_cash_transaction_future_date"))
	case errors.Is(err, errCashTransactionCategoryNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_cash_transaction_category_not_found"))
	case errors.Is(err, errCashTransactionCategoryInactive):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_inactive"))
	case errors.Is(err, errCashTransactionCategoryDirection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_cash_transaction_category_direction"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_reference_exists"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondCreatedOrHXRedirect(c, "/admin/transactions", record)
	}
}

func (s *Server) insertManualCashTransaction(req manualCashTransactionRequest, recordedBy string) (gin.H, error) {
	req.Direction = strings.TrimSpace(req.Direction)
	req.CategoryID = strings.TrimSpace(req.CategoryID)
	req.Description = normalizeCashTransactionCategoryName(req.Description)
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	if err := validateManualCashTransactionRequest(req); err != nil {
		return nil, err
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var categoryDirection, categoryName string
	var categoryActive bool
	err = tx.QueryRow(`SELECT direction,name,active FROM cash_transaction_categories WHERE id=$1`, req.CategoryID).Scan(&categoryDirection, &categoryName, &categoryActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errCashTransactionCategoryNotFound
	}
	if err != nil {
		return nil, err
	}
	if !categoryActive {
		return nil, errCashTransactionCategoryInactive
	}
	if categoryDirection != req.Direction {
		return nil, errCashTransactionCategoryDirection
	}
	if req.ReferenceNo == "" {
		req.ReferenceNo, err = s.nextManualCashReference(tx, req.RecordDate)
		if err != nil {
			return nil, err
		}
	}

	id := newID()
	if _, err := tx.Exec(`INSERT INTO manual_cash_transactions (id,transaction_date,direction,category_id,description,amount,reference_no,note,recorded_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, req.RecordDate, req.Direction, req.CategoryID, req.Description, req.Amount, req.ReferenceNo, req.Note, recordedBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return gin.H{"id": id, "transaction_date": req.RecordDate, "direction": req.Direction, "category_id": req.CategoryID, "category": categoryName, "description": req.Description, "amount": req.Amount, "reference_no": req.ReferenceNo, "note": req.Note, "recorded_by": recordedBy}, nil
}

func (s *Server) nextManualCashReference(tx *sql.Tx, transactionDate string) (string, error) {
	prefix, err := manualCashReferencePrefix(transactionDate)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO manual_cash_transaction_sequences (transaction_date,next_sequence) VALUES ($1,1) ON CONFLICT(transaction_date) DO NOTHING`, transactionDate); err != nil {
		return "", err
	}
	query := `SELECT next_sequence FROM manual_cash_transaction_sequences WHERE transaction_date=$1` + rowLockClause(s.db)
	for {
		var sequence int
		if err := tx.QueryRow(query, transactionDate).Scan(&sequence); err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s%04d", prefix, sequence)
		if _, err := tx.Exec(`UPDATE manual_cash_transaction_sequences SET next_sequence=$1 WHERE transaction_date=$2`, sequence+1, transactionDate); err != nil {
			return "", err
		}
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE reference_no=$1`, candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}

func (s *Server) nextManualCashReferencePreview(transactionDate string) (string, error) {
	prefix, err := manualCashReferencePrefix(transactionDate)
	if err != nil {
		return "", err
	}
	rows, err := s.db.Query(`SELECT reference_no FROM manual_cash_transactions WHERE reference_no LIKE $1`, prefix+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	maximum := 0
	for rows.Next() {
		var reference string
		if err := rows.Scan(&reference); err != nil {
			return "", err
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(reference, prefix))
		if err == nil && sequence > maximum {
			maximum = sequence
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, maximum+1), nil
}

func manualCashReferencePrefix(transactionDate string) (string, error) {
	date, err := time.Parse("2006-01-02", transactionDate)
	if err != nil {
		return "", err
	}
	return "KAS-" + date.Format("20060102") + "-", nil
}

func (s *Server) manualCashNet() (int64, error) {
	var net int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN direction='cash_in' THEN amount ELSE -amount END),0) FROM manual_cash_transactions`).Scan(&net)
	return net, err
}

func (s *Server) manualCashTotals(dateFrom, dateTo string) (income, expense, incomeCount, expenseCount int64, err error) {
	err = s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN direction='cash_in' THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN direction='cash_out' THEN amount ELSE 0 END),0),
		COUNT(CASE WHEN direction='cash_in' THEN 1 END),
		COUNT(CASE WHEN direction='cash_out' THEN 1 END)
		FROM manual_cash_transactions
		WHERE transaction_date >= $1 AND transaction_date <= $2`, dateFrom, dateTo).Scan(&income, &expense, &incomeCount, &expenseCount)
	return
}

func (s *Server) cashTransactionCategoriesForAdmin(includeInactive bool) ([]CashTransactionCategory, error) {
	query := `SELECT id,direction,name,active,CAST(created_at AS TEXT),CAST(updated_at AS TEXT) FROM cash_transaction_categories`
	if !includeInactive {
		query += ` WHERE active=TRUE`
	}
	query += ` ORDER BY direction,name`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var categories []CashTransactionCategory
	for rows.Next() {
		var category CashTransactionCategory
		if err := rows.Scan(&category.ID, &category.Direction, &category.Name, &category.Active, &category.CreatedAt, &category.UpdatedAt); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func (s *Server) createCashTransactionCategory(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req cashTransactionCategoryRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	category, err := s.insertCashTransactionCategory(actor.ID, req)
	switch {
	case errors.Is(err, errInvalidCashTransactionCategory):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_category_exists"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondCreatedOrHXRedirect(c, "/admin/transactions/categories", category)
	}
}

func (s *Server) insertCashTransactionCategory(actorID string, req cashTransactionCategoryRequest) (CashTransactionCategory, error) {
	req.Direction = strings.TrimSpace(req.Direction)
	req.Name = normalizeCashTransactionCategoryName(req.Name)
	if !validCashTransactionDirection(req.Direction) || req.Name == "" {
		return CashTransactionCategory{}, errInvalidCashTransactionCategory
	}
	category := CashTransactionCategory{ID: newID(), Direction: req.Direction, Name: req.Name, Active: true}
	tx, err := s.db.Begin()
	if err != nil {
		return CashTransactionCategory{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO cash_transaction_categories (id,direction,name,active,created_by) VALUES ($1,$2,$3,TRUE,$4)`, category.ID, category.Direction, category.Name, actorID); err != nil {
		return CashTransactionCategory{}, err
	}
	if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,new_name) VALUES ($1,$2,$3,'created',$4)`, newID(), category.ID, actorID, category.Name); err != nil {
		return CashTransactionCategory{}, err
	}
	if err := tx.Commit(); err != nil {
		return CashTransactionCategory{}, err
	}
	return category, nil
}

func (s *Server) updateCashTransactionCategory(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req cashTransactionCategoryUpdateRequest
	if err := c.ShouldBind(&req); err != nil || req.Active == nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	req.Name = normalizeCashTransactionCategoryName(req.Name)
	if req.Name == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category"))
		return
	}
	category, err := s.updateCashTransactionCategoryByID(actor.ID, c.Param("id"), req)
	switch {
	case errors.Is(err, errCashTransactionCategoryNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_cash_transaction_category_not_found"))
	case errors.Is(err, errCashTransactionCategoryNameLocked):
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_cash_transaction_category_name_locked"))
	case isUniqueViolation(err):
		respondError(c, http.StatusConflict, "DUPLICATE_DATA", translate(languageFromRequest(c), "error_cash_transaction_category_exists"))
	case err != nil:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
	default:
		respondOKOrHXRedirect(c, "/admin/transactions/categories", category)
	}
}

func (s *Server) updateCashTransactionCategoryByID(actorID, id string, req cashTransactionCategoryUpdateRequest) (CashTransactionCategory, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return CashTransactionCategory{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current CashTransactionCategory
	err = tx.QueryRow(`SELECT id,direction,name,active,CAST(created_at AS TEXT),CAST(updated_at AS TEXT) FROM cash_transaction_categories WHERE id=$1`, id).Scan(&current.ID, &current.Direction, &current.Name, &current.Active, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CashTransactionCategory{}, errCashTransactionCategoryNotFound
	}
	if err != nil {
		return CashTransactionCategory{}, err
	}
	if current.Name != req.Name {
		var used int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE category_id=$1`, id).Scan(&used); err != nil {
			return CashTransactionCategory{}, err
		}
		if used > 0 {
			return CashTransactionCategory{}, errCashTransactionCategoryNameLocked
		}
	}
	if _, err := tx.Exec(`UPDATE cash_transaction_categories SET name=$1,active=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3`, req.Name, *req.Active, id); err != nil {
		return CashTransactionCategory{}, err
	}
	if current.Name != req.Name {
		if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,'renamed',$4,$5)`, newID(), id, actorID, current.Name, req.Name); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if current.Active != *req.Active {
		action := "deactivated"
		if *req.Active {
			action = "reactivated"
		}
		if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), id, actorID, action, current.Name, current.Name); err != nil {
			return CashTransactionCategory{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CashTransactionCategory{}, err
	}
	current.Name, current.Active = req.Name, *req.Active
	return current, nil
}
