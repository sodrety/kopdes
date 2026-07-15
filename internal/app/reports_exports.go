package app

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type ChartSegment struct {
	Label   string
	Value   int
	Percent int
	Class   string
}

type ChartSegments []ChartSegment

type LineChart struct {
	TitleKey    string
	TitleSuffix string
	XTicks      []ChartAxisLabel
	YTicks      []ChartAxisLabel
	Series      []LineChartSeries
}

type ChartAxisLabel struct {
	Label string
	X     int
	Y     int
}

type LineChartSeries struct {
	LabelKey string
	Class    string
	Points   string
	Dots     []ChartPoint
}

type ChartPoint struct {
	X int
	Y int
}

type AdminOperationalReports struct {
	SavingsByCategory     ChartSegments
	WithdrawalsByStatus   ChartSegments
	LoanExposure          ChartSegments
	RepaymentProgress     ChartSegments
	SavingsLoanComparison LineChart
	BalanceTrend          LineChart
	SavingsByMember       []SavingsReportRow
	WithdrawalsByMember   []WithdrawalReportRow
	Loans                 []AdminLoan
	Repayments            []AdminLoanRepayment
}

type BalanceReport struct {
	TotalSavings         int
	TotalOutstandingLoan int
	PendingWithdrawals   int
	OperationalBalance   int
	TotalAssets          int
	TotalLiabilities     int
	TotalEquity          int
	LiabilityRatio       int
	HealthStatus         string
	CashAsset            int
	LoanReceivable       int
	PrintedAt            string
	Rows                 []BalanceReportRow
}

type BalanceReportRow struct {
	GroupKey string
	LabelKey string
	Amount   int
	Class    string
}

type ProfitLossReport struct {
	TotalIncome        int
	TotalCost          int
	NetProfit          int
	IncomePercent      int
	CostPercent        int
	MarginPercent      int
	IncomeTransactions int
	CostTransactions   int
	MonthlyAverage     int
	PeriodStart        string
	PeriodEnd          string
}

type ProfitLossPeriod struct {
	DateFrom string
	DateTo   string
}

type SavingsReportRow struct {
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	MemberType      string `json:"member_type"`
	MemberTypeLabel string `json:"member_type_label"`
	Pokok           int    `json:"pokok"`
	Wajib           int    `json:"wajib"`
	Sukarela        int    `json:"sukarela"`
	Total           int    `json:"total"`
}

