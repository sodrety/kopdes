package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) adminWithdrawalRequestApplicationFormPrintPage(c *gin.Context) {
	lang := languageFromRequest(c)
	report, err := s.withdrawalRequestApplicationFormData(c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_withdrawal_request_not_found"))
			return
		}
		respondLoanFormPrintInternalError(c)
		return
	}
	background, err := withdrawalApplicationFormSVG(report)
	if err != nil {
		respondLoanFormPrintInternalError(c)
		return
	}
	requestID := url.PathEscape(report.ID)
	renderPage(c, "admin-loan-form-print", gin.H{
		"Lang":              lang,
		"Title":             translate(lang, "withdrawal_application_print"),
		"BackURL":           "/admin/withdrawal-requests#withdrawal-request-review-" + requestID,
		"BackgroundDataURI": template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(background)),
		"Fields":            []loanFormPrintField{},
		"Masks":             []loanFormPrintMask{},
	})
}

func withdrawalApplicationFormSVG(report disbursementApplicationFormData) ([]byte, error) {
	logo, err := staticAssetFS.ReadFile("static/images/lambang-koperasi.png")
	if err != nil {
		return nil, err
	}

	var svg strings.Builder
	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 595.28 841.89" width="595.28" height="841.89"><rect width="595.28" height="841.89" fill="#fff"/>`)
	svg.WriteString(`<image x="24" y="16" width="68" height="68" href="data:image/png;base64,`)
	svg.WriteString(base64.StdEncoding.EncodeToString(logo))
	svg.WriteString(`"/>`)

	writeWithdrawalFormSVGText(&svg, 105, 36, 9, "KOPERASI KONSUMEN", true)
	writeWithdrawalFormSVGText(&svg, 105, 49, 9, "SERBA USAHA KARYAWAN", true)
	writeWithdrawalFormSVGText(&svg, 105, 65, 14, "PD DHARMA JAYA", true)
	writeWithdrawalFormSVGText(&svg, 105, 80, 8, "Jl. Raya Penggilingan, Cakung", false)
	writeWithdrawalFormSVGText(&svg, 105, 93, 8, "Jakarta Timur, DKI Jakarta", false)
	writeWithdrawalFormSVGText(&svg, 180, 119, 14, "SURAT PERMOHONAN PENARIKAN", true)
	writeWithdrawalFormSVGText(&svg, 220, 137, 14, "SIMPANAN MANASUKA", true)
	writeWithdrawalFormSVGLine(&svg, 42, 148, 553, 148)

	writeWithdrawalFormSVGText(&svg, 42, 177, 8, "Nama Anggota :", false)
	writeWithdrawalFormSVGText(&svg, 167, 177, 8, truncateLoanPDFText(report.FullName, 32), false)
	writeWithdrawalFormSVGText(&svg, 42, 198, 8, "NPP Perusahaan :", false)
	writeWithdrawalFormSVGText(&svg, 167, 198, 8, truncateLoanPDFText(report.MemberNo, 28), false)
	writeWithdrawalFormSVGText(&svg, 42, 219, 8, "Status Anggota :", false)
	writeWithdrawalFormSVGText(&svg, 167, 219, 8, truncateLoanPDFText(loanMemberStatusLabel(report.MemberType, report.MemberStatus), 28), false)
	writeWithdrawalFormSVGText(&svg, 42, 245, 8, "Jumlah Penarikan :", false)
	writeWithdrawalFormSVGText(&svg, 167, 245, 8, "Rp "+formatLoanReportAmount(report.WithdrawalAmount), false)
	writeWithdrawalFormSVGText(&svg, 42, 266, 8, "Terbilang :", false)
	writeWithdrawalFormSVGText(&svg, 167, 266, 8, truncateLoanPDFText(indonesianRupiahWords(report.WithdrawalAmount), 91), false)
	writeWithdrawalFormSVGText(&svg, 42, 289, 8, "Keperluan :", false)
	writeWithdrawalFormSVGText(&svg, 167, 289, 8, truncateLoanPDFText(report.Purpose, 78), false)
	writeWithdrawalFormSVGText(&svg, 347, 177, 8, "Kontrak Kerja s.d:", false)
	writeWithdrawalFormSVGLine(&svg, 436, 184, 553, 184)
	writeWithdrawalFormSVGText(&svg, 347, 198, 8, "Nama Bank :", false)
	writeWithdrawalFormSVGText(&svg, 431, 198, 8, truncateLoanPDFText(report.BankName, 18), false)
	writeWithdrawalFormSVGText(&svg, 347, 219, 8, "No Rekening :", false)
	writeWithdrawalFormSVGText(&svg, 431, 219, 8, truncateLoanPDFText(report.BankAccount, 19), false)
	writeWithdrawalFormSVGText(&svg, 42, 332, 8, "Jakarta, "+loanReportDateLong(report.RequestDate), false)
	writeWithdrawalFormSVGText(&svg, 42, 351, 8, "Pemohon", false)
	writeWithdrawalFormSVGText(&svg, 42, 391, 8, truncateLoanPDFText(report.FullName, 32), false)

	writeWithdrawalFormSVGText(&svg, 226, 428, 11, "KOLOM PERTIMBANGAN", true)
	writeWithdrawalFormSVGLine(&svg, 191, 434, 404, 434)
	writeWithdrawalFormSVGText(&svg, 42, 458, 8, "1. Rincian Tabungan :", true)
	writeWithdrawalFormSVGText(&svg, 88, 481, 8, "- Wajib :", false)
	writeWithdrawalFormSVGText(&svg, 260, 481, 8, formatLoanReportAmount(report.Savings.WajibBalance), false)
	writeWithdrawalFormSVGText(&svg, 88, 502, 8, "- Manasuka :", false)
	writeWithdrawalFormSVGText(&svg, 260, 502, 8, formatLoanReportAmount(report.Savings.SukarelaBalance), false)
	writeWithdrawalFormSVGLine(&svg, 88, 508, 329, 508)
	writeWithdrawalFormSVGText(&svg, 88, 529, 8, "Total :", true)
	writeWithdrawalFormSVGText(&svg, 260, 529, 8, formatLoanReportAmount(report.Savings.WajibBalance+report.Savings.SukarelaBalance), false)
	writeWithdrawalFormSVGText(&svg, 88, 550, 8, "Total Plafond :", false)
	writeWithdrawalFormSVGText(&svg, 260, 550, 8, formatLoanReportAmount(maxLoanAmountForMemberSavings(report.MemberType, report.Savings.WajibBalance, report.Savings.SukarelaBalance)), false)
	writeWithdrawalFormSVGText(&svg, 88, 571, 8, "Total Saldo Bisa ditarik :", true)
	writeWithdrawalFormSVGText(&svg, 260, 571, 8, formatLoanReportAmount(report.AvailableWithdrawalLimit), false)

	writeWithdrawalFormSVGText(&svg, 42, 607, 8, "2. Rincian Pinjaman sebelum :", true)
	withdrawalFormSVGTable(&svg, report)
	writeWithdrawalFormSVGText(&svg, 42, 736, 8, "3. Pertimbangan Manager :", true)
	writeWithdrawalFormSVGText(&svg, 232, 736, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageManager), 80), false)
	writeWithdrawalFormSVGLine(&svg, 228, 742, 553, 742)
	writeWithdrawalFormSVGText(&svg, 42, 760, 8, "4. Pertimbangan Ketua II :", true)
	writeWithdrawalFormSVGText(&svg, 232, 760, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageKetuaII), 80), false)
	writeWithdrawalFormSVGLine(&svg, 228, 766, 553, 766)
	writeWithdrawalFormSVGText(&svg, 42, 784, 8, "5. Pertimbangan Ketua I :", true)
	writeWithdrawalFormSVGText(&svg, 232, 784, 7, truncateLoanPDFText(disbursementApprovalFormText(report.Approvals, approvalStageKetuaI), 80), false)
	writeWithdrawalFormSVGLine(&svg, 228, 790, 553, 790)
	writeWithdrawalFormSVGText(&svg, 42, 813, 8, "6. Keputusan :", true)
	writeWithdrawalFormSVGText(&svg, 232, 813, 7, "Disetujui Rp "+formatLoanReportAmount(report.WithdrawalAmount), false)
	svg.WriteString(`</svg>`)
	return []byte(svg.String()), nil
}

