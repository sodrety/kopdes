package app

import (
	"bytes"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/unicode/norm"
)

//go:embed templates/loan-application-form.jpg templates/loan-acceptance-form.jpg
var loanReportTemplates embed.FS

type loanReportData struct {
	Loan            Loan
	Language        string
	MemberNo        string
	MemberName      string
	MemberType      string
	MemberStatus    string
	Purpose         string
	RequestAmount   int64
	RequestDuration int
	RequestDate     string
	Savings         SavingSummary
	Approvals       []ApprovalDecision
	PriorCashLoan   priorLoanReportLine
	PriorGoodsLoan  priorLoanReportLine
}

type priorLoanReportLine struct {
	MonthlyInstallment int64
	PaidInstallments   int64
	RemainingBalance   int64
	HasLoan            bool
}

type loanPDFText struct {
	x, y, size float64
	value      string
	maxRunes   int
}

type loanPDFMask struct {
	x, y, width, height float64
}

type loanPDFObject struct {
	body   string
	stream []byte
}

// Loan form field positions are measured on an A4 preview rendered at 990x1400.
const (
	loanFormReferenceWidth  = 990.0
	loanFormReferenceHeight = 1400.0
)

func (s *Server) exportLoanApplicationFormPDF(c *gin.Context) {
	report, ok := s.loanReportData(c)
	if !ok {
		return
	}
	background, err := loanReportTemplates.ReadFile("templates/loan-application-form.jpg")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	texts := loanApplicationFormText(report)
	pdf, err := buildLoanFormPDF(background, texts, loanApplicationFormMasks(report))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	filename := "form-pengajuan-pinjaman-" + safeLoanReportFilename(report.Loan.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) exportLoanAcceptancePDF(c *gin.Context) {
	report, ok := s.loanReportData(c)
	if !ok {
		return
	}
	background, err := loanReportTemplates.ReadFile("templates/loan-acceptance-form.jpg")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	pdf, err := buildLoanFormPDF(background, loanAcceptanceFormText(report), loanAcceptanceFormMasks(report))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	filename := "surat-akseptasi-pinjaman-" + safeLoanReportFilename(report.Loan.ID) + ".pdf"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) loanReportData(c *gin.Context) (loanReportData, bool) {
	lang := languageFromRequest(c)
	loan, err := s.loanByID(c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error.Loan not found"))
		return loanReportData{}, false
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return loanReportData{}, false
	}

	report := loanReportData{Loan: loan, Language: lang}
	var requestDate any
	err = s.db.QueryRow(
		`SELECT m.member_no, m.full_name, COALESCE(m.member_type,'employee'), COALESCE(m.status,'active'),
			COALESCE(r.purpose,''), COALESCE(r.requested_amount,l.approved_amount),
			COALESCE(r.duration_months,l.duration_months), COALESCE(r.created_at,l.created_at)
		FROM loans l
		JOIN members m ON m.id=l.member_id
		LEFT JOIN loan_requests r ON r.id=l.loan_request_id
		WHERE l.id=$1`, loan.ID,
	).Scan(&report.MemberNo, &report.MemberName, &report.MemberType, &report.MemberStatus, &report.Purpose, &report.RequestAmount, &report.RequestDuration, &requestDate)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return loanReportData{}, false
	}
	report.RequestDate = loanReportDate(requestDate)
	if strings.TrimSpace(report.Purpose) == "" {
		report.Purpose = loanReportTypeLabel(loan.LoanType)
	}

	report.Savings, err = savingSummary(s.db, loan.MemberID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return loanReportData{}, false
	}
	if loan.LoanRequestID != "" {
		report.Approvals, err = approvalHistoryWithOverrides(s.db, "loan_request_approvals", "loan", loan.LoanRequestID, true)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
			return loanReportData{}, false
		}
	}
	if err := s.loadPriorLoanReportLines(&report); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return loanReportData{}, false
	}
	return report, true
}