type WithdrawalReportRow struct {
	MemberNo        string `json:"member_no"`
	FullName        string `json:"full_name"`
	MemberType      string `json:"member_type"`
	MemberTypeLabel string `json:"member_type_label"`
	Pending         int    `json:"pending"`
	Approved        int    `json:"approved"`
	Rejected        int    `json:"rejected"`
	Total           int    `json:"total"`
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

func (s *Server) adminBalanceReportPage(c *gin.Context) {
	report, err := s.balanceReport()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	if export := c.Query("export"); export == "csv" {
		writeBalanceReportCSV(c, report)
		return
	} else if export == "pdf" {
		lang := languageFromRequest(c)
		writeReportPDF(c, "balance-report.pdf", "KKSUK PD Dharma Jaya - "+translate(lang, "balance_report"), []string{
			fmt.Sprintf("%s %s", translate(lang, "printed_at"), report.PrintedAt),
			fmt.Sprintf("%s: Rp %d", translate(lang, "total_assets"), report.TotalAssets),
			fmt.Sprintf("%s: Rp %d", translate(lang, "total_liabilities"), report.TotalLiabilities),
			fmt.Sprintf("%s: Rp %d", translate(lang, "total_equity"), report.TotalEquity),
			fmt.Sprintf("%s: Rp %d", translate(lang, "cash_asset"), report.CashAsset),
			fmt.Sprintf("%s: Rp %d", translate(lang, "loan_receivable"), report.LoanReceivable),
			fmt.Sprintf("%s: Rp %d", translate(lang, "pending_withdrawals"), report.PendingWithdrawals),
			fmt.Sprintf("%s %d%%", translate(lang, "liability_asset_ratio"), report.LiabilityRatio),
			fmt.Sprintf("%s: %s", translate(lang, "health_status"), translate(lang, fmt.Sprintf("health_%s", report.HealthStatus))),
		})
		return
	}
	renderPage(c, "admin-balance-report", pageData(c, "Balance Report - KKSUK PD Dharma Jaya", "reports", "balance_report", "balance_report_description", gin.H{
		"Report": report,
	}))
}

func (s *Server) adminProfitLossReportPage(c *gin.Context) {
	period, err := s.profitLossPeriodFromQuery(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid report period")
		return
	}
	report, err := s.profitLossReport(period)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	exportCSVURL := profitLossExportURL(period, "csv")
	exportPDFURL := profitLossExportURL(period, "pdf")
	if export := c.Query("export"); export == "csv" {
		writeProfitLossReportCSV(c, report)
		return
	} else if export == "pdf" {
		lang := languageFromRequest(c)
		writeReportPDF(c, "profit-loss-report.pdf", "KKSUK PD Dharma Jaya - "+translate(lang, "profit_loss_report"), []string{
			fmt.Sprintf("%s %s %s %s", translate(lang, "period"), report.PeriodStart, translate(lang, "to"), report.PeriodEnd),
			fmt.Sprintf("%s: Rp %d", translate(lang, "total_income"), report.TotalIncome),
			fmt.Sprintf("%s: Rp %d", translate(lang, "total_cost"), report.TotalCost),
			fmt.Sprintf("%s: Rp %d", translate(lang, "net_profit"), report.NetProfit),
			fmt.Sprintf("%s: %d", translate(lang, "income_transactions"), report.IncomeTransactions),
			fmt.Sprintf("%s: %d", translate(lang, "cost_transactions"), report.CostTransactions),
			fmt.Sprintf("%s %d%%", translate(lang, "profit_margin"), report.MarginPercent),
			fmt.Sprintf("%s Rp %d", translate(lang, "monthly_average"), report.MonthlyAverage),
		})
		return
	}
	renderPage(c, "admin-profit-loss-report", pageData(c, "Profit/Loss Report - KKSUK PD Dharma Jaya", "profit-loss", "profit_loss_report", "profit_loss_report_description", gin.H{
		"Report":       report,
		"Period":       period,
		"ExportCSVURL": exportCSVURL,
		"ExportPDFURL": exportPDFURL,
	}))
}

func writeBalanceReportCSV(c *gin.Context, report BalanceReport) {
	c.Header("Content-Disposition", `attachment; filename="balance-report.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"metric", "amount"})
	_ = writer.Write([]string{"total_assets", strconv.Itoa(report.TotalAssets)})
	_ = writer.Write([]string{"total_liabilities", strconv.Itoa(report.TotalLiabilities)})
	_ = writer.Write([]string{"total_equity", strconv.Itoa(report.TotalEquity)})
	_ = writer.Write([]string{"cash_asset", strconv.Itoa(report.CashAsset)})
	_ = writer.Write([]string{"loan_receivable", strconv.Itoa(report.LoanReceivable)})
	_ = writer.Write([]string{"pending_withdrawals", strconv.Itoa(report.PendingWithdrawals)})
	writer.Flush()
}

func writeProfitLossReportCSV(c *gin.Context, report ProfitLossReport) {
	c.Header("Content-Disposition", `attachment; filename="profit-loss-report.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"period_start", "period_end", "total_income", "total_cost", "net_profit", "income_transactions", "cost_transactions", "margin_percent"})
	_ = writer.Write([]string{report.PeriodStart, report.PeriodEnd, strconv.Itoa(report.TotalIncome), strconv.Itoa(report.TotalCost), strconv.Itoa(report.NetProfit), strconv.Itoa(report.IncomeTransactions), strconv.Itoa(report.CostTransactions), strconv.Itoa(report.MarginPercent)})
	writer.Flush()
}

func writeReportPDF(c *gin.Context, filename, title string, lines []string) {
	const linesPerPage = 38
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	if pageCount == 0 {
		pageCount = 1
	}
	fontObject := 3 + pageCount*2
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+page*2))
	}
	objects = append(objects, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount))
	for page := 0; page < pageCount; page++ {
		content := strings.Builder{}
		content.WriteString("BT /F1 16 Tf 50 790 Td (")
		content.WriteString(escapePDFText(title))
		content.WriteString(") Tj /F1 11 Tf 0 -28 Td ")
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[start:end] {
			content.WriteString("(")
			content.WriteString(escapePDFText(line))
			content.WriteString(") Tj 0 -18 Td ")
		}
		content.WriteString("ET")
		contentObject := 4 + page*2
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontObject, contentObject),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()))
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf.Bytes())
}

