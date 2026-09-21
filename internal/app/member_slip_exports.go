package app

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type slipBalance struct {
	Pokok    int64
	Wajib    int64
	Sukarela int64
	Khusus   int64
	SHU      int64
}

func (b slipBalance) total() (int64, error) {
	total, err := checkedReportAdd(b.Pokok, b.Wajib)
	if err != nil {
		return 0, err
	}
	total, err = checkedReportAdd(total, b.Sukarela)
	if err != nil {
		return 0, err
	}
	total, err = checkedReportAdd(total, b.Khusus)
	if err != nil {
		return 0, err
	}
	return checkedReportAdd(total, b.SHU)
}

type savingSlipData struct {
	Member  Member
	AsOf    time.Time
	Current slipBalance
	Total   int64
}

type loanSlipData struct {
	Member     Member
	Loan       Loan
	AsOf       time.Time
	PaidAmount int64
}

func (s *Server) memberSavingsSlipPDF(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	data, err := s.memberSavingSlipData(member)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	pdf, err := buildSavingSlipPDF(data, languageFromRequest(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	filename := fmt.Sprintf("slip-simpanan-%s-latest.pdf", safeSlipFilename(member.MemberNo))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) memberLoanSlipPDF(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	data, err := s.memberLoanSlipData(member, strings.TrimSpace(c.Query("loan_id")))
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(languageFromRequest(c), "error_member_loan_not_found"))
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	pdf, err := buildLoanSlipPDF(data, languageFromRequest(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	filename := fmt.Sprintf("slip-pinjaman-%s-latest.pdf", safeSlipFilename(member.MemberNo))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) memberSavingSlipData(member Member) (savingSlipData, error) {
	rows, err := s.db.Query(`
		SELECT category, type, amount, record_date
		FROM saving_records
		WHERE member_id = $1
		ORDER BY record_date, created_at, id`,
		member.ID,
	)
	if err != nil {
		return savingSlipData{}, err
	}
	defer rows.Close()

	type record struct {
		category string
		typeName string
		amount   int64
		date     string
	}
	var records []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.category, &item.typeName, &item.amount, &item.date); err != nil {
			return savingSlipData{}, err
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return savingSlipData{}, err
	}

	balances := slipBalance{}
	latestRecordDate := ""
	applyRecord := func(item record) error {
		value := item.amount
		if item.typeName == "withdrawal" {
			value = -value
		}
		switch item.category {
		case "pokok":
			balances.Pokok, err = checkedReportAdd(balances.Pokok, value)
		case "wajib":
			balances.Wajib, err = checkedReportAdd(balances.Wajib, value)
		case "sukarela":
			balances.Sukarela, err = checkedReportAdd(balances.Sukarela, value)
		case "khusus":
			balances.Khusus, err = checkedReportAdd(balances.Khusus, value)
		case "shu":
			balances.SHU, err = checkedReportAdd(balances.SHU, value)
		}
		return err
	}
	for _, item := range records {
		if err := applyRecord(item); err != nil {
			return savingSlipData{}, err
		}
		latestRecordDate = item.date
	}
	asOf := time.Now().In(jakartaLocation)
	if latestRecordDate != "" {
		if parsed, parseErr := time.ParseInLocation("2006-01-02", latestRecordDate, jakartaLocation); parseErr == nil {
			asOf = parsed
		}
	}
	total, err := balances.total()
	if err != nil {
		return savingSlipData{}, err
	}
	return savingSlipData{
		Member:  member,
		AsOf:    asOf,
		Current: balances,
		Total:   total,
	}, nil
}

func (s *Server) memberLoanSlipData(member Member, loanID string) (loanSlipData, error) {
	loan, err := s.memberLoanForSlip(member.ID, loanID)
	if err != nil {
		return loanSlipData{}, err
	}
	paidAmount, err := checkedReportSub(loan.TotalObligation, loan.RemainingBalance)
	if err != nil {
		return loanSlipData{}, err
	}
	if paidAmount < 0 {
		paidAmount = 0
	}
	return loanSlipData{Member: member, Loan: loan, AsOf: time.Now().In(jakartaLocation), PaidAmount: paidAmount}, nil
}

func (s *Server) memberLoanForSlip(memberID, loanID string) (Loan, error) {
	query := `SELECT id, loan_request_id, member_id, loan_type, legacy_terms, approved_amount, duration_months, monthly_installment, remaining_balance, start_date, admin_fee_policy, monthly_admin_fee, total_admin_fee, total_obligation, next_due_date, final_due_date, status, approved_by, approved_at, created_at, updated_at FROM loans WHERE member_id = $1 AND status <> 'cancelled'`
	args := []any{memberID}
	if loanID != "" {
		query += ` AND id = $2`
		args = append(args, loanID)
	} else {
		query += ` ORDER BY CASE WHEN remaining_balance > 0 THEN 0 ELSE 1 END, created_at DESC`
	}
	query += ` LIMIT 1`
	return queryLoanRow(s.db.QueryRow(query, args...))
}

func queryLoanRow(row interface{ Scan(...any) error }) (Loan, error) {
	var loan Loan
	err := row.Scan(&loan.ID, &loan.LoanRequestID, &loan.MemberID, &loan.LoanType, &loan.LegacyTerms, &loan.ApprovedAmount, &loan.DurationMonths, &loan.MonthlyInstallment, &loan.RemainingBalance, &loan.StartDate, &loan.AdminFeePolicy, &loan.MonthlyAdminFee, &loan.TotalAdminFee, &loan.TotalObligation, &loan.NextDueDate, &loan.FinalDueDate, &loan.Status, &loan.ApprovedBy, &loan.ApprovedAt, &loan.CreatedAt, &loan.UpdatedAt)
	loan.IsOverdue = loanOverdue(loan.NextDueDate, loan.RemainingBalance)
	return loan, err
}

func safeSlipFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "anggota"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

var slipMonthKeys = [...]string{
	"month_january", "month_february", "month_march", "month_april", "month_may", "month_june",
	"month_july", "month_august", "month_september", "month_october", "month_november", "month_december",
}

func slipMonthLabel(lang string, month time.Month) string {
	if month < time.January || month > time.December {
		return ""
	}
	return strings.ToUpper(translate(lang, slipMonthKeys[int(month)-1]))
}

func slipDateLabel(lang, date string) string {
	parsed, err := time.ParseInLocation("2006-01-02", date, jakartaLocation)
	if err != nil {
		return date
	}
	return fmt.Sprintf("%d %s %d", parsed.Day(), slipMonthLabel(lang, parsed.Month()), parsed.Year())
}

func slipAmount(value int64) string {
	if value == 0 {
		return "-"
	}
	return formatNominal(value)
}

type slipPDF struct {
	width   float64
	height  float64
	content strings.Builder
}

func newSlipPDF() *slipPDF {
	return &slipPDF{width: 841.89, height: 595.276}
}

func (p *slipPDF) text(x, y, size float64, font, value string) {
	value = pdfSafeSlipText(value)
	p.content.WriteString("BT /")
	p.content.WriteString(font)
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(size, 'f', 2, 64))
	p.content.WriteString(" Tf ")
	p.content.WriteString(strconv.FormatFloat(x, 'f', 2, 64))
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(y, 'f', 2, 64))
	p.content.WriteString(" Td (")
	p.content.WriteString(escapePDFText(value))
	p.content.WriteString(") Tj ET\n")
}