func withdrawalFormSVGTable(svg *strings.Builder, report disbursementApplicationFormData) {
	const x0, x1, x2, x3 = 60.0, 212.0, 375.0, 540.0
	const top, headerHeight, rowHeight = 616.0, 22.0, 20.0
	svg.WriteString(`<rect x="60" y="616" width="480" height="22" fill="#c7e8f5"/>`)
	for _, x := range []float64{x0, x1, x2, x3} {
		writeWithdrawalFormSVGLine(svg, x, top, x, top+headerHeight+3*rowHeight)
	}
	for _, y := range []float64{top, top + headerHeight, top + headerHeight + rowHeight, top + headerHeight + 2*rowHeight, top + headerHeight + 3*rowHeight} {
		writeWithdrawalFormSVGLine(svg, x0, y, x3, y)
	}
	writeWithdrawalFormSVGText(svg, x0+5, top+15, 8, "Keterangan", true)
	writeWithdrawalFormSVGText(svg, x1+5, top+15, 8, "Pinjaman Uang", true)
	writeWithdrawalFormSVGText(svg, x2+5, top+15, 8, "Pinjaman Barang", true)
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
		writeWithdrawalFormSVGText(svg, x0+5, baseline, 7, row.label, false)
		writeWithdrawalFormSVGText(svg, x1+5, baseline, 7, row.cash, false)
		writeWithdrawalFormSVGText(svg, x2+5, baseline, 7, row.goods, false)
	}
}

func writeWithdrawalFormSVGText(svg *strings.Builder, x, y, size float64, value string, bold bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	weight := "400"
	if bold {
		weight = "700"
	}
	fmt.Fprintf(svg, `<text x="%.2f" y="%.2f" font-family="Helvetica,Arial,sans-serif" font-size="%.2f" font-weight="%s">%s</text>`, x, y, size, weight, html.EscapeString(value))
}

func writeWithdrawalFormSVGLine(svg *strings.Builder, x1, y1, x2, y2 float64) {
	fmt.Fprintf(svg, `<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="#000" stroke-width="0.6"/>`, x1, y1, x2, y2)
}
