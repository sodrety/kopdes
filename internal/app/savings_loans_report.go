package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type savingsLoansReportRow struct {
	MemberID, MemberNo, FullName, RecordDate, TypeKey, CategoryKey, Reference, Note string
	Amount                                                                          int64
}

// The repayment union is shared with the member profile, including historical events.
func (s *Server) adminSavingsLoansReportPage(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	from, to := strings.TrimSpace(c.Query("date_from")), strings.TrimSpace(c.Query("date_to"))
	for _, date := range []string{from, to} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "savings_loans_invalid_dates"))
				return
			}
		}
	}
	if from != "" && to != "" && from > to {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "savings_loans_invalid_dates"))
		return
	}
	rows, err := s.db.Query(`SELECT m.id,m.member_no,m.full_name,e.record_date,e.type_key,e.category_key,e.amount,e.reference_no,e.note
 FROM (
 SELECT id,member_id,record_date,type AS type_key,'simpanan_' || category AS category_key,amount,reference_no,note FROM saving_records
 UNION ALL
 SELECT id,member_id,start_date AS record_date,'loan' AS type_key,'loan_type_' || loan_type AS category_key,approved_amount AS amount,id AS reference_no,'' AS note FROM loans WHERE status <> 'cancelled'
 UNION ALL
 SELECT id,member_id,record_date,'repayment' AS type_key,'repayment_type_' || event_type AS category_key,amount,reference_no,note FROM (`+repaymentHistoryQuery+`) repayment_history
 ) e JOIN members m ON m.id=e.member_id
 WHERE ($1='' OR LOWER(m.full_name) LIKE $4 ESCAPE '!' OR LOWER(m.member_no) LIKE $4 ESCAPE '!')
 AND ($2='' OR CAST(e.record_date AS TEXT)>=$2) AND ($3='' OR SUBSTR(CAST(e.record_date AS TEXT),1,10)<=$3)
 ORDER BY e.record_date DESC,m.member_no,e.id`, search, from, to, "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))+"%")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer rows.Close()
	report := []savingsLoansReportRow{}
	for rows.Next() {
		var row savingsLoansReportRow
		if err := rows.Scan(&row.MemberID, &row.MemberNo, &row.FullName, &row.RecordDate, &row.TypeKey, &row.CategoryKey, &row.Amount, &row.Reference, &row.Note); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		report = append(report, row)
	}
	if err := rows.Err(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	renderPage(c, "admin-savings-loans-report", pageData(c, translate(languageFromRequest(c), "savings_loans_report"), "reports", "savings_loans_report", "savings_loans_report_description", gin.H{"Rows": report, "Search": search, "DateFrom": from, "DateTo": to}))
}
