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
	Source              string `form:"source"`
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
		Source:              strings.TrimSpace(query.Query("source")),
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
	if filters.Source != "" {
		values.Set("source", filters.Source)
	}
	values.Set("page", strconv.Itoa(page))
	return "/admin/transactions?" + values.Encode()
}

func (s *Server) cashTransactionsQuery(filters CashTransactionFilters) (string, []any) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, transaction_date, member_no, full_name, direction, transaction_type, category_id, description, category_name, source, coa_code, income, expense, amount, reference_no, recorded_by, created_at
		FROM (
			SELECT
				'saving:' || sr.id AS id,
				sr.record_date AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'debit' AS direction,
				'savings' AS transaction_type,
				'' AS category_id,
				'Simpanan ' || sr.category || ' dari ' || m.full_name AS description,
				'' AS category_name,
				COALESCE(sr.source,'bank') AS source,
				COALESCE(sr.coa_code,'') AS coa_code,
				sr.amount AS income,
				0 AS expense,
				sr.amount AS amount,
				sr.reference_no AS reference_no,
				'' AS recorded_by,
				CAST(sr.created_at AS TEXT) AS created_at
			FROM saving_records sr
			JOIN members m ON m.id = sr.member_id
			WHERE sr.type = 'deposit'
			UNION ALL
			SELECT
				'withdrawal:' || sr.id AS id,
				sr.record_date AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'credit' AS direction,
				'withdrawal' AS transaction_type,
				'' AS category_id,
				'Penarikan sukarela oleh ' || m.full_name AS description,
				'' AS category_name,
				COALESCE(sr.source,'bank') AS source,
				COALESCE(sr.coa_code,'') AS coa_code,
				0 AS income,
				sr.amount AS expense,
				sr.amount AS amount,
				sr.reference_no AS reference_no,
				'' AS recorded_by,
				CAST(sr.created_at AS TEXT) AS created_at
			FROM saving_records sr
			JOIN members m ON m.id = sr.member_id
			WHERE sr.type = 'withdrawal'
			UNION ALL
			SELECT
				'loan:' || l.id AS id,
				COALESCE(NULLIF(l.start_date, ''), SUBSTR(CAST(l.approved_at AS TEXT), 1, 10), SUBSTR(CAST(l.created_at AS TEXT), 1, 10)) AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'credit' AS direction,
				'loan' AS transaction_type,
				'' AS category_id,
				'Pencairan pinjaman untuk ' || m.full_name AS description,
				'' AS category_name,
				COALESCE(l.source,'bank') AS source,
				COALESCE(l.coa_code,'') AS coa_code,
				0 AS income,
				l.approved_amount AS expense,
				l.approved_amount AS amount,
				l.loan_request_id AS reference_no,
				'' AS recorded_by,
				CAST(l.created_at AS TEXT) AS created_at
			FROM loans l
			JOIN members m ON m.id = l.member_id
			WHERE l.status <> 'cancelled'
			UNION ALL
			SELECT
				'repayment:' || lr.id AS id,
				lr.record_date AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'debit' AS direction,
				'repayment' AS transaction_type,
				'' AS category_id,
				'Angsuran pinjaman dari ' || m.full_name AS description,
				'' AS category_name,
				COALESCE(lr.source,'bank') AS source,
				COALESCE(lr.coa_code,'') AS coa_code,
				lr.amount AS income,
				0 AS expense,
				lr.amount AS amount,
				lr.reference_no AS reference_no,
				'' AS recorded_by,
				CAST(lr.created_at AS TEXT) AS created_at
			FROM loan_repayments lr
			JOIN members m ON m.id = lr.member_id
			UNION ALL
			SELECT
				'manual:' || mt.id AS id,
				mt.transaction_date AS transaction_date,
				'' AS member_no,
				'' AS full_name,
				COALESCE(mt.accounting_direction, CASE WHEN mt.direction='cash_in' THEN 'debit' ELSE 'credit' END) AS direction,
				'manual' AS transaction_type,
				mt.category_id AS category_id,
				mt.description AS description,
				COALESCE(NULLIF(c.name,''), mt.coa_code, '') AS category_name,
				COALESCE(mt.source,'bank') AS source,
				COALESCE(mt.coa_code, c.account_code, '') AS coa_code,
				CASE WHEN mt.direction = 'cash_in' THEN mt.amount ELSE 0 END AS income,
				CASE WHEN mt.direction = 'cash_out' THEN mt.amount ELSE 0 END AS expense,
				mt.amount AS amount,
				mt.reference_no AS reference_no,
				COALESCE(NULLIF(u.full_name, ''), u.email) AS recorded_by,
				CAST(mt.created_at AS TEXT) AS created_at
			FROM manual_cash_transactions mt
			LEFT JOIN cash_transaction_categories c ON c.id = mt.category_id
			JOIN users u ON u.id = mt.recorded_by
		) cash_transactions
		WHERE 1 = 1`)

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
		query.WriteString(fmt.Sprintf(" OR coa_code = $%d)", len(args)))
	}
	if filters.Source != "" && validTransactionSource(filters.Source) {
		addFilter("source =", filters.Source)
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
	if row.Source == transactionSourceCash {
		if row.Direction == accountingDirectionDebit {
			page.Summary.CashBalance += row.Amount
		} else {
			page.Summary.CashBalance -= row.Amount
		}
	} else {
		if row.Direction == accountingDirectionDebit {
			page.Summary.BankBalance += row.Amount
		} else {
			page.Summary.BankBalance -= row.Amount
		}
	}
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
			COALESCE(SUM(expense), 0),
			COALESCE(SUM(CASE WHEN source = 'cash' AND direction = 'debit' THEN amount WHEN source = 'cash' THEN -amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source <> 'cash' AND direction = 'debit' THEN amount WHEN source <> 'cash' THEN -amount ELSE 0 END), 0)
		FROM (%s) cash_transactions`, baseQuery)

	var page CashTransactionPage
	var totalRows int
	if err := s.db.QueryRow(summaryQuery, filterArgs...).Scan(
		&totalRows,
		&page.Summary.TotalIncome,
		&page.Summary.TotalExpense,
		&page.Summary.CashBalance,
		&page.Summary.BankBalance,
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
