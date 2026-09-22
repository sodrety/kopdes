package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type coaImportRow struct {
	ExcelRow      int
	Code          string
	Name          string
	ParentCode    string
	AccountType   string
	Subtype       string
	NormalBalance string
	IsGroup       bool
	Active        bool
	Status        string
	ErrorMessage  string
}

type COAImportResult struct {
	BatchID string         `json:"batch_id"`
	Status  string         `json:"status"`
	Valid   int            `json:"valid"`
	Invalid int            `json:"invalid"`
	Rows    []coaImportRow `json:"rows"`
}

var errInvalidCOAImport = errors.New("invalid coa import")

func parseCOABoolean(value string, defaultValue bool) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return defaultValue, nil
	}
	switch normalized {
	case "true", "yes", "y", "1", "active", "posting", "postable":
		return true, nil
	case "false", "no", "n", "0", "inactive", "group", "header":
		return false, nil
	default:
		return false, errors.New("expected true or false")
	}
}

func coaImportHeaderIndexes(row []string) map[string]int {
	result := map[string]int{}
	for index, value := range row {
		key := strings.ToLower(strings.TrimSpace(value))
		key = strings.Join(strings.Fields(key), " ")
		result[key] = index
	}
	return result
}

func coaImportCell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func coaImportRequiredHeader(headers map[string]int, aliases ...string) (int, bool) {
	for _, alias := range aliases {
		if index, ok := headers[alias]; ok {
			return index, true
		}
	}
	return -1, false
}

func parseCOAImportRows(rows [][]string) ([]coaImportRow, error) {
	if len(rows) < 2 {
		return nil, errInvalidCOAImport
	}
	headers := coaImportHeaderIndexes(rows[0])
	codeColumn, hasCode := coaImportRequiredHeader(headers, "code", "account code", "acct code")
	nameColumn, hasName := coaImportRequiredHeader(headers, "name", "account name", "acct name")
	parentColumn, hasParent := coaImportRequiredHeader(headers, "parent code", "parent", "parent account code")
	typeColumn, hasType := coaImportRequiredHeader(headers, "account type", "type")
	subtypeColumn, hasSubtype := coaImportRequiredHeader(headers, "subtype", "account subtype")
	normalColumn, hasNormal := coaImportRequiredHeader(headers, "normal balance", "normal")
	groupColumn, hasGroup := coaImportRequiredHeader(headers, "is group", "group", "posting", "is posting")
	activeColumn, hasActive := coaImportRequiredHeader(headers, "active", "status")
	if !hasCode || !hasName || !hasParent || !hasType || !hasNormal || !hasGroup || !hasActive {
		return nil, errInvalidCOAImport
	}
	if !hasSubtype {
		subtypeColumn = -1
	}
	parsed := make([]coaImportRow, 0, len(rows)-1)
	seenCodes := map[string]int{}
	for index, row := range rows[1:] {
		item := coaImportRow{ExcelRow: index + 2, Code: coaImportCell(row, codeColumn), Name: coaImportCell(row, nameColumn), ParentCode: coaImportCell(row, parentColumn), AccountType: strings.ToLower(coaImportCell(row, typeColumn)), Subtype: strings.ToLower(coaImportCell(row, subtypeColumn)), NormalBalance: strings.ToUpper(coaImportCell(row, normalColumn)), Active: true, Status: "valid"}
		isGroupValue := coaImportCell(row, groupColumn)
		if strings.ToLower(strings.TrimSpace(headersKeyForColumn(headers, groupColumn))) == "is posting" || strings.ToLower(strings.TrimSpace(headersKeyForColumn(headers, groupColumn))) == "posting" {
			posting, err := parseCOABoolean(isGroupValue, true)
			if err != nil {
				item.Status, item.ErrorMessage = "invalid", "posting flag must be true or false"
			} else {
				item.IsGroup = !posting
			}
		} else {
			item.IsGroup, _ = parseCOABoolean(isGroupValue, false)
			if _, err := parseCOABoolean(isGroupValue, false); err != nil {
				item.Status, item.ErrorMessage = "invalid", "group flag must be true or false"
			}
		}
		if active, err := parseCOABoolean(coaImportCell(row, activeColumn), true); err != nil {
			item.Status, item.ErrorMessage = "invalid", "active flag must be true or false"
		} else {
			item.Active = active
		}
		if item.Code == "" || item.Name == "" || item.AccountType == "" || item.NormalBalance == "" {
			item.Status, item.ErrorMessage = "invalid", "code, name, parent code, account type, normal balance, group/posting, and active are required"
		}
		if item.NormalBalance != "D" && item.NormalBalance != "C" {
			item.Status, item.ErrorMessage = "invalid", "normal balance must be D or C"
		}
		switch item.AccountType {
		case "asset", "liability", "equity", "revenue", "expense":
		default:
			item.Status, item.ErrorMessage = "invalid", "account type is invalid"
		}
		if previous, exists := seenCodes[item.Code]; exists {
			item.Status, item.ErrorMessage = "invalid", "duplicate code also appears on Excel row "+strconv.Itoa(previous)
		} else if item.Code != "" {
			seenCodes[item.Code] = item.ExcelRow
		}
		parsed = append(parsed, item)
	}
	for index := range parsed {
		item := &parsed[index]
		if item.Status != "valid" || item.ParentCode == "" {
			continue
		}
		if item.ParentCode == item.Code {
			item.Status, item.ErrorMessage = "invalid", "parent code cannot equal code"
			continue
		}
		if _, ok := seenCodes[item.ParentCode]; !ok {
			item.Status, item.ErrorMessage = "invalid", "parent code is not present in the file"
		}
	}
	return parsed, nil
}

