package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type AccountingMapping struct {
	ID              string `json:"id"`
	MappingKey      string `json:"mapping_key"`
	TransactionType string `json:"transaction_type"`
	Component       string `json:"component"`
	LoanType        string `json:"loan_type,omitempty"`
	COACode         string `json:"coa_code"`
	EffectiveFrom   string `json:"effective_from,omitempty"`
	EffectiveTo     string `json:"effective_to,omitempty"`
	Active          bool   `json:"active"`
}

var (
	errInvalidAccountingMapping  = errors.New("invalid accounting mapping")
	errAccountingMappingConflict = errors.New("accounting mapping date range conflicts with an active mapping")
	errAccountingMappingNotFound = errors.New("accounting mapping not found")
)

type accountingMappingRequest struct {
	TransactionType string `json:"transaction_type" form:"transaction_type"`
	Component       string `json:"component" form:"component"`
	LoanType        string `json:"loan_type" form:"loan_type"`
	COACode         string `json:"coa_code" form:"coa_code"`
	EffectiveFrom   string `json:"effective_from" form:"effective_from"`
	EffectiveTo     string `json:"effective_to" form:"effective_to"`
	Active          *bool  `json:"active" form:"active"`
}

func normalizeAccountingMappingRequest(req accountingMappingRequest) (accountingMappingRequest, error) {
	req.TransactionType = strings.ToLower(strings.TrimSpace(req.TransactionType))
	req.Component = strings.ToLower(strings.TrimSpace(req.Component))
	req.LoanType = strings.ToLower(strings.TrimSpace(req.LoanType))
	req.COACode = strings.TrimSpace(req.COACode)
	req.EffectiveFrom = strings.TrimSpace(req.EffectiveFrom)
	req.EffectiveTo = strings.TrimSpace(req.EffectiveTo)
	if req.TransactionType == "" || req.Component == "" || req.COACode == "" {
		return accountingMappingRequest{}, errInvalidAccountingMapping
	}
	for _, value := range []string{req.EffectiveFrom, req.EffectiveTo} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return accountingMappingRequest{}, errInvalidAccountingMapping
		}
	}
	if req.EffectiveFrom != "" && req.EffectiveTo != "" && req.EffectiveFrom > req.EffectiveTo {
		return accountingMappingRequest{}, errInvalidAccountingMapping
	}
	if req.Active == nil {
		active := true
		req.Active = &active
	}
	return req, nil
}

func accountingMappingKey(req accountingMappingRequest) string {
	return strings.Join([]string{req.TransactionType, req.Component, req.LoanType, req.EffectiveFrom, req.EffectiveTo}, "|")
}

func (s *Server) accountingMappings(c *gin.Context) {
	result, err := s.accountingMappingsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"mappings": result})
}

func (s *Server) accountingMappingsForAdmin() ([]AccountingMapping, error) {
	rows, err := s.db.Query(`SELECT id,mapping_key,transaction_type,component,COALESCE(loan_type,''),COALESCE(coa_code,''),COALESCE(effective_from,''),COALESCE(effective_to,''),active FROM accounting_mappings ORDER BY transaction_type,component,loan_type,effective_from,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AccountingMapping, 0)
	for rows.Next() {
		var item AccountingMapping
		if err := rows.Scan(&item.ID, &item.MappingKey, &item.TransactionType, &item.Component, &item.LoanType, &item.COACode, &item.EffectiveFrom, &item.EffectiveTo, &item.Active); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) saveAccountingMapping(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	var req accountingMappingRequest
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_accounting_mapping"))
		return
	}
	req, err := normalizeAccountingMappingRequest(req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_accounting_mapping"))
		return
	}

	mappingID := strings.TrimSpace(c.Param("id"))
	created := mappingID == ""
	if created {
		mappingID = newID()
	}
	mappingKey := accountingMappingKey(req)
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := s.coaAccountForPostingTx(tx, req.COACode); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_transaction_coa"))
		return
	}
	if created {
		var existingID string
		if err := tx.QueryRow(`SELECT id FROM accounting_mappings WHERE mapping_key=$1`, mappingKey).Scan(&existingID); err == nil {
			mappingID = existingID
			created = false
		} else if !errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
	}
	active := *req.Active
	var conflictID string
	err = tx.QueryRow(`SELECT id FROM accounting_mappings
		WHERE transaction_type=$1 AND component=$2 AND loan_type=$3 AND active=TRUE AND $7=TRUE
		  AND id<>$4
		  AND (effective_from IS NULL OR $5='' OR effective_from <= $5)
		  AND (effective_to IS NULL OR $6='' OR effective_to >= $6)
		LIMIT 1`, req.TransactionType, req.Component, req.LoanType, mappingID, req.EffectiveTo, req.EffectiveFrom, active).Scan(&conflictID)
	if err == nil {
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_accounting_mapping_conflict"))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	var oldCOA string
	if !created {
		if err := tx.QueryRow(`SELECT COALESCE(coa_code,'') FROM accounting_mappings WHERE id=$1`, mappingID).Scan(&oldCOA); errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_accounting_mapping_not_found"))
			return
		} else if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		if _, err := tx.Exec(`UPDATE accounting_mappings SET mapping_key=$1,transaction_type=$2,component=$3,loan_type=$4,coa_code=$5,effective_from=$6,effective_to=$7,active=$8,updated_at=CURRENT_TIMESTAMP WHERE id=$9`, mappingKey, req.TransactionType, req.Component, req.LoanType, req.COACode, nullIfEmpty(req.EffectiveFrom), nullIfEmpty(req.EffectiveTo), active, mappingID); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
	} else if _, err := tx.Exec(`INSERT INTO accounting_mappings (id,mapping_key,transaction_type,component,loan_type,coa_code,effective_from,effective_to,active,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, mappingID, mappingKey, req.TransactionType, req.Component, req.LoanType, req.COACode, nullIfEmpty(req.EffectiveFrom), nullIfEmpty(req.EffectiveTo), active, user.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if err := recordFinancialTransactionAuditTx(tx, mappingID, "accounting_mapping", user.ID, "coa_code", oldCOA, req.COACode, ""); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	c.JSON(map[bool]int{true: http.StatusCreated, false: http.StatusOK}[created], gin.H{"id": mappingID, "mapping_key": mappingKey, "status": "saved"})
}
