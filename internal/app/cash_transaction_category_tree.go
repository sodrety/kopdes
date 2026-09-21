package app

import (
	"database/sql"
	"strings"
)

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func cashTransactionCategoryKey(direction, accountCode, name string) string {
	base := strings.ToLower(strings.TrimSpace(accountCode))
	if base == "" {
		base = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), "-"))
	}
	base = strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ".", "-", ",", "-", "(", "", ")", "").Replace(base)
	base = strings.Trim(base, "-_")
	key := strings.Trim(strings.ToLower(direction), "-_") + "-" + base
	if len(key) > 100 {
		key = key[:100]
	}
	return key
}

func validateCashTransactionCategoryParentTx(tx *sql.Tx, categoryID, parentID, direction string) error {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil
	}
	if parentID == categoryID {
		return errCashTransactionCategoryCycle
	}
	var parentDirection string
	var parentParentID sql.NullString
	if err := tx.QueryRow(`SELECT direction,parent_id FROM cash_transaction_categories WHERE id=$1`, parentID).Scan(&parentDirection, &parentParentID); err != nil {
		if err == sql.ErrNoRows {
			return errCashTransactionCategoryParentNotFound
		}
		return err
	}
	if parentDirection != direction {
		return errCashTransactionCategoryParentDirection
	}
	seen := map[string]bool{categoryID: true}
	currentID := parentID
	for currentID != "" {
		if seen[currentID] {
			return errCashTransactionCategoryCycle
		}
		seen[currentID] = true
		var nextParent sql.NullString
		if err := tx.QueryRow(`SELECT parent_id FROM cash_transaction_categories WHERE id=$1`, currentID).Scan(&nextParent); err != nil {
			if err == sql.ErrNoRows {
				return errCashTransactionCategoryParentNotFound
			}
			return err
		}
		currentID = strings.TrimSpace(nextParent.String)
	}
	return nil
}

func flattenCashTransactionCategories(categories []CashTransactionCategory) []CashTransactionCategory {
	children := map[string][]CashTransactionCategory{}
	roots := map[string][]CashTransactionCategory{}
	names := map[string]string{}
	for _, category := range categories {
		names[category.ID] = category.Name
		if category.ParentID == "" {
			roots[category.Direction] = append(roots[category.Direction], category)
			continue
		}
		children[category.ParentID] = append(children[category.ParentID], category)
	}
	for _, list := range roots {
		sortCashTransactionCategories(list)
	}
	for parentID, list := range children {
		sortCashTransactionCategories(list)
		children[parentID] = list
	}

	result := make([]CashTransactionCategory, 0, len(categories))
	visited := map[string]bool{}
	var walk func(CashTransactionCategory, int)
	walk = func(category CashTransactionCategory, depth int) {
		if visited[category.ID] {
			return
		}
		visited[category.ID] = true
		category.Depth = depth
		category.DisplayName = strings.Repeat("\u00a0\u00a0", depth) + category.Name
		category.ParentName = names[category.ParentID]
		category.HasChildren = category.HasChildren || len(children[category.ID]) > 0
		result = append(result, category)
		for _, child := range children[category.ID] {
			walk(child, depth+1)
		}
	}
	for _, direction := range []string{"cash_in", "cash_out"} {
		for _, root := range roots[direction] {
			walk(root, 0)
		}
	}
	// Keep malformed legacy rows visible instead of silently dropping them.
	if len(result) != len(categories) {
		remaining := make([]CashTransactionCategory, 0, len(categories)-len(result))
		for _, category := range categories {
			if !visited[category.ID] {
				remaining = append(remaining, category)
			}
		}
		sortCashTransactionCategories(remaining)
		for _, category := range remaining {
			walk(category, 0)
		}
	}
	return result
}

func cashTransactionCategoryLeaves(categories []CashTransactionCategory) []CashTransactionCategory {
	result := make([]CashTransactionCategory, 0, len(categories))
	for _, category := range categories {
		if !category.IsGroup && !category.HasChildren {
			result = append(result, category)
		}
	}
	return result
}

func sortCashTransactionCategories(categories []CashTransactionCategory) {
	for i := 1; i < len(categories); i++ {
		current := categories[i]
		j := i - 1
		for j >= 0 && cashTransactionCategoryLess(current, categories[j]) {
			categories[j+1] = categories[j]
			j--
		}
		categories[j+1] = current
	}
}

func cashTransactionCategoryLess(left, right CashTransactionCategory) bool {
	leftCode := left.AccountCode
	rightCode := right.AccountCode
	if leftCode == "" {
		leftCode = "999999999"
	}
	if rightCode == "" {
		rightCode = "999999999"
	}
	if leftCode != rightCode {
		return leftCode < rightCode
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.ID < right.ID
}