func escapePDFText(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `(`, `\(`)
	return strings.ReplaceAll(value, `)`, `\)`)
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
	repayments, err := s.repaymentsForAdmin(RepaymentFilters{})
	if err != nil {
		return AdminOperationalReports{}, err
	}
	savingsTotal := sumChartValues(savings)
	remainingLoan := chartValueByLabel(loanExposure, "remaining_balance")
	return AdminOperationalReports{
		SavingsByCategory:     savings,
		WithdrawalsByStatus:   withdrawals,
		LoanExposure:          loanExposure,
		RepaymentProgress:     repaymentProgress,
		SavingsLoanComparison: dashboardSavingsLoanComparisonChart(savingsTotal, remainingLoan),
		BalanceTrend:          dashboardBalanceTrendChart(savingsTotal - remainingLoan),
		SavingsByMember:       savingsByMember,
		WithdrawalsByMember:   withdrawalsByMember,
		Loans:                 loans,
		Repayments:            repayments,
	}, nil
}

func (s *Server) balanceReport() (BalanceReport, error) {
	savings, err := s.savingsCategoryChart()
	if err != nil {
		return BalanceReport{}, err
	}
	loanExposure, err := s.loanExposureChart()
	if err != nil {
		return BalanceReport{}, err
	}
	pendingWithdrawals, err := s.pendingWithdrawalAmount()
	if err != nil {
		return BalanceReport{}, err
	}
	totalSavings := sumChartValues(savings)
	totalOutstandingLoan := chartValueByLabel(loanExposure, "remaining_balance")
	cashAsset := totalSavings - pendingWithdrawals
	totalAssets := cashAsset + totalOutstandingLoan
	totalLiabilities := totalSavings
	totalEquity := totalAssets - totalLiabilities
	liabilityRatio := 0
	if totalAssets > 0 {
		liabilityRatio = totalLiabilities * 100 / totalAssets
	}
	healthStatus := "Baik"
	if liabilityRatio >= 80 {
		healthStatus = "Perlu Perhatian"
	} else if liabilityRatio >= 50 {
		healthStatus = "Cukup"
	}
	report := BalanceReport{
		TotalSavings:         totalSavings,
		TotalOutstandingLoan: totalOutstandingLoan,
		PendingWithdrawals:   pendingWithdrawals,
		OperationalBalance:   totalSavings - totalOutstandingLoan - pendingWithdrawals,
		TotalAssets:          totalAssets,
		TotalLiabilities:     totalLiabilities,
		TotalEquity:          totalEquity,
		LiabilityRatio:       liabilityRatio,
		HealthStatus:         healthStatus,
		CashAsset:            cashAsset,
		LoanReceivable:       totalOutstandingLoan,
		PrintedAt:            time.Now().Format("02 January 2006 15:04:05"),
		Rows: []BalanceReportRow{
			{GroupKey: "balance_group_savings", LabelKey: "simpanan_pokok", Amount: chartValueByLabel(savings, "simpanan_pokok"), Class: "balance-positive"},
			{GroupKey: "balance_group_savings", LabelKey: "simpanan_wajib", Amount: chartValueByLabel(savings, "simpanan_wajib"), Class: "balance-positive"},
			{GroupKey: "balance_group_savings", LabelKey: "simpanan_sukarela", Amount: chartValueByLabel(savings, "simpanan_sukarela"), Class: "balance-positive"},
			{GroupKey: "balance_group_loans", LabelKey: "remaining_loan", Amount: -totalOutstandingLoan, Class: "balance-negative"},
			{GroupKey: "balance_group_withdrawals", LabelKey: "pending_withdrawals", Amount: -pendingWithdrawals, Class: "balance-warning"},
		},
	}
	return report, nil
}

