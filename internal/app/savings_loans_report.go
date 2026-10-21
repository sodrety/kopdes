package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type savingsLoansReportAmounts struct {
	PokokBalance, WajibBalance, SukarelaBalance, SHUBalance, KhususBalance int64
	SavingsBalance, RunningLoanTotal, RemainingLoanBalance                 int64
}

type savingsLoansReportRow struct {
	MemberID, MemberNo, FullName string
	savingsLoansReportAmounts
}

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
	// Aggregate savings and loans independently so multiple records cannot multiply balances.
	rows, err := s.db.Query(`WITH savings AS (
 SELECT member_id,
 SUM(CASE WHEN category='pokok' THEN CASE WHEN type='deposit' THEN amount ELSE -amount END ELSE 0 END) AS pokok,
 SUM(CASE WHEN category='wajib' THEN CASE WHEN type='deposit' THEN amount ELSE -amount END ELSE 0 END) AS wajib,
 SUM(CASE WHEN category='sukarela' THEN CASE WHEN type='deposit' THEN amount ELSE -amount END ELSE 0 END) AS sukarela,
 SUM(CASE WHEN category='shu' THEN CASE WHEN type='deposit' THEN amount ELSE -amount END ELSE 0 END) AS shu,
 SUM(CASE WHEN category='khusus' THEN CASE WHEN type='deposit' THEN amount ELSE -amount END ELSE 0 END) AS khusus,
 SUM(CASE WHEN type='deposit' THEN amount ELSE -amount END) AS balance
 FROM saving_records GROUP BY member_id
 ), running_loans AS (
 SELECT member_id,SUM(total_obligation) AS obligation,SUM(remaining_balance) AS balance
 FROM loans WHERE status <> 'cancelled' AND remaining_balance>0 GROUP BY member_id
 ), activity AS (
 SELECT member_id,record_date FROM saving_records
 UNION ALL SELECT member_id,start_date AS record_date FROM loans WHERE status <> 'cancelled'
 UNION ALL SELECT member_id,record_date FROM (`+repaymentHistoryQuery+`) repayment_history
 )
 SELECT m.id,m.member_no,m.full_name,
 COALESCE(s.pokok,0),COALESCE(s.wajib,0),COALESCE(s.sukarela,0),COALESCE(s.shu,0),COALESCE(s.khusus,0),COALESCE(s.balance,0),
 COALESCE(l.obligation,0),COALESCE(l.balance,0)
 FROM members m LEFT JOIN savings s ON s.member_id=m.id LEFT JOIN running_loans l ON l.member_id=m.id
 WHERE ($1='' OR LOWER(m.full_name) LIKE $4 ESCAPE '!' OR LOWER(m.member_no) LIKE $4 ESCAPE '!')
 AND (($2='' AND $3='') OR EXISTS (
 SELECT 1 FROM activity a WHERE a.member_id=m.id
 AND ($2='' OR SUBSTR(CAST(a.record_date AS TEXT),1,10)>=$2)
 AND ($3='' OR SUBSTR(CAST(a.record_date AS TEXT),1,10)<=$3)
 ))
 ORDER BY LOWER(m.full_name),m.member_no,m.id`, search, from, to, "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))+"%")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	defer rows.Close()
	report := []savingsLoansReportRow{}
	var totals savingsLoansReportAmounts
	for rows.Next() {
		var row savingsLoansReportRow
		if err := rows.Scan(&row.MemberID, &row.MemberNo, &row.FullName, &row.PokokBalance, &row.WajibBalance, &row.SukarelaBalance, &row.SHUBalance, &row.KhususBalance, &row.SavingsBalance, &row.RunningLoanTotal, &row.RemainingLoanBalance); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		if err := totals.add(row.savingsLoansReportAmounts); err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
			return
		}
		report = append(report, row)
	}
	if err := rows.Err(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	renderPage(c, "admin-savings-loans-report", pageData(c, translate(languageFromRequest(c), "savings_loans_report"), "savings-loans-report", "savings_loans_report", "savings_loans_report_description", gin.H{"Rows": report, "Totals": totals, "Search": search, "DateFrom": from, "DateTo": to}))
}

func (total *savingsLoansReportAmounts) add(row savingsLoansReportAmounts) error {
	for _, amount := range []struct {
		target *int64
		value  int64
	}{
		{&total.PokokBalance, row.PokokBalance}, {&total.WajibBalance, row.WajibBalance},
		{&total.SukarelaBalance, row.SukarelaBalance}, {&total.SHUBalance, row.SHUBalance},
		{&total.KhususBalance, row.KhususBalance}, {&total.SavingsBalance, row.SavingsBalance},
		{&total.RunningLoanTotal, row.RunningLoanTotal}, {&total.RemainingLoanBalance, row.RemainingLoanBalance},
	} {
		value, err := checkedReportAdd(*amount.target, amount.value)
		if err != nil {
			return err
		}
		*amount.target = value
	}
	return nil
}
