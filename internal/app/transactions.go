package app

import (
	"fmt"
	"strings"
)

type CashTransactionFilters struct {
	DateFrom string `form:"date_from"`
	DateTo   string `form:"date_to"`
	Category string `form:"category"`
	Type     string `form:"type"`
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
	Income          int64  `json:"income"`
	Expense         int64  `json:"expense"`
	Amount          int64  `json:"amount"`
	ReferenceNo     string `json:"reference_no"`
	CreatedAt       string `json:"created_at"`
}

type CashTransactionPage struct {
	Rows    []CashTransactionRow
	Summary CashTransactionSummary
}

func cashTransactionFiltersFromQuery(query interface{ Query(string) string }) CashTransactionFilters {
	return CashTransactionFilters{
		DateFrom: strings.TrimSpace(query.Query("date_from")),
		DateTo:   strings.TrimSpace(query.Query("date_to")),
		Category: strings.TrimSpace(query.Query("category")),
		Type:     strings.TrimSpace(query.Query("type")),
	}
}

func validCashTransactionDirection(value string) bool {
	return value == "cash_in" || value == "cash_out"
}

func validCashTransactionType(value string) bool {
	switch value {
	case "savings", "withdrawal", "loan", "repayment":
		return true
	default:
		return false
	}
}

func (s *Server) cashTransactionsForAdmin(filters CashTransactionFilters) (CashTransactionPage, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, transaction_date, member_no, full_name, direction, transaction_type, description, income, expense, amount, reference_no, created_at
		FROM (
			SELECT
				'saving:' || sr.id AS id,
				sr.record_date AS transaction_date,
				m.member_no AS member_no,
				m.full_name AS full_name,
				'cash_in' AS direction,
				'savings' AS transaction_type,
				'Simpanan ' || sr.category || ' dari ' || m.full_name AS description,
				sr.amount AS income,
				0 AS expense,
				sr.amount AS amount,
				sr.reference_no AS reference_no,
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
				'Penarikan sukarela oleh ' || m.full_name AS description,
				0 AS income,
				sr.amount AS expense,
				sr.amount AS amount,
				sr.reference_no AS reference_no,
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
				'Pencairan pinjaman untuk ' || m.full_name AS description,
				0 AS income,
				l.approved_amount AS expense,
				l.approved_amount AS amount,
				l.loan_request_id AS reference_no,
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
				'Angsuran pinjaman dari ' || m.full_name AS description,
				lr.amount AS income,
				0 AS expense,
				lr.amount AS amount,
				lr.reference_no AS reference_no,
				CAST(lr.created_at AS TEXT) AS created_at
			FROM loan_repayments lr
			JOIN members m ON m.id = lr.member_id
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
	query.WriteString(" ORDER BY transaction_date DESC, created_at DESC")

	rows, err := s.db.Query(query.String(), args...)
	if err != nil {
		return CashTransactionPage{}, err
	}
	defer rows.Close()

	var page CashTransactionPage
	for rows.Next() {
		var row CashTransactionRow
		if err := rows.Scan(&row.ID, &row.TransactionDate, &row.MemberNo, &row.FullName, &row.Direction, &row.Type, &row.Description, &row.Income, &row.Expense, &row.Amount, &row.ReferenceNo, &row.CreatedAt); err != nil {
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