func (s *Server) pendingWithdrawalAmount() (int, error) {
	var amount int
	err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM withdrawal_requests WHERE status = 'pending'`).Scan(&amount)
	return amount, err
}

func (s *Server) profitLossPeriodFromQuery(c *gin.Context) (ProfitLossPeriod, error) {
	period := ProfitLossPeriod{
		DateFrom: strings.TrimSpace(c.Query("date_from")),
		DateTo:   strings.TrimSpace(c.Query("date_to")),
	}
	var from, to time.Time
	var err error
	if period.DateFrom != "" {
		from, err = time.Parse("2006-01-02", period.DateFrom)
		if err != nil {
			return ProfitLossPeriod{}, err
		}
	}
	if period.DateTo != "" {
		to, err = time.Parse("2006-01-02", period.DateTo)
		if err != nil {
			return ProfitLossPeriod{}, err
		}
	}
	if period.DateFrom != "" && period.DateTo != "" && to.Before(from) {
		return ProfitLossPeriod{}, fmt.Errorf("date_to before date_from")
	}
	return period, nil
}

func profitLossExportURL(period ProfitLossPeriod, format string) string {
	values := url.Values{}
	if period.DateFrom != "" {
		values.Set("date_from", period.DateFrom)
	}
	if period.DateTo != "" {
		values.Set("date_to", period.DateTo)
	}
	values.Set("export", format)
	return "/admin/reports/profit-loss?" + values.Encode()
}

func (s *Server) profitLossReport(period ProfitLossPeriod) (ProfitLossReport, error) {
	dateFrom := period.DateFrom
	dateTo := period.DateTo
	if dateTo == "" {
		dateTo = time.Now().Format("2006-01-02")
	}
	if dateFrom == "" {
		var earliestActivity string
		if err := s.db.QueryRow(`SELECT COALESCE(MIN(activity_date), '') FROM (
			SELECT record_date AS activity_date FROM saving_records
			UNION ALL
			SELECT record_date AS activity_date FROM loan_repayments
		) activity`).Scan(&earliestActivity); err != nil {
			return ProfitLossReport{}, err
		}
		if earliestActivity != "" {
			dateFrom = earliestActivity
		} else {
			dateFrom = dateTo
		}
	}

	var savingDeposits, loanRepayments, savingWithdrawals int
	var depositCount, repaymentCount, withdrawalCount int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM saving_records WHERE type = 'deposit' AND record_date >= $1 AND record_date <= $2`, dateFrom, dateTo).Scan(&savingDeposits, &depositCount); err != nil {
		return ProfitLossReport{}, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM loan_repayments WHERE record_date >= $1 AND record_date <= $2`, dateFrom, dateTo).Scan(&loanRepayments, &repaymentCount); err != nil {
		return ProfitLossReport{}, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM saving_records WHERE type = 'withdrawal' AND record_date >= $1 AND record_date <= $2`, dateFrom, dateTo).Scan(&savingWithdrawals, &withdrawalCount); err != nil {
		return ProfitLossReport{}, err
	}
	totalIncome := savingDeposits + loanRepayments
	totalCost := savingWithdrawals
	totalActivity := totalIncome + totalCost
	incomePercent := 0
	costPercent := 0
	if totalActivity > 0 {
		incomePercent = totalIncome * 100 / totalActivity
		costPercent = totalCost * 100 / totalActivity
	}
	netProfit := totalIncome - totalCost
	marginPercent := 0
	if totalIncome > 0 {
		marginPercent = netProfit * 100 / totalIncome
	}
	periodStart, err := time.Parse("2006-01-02", dateFrom)
	if err != nil {
		return ProfitLossReport{}, err
	}
	periodEnd, err := time.Parse("2006-01-02", dateTo)
	if err != nil {
		return ProfitLossReport{}, err
	}
	monthCount := monthsInclusive(periodStart, periodEnd)
	return ProfitLossReport{
		TotalIncome:        totalIncome,
		TotalCost:          totalCost,
		NetProfit:          netProfit,
		IncomePercent:      incomePercent,
		CostPercent:        costPercent,
		MarginPercent:      marginPercent,
		IncomeTransactions: depositCount + repaymentCount,
		CostTransactions:   withdrawalCount,
		MonthlyAverage:     netProfit / monthCount,
		PeriodStart:        periodStart.Format("02/01/2006"),
		PeriodEnd:          periodEnd.Format("02/01/2006"),
	}, nil
}

