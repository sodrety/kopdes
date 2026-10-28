package app

import (
	"bytes"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed templates/loan-disbursement-application-form.jpg
var disbursementApplicationFormTemplate embed.FS

var errDisbursementApplicationFormUnavailable = errors.New("disbursement application form is unavailable")

type disbursementFormMember struct {
	MemberNo    string
	FullName    string
	MemberType  string
	Status      string
	BankName    string
	BankAccount string
}

type disbursementApplicationFormData struct {
	ID                       string
	MemberID                 string
	MemberNo                 string
	FullName                 string
	MemberType               string
	MemberStatus             string
	BankName                 string
	BankAccount              string
	Purpose                  string
	RequestDate              string
	RequestedAmount          int64
	RequestedDuration        int
	ApprovedAmount           int64
	ApprovedDuration         int
	TotalAdminFee            int64
	TotalObligation          int64
	MonthlyInstallment       int64
	WithdrawalAmount         int64
	AvailableWithdrawalLimit int64
	Savings                  SavingSummary
	PriorCashLoan            priorLoanReportLine
	PriorGoodsLoan           priorLoanReportLine
	Approvals                []ApprovalDecision
}

func (s *Server) exportLoanDisbursementApplicationFormPDF(c *gin.Context) {
	lang := languageFromRequest(c)
	user, _ := currentUser(c)
	if user.Role != "bendahara" {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_loan_disbursement_bendahara_only"))
		return
	}
	report, err := s.loanDisbursementApplicationFormData(c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDisbursementApplicationFormUnavailable) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Loan request not found"))
			return
		}
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	pdf, err := buildLoanApplicationFormPDF(report)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	filename := "form-pengajuan-pinjaman-" + safeLoanReportFilename(report.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) exportLoanRequestApplicationFormPDF(c *gin.Context) {
	lang := languageFromRequest(c)
	report, err := s.loanRequestApplicationFormData(c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDisbursementApplicationFormUnavailable) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Loan request not found"))
			return
		}
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	pdf, err := buildLoanApplicationFormPDF(report)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	filename := "form-pengajuan-pinjaman-" + safeLoanReportFilename(report.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) loanRequestApplicationFormData(requestID string) (disbursementApplicationFormData, error) {
	request, err := s.loanRequestByID(requestID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	return s.loanApplicationFormDataFromRequest(request)
}

func (s *Server) loanDisbursementApplicationFormData(requestID string) (disbursementApplicationFormData, error) {
	request, err := s.loanRequestByID(requestID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	if request.Status != "approved" || request.DisbursementDate != "" {
		return disbursementApplicationFormData{}, errDisbursementApplicationFormUnavailable
	}
	return s.loanApplicationFormDataFromRequest(request)
}

func (s *Server) loanApplicationFormDataFromRequest(request LoanRequest) (disbursementApplicationFormData, error) {
	member, err := s.disbursementFormMember(request.MemberID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	report := disbursementApplicationFormData{
		ID:                request.ID,
		MemberID:          request.MemberID,
		MemberNo:          member.MemberNo,
		FullName:          member.FullName,
		MemberType:        member.MemberType,
		MemberStatus:      member.Status,
		BankName:          member.BankName,
		BankAccount:       member.BankAccount,
		Purpose:           strings.TrimSpace(request.Purpose),
		RequestDate:       loanReportDate(request.CreatedAt),
		RequestedAmount:   request.RequestedAmount,
		RequestedDuration: request.DurationMonths,
		ApprovedAmount:    request.ProposedApprovedAmount,
		ApprovedDuration:  request.ProposedDurationMonths,
		TotalAdminFee:     request.ProposedTotalAdminFee,
		TotalObligation:   request.ProposedTotalObligation,
	}
	if report.ApprovedAmount <= 0 {
		report.ApprovedAmount = request.RequestedAmount
	}
	if report.ApprovedDuration <= 0 {
		report.ApprovedDuration = request.DurationMonths
	}
	if report.TotalObligation <= 0 && report.ApprovedAmount > 0 {
		report.TotalObligation = report.ApprovedAmount + report.TotalAdminFee
	}
	if report.TotalObligation > 0 && report.ApprovedDuration > 0 {
		startDate := strings.TrimSpace(request.ProposedStartDate)
		if startDate == "" {
			startDate = time.Now().In(jakartaLocation).Format("2006-01-02")
		}
		schedule, err := buildLoanScheduleFromObligation(report.TotalObligation, report.ApprovedDuration, startDate, maxLoanDurationForType(request.LoanType))
		if err != nil {
			return disbursementApplicationFormData{}, err
		}
		if len(schedule.Installments) > 0 {
			report.MonthlyInstallment = schedule.Installments[0].ScheduledAmount
		}
	}
	if report.Purpose == "" {
		report.Purpose = loanReportTypeLabel(request.LoanType)
	}
	report.Approvals, err = approvalHistoryWithOverrides(s.db, "loan_request_approvals", "loan", request.ID, true)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	if err := s.loadDisbursementFormBalances(&report); err != nil {
		return disbursementApplicationFormData{}, err
	}
	return report, nil
}

func buildLoanApplicationFormPDF(report disbursementApplicationFormData) ([]byte, error) {
	background, err := disbursementApplicationFormTemplate.ReadFile("templates/loan-disbursement-application-form.jpg")
	if err != nil {
		return nil, err
	}
	// Clear the template's signature rule so the member name replaces it.
	masks := []loanPDFMask{{x: 70, y: 686, width: 236, height: 14}}
	return buildLoanFormPDF(background, loanDisbursementApplicationFormText(report), masks)
}

func (s *Server) exportWithdrawalDisbursementApplicationFormPDF(c *gin.Context) {
	lang := languageFromRequest(c)
	user, _ := currentUser(c)
	if user.Role != "bendahara" {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_withdrawal_disbursement_bendahara_only"))
		return
	}
	report, err := s.withdrawalDisbursementApplicationFormData(c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDisbursementApplicationFormUnavailable) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Withdrawal request not found"))
			return
		}
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	pdf, err := buildWithdrawalDisbursementApplicationFormPDF(report)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	filename := "form-penarikan-simpanan-" + safeLoanReportFilename(report.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) withdrawalDisbursementApplicationFormData(requestID string) (disbursementApplicationFormData, error) {
	return s.withdrawalApplicationFormData(requestID, true)
}

func (s *Server) withdrawalRequestApplicationFormData(requestID string) (disbursementApplicationFormData, error) {
	return s.withdrawalApplicationFormData(requestID, false)
}

func (s *Server) withdrawalApplicationFormData(requestID string, requireDisbursementReady bool) (disbursementApplicationFormData, error) {
	request, err := s.withdrawalRequestByID(requestID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	if requireDisbursementReady && (request.Status != "approved" || request.DisbursementDate != "" || request.SavingRecordID != "") {
		return disbursementApplicationFormData{}, errDisbursementApplicationFormUnavailable
	}
	member, err := s.disbursementFormMember(request.MemberID)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	report := disbursementApplicationFormData{
		ID:               request.ID,
		MemberID:         request.MemberID,
		MemberNo:         member.MemberNo,
		FullName:         member.FullName,
		MemberType:       member.MemberType,
		MemberStatus:     member.Status,
		BankName:         member.BankName,
		BankAccount:      member.BankAccount,
		Purpose:          strings.TrimSpace(request.Note),
		RequestDate:      loanReportDate(request.CreatedAt),
		WithdrawalAmount: request.Amount,
	}
	report.Approvals, err = approvalHistoryWithOverrides(s.db, "withdrawal_request_approvals", "withdrawal", request.ID, true)
	if err != nil {
		return disbursementApplicationFormData{}, err
	}
	if err := s.loadDisbursementFormBalances(&report); err != nil {
		return disbursementApplicationFormData{}, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM withdrawal_reservations WHERE request_id=$1 AND status='active'`, request.ID).Scan(&report.AvailableWithdrawalLimit); err != nil {
		return disbursementApplicationFormData{}, err
	}
	report.AvailableWithdrawalLimit += report.Savings.AvailableWithdrawalBalance
	return report, nil
}

func (s *Server) exportWithdrawalRequestApplicationFormPDF(c *gin.Context) {
	lang := languageFromRequest(c)
	report, err := s.withdrawalRequestApplicationFormData(c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Withdrawal request not found"))
			return
		}
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	pdf, err := buildWithdrawalDisbursementApplicationFormPDF(report)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	filename := "form-penarikan-simpanan-" + safeLoanReportFilename(report.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) disbursementFormMember(memberID string) (disbursementFormMember, error) {
	var member disbursementFormMember
	err := s.db.QueryRow(
		`SELECT COALESCE(NULLIF(TRIM(old_npp),''),member_no,''), COALESCE(full_name,''), COALESCE(member_type,'employee'), COALESCE(status,'active'), COALESCE(NULLIF(TRIM(bank_name),''),''), COALESCE(NULLIF(TRIM(bank_account),''),'') FROM members WHERE id=$1`,
		memberID,
	).Scan(&member.MemberNo, &member.FullName, &member.MemberType, &member.Status, &member.BankName, &member.BankAccount)
	return member, err
}

func (s *Server) loadDisbursementFormBalances(report *disbursementApplicationFormData) error {
	summary, err := s.savingSummary(report.MemberID)
	if err != nil {
		return err
	}
	report.Savings = summary
	loanReport := loanReportData{Loan: Loan{MemberID: report.MemberID}}
	if err := s.loadPriorLoanReportLines(&loanReport); err != nil {
		return err
	}
	report.PriorCashLoan = loanReport.PriorCashLoan
	report.PriorGoodsLoan = loanReport.PriorGoodsLoan
	return nil
}

func loanDisbursementApplicationFormText(report disbursementApplicationFormData) []loanPDFText {
	const (
		mainFieldSize     = 9.5
		considerationSize = 8.5
		bankValueX        = 746
		bankValueY        = 308
	)
	totalSavings := report.Savings.WajibBalance + report.Savings.SukarelaBalance
	loanCeiling := maxLoanAmountForMemberSavings(report.MemberType, report.Savings.WajibBalance, report.Savings.SukarelaBalance)
	texts := []loanPDFText{
		{257, 223, mainFieldSize, report.FullName, 50},
		{257, 247, mainFieldSize, report.MemberNo, 35},
		{257, 270, mainFieldSize, loanMemberStatusLabel(report.MemberType, report.MemberStatus), 44},
		{bankValueX, bankValueY, mainFieldSize, report.BankName, 24},
		{bankValueX, bankValueY + 22, mainFieldSize, report.BankAccount, 27},
		{257, 330, mainFieldSize, formatLoanReportAmount(report.RequestedAmount), 28},
		{257, 353, mainFieldSize, indonesianRupiahWords(report.RequestedAmount), 104},
		{257, 412, mainFieldSize, report.Purpose, 90},
		// Keep the tenor in the blank before the template's printed "Bulan" label.
		{310, 461, mainFieldSize, strconv.Itoa(report.RequestedDuration), 5},
		{138, 587, mainFieldSize, loanReportDateLong(report.RequestDate), 30},
		{70, 694, considerationSize, report.FullName, 36},
		{310, 806, considerationSize, formatLoanReportAmount(report.Savings.WajibBalance), 28},
		{310, 830, considerationSize, formatLoanReportAmount(report.Savings.SukarelaBalance), 28},
		{310, 871, considerationSize, formatLoanReportAmount(totalSavings), 28},
		{310, 897, considerationSize, formatLoanReportAmount(loanCeiling), 28},
		{350, 982, considerationSize, formatPriorLoanAmount(report.PriorCashLoan, report.PriorCashLoan.MonthlyInstallment), 28},
		{605, 982, considerationSize, formatPriorLoanAmount(report.PriorGoodsLoan, report.PriorGoodsLoan.MonthlyInstallment), 28},
		{350, 1007, considerationSize, formatPriorLoanInstallments(report.PriorCashLoan), 18},
		{605, 1007, considerationSize, formatPriorLoanInstallments(report.PriorGoodsLoan), 18},
		{350, 1032, considerationSize, formatPriorLoanAmount(report.PriorCashLoan, report.PriorCashLoan.RemainingBalance), 28},
		{605, 1032, considerationSize, formatPriorLoanAmount(report.PriorGoodsLoan, report.PriorGoodsLoan.RemainingBalance), 28},
		{100, 1121, considerationSize, formatLoanReportAmount(report.ApprovedAmount), 24},
		{304, 1121, considerationSize, formatLoanReportAmount(report.TotalAdminFee), 24},
		{478, 1121, considerationSize, strconv.Itoa(report.ApprovedDuration), 4},
		{541, 1121, considerationSize, formatLoanReportAmount(report.TotalObligation), 25},
		{748, 1121, considerationSize, formatLoanReportAmount(report.MonthlyInstallment), 24},
		{336, 1208, considerationSize, disbursementApprovalFormText(report.Approvals, approvalStageManager), 90},
		{336, 1241, considerationSize, disbursementApprovalFormText(report.Approvals, approvalStageKetuaII), 90},
		{336, 1274, considerationSize, disbursementApprovalFormText(report.Approvals, approvalStageKetuaI), 90},
		{336, 1318, considerationSize, fmt.Sprintf("Disetujui: Rp %s, %d bulan", formatLoanReportAmount(report.ApprovedAmount), report.ApprovedDuration), 88},
	}
	return texts
}

func formatPriorLoanAmount(line priorLoanReportLine, amount int64) string {
	if !line.HasLoan {
		return ""
	}
	return formatLoanReportAmount(amount)
}

func formatPriorLoanInstallments(line priorLoanReportLine) string {
	if !line.HasLoan {
		return ""
	}
	return strconv.FormatInt(line.PaidInstallments, 10)
}

func disbursementApprovalFormText(approvals []ApprovalDecision, stage string) string {
	for _, approval := range approvals {
		if approval.Stage != stage {
			continue
		}
		parts := []string{loanApprovalDecisionLabel(approval.Decision)}
		if name := strings.TrimSpace(approval.OfficerName); name != "" {
			parts = append(parts, name)
		}
		if date := loanReportDate(approval.CreatedAt); date != "" {
			parts = append(parts, date)
		}
		if note := strings.TrimSpace(approval.Note); note != "" {
			parts = append(parts, note)
		} else if reason := strings.TrimSpace(approval.Reason); reason != "" {
			parts = append(parts, reason)
		}
		return strings.Join(parts, " - ")
	}
	return ""
}

func buildWithdrawalDisbursementApplicationFormPDF(report disbursementApplicationFormData) ([]byte, error) {
	const pageWidth, pageHeight = 595.28, 841.89
	var content strings.Builder
	writeWithdrawalFormText(&content, pageHeight, 42, 36, 9, "KOPERASI KONSUMEN", true)
	writeWithdrawalFormText(&content, pageHeight, 42, 49, 9, "SERBA USAHA KARYAWAN", true)
	writeWithdrawalFormText(&content, pageHeight, 42, 65, 14, "PD DHARMA JAYA", true)
	writeWithdrawalFormText(&content, pageHeight, 42, 80, 8, "Jl. Raya Penggilingan, Cakung", false)
	writeWithdrawalFormText(&content, pageHeight, 42, 93, 8, "Jakarta Timur, DKI Jakarta", false)
	writeWithdrawalFormText(&content, pageHeight, 180, 119, 14, "SURAT PERMOHONAN PENARIKAN", true)
	writeWithdrawalFormText(&content, pageHeight, 220, 137, 14, "SIMPANAN MANASUKA", true)
	writeWithdrawalFormLine(&content, pageHeight, 42, 148, 553, 148)

	writeWithdrawalFormText(&content, pageHeight, 42, 177, 8, "Nama Anggota :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 177, 8, truncateLoanPDFText(report.FullName, 32), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 198, 8, "NPP Perusahaan :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 198, 8, truncateLoanPDFText(report.MemberNo, 28), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 219, 8, "Status Anggota :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 219, 8, truncateLoanPDFText(loanMemberStatusLabel(report.MemberType, report.MemberStatus), 28), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 245, 8, "Jumlah Penarikan :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 245, 8, "Rp "+formatLoanReportAmount(report.WithdrawalAmount), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 266, 8, "Terbilang :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 266, 8, truncateLoanPDFText(indonesianRupiahWords(report.WithdrawalAmount), 91), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 289, 8, "Keperluan :", false)
	writeWithdrawalFormText(&content, pageHeight, 167, 289, 8, truncateLoanPDFText(report.Purpose, 78), false)
	writeWithdrawalFormText(&content, pageHeight, 347, 177, 8, "Kontrak Kerja s.d:", false)
	writeWithdrawalFormLine(&content, pageHeight, 436, 184, 553, 184)
	writeWithdrawalFormText(&content, pageHeight, 347, 198, 8, "Nama Bank :", false)
	writeWithdrawalFormText(&content, pageHeight, 431, 198, 8, truncateLoanPDFText(report.BankName, 18), false)
	writeWithdrawalFormText(&content, pageHeight, 347, 219, 8, "No Rekening :", false)
	writeWithdrawalFormText(&content, pageHeight, 431, 219, 8, truncateLoanPDFText(report.BankAccount, 19), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 332, 8, "Jakarta, "+loanReportDateLong(report.RequestDate), false)
	writeWithdrawalFormText(&content, pageHeight, 42, 351, 8, "Pemohon", false)
	writeWithdrawalFormText(&content, pageHeight, 42, 391, 8, truncateLoanPDFText(report.FullName, 32), false)

	writeWithdrawalFormText(&content, pageHeight, 226, 428, 11, "KOLOM PERTIMBANGAN", true)
	writeWithdrawalFormLine(&content, pageHeight, 191, 434, 404, 434)
	writeWithdrawalFormText(&content, pageHeight, 42, 458, 8, "1. Rincian Tabungan :", true)
	writeWithdrawalFormText(&content, pageHeight, 88, 481, 8, "- Wajib :", false)
	writeWithdrawalFormText(&content, pageHeight, 260, 481, 8, formatLoanReportAmount(report.Savings.WajibBalance), false)
	writeWithdrawalFormText(&content, pageHeight, 88, 502, 8, "- Manasuka :", false)
	writeWithdrawalFormText(&content, pageHeight, 260, 502, 8, formatLoanReportAmount(report.Savings.SukarelaBalance), false)
	writeWithdrawalFormLine(&content, pageHeight, 88, 508, 329, 508)
	writeWithdrawalFormText(&content, pageHeight, 88, 529, 8, "Total :", true)
	writeWithdrawalFormText(&content, pageHeight, 260, 529, 8, formatLoanReportAmount(report.Savings.WajibBalance+report.Savings.SukarelaBalance), false)
	writeWithdrawalFormText(&content, pageHeight, 88, 550, 8, "Total Plafond :", false)
	writeWithdrawalFormText(&content, pageHeight, 260, 550, 8, formatLoanReportAmount(maxLoanAmountForMemberSavings(report.MemberType, report.Savings.WajibBalance, report.Savings.SukarelaBalance)), false)
	writeWithdrawalFormText(&content, pageHeight, 88, 571, 8, "Total Saldo Bisa ditarik :", true)
	writeWithdrawalFormText(&content, pageHeight, 260, 571, 8, formatLoanReportAmount(report.AvailableWithdrawalLimit), false)

	writeWithdrawalFormText(&content, pageHeight, 42, 607, 8, "2. Rincian Pinjaman sebelum :", true)
	withdrawalFormTable(&content, pageHeight, report)
	writeWithdrawalFormText(&content, pageHeight, 42, 736, 8, "3. Pertimbangan Manager :", true)
	writeWithdrawalFormText(&content, pageHeight, 232, 736, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageManager), 80), false)
	writeWithdrawalFormLine(&content, pageHeight, 228, 742, 553, 742)
	writeWithdrawalFormText(&content, pageHeight, 42, 760, 8, "4. Pertimbangan Ketua II :", true)
	writeWithdrawalFormText(&content, pageHeight, 232, 760, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageKetuaII), 80), false)
	writeWithdrawalFormLine(&content, pageHeight, 228, 766, 553, 766)
	writeWithdrawalFormText(&content, pageHeight, 42, 784, 8, "5. Pertimbangan Ketua I :", true)
	writeWithdrawalFormText(&content, pageHeight, 232, 784, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageKetuaI), 80), false)
	writeWithdrawalFormLine(&content, pageHeight, 228, 790, 553, 790)
	writeWithdrawalFormText(&content, pageHeight, 42, 813, 8, "6. Keputusan :", true)
	writeWithdrawalFormText(&content, pageHeight, 232, 813, 7, fmt.Sprintf("Disetujui Rp %s", formatLoanReportAmount(report.WithdrawalAmount)), false)

	contentBytes := []byte(content.String())
	objects := []loanPDFObject{
		{body: "<< /Type /Catalog /Pages 2 0 R >>"},
		{body: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{body: fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> /Contents 4 0 R >>", pageWidth, pageHeight)},
		{body: fmt.Sprintf("<< /Length %d >>", len(contentBytes)), stream: contentBytes},
		{body: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"},
		{body: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>"},
	}
	return writeDisbursementFormPDF(objects)
}

func withdrawalFormTable(content *strings.Builder, pageHeight float64, report disbursementApplicationFormData) {
	const x0, x1, x2, x3 = 60.0, 212.0, 375.0, 540.0
	const top, headerHeight, rowHeight = 616.0, 22.0, 20.0
	writeWithdrawalFormFill(content, pageHeight, x0, top, x3-x0, headerHeight, 0.78, 0.91, 0.96)
	for _, x := range []float64{x0, x1, x2, x3} {
		writeWithdrawalFormLine(content, pageHeight, x, top, x, top+headerHeight+3*rowHeight)
	}
	for _, y := range []float64{top, top + headerHeight, top + headerHeight + rowHeight, top + headerHeight + 2*rowHeight, top + headerHeight + 3*rowHeight} {
		writeWithdrawalFormLine(content, pageHeight, x0, y, x3, y)
	}
	writeWithdrawalFormText(content, pageHeight, x0+5, top+15, 8, "Keterangan", true)
	writeWithdrawalFormText(content, pageHeight, x1+5, top+15, 8, "Pinjaman Uang", true)
	writeWithdrawalFormText(content, pageHeight, x2+5, top+15, 8, "Pinjaman Barang", true)
	rows := []struct {
		label string
		cash  string
		goods string
	}{
		{"Angsuran Perbulan", formatPriorLoanAmount(report.PriorCashLoan, report.PriorCashLoan.MonthlyInstallment), formatPriorLoanAmount(report.PriorGoodsLoan, report.PriorGoodsLoan.MonthlyInstallment)},
		{"Sudah Mencicil Sebanyak", withdrawalFormInstallments(report.PriorCashLoan), withdrawalFormInstallments(report.PriorGoodsLoan)},
		{"Sisa Saldo Pinjaman", formatPriorLoanAmount(report.PriorCashLoan, report.PriorCashLoan.RemainingBalance), formatPriorLoanAmount(report.PriorGoodsLoan, report.PriorGoodsLoan.RemainingBalance)},
	}
	for index, row := range rows {
		baseline := top + headerHeight + float64(index+1)*rowHeight - 6
		writeWithdrawalFormText(content, pageHeight, x0+5, baseline, 7, row.label, false)
		writeWithdrawalFormText(content, pageHeight, x1+5, baseline, 7, row.cash, false)
		writeWithdrawalFormText(content, pageHeight, x2+5, baseline, 7, row.goods, false)
	}
}

func withdrawalFormInstallments(line priorLoanReportLine) string {
	if !line.HasLoan {
		return ""
	}
	return strconv.FormatInt(line.PaidInstallments, 10) + " Kali / Bulan"
}

func writeWithdrawalFormText(content *strings.Builder, pageHeight, x, topBaseline, size float64, value string, bold bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	value = truncateLoanPDFText(value, 120)
	font := "F1"
	if bold {
		font = "F2"
	}
	value = escapePDFText(loanPDFASCII(value))
	fmt.Fprintf(content, "BT /%s %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", font, size, x, pageHeight-topBaseline, value)
}

func writeWithdrawalFormLine(content *strings.Builder, pageHeight, x1, top1, x2, top2 float64) {
	fmt.Fprintf(content, "q 0.6 w %.2f %.2f m %.2f %.2f l S Q\n", x1, pageHeight-top1, x2, pageHeight-top2)
}

func writeWithdrawalFormFill(content *strings.Builder, pageHeight, x, top, width, height, red, green, blue float64) {
	fmt.Fprintf(content, "q %.2f %.2f %.2f rg %.2f %.2f %.2f %.2f re f Q\n", red, green, blue, x, pageHeight-top-height, width, height)
}

func writeDisbursementFormPDF(objects []loanPDFObject) ([]byte, error) {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\n", index+1, object.body)
		if object.stream != nil {
			pdf.WriteString("stream\n")
			pdf.Write(object.stream)
			pdf.WriteString("\nendstream\n")
		}
		pdf.WriteString("endobj\n")
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return pdf.Bytes(), nil
}