func (s *Server) loadPriorLoanReportLines(report *loanReportData) error {
	rows, err := s.db.Query(
		`SELECT l.loan_type,
			COALESCE(SUM(l.monthly_installment),0),
			COALESCE(SUM((SELECT COUNT(*) FROM loan_installments i WHERE i.loan_id=l.id AND i.paid_amount>=i.scheduled_amount)),0),
			COALESCE(SUM(l.remaining_balance),0)
		FROM loans l
		WHERE l.member_id=$1 AND l.id<>$2 AND l.remaining_balance>0 AND l.status NOT IN ('cancelled','paid')
		GROUP BY l.loan_type`, report.Loan.MemberID, report.Loan.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var loanType string
		var line priorLoanReportLine
		if err := rows.Scan(&loanType, &line.MonthlyInstallment, &line.PaidInstallments, &line.RemainingBalance); err != nil {
			return err
		}
		line.HasLoan = true
		if loanType == "regular" {
			report.PriorCashLoan = line
		} else {
			report.PriorGoodsLoan.MonthlyInstallment += line.MonthlyInstallment
			report.PriorGoodsLoan.PaidInstallments += line.PaidInstallments
			report.PriorGoodsLoan.RemainingBalance += line.RemainingBalance
			report.PriorGoodsLoan.HasLoan = true
		}
	}
	return rows.Err()
}

func loanApplicationFormText(report loanReportData) []loanPDFText {
	requestAmount := report.RequestAmount
	if requestAmount <= 0 {
		requestAmount = report.Loan.ApprovedAmount
	}
	requestMonths := report.RequestDuration
	if requestMonths <= 0 {
		requestMonths = report.Loan.DurationMonths
	}
	texts := []loanPDFText{
		{220, 259.1, 8, report.MemberName, 62},
		{220, 285.7, 8, report.MemberNo, 45},
		{220, 312.3, 8, "", 0}, // Workplace is not recorded in the member profile.
		{220, 338.9, 8, loanMemberStatusLabel(report.MemberType, report.MemberStatus), 52},
		{242, 365.6, 8, formatLoanReportAmount(requestAmount), 48},
		{220, 392.2, 8, indonesianRupiahWords(requestAmount), 100},
		{129, 496.5, 7, translate(report.Language, "purpose") + ": " + report.Purpose, 115},
		{390, 525.2, 8, strconv.Itoa(requestMonths), 24},
		{139, 589.1, 8, loanReportDateLong(report.RequestDate), 45},
		{410, 830.2, 8, formatLoanReportAmount(report.Savings.WajibBalance), 32},
		{410, 856.8, 8, formatLoanReportAmount(report.Savings.SukarelaBalance), 32},
		{410, 892.8, 8, formatLoanReportAmount(report.Savings.WajibBalance + report.Savings.SukarelaBalance), 32},
	}
	texts = append(texts, priorLoanFormText(report.PriorCashLoan, 410)...)
	texts = append(texts, priorLoanFormText(report.PriorGoodsLoan, 708)...)
	texts = append(texts,
		loanApprovalFormText(report, approvalStageManager, 1164.5),
		loanApprovalFormText(report, approvalStageKetuaI, 1217.7),
		loanApprovalFormText(report, approvalStageKetuaII, 1270.9),
		loanPDFText{376, 1324.2, 7, fmt.Sprintf("Disetujui: Rp %s, %d bulan", formatLoanReportAmount(report.Loan.ApprovedAmount), report.Loan.DurationMonths), 95},
	)
	return texts
}

func loanApplicationFormMasks(report loanReportData) []loanPDFMask {
	requestAmount := report.RequestAmount
	if requestAmount <= 0 {
		requestAmount = report.Loan.ApprovedAmount
	}
	requestMonths := report.RequestDuration
	if requestMonths <= 0 {
		requestMonths = report.Loan.DurationMonths
	}
	var masks []loanPDFMask
	masks = appendLoanPDFFieldMask(masks, report.MemberName, 210, 259.1, 305)
	masks = appendLoanPDFFieldMask(masks, report.MemberNo, 210, 285.7, 305)
	masks = appendLoanPDFFieldMask(masks, loanMemberStatusLabel(report.MemberType, report.MemberStatus), 210, 338.9, 305)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(requestAmount), 236, 365.6, 171)
	masks = appendLoanPDFFieldMask(masks, indonesianRupiahWords(requestAmount), 212, 392.2, 718)
	masks = appendLoanPDFFieldMask(masks, strconv.Itoa(requestMonths), 350, 525.2, 91)
	masks = appendLoanPDFFieldMask(masks, loanReportDateLong(report.RequestDate), 130, 589.1, 157)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Savings.WajibBalance), 391, 830.2, 218)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Savings.SukarelaBalance), 399, 856.8, 210)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Savings.WajibBalance+report.Savings.SukarelaBalance), 391, 892.8, 218)
	for _, stage := range []struct {
		name string
		y    float64
	}{
		{approvalStageManager, 1164.5},
		{approvalStageKetuaI, 1217.7},
		{approvalStageKetuaII, 1270.9},
	} {
		approval := loanApprovalFormText(report, stage.name, stage.y)
		masks = appendLoanPDFFieldMask(masks, approval.value, 352, stage.y, 550)
	}
	decision := fmt.Sprintf("Disetujui: Rp %s, %d bulan", formatLoanReportAmount(report.Loan.ApprovedAmount), report.Loan.DurationMonths)
	return appendLoanPDFFieldMask(masks, decision, 352, 1324.2, 550)
}