func monthsInclusive(start, end time.Time) int {
	months := (end.Year()-start.Year())*12 + int(end.Month()-start.Month()) + 1
	if months < 1 {
		return 1
	}
	return months
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
	var approved, obligation, remaining, repaid int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(approved_amount), 0), COALESCE(SUM(total_obligation), 0), COALESCE(SUM(remaining_balance), 0) FROM loans WHERE status <> 'cancelled'`).Scan(&approved, &obligation, &remaining); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM loan_repayments`).Scan(&repaid); err != nil {
		return nil, err
	}
	return chartSegments([]ChartSegment{
		{Label: "approved_principal", Value: approved, Class: "chart-pinjaman"},
		{Label: "total_obligation", Value: obligation, Class: "chart-warning"},
		{Label: "remaining_balance", Value: remaining, Class: "chart-pinjaman"},
		{Label: "actual_repayment", Value: repaid, Class: "chart-simpanan"},
	}), nil
}

func (s *Server) repaymentProgressChart() (ChartSegments, error) {
	var scheduled, actual int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(total_obligation), 0) FROM loans WHERE status <> 'cancelled'`).Scan(&scheduled); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM loan_repayments`).Scan(&actual); err != nil {
		return nil, err
	}
	return chartSegments([]ChartSegment{
		{Label: "scheduled_repayment", Value: scheduled, Class: "chart-pinjaman"},
		{Label: "actual_repayment", Value: actual, Class: "chart-simpanan"},
	}), nil
}

