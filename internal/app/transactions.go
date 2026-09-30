package app

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const adminCashTransactionPageSize = 50

type CashTransactionFilters struct {
	DateFrom            string `form:"date_from"`
	DateTo              string `form:"date_to"`
	Category            string `form:"category"`
	Type                string `form:"type"`
	TransactionCategory string `form:"transaction_category"`
}

type CashTransactionSummary struct {
	TotalIncome   int64
	TotalExpense  int64
	EndingBalance int64
	CashBalance   int64
	BankBalance   int64
}

type CashTransactionRow struct {
	ID              string `json:"id"`
	TransactionDate string `json:"transaction_date"`
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	Direction       string `json:"direction"`
	Source          string `json:"source"`
	COACode         string `json:"coa_code,omitempty"`
	Type            string `json:"type"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	Income          int64  `json:"income"`
	Expense         int64  `json:"expense"`
	Amount          int64  `json:"amount"`
	ReferenceNo     string `json:"reference_no"`
	RecordedBy      string `json:"recorded_by"`
	CreatedAt       string `json:"created_at"`
}

type CashTransactionPage struct {
	Rows       []CashTransactionRow
	Summary    CashTransactionSummary
	Pagination CashTransactionPagination
}

type CashTransactionPagination struct {
	Page        int
	TotalPages  int
	PreviousURL string
	NextURL     string
}

func cashTransactionFiltersFromQuery(query interface{ Query(string) string }) CashTransactionFilters {
	return CashTransactionFilters{
		DateFrom:            strings.TrimSpace(query.Query("date_from")),
		DateTo:              strings.TrimSpace(query.Query("date_to")),
		Category:            strings.TrimSpace(query.Query("category")),
		Type:                strings.TrimSpace(query.Query("type")),
		TransactionCategory: strings.TrimSpace(query.Query("transaction_category")),
	}
}

func validCashTransactionDirection(value string) bool {
	return value == "cash_in" || value == "cash_out"
}

func validAccountingTransactionDirection(value string) bool {
	return validAccountingDirection(value)
}

func validCashTransactionType(value string) bool {
	switch value {
	case "savings", "withdrawal", "loan", "repayment":
		return true
	case "manual":
		return true
	default:
		return false
	}
}

func cashTransactionPageFromQuery(query interface{ Query(string) string }) int {
	page, err := strconv.Atoi(strings.TrimSpace(query.Query("page")))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func cashTransactionPaginationURL(filters CashTransactionFilters, page int) string {
	values := url.Values{}
	if filters.DateFrom != "" {
		values.Set("date_from", filters.DateFrom)
	}
	if filters.DateTo != "" {
		values.Set("date_to", filters.DateTo)
	}
	if filters.Category != "" {
		values.Set("category", filters.Category)
	}
	if filters.Type != "" {
		values.Set("type", filters.Type)
	}
	if filters.TransactionCategory != "" {
		values.Set("transaction_category", filters.TransactionCategory)
	}
	values.Set("page", strconv.Itoa(page))
	return "/admin/transactions?" + values.Encode()
}

func (s *Server) cashTransactionsQuery(filters CashTransactionFilters) (string, []any) {
	coaIsCashBank := `a.account_type='asset' AND (COALESCE(a.subtype,'') IN ('cash','bank') OR COALESCE(a.system_key,'') IN ('CASH','BANK'))`
	stringAgg := `string_agg(DISTINCT CASE WHEN ` + coaIsCashBank + ` THEN l.coa_code END, ', ')`
	if strings.Contains(strings.ToLower(fmt.Sprintf("%T", s.db.Driver())), "sqlite") {
		stringAgg = `GROUP_CONCAT(DISTINCT CASE WHEN ` + coaIsCashBank + ` THEN l.coa_code END)`
	}
	query := strings.Builder{}
	query.WriteString(`WITH cash_movement AS (
		SELECT e.transaction_id,e.transaction_type,e.id AS journal_id,e.status,
			COALESCE(SUM(CASE WHEN a.account_type='asset' AND (COALESCE(a.subtype,'') IN ('cash','bank') OR COALESCE(a.system_key,'') IN ('CASH','BANK'))
				THEN CASE WHEN l.side='debit' THEN l.amount ELSE -l.amount END ELSE 0 END),0) AS net_amount,
			COALESCE(` + stringAgg + `,'') AS coa_codes
		FROM financial_journal_entries e
		LEFT JOIN financial_journal_lines l ON l.journal_id=e.id
		LEFT JOIN coa_accounts a ON a.code=l.coa_code
		GROUP BY e.id
	)
	SELECT id,transaction_date,member_no,full_name,direction,transaction_type,category_id,description,category_name,source,coa_code,income,expense,amount,reference_no,recorded_by,created_at
	FROM (
		SELECT 'saving:' || sr.id AS id,sr.record_date AS transaction_date,m.member_no,m.full_name,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN CASE WHEN cm.net_amount>0 THEN 'debit' ELSE 'credit' END ELSE 'pending_mapping' END AS direction,
			'savings' AS transaction_type,'' AS category_id,'Simpanan ' || sr.category || ' dari ' || m.full_name AS description,'' AS category_name,'' AS source,COALESCE(cm.coa_codes,'') AS coa_code,
			CASE WHEN cm.status='posted' AND cm.net_amount>0 THEN cm.net_amount ELSE 0 END AS income,
			CASE WHEN cm.status='posted' AND cm.net_amount<0 THEN -cm.net_amount ELSE 0 END AS expense,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN ABS(cm.net_amount) ELSE sr.amount END AS amount,
			sr.reference_no AS reference_no,COALESCE(NULLIF(u.full_name,''),u.email,'') AS recorded_by,CAST(sr.created_at AS TEXT) AS created_at
		FROM saving_records sr JOIN members m ON m.id=sr.member_id LEFT JOIN users u ON u.id=sr.recorded_by
		LEFT JOIN cash_movement cm ON cm.transaction_id=sr.id AND cm.transaction_type='savings' WHERE sr.type='deposit'
		UNION ALL
		SELECT 'withdrawal:' || sr.id,sr.record_date,m.member_no,m.full_name,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN CASE WHEN cm.net_amount>0 THEN 'debit' ELSE 'credit' END ELSE 'pending_mapping' END,
			'withdrawal','', 'Penarikan sukarela oleh ' || m.full_name,'','',COALESCE(cm.coa_codes,''),
			CASE WHEN cm.status='posted' AND cm.net_amount>0 THEN cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<0 THEN -cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN ABS(cm.net_amount) ELSE sr.amount END,
			sr.reference_no,COALESCE(NULLIF(u.full_name,''),u.email,''),CAST(sr.created_at AS TEXT)
		FROM saving_records sr JOIN members m ON m.id=sr.member_id LEFT JOIN users u ON u.id=sr.recorded_by
		LEFT JOIN cash_movement cm ON cm.transaction_id=sr.id AND cm.transaction_type='withdrawal' WHERE sr.type='withdrawal'
		UNION ALL
		SELECT 'loan:' || l.id,COALESCE(NULLIF(l.start_date,''),SUBSTR(CAST(l.approved_at AS TEXT),1,10),SUBSTR(CAST(l.created_at AS TEXT),1,10)),m.member_no,m.full_name,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN CASE WHEN cm.net_amount>0 THEN 'debit' ELSE 'credit' END ELSE 'pending_mapping' END,
			'loan','','Pencairan pinjaman untuk ' || m.full_name,'','',COALESCE(cm.coa_codes,''),
			CASE WHEN cm.status='posted' AND cm.net_amount>0 THEN cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<0 THEN -cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN ABS(cm.net_amount) ELSE l.approved_amount END,
			l.loan_request_id,COALESCE(NULLIF(u.full_name,''),u.email,''),CAST(l.created_at AS TEXT)
		FROM loans l JOIN members m ON m.id=l.member_id LEFT JOIN users u ON u.id=l.approved_by
		LEFT JOIN cash_movement cm ON cm.transaction_id=l.id AND cm.transaction_type='loan' WHERE l.status<>'cancelled'
		UNION ALL
		SELECT 'repayment:' || lr.id,lr.record_date,m.member_no,m.full_name,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN CASE WHEN cm.net_amount>0 THEN 'debit' ELSE 'credit' END ELSE 'pending_mapping' END,
			'repayment','','Angsuran pinjaman dari ' || m.full_name,'','',COALESCE(cm.coa_codes,''),
			CASE WHEN cm.status='posted' AND cm.net_amount>0 THEN cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<0 THEN -cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN ABS(cm.net_amount) ELSE lr.amount END,
			lr.reference_no,COALESCE(NULLIF(u.full_name,''),u.email,''),CAST(lr.created_at AS TEXT)
		FROM loan_repayments lr JOIN members m ON m.id=lr.member_id LEFT JOIN users u ON u.id=lr.recorded_by
		LEFT JOIN cash_movement cm ON cm.transaction_id=lr.id AND cm.transaction_type='repayment'
		UNION ALL
		SELECT 'manual:' || mt.id,mt.transaction_date,'','',
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN CASE WHEN cm.net_amount>0 THEN 'debit' ELSE 'credit' END ELSE 'pending_mapping' END,
			'manual',COALESCE(mt.category_id,''),mt.description,COALESCE(NULLIF(c.name,''),''),'',COALESCE(cm.coa_codes,''),
			CASE WHEN cm.status='posted' AND cm.net_amount>0 THEN cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<0 THEN -cm.net_amount ELSE 0 END,
			CASE WHEN cm.status='posted' AND cm.net_amount<>0 THEN ABS(cm.net_amount) ELSE mt.amount END,
			mt.reference_no,COALESCE(NULLIF(u.full_name,''),u.email,''),CAST(mt.created_at AS TEXT)
		FROM manual_cash_transactions mt LEFT JOIN cash_transaction_categories c ON c.id=mt.category_id
		LEFT JOIN users u ON u.id=mt.recorded_by LEFT JOIN cash_movement cm ON cm.transaction_id=mt.id AND cm.transaction_type='manual'
	) cash_transactions WHERE 1=1`)

	var args []any
	addFilter := func(condition string, value any) {
		args = append(args, value)
		query.WriteString(fmt.Sprintf(" AND %s $%d", condition, len(args)))
	}
	if filters.DateFrom != "" {
		addFilter("transaction_date >=", filters.DateFrom)
	}
	if filters.DateTo != "" {
		addFilter("transaction_date <=", filters.DateTo)
	}
	if filters.Category != "" && validCashTransactionDirection(filters.Category) {
		direction := accountingDirectionForLegacyDirection(filters.Category)
		addFilter("direction =", direction)
	} else if filters.Category != "" && validAccountingTransactionDirection(filters.Category) {
		addFilter("direction =", filters.Category)
	}
	if filters.Type != "" && validCashTransactionType(filters.Type) {
		addFilter("transaction_type =", filters.Type)
	}
	if filters.TransactionCategory != "" {
		addFilter("(category_id =", filters.TransactionCategory)
		query.WriteString(fmt.Sprintf(" OR coa_code LIKE '%%' || $%d || '%%')", len(args)))
	}
	return query.String(), args
}

func (s *Server) cashTransactionRows(query string, args ...any) ([]CashTransactionRow, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []CashTransactionRow
	for rows.Next() {
		var row CashTransactionRow
		var categoryID sql.NullString
		if err := rows.Scan(&row.ID, &row.TransactionDate, &row.MemberNo, &row.FullName, &row.Direction, &row.Type, &categoryID, &row.Description, &row.Category, &row.Source, &row.COACode, &row.Income, &row.Expense, &row.Amount, &row.ReferenceNo, &row.RecordedBy, &row.CreatedAt); err != nil {
			return nil, err
		}
		transactions = append(transactions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return transactions, nil
}

func (page *CashTransactionPage) addSummary(row CashTransactionRow) {
	page.Summary.TotalIncome += row.Income
	page.Summary.TotalExpense += row.Expense
}

func (s *Server) cashTransactionsForAdmin(filters CashTransactionFilters) (CashTransactionPage, error) {
	query, args := s.cashTransactionsQuery(filters)
	query += " ORDER BY transaction_date DESC, created_at DESC, id DESC"
	rows, err := s.cashTransactionRows(query, args...)
	if err != nil {
		return CashTransactionPage{}, err
	}

	var page CashTransactionPage
	page.Rows = rows
	for _, row := range rows {
		page.addSummary(row)
	}
	page.Summary.EndingBalance = page.Summary.TotalIncome - page.Summary.TotalExpense
	return page, nil
}

func (s *Server) cashTransactionsPageForAdmin(filters CashTransactionFilters, pageNumber, pageSize int) (CashTransactionPage, error) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 {
		pageSize = adminCashTransactionPageSize
	}

	baseQuery, filterArgs := s.cashTransactionsQuery(filters)
	summaryQuery := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(income), 0),
			COALESCE(SUM(expense), 0)
		FROM (%s) cash_transactions`, baseQuery)

	var page CashTransactionPage
	var totalRows int
	if err := s.db.QueryRow(summaryQuery, filterArgs...).Scan(
		&totalRows,
		&page.Summary.TotalIncome,
		&page.Summary.TotalExpense,
	); err != nil {
		return CashTransactionPage{}, err
	}
	page.Summary.EndingBalance = page.Summary.TotalIncome - page.Summary.TotalExpense

	totalPages := (totalRows + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if pageNumber > totalPages {
		pageNumber = totalPages
	}

	dataQuery := baseQuery + " ORDER BY transaction_date DESC, created_at DESC, id DESC"
	dataArgs := append([]any(nil), filterArgs...)
	dataArgs = append(dataArgs, pageSize, (pageNumber-1)*pageSize)
	dataQuery += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(filterArgs)+1, len(filterArgs)+2)
	rows, err := s.cashTransactionRows(dataQuery, dataArgs...)
	if err != nil {
		return CashTransactionPage{}, err
	}

	page.Rows = rows
	page.Pagination = CashTransactionPagination{Page: pageNumber, TotalPages: totalPages}
	if pageNumber > 1 {
		page.Pagination.PreviousURL = cashTransactionPaginationURL(filters, pageNumber-1)
	}
	if pageNumber < totalPages {
		page.Pagination.NextURL = cashTransactionPaginationURL(filters, pageNumber+1)
	}
	return page, nil
}
