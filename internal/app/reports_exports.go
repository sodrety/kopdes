package app

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
)

type ChartSegment struct {
	Label   string
	Value   int
	Percent int
	Class   string
}

type ChartSegments []ChartSegment

type AdminOperationalReports struct {
	SavingsByCategory   ChartSegments
	WithdrawalsByStatus ChartSegments
	LoanExposure        ChartSegments
	RepaymentProgress   ChartSegments
	SavingsByMember     []SavingsReportRow
	WithdrawalsByMember []WithdrawalReportRow
	Loans               []AdminLoan
	Repayments          []AdminLoanRepayment
}

type SavingsReportRow struct {
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	Pokok    int    `json:"pokok"`
	Wajib    int    `json:"wajib"`
	Sukarela int    `json:"sukarela"`
	Total    int    `json:"total"`
}

type WithdrawalReportRow struct {
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	Pending  int    `json:"pending"`
	Approved int    `json:"approved"`
	Rejected int    `json:"rejected"`
	Total    int    `json:"total"`
}

func (s *Server) adminReports(c *gin.Context) {
	reports, err := s.adminOperationalReports()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, reports)
}

func (s *Server) adminReportsPage(c *gin.Context) {
	reports, err := s.adminOperationalReports()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-reports", pageData(c, "Reports - KKSUK PD Dharma Jaya", "reports", "reports", "operational_reports", gin.H{
		"Reports": reports,
	}))
}

func (s *Server) adminOperationalReports() (AdminOperationalReports, error) {
	savings, err := s.savingsCategoryChart()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	withdrawals, err := s.withdrawalStatusChart()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	loanExposure, err := s.loanExposureChart()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	repaymentProgress, err := s.repaymentProgressChart()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	savingsByMember, err := s.savingsReportByMember()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	withdrawalsByMember, err := s.withdrawalReportByMember()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	loans, err := s.loansForAdmin("")
	if err != nil {
		return AdminOperationalReports{}, err
	}
	repayments, err := s.repaymentsForAdmin()
	if err != nil {
		return AdminOperationalReports{}, err
	}
	return AdminOperationalReports{
		SavingsByCategory:   savings,
		WithdrawalsByStatus: withdrawals,
		LoanExposure:        loanExposure,
		RepaymentProgress:   repaymentProgress,
		SavingsByMember:     savingsByMember,
		WithdrawalsByMember: withdrawalsByMember,
		Loans:               loans,
		Repayments:          repayments,
	}, nil
}

func (s *Server) savingsCategoryChart() (ChartSegments, error) {
	values := map[string]int{"pokok": 0, "wajib": 0, "sukarela": 0}
	rows, err := s.db.Query(`SELECT category, COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE -amount END), 0) FROM saving_records GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var value int
		if err := rows.Scan(&category, &value); err != nil {
			return nil, err
		}
		values[category] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chartSegments([]ChartSegment{
		{Label: "simpanan_pokok", Value: values["pokok"], Class: "chart-simpanan"},
		{Label: "simpanan_wajib", Value: values["wajib"], Class: "chart-simpanan"},
		{Label: "simpanan_sukarela", Value: values["sukarela"], Class: "chart-simpanan"},
	}), nil
}

func (s *Server) withdrawalStatusChart() (ChartSegments, error) {
	values := map[string]int{"pending": 0, "approved": 0, "rejected": 0}
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM withdrawal_requests GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var value int
		if err := rows.Scan(&status, &value); err != nil {
			return nil, err
		}
		values[status] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chartSegments([]ChartSegment{
		{Label: "status_pending", Value: values["pending"], Class: "chart-warning"},
		{Label: "status_approved", Value: values["approved"], Class: "chart-simpanan"},
		{Label: "status_rejected", Value: values["rejected"], Class: "chart-danger"},
	}), nil
}

func (s *Server) loanExposureChart() (ChartSegments, error) {
	var approved, remaining int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(approved_amount), 0), COALESCE(SUM(remaining_balance), 0) FROM loans WHERE status = 'active'`).Scan(&approved, &remaining); err != nil {
		return nil, err
	}
	repaid := approved - remaining
	if repaid < 0 {
		repaid = 0
	}
	return chartSegments([]ChartSegment{
		{Label: "approved_principal", Value: approved, Class: "chart-pinjaman"},
		{Label: "remaining_balance", Value: remaining, Class: "chart-pinjaman"},
		{Label: "actual_repayment", Value: repaid, Class: "chart-simpanan"},
	}), nil
}