func appendLoanPDFFieldMask(masks []loanPDFMask, value string, x, baselineY, width float64) []loanPDFMask {
	if strings.TrimSpace(value) == "" {
		return masks
	}
	return append(masks, loanPDFMask{x: x, y: baselineY - 11, width: width, height: 14})
}

func priorLoanFormText(line priorLoanReportLine, x float64) []loanPDFText {
	if !line.HasLoan {
		return nil
	}
	return []loanPDFText{
		{x, 1000.9, 7, formatLoanReportAmount(line.MonthlyInstallment), 30},
		{x, 1028.1, 7, strconv.FormatInt(line.PaidInstallments, 10) + " Kali", 24},
		{x, 1055.7, 7, formatLoanReportAmount(line.RemainingBalance), 30},
		{x, 1083, 7, "Aktif", 18},
	}
}

func loanApprovalFormText(report loanReportData, stage string, y float64) loanPDFText {
	for _, approval := range report.Approvals {
		if approval.Stage != stage {
			continue
		}
		parts := []string{loanApprovalDecisionLabel(approval.Decision)}
		if strings.TrimSpace(approval.OfficerName) != "" {
			parts = append(parts, approval.OfficerName)
		}
		if date := loanReportDate(approval.CreatedAt); date != "" {
			parts = append(parts, date)
		}
		if strings.TrimSpace(approval.Note) != "" {
			parts = append(parts, approval.Note)
		} else if strings.TrimSpace(approval.Reason) != "" {
			parts = append(parts, approval.Reason)
		}
		return loanPDFText{376, y, 7, strings.Join(parts, " - "), 104}
	}
	return loanPDFText{x: 376, y: y, size: 7}
}

func loanAcceptanceFormText(report loanReportData) []loanPDFText {
	date := loanReportDateLong(report.Loan.StartDate)
	if date == "" {
		date = loanReportDateLong(time.Now().In(jakartaLocation).Format("2006-01-02"))
	}
	amountWords, remainingAmountWords := splitLoanPDFText(indonesianRupiahWords(report.Loan.ApprovedAmount), 58)
	if remainingAmountWords == "" {
		amountWords += ")"
	}
	texts := []loanPDFText{
		{275, 312.5, 8, date, 42},
		{366, 396.5, 8, report.MemberName + " (" + report.MemberNo + ")", 65},
		// The membership choices are printed on the source form; the loan profile does not
		// identify which of those choices to mark.
		{316, 452.5, 8, "", 0}, // Workplace is not recorded in the member profile.
		{230, 536.5, 8, formatLoanReportAmount(report.Loan.ApprovedAmount), 32},
		{508, 536.5, 8, amountWords, 59},
		{632, 592.4, 8, date, 32},
		{301, 620.5, 8, strconv.Itoa(report.Loan.DurationMonths), 16},
		{642, 620.5, 8, formatLoanReportAmount(report.Loan.MonthlyInstallment), 28},
		{553, 833.2, 8, date, 42},
		{337, 1098.2, 8, formatLoanReportAmount(report.Loan.ApprovedAmount), 32},
		{337, 1140.5, 8, formatLoanReportAmount(report.Loan.TotalAdminFee), 32},
		{337, 1182.7, 8, formatLoanReportAmount(report.Loan.TotalObligation), 32},
		{66, 1272, 7, translate(report.Language, "purpose") + ": " + report.Purpose, 122},
	}
	if remainingAmountWords != "" {
		texts = append(texts, loanPDFText{126, 564.5, 8, remainingAmountWords, 100})
	}
	return texts
}

