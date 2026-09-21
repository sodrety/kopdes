package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type cashTransactionCategoryImportRow struct {
	CategoryKey   string
	AccountCode   string
	Name          string
	Direction     string
	ParentKey     string
	NormalBalance string
	IsGroup       bool
	Active        bool
}

var errInvalidCashTransactionCategoryTemplate = errors.New("invalid cash transaction category template")

func (s *Server) downloadCashTransactionCategoryTemplate(c *gin.Context) {
	c.Header("Content-Disposition", `attachment; filename="cash-transaction-category-template.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", cashTransactionCategoryTemplateXLSX)
}

func (s *Server) importCashTransactionCategoryTemplate(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(languageFromRequest(c), "error_authentication_required"))
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size <= 0 {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category_template"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_cash_transaction_category_template"))
		return
	}
	defer file.Close()
	rows, err := parseCashTransactionCategoryTemplate(file)
	if err != nil {
		respondCashTransactionCategoryImportError(c, err)
		return
	}
	imported, err := s.applyCashTransactionCategoryImport(actor.ID, rows)
	if err != nil {
		respondCashTransactionCategoryImportError(c, err)
		return
	}
	respondOKOrHXRedirect(c, "/admin/transactions/categories", gin.H{"imported": imported})
}

func parseCashTransactionCategoryTemplate(reader io.Reader) ([]cashTransactionCategoryImportRow, error) {
	workbook, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, errInvalidCashTransactionCategoryTemplate
	}
	defer workbook.Close()
	required := [][]string{
		{"category key", "kunci kategori"},
		{"category name", "nama kategori"},
		{"direction", "arah kas"},
		{"active", "aktif", "aktif?"},
	}
	var rows [][]string
	var headers map[string]int
	headerRow := -1
	for _, sheet := range workbook.GetSheetList() {
		candidateRows, readErr := workbook.GetRows(sheet)
		if readErr != nil || len(candidateRows) < 2 {
			continue
		}
		for candidateHeaderRow, candidateValues := range candidateRows {
			if candidateHeaderRow > 9 {
				break
			}
			candidateHeaders := cashTransactionCategoryHeaderIndexes(candidateValues)
			matches := true
			for _, aliases := range required {
				if _, ok := firstCashCategoryHeader(candidateHeaders, aliases...); !ok {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			rows, headers, headerRow = candidateRows, candidateHeaders, candidateHeaderRow
			break
		}
		if headerRow >= 0 {
			break
		}
	}
	if len(rows) <= headerRow+1 || headers == nil || headerRow < 0 {
		return nil, errInvalidCashTransactionCategoryTemplate
	}
	if len(rows) > 1001 {
		return nil, errInvalidCashTransactionCategoryTemplate
	}

	result := make([]cashTransactionCategoryImportRow, 0, len(rows)-1)
	seenKeys := map[string]bool{}
	for rowIndex, values := range rows[headerRow+1:] {
		if allCashCategoryCellsEmpty(values) {
			continue
		}
		excelRow := rowIndex + 2
		direction, directionOK := normalizeCashTransactionImportDirection(valueForAliases(values, headers, "direction", "arah kas"))
		balanceValue, hasBalance := valueForAliasesWithPresence(values, headers, "normal balance", "saldo normal", "arah akun")
		normalBalance, normalBalanceOK := normalizeCashTransactionImportNormalBalance(balanceValue)
		if !hasBalance {
			normalBalanceOK = true
		}
		category := cashTransactionCategoryImportRow{
			CategoryKey:   normalizeCashTransactionCategoryKey(valueForAliases(values, headers, "category key", "kunci kategori")),
			AccountCode:   normalizeCashTransactionCategoryAccountCode(valueForAliases(values, headers, "account code", "kode akun")),
			Name:          normalizeCashTransactionCategoryName(valueForAliases(values, headers, "category name", "nama kategori")),
			Direction:     direction,
			ParentKey:     normalizeCashTransactionCategoryKey(valueForAliases(values, headers, "parent key", "kunci induk")),
			NormalBalance: normalBalance,
			Active:        true,
		}
		if isGroup, ok := valueForAliasesWithPresence(values, headers, "is group", "grup?"); ok && isGroup != "" {
			category.IsGroup, err = parseCashCategoryBoolean(isGroup)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", excelRow, errInvalidCashTransactionCategoryTemplate)
			}
		}
		if active, ok := valueForAliasesWithPresence(values, headers, "active", "aktif?"); ok {
			category.Active, err = parseCashCategoryBoolean(active)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", excelRow, errInvalidCashTransactionCategoryTemplate)
			}
		}
		if !directionOK || !normalBalanceOK || !validCashTransactionCategoryKey(category.CategoryKey) || category.Name == "" || len(category.Name) > 100 || !validCashTransactionDirection(category.Direction) || !validCashTransactionNormalBalance(category.NormalBalance) {
			return nil, fmt.Errorf("row %d: %w", excelRow, errInvalidCashTransactionCategoryTemplate)
		}
		if seenKeys[category.CategoryKey] {
			return nil, fmt.Errorf("row %d: %w", excelRow, errInvalidCashTransactionCategoryTemplate)
		}
		seenKeys[category.CategoryKey] = true
		result = append(result, category)
	}
	if len(result) == 0 {
		return nil, errInvalidCashTransactionCategoryTemplate
	}
	return result, nil
}

func cashTransactionCategoryHeaderIndexes(values []string) map[string]int {
	indexes := map[string]int{}
	for index, value := range values {
		name := strings.ToLower(strings.TrimSpace(value))
		name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
		name = strings.Join(strings.Fields(name), " ")
		indexes[name] = index
	}
	return indexes
}

func firstCashCategoryHeader(headers map[string]int, aliases ...string) (int, bool) {
	for _, alias := range aliases {
		if column, ok := headers[alias]; ok {
			return column, true
		}
	}
	return 0, false
}

func valueForAliases(values []string, headers map[string]int, aliases ...string) string {
	value, _ := valueForAliasesWithPresence(values, headers, aliases...)
	return value
}

func valueForAliasesWithPresence(values []string, headers map[string]int, aliases ...string) (string, bool) {
	column, ok := firstCashCategoryHeader(headers, aliases...)
	if !ok || column >= len(values) {
		return "", false
	}
	return strings.TrimSpace(values[column]), true
}

func normalizeCashTransactionImportDirection(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cash_in", "kas masuk", "masuk":
		return "cash_in", true
	case "cash_out", "kas keluar", "keluar":
		return "cash_out", true
	default:
		return "", false
	}
}

func normalizeCashTransactionImportNormalBalance(value string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "":
		return "", true
	case "D", "DEBIT":
		return "D", true
	case "C", "K", "KREDIT", "CREDIT":
		return "C", true
	default:
		return "", false
	}
}

func allCashCategoryCellsEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseCashCategoryBoolean(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y", "ya", "active", "aktif":
		return true, nil
	case "false", "0", "no", "n", "tidak", "inactive", "nonaktif":
		return false, nil
	default:
		return false, errInvalidCashTransactionCategoryTemplate
	}
}

func respondCashTransactionCategoryImportError(c *gin.Context, err error) {
	messageKey := "error_invalid_cash_transaction_category_template"
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, errInvalidCashTransactionCategoryTemplate):
		messageKey = "error_invalid_cash_transaction_category_template"
	case errors.Is(err, errCashTransactionCategoryNameLocked):
		messageKey = "error_cash_transaction_category_name_locked"
	case errors.Is(err, errCashTransactionCategoryDirectionLocked):
		messageKey = "error_cash_transaction_category_direction_locked"
	case errors.Is(err, errCashTransactionCategoryParentNotFound):
		messageKey = "error_cash_transaction_category_parent_not_found"
	case errors.Is(err, errCashTransactionCategoryParentDirection):
		messageKey = "error_cash_transaction_category_parent_direction"
	case errors.Is(err, errCashTransactionCategoryCycle):
		messageKey = "error_cash_transaction_category_cycle"
	case isUniqueViolation(err):
		messageKey = "error_cash_transaction_category_exists"
	default:
		status = http.StatusInternalServerError
		messageKey = "error.Internal server error"
	}
	code := "VALIDATION_ERROR"
	if status == http.StatusInternalServerError {
		code = "INTERNAL_SERVER_ERROR"
	}
	respondError(c, status, code, translate(languageFromRequest(c), messageKey))
}

type existingCashTransactionCategory struct {
	ID            string
	CategoryKey   string
	Direction     string
	Name          string
	AccountCode   string
	ParentID      string
	NormalBalance string
	IsGroup       bool
	Active        bool
}

func (s *Server) applyCashTransactionCategoryImport(actorID string, rows []cashTransactionCategoryImportRow) (int, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	existingByKey := map[string]existingCashTransactionCategory{}
	existingRows, err := tx.Query(`SELECT id,COALESCE(NULLIF(category_key,''),id),direction,name,COALESCE(account_code,''),COALESCE(parent_id,''),COALESCE(normal_balance,''),is_group,active FROM cash_transaction_categories`)
	if err != nil {
		return 0, err
	}
	for existingRows.Next() {
		var category existingCashTransactionCategory
		if err := existingRows.Scan(&category.ID, &category.CategoryKey, &category.Direction, &category.Name, &category.AccountCode, &category.ParentID, &category.NormalBalance, &category.IsGroup, &category.Active); err != nil {
			existingRows.Close()
			return 0, err
		}
		existingByKey[normalizeCashTransactionCategoryKey(category.CategoryKey)] = category
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		return 0, err
	}
	existingRows.Close()

	targetIDs := map[string]string{}
	for _, row := range rows {
		if current, ok := existingByKey[row.CategoryKey]; ok {
			targetIDs[row.CategoryKey] = current.ID
		} else {
			targetIDs[row.CategoryKey] = newID()
		}
	}
	parentIDs := map[string]string{}
	parentDirections := map[string]string{}
	parentByID := map[string]string{}
	for key, current := range existingByKey {
		parentDirections[key] = current.Direction
		parentByID[current.ID] = current.ParentID
	}
	for _, row := range rows {
		if row.ParentKey == "" {
			continue
		}
		parentID, inImport := targetIDs[row.ParentKey]
		if !inImport {
			current, ok := existingByKey[row.ParentKey]
			if !ok {
				return 0, errCashTransactionCategoryParentNotFound
			}
			parentID = current.ID
		}
		parentIDs[row.CategoryKey] = parentID
		parentDirection := parentDirections[row.ParentKey]
		if inImport {
			for _, candidate := range rows {
				if candidate.CategoryKey == row.ParentKey {
					parentDirection = candidate.Direction
					break
				}
			}
		}
		if parentDirection != row.Direction {
			return 0, errCashTransactionCategoryParentDirection
		}
	}
	for _, row := range rows {
		prospectiveID := targetIDs[row.CategoryKey]
		parentByID[prospectiveID] = parentIDs[row.CategoryKey]
	}
	for _, row := range rows {
		seen := map[string]bool{}
		currentID := targetIDs[row.CategoryKey]
		for currentID != "" {
			if seen[currentID] {
				return 0, errCashTransactionCategoryCycle
			}
			seen[currentID] = true
			parentID := parentByID[currentID]
			if parentID == "" {
				break
			}
			currentID = parentID
		}
	}

	for _, row := range rows {
		current, exists := existingByKey[row.CategoryKey]
		parentID := parentIDs[row.CategoryKey]
		if !exists {
			if _, err := tx.Exec(`INSERT INTO cash_transaction_categories (id,category_key,account_code,parent_id,direction,name,normal_balance,is_group,active,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, targetIDs[row.CategoryKey], row.CategoryKey, nullIfEmpty(row.AccountCode), nullIfEmpty(parentID), row.Direction, row.Name, nullIfEmpty(row.NormalBalance), row.IsGroup, row.Active, actorID); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,new_name) VALUES ($1,$2,$3,'created',$4)`, newID(), targetIDs[row.CategoryKey], actorID, row.Name); err != nil {
				return 0, err
			}
			continue
		}
		if current.Name != row.Name || current.Direction != row.Direction {
			var used int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE category_id=$1`, current.ID).Scan(&used); err != nil {
				return 0, err
			}
			if used > 0 && current.Name != row.Name {
				return 0, errCashTransactionCategoryNameLocked
			}
			if used > 0 && current.Direction != row.Direction {
				return 0, errCashTransactionCategoryDirectionLocked
			}
		}
		if _, err := tx.Exec(`UPDATE cash_transaction_categories SET account_code=$1,parent_id=$2,direction=$3,name=$4,normal_balance=$5,is_group=$6,active=$7,updated_at=CURRENT_TIMESTAMP WHERE id=$8`, nullIfEmpty(row.AccountCode), nullIfEmpty(parentID), row.Direction, row.Name, nullIfEmpty(row.NormalBalance), row.IsGroup, row.Active, current.ID); err != nil {
			return 0, err
		}
		if current.Name != row.Name {
			if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,'renamed',$4,$5)`, newID(), current.ID, actorID, current.Name, row.Name); err != nil {
				return 0, err
			}
		}
		if current.Active != row.Active {
			action := "deactivated"
			if row.Active {
				action = "reactivated"
			}
			if _, err := tx.Exec(`INSERT INTO cash_transaction_category_audits (id,category_id,actor_id,action,old_name,new_name) VALUES ($1,$2,$3,$4,$5,$6)`, newID(), current.ID, actorID, action, current.Name, row.Name); err != nil {
				return 0, err
			}
		}
	}
	for _, parentID := range parentIDs {
		if _, err := tx.Exec(`UPDATE cash_transaction_categories SET is_group=TRUE WHERE id=$1`, parentID); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`UPDATE cash_transaction_categories SET is_group=TRUE WHERE id IN (SELECT DISTINCT parent_id FROM cash_transaction_categories WHERE parent_id IS NOT NULL)`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rows), nil
}