func (s *Server) repaymentProgressChart() (ChartSegments, error) {
	var scheduled, actual int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(approved_amount), 0), COALESCE(SUM(approved_amount - remaining_balance), 0) FROM loans`).Scan(&scheduled, &actual); err != nil {
		return nil, err
	}
	return chartSegments([]ChartSegment{
		{Label: "scheduled_repayment", Value: scheduled, Class: "chart-pinjaman"},
		{Label: "actual_repayment", Value: actual, Class: "chart-simpanan"},
	}), nil
}

func (s *Server) savingsReportByMember() ([]SavingsReportRow, error) {
	rows, err := s.db.Query(
		`SELECT m.member_no, m.full_name,
			COALESCE(SUM(CASE WHEN sr.category = 'pokok' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'pokok' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS pokok,
			COALESCE(SUM(CASE WHEN sr.category = 'wajib' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'wajib' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS wajib,
			COALESCE(SUM(CASE WHEN sr.category = 'sukarela' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'sukarela' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS sukarela
		FROM members m
		INNER JOIN saving_records sr ON sr.member_id = m.id
		GROUP BY m.id, m.member_no, m.full_name
		ORDER BY m.full_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reportRows []SavingsReportRow
	for rows.Next() {
		var row SavingsReportRow
		if err := rows.Scan(&row.MemberNo, &row.FullName, &row.Pokok, &row.Wajib, &row.Sukarela); err != nil {
			return nil, err
		}
		row.Total = row.Pokok + row.Wajib + row.Sukarela
		reportRows = append(reportRows, row)
	}
	return reportRows, rows.Err()
}

func (s *Server) withdrawalReportByMember() ([]WithdrawalReportRow, error) {
	rows, err := s.db.Query(
		`SELECT m.member_no, m.full_name,
			COALESCE(SUM(CASE WHEN wr.status = 'pending' THEN 1 ELSE 0 END), 0) AS pending,
			COALESCE(SUM(CASE WHEN wr.status = 'approved' THEN 1 ELSE 0 END), 0) AS approved,
			COALESCE(SUM(CASE WHEN wr.status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected
		FROM members m
		INNER JOIN withdrawal_requests wr ON wr.member_id = m.id
		GROUP BY m.id, m.member_no, m.full_name
		ORDER BY m.full_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reportRows []WithdrawalReportRow
	for rows.Next() {
		var row WithdrawalReportRow
		if err := rows.Scan(&row.MemberNo, &row.FullName, &row.Pending, &row.Approved, &row.Rejected); err != nil {
			return nil, err
		}
		row.Total = row.Pending + row.Approved + row.Rejected
		reportRows = append(reportRows, row)
	}
	return reportRows, rows.Err()
}

func chartSegments(segments []ChartSegment) ChartSegments {
	max := 0
	for _, segment := range segments {
		if segment.Value > max {
			max = segment.Value
		}
	}
	for i := range segments {
		if max == 0 {
			segments[i].Percent = 0
			continue
		}
		segments[i].Percent = segments[i].Value * 100 / max
		if segments[i].Value > 0 && segments[i].Percent < 4 {
			segments[i].Percent = 4
		}
	}
	return ChartSegments(segments)
}

func (segments ChartSegments) HasData() bool {
	for _, segment := range segments {
		if segment.Value != 0 {
			return true
		}
	}
	return false
}

func (s *Server) exportSavingsCSV(c *gin.Context) {
	records, err := s.savingsForAdmin(savingFiltersFromQuery(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "simpanan-export.csv", []string{"member_no", "member", "category", "type", "amount", "date", "reference_no", "note", "recorded_by"}, func(w *csv.Writer) error {
		for _, record := range records {
			if err := w.Write([]string{record.MemberNo, record.FullName, record.Category, record.Type, strconv.Itoa(record.Amount), record.RecordDate, record.ReferenceNo, record.Note, record.RecordedBy}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) exportWithdrawalRequestsCSV(c *gin.Context) {
	requests, err := s.withdrawalRequestsForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "penarikan-export.csv", []string{"member_no", "member", "amount", "status", "requested_at", "reviewed_at", "note", "review_note", "saving_record_id"}, func(w *csv.Writer) error {
		for _, request := range requests {
			if err := w.Write([]string{request.MemberNo, request.FullName, strconv.Itoa(request.Amount), request.Status, request.CreatedAt, request.ReviewedAt, request.Note, request.RejectionReason, request.SavingRecordID}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) exportLoansCSV(c *gin.Context) {
	loans, err := s.loansForAdmin(c.Query("status"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "pinjaman-export.csv", []string{"member_no", "member", "approved_amount", "duration_months", "monthly_installment", "remaining_balance", "status", "approved_at"}, func(w *csv.Writer) error {
		for _, loan := range loans {
			if err := w.Write([]string{loan.MemberNo, loan.FullName, strconv.Itoa(loan.ApprovedAmount), strconv.Itoa(loan.DurationMonths), strconv.Itoa(loan.MonthlyInstallment), strconv.Itoa(loan.RemainingBalance), loan.Status, loan.ApprovedAt}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) exportRepaymentsCSV(c *gin.Context) {
	repayments, err := s.repaymentsForAdmin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "angsuran-export.csv", []string{"member_no", "member", "loan_id", "amount", "date", "reference_no", "note"}, func(w *csv.Writer) error {
		for _, repayment := range repayments {
			if err := w.Write([]string{repayment.MemberNo, repayment.FullName, repayment.LoanID, strconv.Itoa(repayment.Amount), repayment.RecordDate, repayment.ReferenceNo, repayment.Note}); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeCSV(c *gin.Context, filename string, header []string, writeRows func(*csv.Writer) error) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write(header)
	if err := writeRows(writer); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	writer.Flush()
}

func savingsExportPath(filters SavingFilters) string {
	values := url.Values{}
	if filters.MemberID != "" {
		values.Set("member_id", filters.MemberID)
	}
	if filters.Type != "" {
		values.Set("type", filters.Type)
	}
	if filters.Category != "" {
		values.Set("category", filters.Category)
	}
	if filters.DateFrom != "" {
		values.Set("date_from", filters.DateFrom)
	}
	if filters.DateTo != "" {
		values.Set("date_to", filters.DateTo)
	}
	if encoded := values.Encode(); encoded != "" {
		return "/api/admin/exports/savings.csv?" + encoded
	}
	return "/api/admin/exports/savings.csv"
}