func loanAcceptanceFormMasks(report loanReportData) []loanPDFMask {
	date := loanReportDateLong(report.Loan.StartDate)
	if date == "" {
		date = loanReportDateLong(time.Now().In(jakartaLocation).Format("2006-01-02"))
	}
	amountWords, remainingAmountWords := splitLoanPDFText(indonesianRupiahWords(report.Loan.ApprovedAmount), 58)
	var masks []loanPDFMask
	masks = appendLoanPDFFieldMask(masks, date, 171, 312.5, 235)
	masks = appendLoanPDFFieldMask(masks, report.MemberName+" ("+report.MemberNo+")", 370, 396.5, 413)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Loan.ApprovedAmount), 230, 536.5, 271)
	masks = appendLoanPDFFieldMask(masks, amountWords, 507, 536.5, 342)
	if remainingAmountWords == "" {
		masks = append(masks, loanPDFMask{x: 126, y: 564.5 - 16, width: 730, height: 22})
	} else {
		masks = append(masks, loanPDFMask{x: 126, y: 564.5 - 11, width: 721, height: 14})
	}
	masks = appendLoanPDFFieldMask(masks, date, 631, 592.4, 278)
	masks = appendLoanPDFFieldMask(masks, strconv.Itoa(report.Loan.DurationMonths), 259, 620.5, 80)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Loan.MonthlyInstallment), 640, 620.5, 274)
	if strings.TrimSpace(date) != "" {
		masks = append(masks, loanPDFMask{x: 552, y: 833.2 - 16, width: 261, height: 22})
	}
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Loan.ApprovedAmount), 334, 1098.2, 219)
	masks = appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Loan.TotalAdminFee), 334, 1140.5, 219)
	return appendLoanPDFFieldMask(masks, formatLoanReportAmount(report.Loan.TotalObligation), 334, 1182.7, 219)
}

func splitLoanPDFText(value string, maxRunes int) (string, string) {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes), ""
	}
	splitAt := maxRunes
	for splitAt > 0 && runes[splitAt] != ' ' {
		splitAt--
	}
	if splitAt == 0 {
		splitAt = maxRunes
	}
	return strings.TrimSpace(string(runes[:splitAt])), strings.TrimSpace(string(runes[splitAt:]))
}

func buildLoanFormPDF(background []byte, texts []loanPDFText, masks []loanPDFMask) ([]byte, error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(background))
	if err != nil {
		return nil, err
	}
	const pageWidth, pageHeight = 595.28, 841.89
	var content strings.Builder
	fmt.Fprintf(&content, "q %.2f 0 0 %.2f 0 0 cm /Im1 Do Q\n", pageWidth, pageHeight)
	for _, mask := range masks {
		x := mask.x * pageWidth / loanFormReferenceWidth
		y := pageHeight - (mask.y+mask.height)*pageHeight/loanFormReferenceHeight
		width := mask.width * pageWidth / loanFormReferenceWidth
		height := mask.height * pageHeight / loanFormReferenceHeight
		fmt.Fprintf(&content, "q 1 1 1 rg %.2f %.2f %.2f %.2f re f Q\n", x, y, width, height)
	}
	for _, item := range texts {
		value := strings.TrimSpace(item.value)
		if value == "" {
			continue
		}
		value = truncateLoanPDFText(value, item.maxRunes)
		x := item.x * pageWidth / loanFormReferenceWidth
		y := pageHeight - item.y*pageHeight/loanFormReferenceHeight
		fmt.Fprintf(&content, "BT /F1 %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", item.size, x, y, escapePDFText(loanPDFASCII(value)))
	}
	contentBytes := []byte(content.String())
	objects := []loanPDFObject{
		{body: "<< /Type /Catalog /Pages 2 0 R >>"},
		{body: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{body: fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im1 5 0 R >> /Font << /F1 6 0 R >> >> /Contents 4 0 R >>", pageWidth, pageHeight)},
		{body: fmt.Sprintf("<< /Length %d >>", len(contentBytes)), stream: contentBytes},
		{body: fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", config.Width, config.Height, len(background)), stream: background},
		{body: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"},
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\n", i+1, object.body)
		if object.stream != nil {
			pdf.WriteString("stream\n")
			pdf.Write(object.stream)
			pdf.WriteString("\nendstream\n")
		}
		pdf.WriteString("endobj\n")
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return pdf.Bytes(), nil
}

func loanPDFASCII(value string) string {
	value = norm.NFD.String(value)
	var result strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 32 && r <= 126:
			result.WriteRune(r)
		case r == '\u2018' || r == '\u2019':
			result.WriteByte('\'')
		case r == '\u2013' || r == '\u2014':
			result.WriteByte('-')
		case unicode.IsSpace(r):
			result.WriteByte(' ')
		default:
			result.WriteByte('?')
		}
	}
	return result.String()
}

func truncateLoanPDFText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func loanMemberStatusLabel(memberType, memberStatus string) string {
	typeLabel := memberTypeLabel(memberType)
	if typeLabel == "" {
		typeLabel = "Anggota"
	}
	statusLabel := "Aktif"
	switch memberStatus {
	case "inactive":
		statusLabel = "Tidak aktif"
	case "suspended":
		statusLabel = "Ditangguhkan"
	}
	return typeLabel + " - " + statusLabel
}

func loanApprovalDecisionLabel(decision string) string {
	if decision == "approved" {
		return "Disetujui"
	}
	if decision == "rejected" {
		return "Ditolak"
	}
	return decision
}

func loanReportTypeLabel(loanType string) string {
	switch loanType {
	case "regular":
		return "Pinjaman uang"
	case "secondary_goods":
		return "Pinjaman barang sekunder"
	case "goods_purchase_paylater":
		return "Pembelian barang / paylater"
	default:
		return "Pinjaman"
	}
}

func loanReportDate(value any) string {
	switch typed := value.(type) {
	case time.Time:
		return typed.In(jakartaLocation).Format("2006-01-02")
	case string:
		return parseLoanReportDateString(typed)
	case []byte:
		return parseLoanReportDateString(string(typed))
	default:
		return ""
	}
}

func parseLoanReportDateString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		if _, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return value[:10]
		}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if date, err := time.Parse(layout, value); err == nil {
			return date.In(jakartaLocation).Format("2006-01-02")
		}
	}
	return value
}

