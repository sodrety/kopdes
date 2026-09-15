package app

import (
	"fmt"
	"strings"
)

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
}

type CashTransactionRow struct {
	ID              string `json:"id"`
	TransactionDate string `json:"transaction_date"`
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	Direction       string `json:"direction"`
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
	Rows    []CashTransactionRow
	Summary CashTransactionSummary
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

func (s *Server) cashTransactionsForAdmin(filters CashTransactionFilters) (CashTransactionPage, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, transaction_date, member_no, full_name, direction, transaction_type, category_id, description, category_name, income, expense, amount, reference_no, recorded_by, created_at
		FROM (
			SELECT
				'saving:' || sr.id AS id,
				sr.record_date AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'cash_in' AS direction,
				'savings' AS transaction_type,
				'' AS category_id,
				'Simpanan ' || sr.category || ' dari ' || m.full_name AS description,
				'' AS category_name,
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
				'cash_out' AS direction,
				'withdrawal' AS transaction_type,
				'' AS category_id,
				'Penarikan sukarela oleh ' || m.full_name AS description,
				'' AS category_name,
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
				'cash_out' AS direction,
				'loan' AS transaction_type,
				'' AS category_id,
				'Pencairan pinjaman untuk ' || m.full_name AS description,
				'' AS category_name,
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
				'cash_in' AS direction,
				'repayment' AS transaction_type,
				'' AS category_id,
				'Angsuran pinjaman dari ' || m.full_name AS description,
				'' AS category_name,
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
				mt.direction AS direction,
				'manual' AS transaction_type,
				mt.category_id AS category_id,
				mt.description AS description,
				c.name AS category_name,
				CASE WHEN mt.direction = 'cash_in' THEN mt.amount ELSE 0 END AS income,
				CASE WHEN mt.direction = 'cash_out' THEN mt.amount ELSE 0 END AS expense,
				mt.amount AS amount,
				mt.reference_no AS reference_no,
				COALESCE(NULLIF(u.full_name, ''), u.email) AS recorded_by,
				CAST(mt.created_at AS TEXT) AS created_at
			FROM manual_cash_transactions mt
			JOIN cash_transaction_categories c ON c.id = mt.category_id
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
		addFilter("direction =", filters.Category)
	}
	if filters.Type != "" && validCashTransactionType(filters.Type) {
		addFilter("transaction_type =", filters.Type)
	}
	if filters.TransactionCategory != "" {
		addFilter("category_id =", filters.TransactionCategory)
	}
	query.WriteString(" ORDER BY transaction_date DESC, created_at DESC")

	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return CashTransactionPage{}, err
	}
	defer rows.Close()

	var page CashTransactionPage
	for rows.Next() {
		var row CashTransactionRow
		var categoryID string
		if err := rows.Scan(&row.ID, &row.TransactionDate, &row.MemberNo, &row.FullName, &row.Direction, &row.Type, &categoryID, &row.Description, &row.Category, &row.Income, &row.Expense, &row.Amount, &row.ReferenceNo, &row.RecordedBy, &row.CreatedAt); err != nil {
			return CashTransactionPage{}, err
		}
		page.Rows = append(page.Rows, row)
		page.Summary.TotalIncome += row.Income
		page.Summary.TotalExpense += row.Expense
	}
	if err := rows.Err(); err != nil {
		return CashTransactionPage{}, err
	}
	page.Summary.EndingBalance = page.Summary.TotalIncome - page.Summary.TotalExpense
	return page, nil
}