func (p *slipPDF) textRight(x, y, size float64, font, value string) {
	p.text(x-slipTextWidth(pdfSafeSlipText(value), size), y, size, font, value)
}

func (p *slipPDF) textCenter(x, y, size float64, font, value string) {
	p.text(x-slipTextWidth(pdfSafeSlipText(value), size)/2, y, size, font, value)
}

func (p *slipPDF) line(x1, y1, x2, y2, width float64) {
	p.content.WriteString("q 0 0 0 RG ")
	p.content.WriteString(strconv.FormatFloat(width, 'f', 2, 64))
	p.content.WriteString(" w ")
	p.content.WriteString(strconv.FormatFloat(x1, 'f', 2, 64))
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(y1, 'f', 2, 64))
	p.content.WriteString(" m ")
	p.content.WriteString(strconv.FormatFloat(x2, 'f', 2, 64))
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(y2, 'f', 2, 64))
	p.content.WriteString(" l S Q\n")
}

func (p *slipPDF) dashedLine(x1, y1, x2, y2, width float64) {
	p.content.WriteString("q 0 0 0 RG ")
	p.content.WriteString(strconv.FormatFloat(width, 'f', 2, 64))
	p.content.WriteString(" w [2 2] 0 d ")
	p.content.WriteString(strconv.FormatFloat(x1, 'f', 2, 64))
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(y1, 'f', 2, 64))
	p.content.WriteString(" m ")
	p.content.WriteString(strconv.FormatFloat(x2, 'f', 2, 64))
	p.content.WriteString(" ")
	p.content.WriteString(strconv.FormatFloat(y2, 'f', 2, 64))
	p.content.WriteString(" l S Q\n")
}