func loanReportDateLong(value string) string {
	date, err := time.ParseInLocation("2006-01-02", parseLoanReportDateString(value), jakartaLocation)
	if err != nil {
		return value
	}
	months := [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	return fmt.Sprintf("%d %s %d", date.Day(), months[date.Month()-1], date.Year())
}

func formatLoanReportAmount(amount int64) string {
	value := strconv.FormatInt(amount, 10)
	if len(value) <= 3 {
		return value
	}
	first := len(value) % 3
	if first == 0 {
		first = 3
	}
	var result strings.Builder
	result.WriteString(value[:first])
	for i := first; i < len(value); i += 3 {
		result.WriteByte('.')
		result.WriteString(value[i : i+3])
	}
	return result.String()
}

func indonesianRupiahWords(amount int64) string {
	if amount < 0 {
		return "minus " + indonesianRupiahWords(-amount)
	}
	if amount == 0 {
		return "nol rupiah"
	}
	return indonesianNumberWords(amount) + " rupiah"
}

func indonesianNumberWords(value int64) string {
	numbers := [...]string{"nol", "satu", "dua", "tiga", "empat", "lima", "enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas"}
	switch {
	case value < 12:
		return numbers[value]
	case value < 20:
		return indonesianNumberWords(value-10) + " belas"
	case value < 100:
		return indonesianNumberWords(value/10) + " puluh" + remainderWords(value%10)
	case value < 200:
		return "seratus" + remainderWords(value-100)
	case value < 1000:
		return indonesianNumberWords(value/100) + " ratus" + remainderWords(value%100)
	case value < 2000:
		return "seribu" + remainderWords(value-1000)
	case value < 1_000_000:
		return indonesianNumberWords(value/1000) + " ribu" + remainderWords(value%1000)
	case value < 1_000_000_000:
		return indonesianNumberWords(value/1_000_000) + " juta" + remainderWords(value%1_000_000)
	case value < 1_000_000_000_000:
		return indonesianNumberWords(value/1_000_000_000) + " miliar" + remainderWords(value%1_000_000_000)
	default:
		return strconv.FormatInt(value, 10)
	}
}

func remainderWords(value int64) string {
	if value == 0 {
		return ""
	}
	return " " + indonesianNumberWords(value)
}

func safeLoanReportFilename(value string) string {
	value = strings.TrimSpace(value)
	var result strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return "approved-loan"
	}
	return result.String()
}