func headersKeyForColumn(headers map[string]int, column int) string {
	for key, value := range headers {
		if value == column {
			return key
		}
	}
	return ""
}

func (s *Server) importCOADraft(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_coa_import"))
		return
	}
	defer file.Close()
	workbook, err := excelize.OpenReader(file)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_coa_import"))
		return
	}
	defer workbook.Close()
	rows, err := workbook.GetRows(workbook.GetSheetName(0))
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_coa_import"))
		return
	}
	parsed, err := parseCOAImportRows(rows)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_coa_import"))
		return
	}
	batchID := newID()
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO coa_import_batches (id,file_name,status,uploaded_by) VALUES ($1,$2,'draft',$3)`, batchID, header.Filename, user.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	result := COAImportResult{BatchID: batchID, Status: "draft", Rows: parsed}
	for _, item := range parsed {
		if item.Status == "valid" {
			result.Valid++
		} else {
			result.Invalid++
		}
		if _, err := tx.Exec(`INSERT INTO coa_import_rows (id,batch_id,excel_row,code,name,parent_code,account_type,subtype,normal_balance,is_group,active,status,error_message) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, newID(), batchID, item.ExcelRow, item.Code, item.Name, item.ParentCode, item.AccountType, item.Subtype, item.NormalBalance, item.IsGroup, item.Active, item.Status, item.ErrorMessage); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if result.Invalid > 0 {
		c.JSON(http.StatusUnprocessableEntity, result)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *Server) activateCOADraft(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	batchID := strings.TrimSpace(c.Param("id"))
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRow(`SELECT status FROM coa_import_batches WHERE id=$1`, batchID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_coa_import_not_found"))
		return
	} else if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	} else if status != "draft" {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_coa_import_not_draft"))
		return
	}
	var invalid int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM coa_import_rows WHERE batch_id=$1 AND status<>'valid'`, batchID).Scan(&invalid); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if invalid > 0 {
		respondError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_coa_import_has_errors"))
		return
	}
	for _, systemKey := range []string{"CASH", "BANK"} {
		var active, isGroup bool
		if err := tx.QueryRow(`SELECT active,is_group FROM coa_accounts WHERE system_key=$1`, systemKey).Scan(&active, &isGroup); err != nil || !active || isGroup {
			respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_coa_activation_failed"))
			return
		}
	}
	rows, err := tx.Query(`SELECT code,name,parent_code,account_type,subtype,normal_balance,is_group,active FROM coa_import_rows WHERE batch_id=$1 ORDER BY excel_row`, batchID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	items := make([]coaImportRow, 0)
	for rows.Next() {
		var item coaImportRow
		if err := rows.Scan(&item.Code, &item.Name, &item.ParentCode, &item.AccountType, &item.Subtype, &item.NormalBalance, &item.IsGroup, &item.Active); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if err := rows.Close(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	ordered := make([]coaImportRow, 0, len(items))
	remaining := append([]coaImportRow(nil), items...)
	seenCodes := map[string]bool{}
	for len(remaining) > 0 {
		progress := false
		next := make([]coaImportRow, 0, len(remaining))
		for _, item := range remaining {
			if item.ParentCode != "" && !seenCodes[item.ParentCode] {
				next = append(next, item)
				continue
			}
			ordered = append(ordered, item)
			seenCodes[item.Code] = true
			progress = true
		}
		if !progress {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_coa_activation_failed"))
			return
		}
		remaining = next
	}
	for _, item := range ordered {
		var existing struct {
			ID, AccountType, Subtype, NormalBalance, ParentCode string
			IsGroup, Active                                     bool
			SystemKey                                           string
		}
		err := tx.QueryRow(`SELECT id,account_type,subtype,normal_balance,COALESCE(parent_code,''),is_group,active,COALESCE(system_key,'') FROM coa_accounts WHERE code=$1`, item.Code).Scan(&existing.ID, &existing.AccountType, &existing.Subtype, &existing.NormalBalance, &existing.ParentCode, &existing.IsGroup, &existing.Active, &existing.SystemKey)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.Exec(`INSERT INTO coa_accounts (id,code,name,parent_code,account_type,subtype,normal_balance,is_group,active,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, newID(), item.Code, item.Name, nullIfEmpty(item.ParentCode), item.AccountType, item.Subtype, item.NormalBalance, item.IsGroup, item.Active, user.ID); err != nil {
				respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_coa_activation_failed"))
				return
			}
			continue
		}
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		if existing.SystemKey != "" {
			continue
		}
		var used int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM financial_journal_lines WHERE coa_code=$1`, item.Code).Scan(&used); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		if used > 0 && (existing.AccountType != item.AccountType || existing.Subtype != item.Subtype || existing.NormalBalance != item.NormalBalance || existing.ParentCode != item.ParentCode || existing.IsGroup != item.IsGroup) {
			respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", translate(languageFromRequest(c), "error_coa_used_semantics_locked"))
			return
		}
		if _, err := tx.Exec(`UPDATE coa_accounts SET name=$1,parent_code=$2,account_type=$3,subtype=$4,normal_balance=$5,is_group=$6,active=$7,updated_at=CURRENT_TIMESTAMP WHERE code=$8`, item.Name, nullIfEmpty(item.ParentCode), item.AccountType, item.Subtype, item.NormalBalance, item.IsGroup, item.Active, item.Code); err != nil {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_coa_activation_failed"))
			return
		}
	}
	if _, err := tx.Exec(`UPDATE coa_import_batches SET status='activated',activated_at=CURRENT_TIMESTAMP WHERE id=$1`, batchID); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch_id": batchID, "status": "activated"})
}

func (s *Server) coaImportBatch(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("id"))
	var result COAImportResult
	if err := s.db.QueryRow(`SELECT id,status FROM coa_import_batches WHERE id=$1`, batchID).Scan(&result.BatchID, &result.Status); errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_coa_import_not_found"))
		return
	} else if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	rows, err := s.db.Query(`SELECT excel_row,code,name,parent_code,account_type,subtype,normal_balance,is_group,active,status,error_message FROM coa_import_rows WHERE batch_id=$1 ORDER BY excel_row`, batchID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item coaImportRow
		if err := rows.Scan(&item.ExcelRow, &item.Code, &item.Name, &item.ParentCode, &item.AccountType, &item.Subtype, &item.NormalBalance, &item.IsGroup, &item.Active, &item.Status, &item.ErrorMessage); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		result.Rows = append(result.Rows, item)
		if item.Status == "valid" {
			result.Valid++
		} else {
			result.Invalid++
		}
	}
	if err := rows.Err(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	c.JSON(http.StatusOK, result)
}