func (p *slipPDF) rectangle(x, y, width, height float64, fill bool, strokeWidth float64) {
	if fill {
		p.content.WriteString("q 0.86 0.86 0.86 rg ")
	} else {
		p.content.WriteString("q 1 1 1 rg ")
	}
	p.content.WriteString("0 0 0 RG ")
	p.content.WriteString(strconv.FormatFloat(strokeWidth, 'f', 2, 64))
	p.content.WriteString(" w ")
	for _, value := range []float64{x, y, width, height} {
		p.content.WriteString(strconv.FormatFloat(value, 'f', 2, 64))
		p.content.WriteByte(' ')
	}
	p.content.WriteString("re B Q\n")
}

func (p *slipPDF) image(x, y, width, height float64) {
	p.content.WriteString("q ")
	for _, value := range []float64{width, 0, 0, height, x, y} {
		p.content.WriteString(strconv.FormatFloat(value, 'f', 2, 64))
		p.content.WriteByte(' ')
	}
	p.content.WriteString("cm /Im1 Do Q\n")
}

func pdfSafeSlipText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return '?'
		}
		return r
	}, value)
}

func slipTextWidth(value string, size float64) float64 {
	return float64(len([]rune(value))) * size * 0.52
}

func (p *slipPDF) bytes() ([]byte, error) {
	logo, err := slipLogoObject()
	if err != nil {
		return nil, err
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [7 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Oblique >>",
		logo,
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> /XObject << /Im1 6 0 R >> >> /Contents 8 0 R >>", p.width, p.height),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", p.content.Len(), p.content.String()),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return pdf.Bytes(), nil
}

func slipLogoObject() (string, error) {
	logoBytes, err := staticAssetFS.ReadFile("static/images/lambang-koperasi.png")
	if err != nil {
		return "", err
	}
	decoded, err := png.Decode(bytes.NewReader(logoBytes))
	if err != nil {
		return "", err
	}
	bounds := decoded.Bounds()
	var raw bytes.Buffer
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			if a < 65535 {
				r = (r*a + 65535*(65535-a)) / 65535
				g = (g*a + 65535*(65535-a)) / 65535
				b = (b*a + 65535*(65535-a)) / 65535
			}
			raw.WriteByte(byte(r >> 8))
			raw.WriteByte(byte(g >> 8))
			raw.WriteByte(byte(b >> 8))
		}
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(raw.Bytes()); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", bounds.Dx(), bounds.Dy(), compressed.Len(), compressed.Bytes()), nil
}

func drawSlipHeader(pdf *slipPDF, lang, title string, asOf time.Time, member Member) {
	pdf.image(50, 480, 72, 70)
	pdf.text(137, 550, 14, "F2", "KKSUK Perumda Dharma Jaya")
	pdf.text(137, 531, 11, "F1", "Jl. Raya Penggilingan No 25")
	pdf.text(137, 514, 11, "F1", "Penggilingan, Cakung, Jakarta Timur")
	pdf.text(137, 496, 11, "F2", "13690")
	pdf.textRight(790, 550, 15, "F2", title)
	pdf.textRight(790, 531, 11, "F3", translate(lang, "slip_latest_value"))
	pdf.textRight(790, 514, 9.5, "F1", fmt.Sprintf("%s %s", translate(lang, "slip_as_of"), slipDateLabel(lang, asOf.Format("2006-01-02"))))
	pdf.line(137, 480, 790, 480, 1.5)
	pdf.text(50, 442, 9.5, "F1", translate(lang, "slip_member_name"))
	pdf.text(157, 442, 9.5, "F2", ": "+member.FullName)
	pdf.text(450, 442, 9.5, "F1", translate(lang, "slip_workplace"))
	pdf.text(550, 442, 9.5, "F1", ": -")
}