func (s *Server) savingsReportByMember() ([]SavingsReportRow, error) {
	rows, err := s.db.Query(
		`SELECT m.member_no, m.full_name, m.member_type,
			COALESCE(SUM(CASE WHEN sr.category = 'pokok' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'pokok' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS pokok,
			COALESCE(SUM(CASE WHEN sr.category = 'wajib' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'wajib' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS wajib,
			COALESCE(SUM(CASE WHEN sr.category = 'sukarela' AND sr.type = 'deposit' THEN sr.amount WHEN sr.category = 'sukarela' AND sr.type = 'withdrawal' THEN -sr.amount ELSE 0 END), 0) AS sukarela
		FROM members m
		INNER JOIN saving_records sr ON sr.member_id = m.id
		GROUP BY m.id, m.member_no, m.full_name, m.member_type
		ORDER BY m.full_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reportRows []SavingsReportRow
	for rows.Next() {
		var row SavingsReportRow
		if err := rows.Scan(&row.MemberNo, &row.FullName, &row.MemberType, &row.Pokok, &row.Wajib, &row.Sukarela); err != nil {
			return nil, err
		}
		row.MemberTypeLabel = memberTypeLabel(row.MemberType)
		row.Total = row.Pokok + row.Wajib + row.Sukarela
		reportRows = append(reportRows, row)
	}
	return reportRows, rows.Err()
}

func (s *Server) withdrawalReportByMember() ([]WithdrawalReportRow, error) {
	rows, err := s.db.Query(
		`SELECT m.member_no, m.full_name, m.member_type,
			COALESCE(SUM(CASE WHEN wr.status = 'pending' THEN 1 ELSE 0 END), 0) AS pending,
			COALESCE(SUM(CASE WHEN wr.status = 'approved' THEN 1 ELSE 0 END), 0) AS approved,
			COALESCE(SUM(CASE WHEN wr.status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected
		FROM members m
		INNER JOIN withdrawal_requests wr ON wr.member_id = m.id
		GROUP BY m.id, m.member_no, m.full_name, m.member_type
		ORDER BY m.full_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reportRows []WithdrawalReportRow
	for rows.Next() {
		var row WithdrawalReportRow
		if err := rows.Scan(&row.MemberNo, &row.FullName, &row.MemberType, &row.Pending, &row.Approved, &row.Rejected); err != nil {
			return nil, err
		}
		row.MemberTypeLabel = memberTypeLabel(row.MemberType)
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

func sumChartValues(segments ChartSegments) int {
	total := 0
	for _, segment := range segments {
		total += segment.Value
	}
	return total
}

func chartValueByLabel(segments ChartSegments, label string) int {
	for _, segment := range segments {
		if segment.Label == label {
			return segment.Value
		}
	}
	return 0
}

func dashboardSavingsLoanComparisonChart(savingsTotal, loanTotal int) LineChart {
	months := []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
	values := []int{savingsTotal, loanTotal}
	maxValue := nicePositiveAxisMax(maxInt(values...))
	seriesValues := [][]int{
		repeatedValues(savingsTotal, len(months)),
		repeatedValues(loanTotal, len(months)),
	}
	return LineChart{
		TitleKey:    "savings_loans_comparison",
		TitleSuffix: fmt.Sprintf("(%d)", time.Now().Year()),
		XTicks:      xAxisLabels(months, 76, 720, 252),
		YTicks: []ChartAxisLabel{
			{Label: compactRupiahAxisLabel(maxValue), X: 58, Y: 44},
			{Label: compactRupiahAxisLabel(maxValue / 2), X: 58, Y: 139},
			{Label: compactRupiahAxisLabel(0), X: 58, Y: 234},
		},
		Series: []LineChartSeries{
			lineChartSeries("savings", "chart-line-simpanan", seriesValues[0], 0, maxValue),
			lineChartSeries("pinjaman", "chart-line-pinjaman", seriesValues[1], 0, maxValue),
		},
	}
}

func dashboardBalanceTrendChart(balance int) LineChart {
	year := time.Now().Year()
	months := []string{
		fmt.Sprintf("Jan %d", year),
		fmt.Sprintf("Feb %d", year),
		fmt.Sprintf("Mar %d", year),
		fmt.Sprintf("Apr %d", year),
		fmt.Sprintf("Mei %d", year),
		fmt.Sprintf("Jun %d", year),
	}
	maxValue := nicePositiveAxisMax(absInt(balance))
	return LineChart{
		TitleKey: "balance_trend_6_months",
		XTicks:   xAxisLabels(months, 76, 720, 252),
		YTicks: []ChartAxisLabel{
			{Label: compactRupiahAxisLabel(maxValue), X: 58, Y: 44},
			{Label: compactRupiahAxisLabel(maxValue / 2), X: 58, Y: 91},
			{Label: compactRupiahAxisLabel(0), X: 58, Y: 139},
			{Label: compactRupiahAxisLabel(-(maxValue / 2)), X: 58, Y: 186},
			{Label: compactRupiahAxisLabel(-maxValue), X: 58, Y: 234},
		},
		Series: []LineChartSeries{
			lineChartSeries("neraca", "chart-line-neraca", repeatedValues(balance, len(months)), -maxValue, maxValue),
		},
	}
}

func xAxisLabels(labels []string, left, right, y int) []ChartAxisLabel {
	ticks := make([]ChartAxisLabel, len(labels))
	width := right - left
	for i, label := range labels {
		x := left
		if len(labels) > 1 {
			x = left + width*i/(len(labels)-1)
		}
		ticks[i] = ChartAxisLabel{Label: label, X: x, Y: y}
	}
	return ticks
}

func lineChartSeries(labelKey, class string, values []int, minValue, maxValue int) LineChartSeries {
	points := lineChartPoints(values, minValue, maxValue)
	pointString := ""
	for i, point := range points {
		if i > 0 {
			pointString += " "
		}
		pointString += fmt.Sprintf("%d,%d", point.X, point.Y)
	}
	return LineChartSeries{LabelKey: labelKey, Class: class, Points: pointString, Dots: points}
}

func lineChartPoints(values []int, minValue, maxValue int) []ChartPoint {
	const (
		left   = 76
		right  = 720
		top    = 44
		bottom = 234
	)
	if maxValue <= minValue {
		maxValue = minValue + 1
	}
	width := right - left
	height := bottom - top
	points := make([]ChartPoint, len(values))
	for i, value := range values {
		x := left
		if len(values) > 1 {
			x = left + width*i/(len(values)-1)
		}
		y := top + (maxValue-value)*height/(maxValue-minValue)
		points[i] = ChartPoint{X: x, Y: y}
	}
	return points
}

func repeatedValues(value, count int) []int {
	values := make([]int, count)
	for i := range values {
		values[i] = value
	}
	return values
}

func nicePositiveAxisMax(value int) int {
	if value <= 0 {
		return 1
	}
	step := 500000
	if value <= step {
		return step
	}
	return ((value + step - 1) / step) * step
}

func compactRupiahAxisLabel(value int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	switch {
	case value >= 1000000:
		whole := value / 1000000
		if value%1000000 == 500000 {
			return fmt.Sprintf("Rp %s%d,5 jt", sign, whole)
		}
		return fmt.Sprintf("Rp %s%d jt", sign, whole)
	case value >= 1000:
		return fmt.Sprintf("Rp %s%d rb", sign, value/1000)
	default:
		return fmt.Sprintf("Rp %s%d", sign, value)
	}
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (s *Server) exportSavingsCSV(c *gin.Context) {
	records, err := s.savingsForAdmin(savingFiltersFromQuery(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "simpanan-export.csv", []string{"member_no", "member", translate(languageFromRequest(c), "member_type"), "category", "type", "amount", "date", "reference_no", "note", "recorded_by"}, func(w *csv.Writer) error {
		for _, record := range records {
			if err := w.Write([]string{record.MemberNo, record.FullName, record.MemberTypeLabel, record.Category, record.Type, strconv.Itoa(record.Amount), record.RecordDate, record.ReferenceNo, record.Note, record.RecordedBy}); err != nil {
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
	writeCSV(c, "penarikan-export.csv", []string{"member_no", "member", translate(languageFromRequest(c), "member_type"), "amount", "status", "requested_at", "reviewed_at", "note", "review_note", "saving_record_id"}, func(w *csv.Writer) error {
		for _, request := range requests {
			if err := w.Write([]string{request.MemberNo, request.FullName, request.MemberTypeLabel, strconv.Itoa(request.Amount), request.Status, request.CreatedAt, request.ReviewedAt, request.Note, request.RejectionReason, request.SavingRecordID}); err != nil {
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
	writeCSV(c, "pinjaman-export.csv", []string{"member_no", "member", translate(languageFromRequest(c), "member_type"), "approved_amount", "duration_months", "monthly_installment", "remaining_balance", "status", "approved_at", "start_date", "interest_rate_bps", "total_interest", "total_obligation", "next_due_date", "final_due_date"}, func(w *csv.Writer) error {
		for _, loan := range loans {
			if err := w.Write([]string{loan.MemberNo, loan.FullName, loan.MemberTypeLabel, strconv.Itoa(loan.ApprovedAmount), strconv.Itoa(loan.DurationMonths), strconv.Itoa(loan.MonthlyInstallment), strconv.Itoa(loan.RemainingBalance), loan.Status, loan.ApprovedAt, loan.StartDate, strconv.Itoa(loan.InterestRateBPS), strconv.Itoa(loan.TotalInterest), strconv.Itoa(loan.TotalObligation), loan.NextDueDate, loan.FinalDueDate}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) exportRepaymentsCSV(c *gin.Context) {
	repayments, err := s.repaymentsForAdmin(RepaymentFilters{})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	writeCSV(c, "angsuran-export.csv", []string{"member_no", "member", translate(languageFromRequest(c), "member_type"), "loan_id", "amount", "date", "reference_no", "note"}, func(w *csv.Writer) error {
		for _, repayment := range repayments {
			if err := w.Write([]string{repayment.MemberNo, repayment.FullName, repayment.MemberTypeLabel, repayment.LoanID, strconv.Itoa(repayment.Amount), repayment.RecordDate, repayment.ReferenceNo, repayment.Note}); err != nil {
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