func drawSavingSlip(pdf *slipPDF, data savingSlipData, lang string) {
	drawSlipHeader(pdf, lang, translate(lang, "slip_savings_title"), data.AsOf, data.Member)
	pdf.text(50, 398, 10, "F2", translate(lang, "slip_latest_value"))
	pdf.line(50, 382, 790, 382, 1.5)

	type savingValue struct {
		label string
		value int64
		x     float64
	}
	values := []savingValue{
		{label: "slip_principal_savings", value: data.Current.Pokok, x: 50},
		{label: "slip_required_savings", value: data.Current.Wajib, x: 440},
		{label: "slip_voluntary_savings", value: data.Current.Sukarela, x: 50},
		{label: "slip_special_savings", value: data.Current.Khusus, x: 440},
		{label: "slip_shu_savings", value: data.Current.SHU, x: 50},
	}
	for index, item := range values {
		y := 342 - float64(index/2)*45
		if index%2 == 1 {
			y = 342 - float64(index/2)*45
		}
		pdf.text(item.x, y, 9.5, "F2", translate(lang, item.label))
		pdf.text(item.x+135, y, 9.5, "F1", "Rp")
		pdf.textRight(item.x+350, y, 9.5, "F1", slipAmount(item.value))
		pdf.dashedLine(item.x, y-8, item.x+350, y-8, 0.35)
	}

	pdf.text(50, 188, 10, "F2", translate(lang, "slip_total_savings"))
	pdf.rectangle(185, 176, 275, 24, false, 1.5)
	pdf.text(195, 184, 10, "F2", "Rp")
	pdf.textRight(450, 184, 10, "F2", slipAmount(data.Total))
}

func drawLoanSlip(pdf *slipPDF, data loanSlipData, lang string) {
	drawSlipHeader(pdf, lang, translate(lang, "slip_loan_title"), data.AsOf, data.Member)
	pdf.text(50, 398, 10, "F2", translate(lang, "slip_latest_value"))
	pdf.line(50, 382, 790, 382, 1.5)

	type loanValue struct {
		label string
		value string
		x     float64
	}
	values := []loanValue{
		{label: "slip_loan_taken_date", value: slipDateLabel(lang, data.Loan.StartDate), x: 50},
		{label: "approved_principal", value: "Rp " + slipAmount(data.Loan.ApprovedAmount), x: 440},
		{label: "slip_principal_plus_admin", value: "Rp " + slipAmount(data.Loan.TotalObligation), x: 50},
		{label: "monthly_installment", value: "Rp " + slipAmount(data.Loan.MonthlyInstallment), x: 440},
		{label: "slip_paid_amount", value: "Rp " + slipAmount(data.PaidAmount), x: 50},
		{label: "slip_remaining_debt", value: "Rp " + slipAmount(data.Loan.RemainingBalance), x: 440},
		{label: "status", value: translate(lang, "status_"+data.Loan.Status), x: 50},
		{label: "next_due_date", value: slipDateLabel(lang, data.Loan.NextDueDate), x: 440},
		{label: "final_due_date", value: slipDateLabel(lang, data.Loan.FinalDueDate), x: 50},
	}
	for index, item := range values {
		y := 342 - float64(index/2)*45
		pdf.text(item.x, y, 9.5, "F2", translate(lang, item.label))
		pdf.text(item.x, y-15, 9.5, "F1", item.value)
		pdf.dashedLine(item.x, y-23, item.x+350, y-23, 0.35)
	}
}

func buildSavingSlipPDF(data savingSlipData, lang string) ([]byte, error) {
	pdf := newSlipPDF()
	drawSavingSlip(pdf, data, lang)
	return pdf.bytes()
}

func buildLoanSlipPDF(data loanSlipData, lang string) ([]byte, error) {
	pdf := newSlipPDF()
	drawLoanSlip(pdf, data, lang)
	return pdf.bytes()
}
