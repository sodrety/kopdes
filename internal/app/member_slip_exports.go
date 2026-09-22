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

type savingSlipMonth struct {
	Month  time.Month
	Stored slipBalance
	Values slipBalance
}

type savingSlipData struct {
	Member  Member
	Period  time.Time
	Opening slipBalance
	Months  []savingSlipMonth
	Current slipBalance
	Special int64
	Total   int64
}

type loanSlipMonth struct {
	Month  time.Month
	Amount int64
}

type loanSlipData struct {
	Member       Member
	Loan         Loan
	Period       time.Time
	Opening      int64
	Months       []loanSlipMonth
	TotalPaidDue int64
	Remaining    int64
}

func currentSlipPeriod() time.Time {
	now := time.Now().In(jakartaLocation)
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jakartaLocation)
}

func (s *Server) memberSavingsSlipPDF(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	data, err := s.memberSavingSlipData(member, currentSlipPeriod())
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	pdf, err := buildSavingSlipPDF(data, languageFromRequest(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(languageFromRequest(c), "error.Internal server error"))
		return
	}
	filename := fmt.Sprintf("slip-simpanan-%s-%s.pdf", safeSlipFilename(member.MemberNo), data.Period.Format("2006-01"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) memberLoanSlipPDF(c *gin.Context) {
	member, ok := s.profileMember(c)
	if !ok {
		return
	}
	period := currentSlipPeriod()
	data, err := s.memberLoanSlipData(member, strings.TrimSpace(c.Query("loan_id")), period)
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
	filename := fmt.Sprintf("slip-pinjaman-%s-%s.pdf", safeSlipFilename(member.MemberNo), data.Period.Format("2006-01"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

func (s *Server) memberSavingSlipData(member Member, period time.Time) (savingSlipData, error) {
	rows, err := s.db.Query(`
		SELECT category, type, amount, record_date
		FROM saving_records
		WHERE member_id = $1 AND record_date <= $2
		ORDER BY record_date, created_at, id`,
		member.ID,
		time.Now().In(jakartaLocation).Format("2006-01-02"),
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

	if len(records) > 0 {
		if latest, parseErr := time.ParseInLocation("2006-01-02", records[len(records)-1].date, jakartaLocation); parseErr == nil {
			period = time.Date(latest.Year(), latest.Month(), 1, 0, 0, 0, 0, jakartaLocation)
		}
	}
	period = time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, jakartaLocation)
	yearStart := fmt.Sprintf("%04d-01-01", period.Year())
	balances := slipBalance{}
	opening := slipBalance{}
	index := 0
	applyRecord := func(item record, target *slipBalance) error {
		value := item.amount
		if item.typeName == "withdrawal" {
			value = -value
		}
		var applyErr error
		switch item.category {
		case "pokok":
			target.Pokok, applyErr = checkedReportAdd(target.Pokok, value)
		case "wajib":
			target.Wajib, applyErr = checkedReportAdd(target.Wajib, value)
		case "sukarela":
			target.Sukarela, applyErr = checkedReportAdd(target.Sukarela, value)
		case "khusus":
			target.Khusus, applyErr = checkedReportAdd(target.Khusus, value)
		case "shu":
			target.SHU, applyErr = checkedReportAdd(target.SHU, value)
		}
		return applyErr
	}
	for index < len(records) && records[index].date < yearStart {
		if err := applyRecord(records[index], &balances); err != nil {
			return savingSlipData{}, err
		}
		index++
	}
	opening = balances

	months := make([]savingSlipMonth, 0, 12)
	for month := time.January; month <= time.December; month++ {
		nextMonth := time.Date(period.Year(), month+1, 1, 0, 0, 0, 0, jakartaLocation)
		nextMonthDate := nextMonth.Format("2006-01-02")
		stored := slipBalance{}
		for index < len(records) && records[index].date < nextMonthDate {
			item := records[index]
			if err := applyRecord(item, &balances); err != nil {
				return savingSlipData{}, err
			}
			if err := applyRecord(item, &stored); err != nil {
				return savingSlipData{}, err
			}
			index++
		}
		months = append(months, savingSlipMonth{Month: month, Stored: stored, Values: balances})
	}
	current := months[int(period.Month())-1].Values
	total, err := current.total()
	if err != nil {
		return savingSlipData{}, err
	}
	return savingSlipData{
		Member:  member,
		Period:  period,
		Opening: opening,
		Months:  months,
		Current: current,
		Special: current.Khusus,
		Total:   total,
	}, nil
}

func (s *Server) memberLoanSlipData(member Member, loanID string, period time.Time) (loanSlipData, error) {
	loan, err := s.memberLoanForSlip(member.ID, loanID)
	if err != nil {
		return loanSlipData{}, err
	}
	installments, err := s.loanInstallments(loan.ID)
	if err != nil {
		return loanSlipData{}, err
	}

	period = time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, jakartaLocation)
	if loan.FinalDueDate != "" {
		finalDueDate, err := time.ParseInLocation("2006-01-02", loan.FinalDueDate, jakartaLocation)
		if err != nil {
			return loanSlipData{}, err
		}
		finalPeriod := time.Date(finalDueDate.Year(), finalDueDate.Month(), 1, 0, 0, 0, 0, jakartaLocation)
		if finalPeriod.Before(period) {
			period = finalPeriod
		}
	}
	yearStart := time.Date(period.Year(), time.January, 1, 0, 0, 0, 0, jakartaLocation)
	periodEnd := time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, jakartaLocation)
	months := make([]loanSlipMonth, 12)
	for month := time.January; month <= time.December; month++ {
		months[int(month)-1].Month = month
	}
	var opening, totalDue int64
	for _, installment := range installments {
		dueDate, err := time.ParseInLocation("2006-01-02", installment.DueDate, jakartaLocation)
		if err != nil {
			return loanSlipData{}, err
		}
		if dueDate.Before(yearStart) {
			opening, err = checkedReportAdd(opening, installment.ScheduledAmount)
			if err != nil {
				return loanSlipData{}, err
			}
		}
		if dueDate.Before(periodEnd) {
			totalDue, err = checkedReportAdd(totalDue, installment.ScheduledAmount)
			if err != nil {
				return loanSlipData{}, err
			}
		}
		if dueDate.Year() == period.Year() && dueDate.Month() >= time.January && dueDate.Month() <= time.December && dueDate.Before(periodEnd) {
			index := int(dueDate.Month()) - 1
			months[index].Amount, err = checkedReportAdd(months[index].Amount, installment.ScheduledAmount)
			if err != nil {
				return loanSlipData{}, err
			}
		}
	}
	remaining, err := checkedReportSub(loan.TotalObligation, totalDue)
	if err != nil {
		return loanSlipData{}, err
	}
	if remaining < 0 {
		remaining = 0
	}
	return loanSlipData{Member: member, Loan: loan, Period: period, Opening: opening, Months: months, TotalPaidDue: totalDue, Remaining: remaining}, nil
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

func drawSlipHeader(pdf *slipPDF, lang, title string, period time.Time, member Member) {
	pdf.image(50, 480, 72, 70)
	pdf.text(137, 550, 14, "F2", "KKSUK Perumda Dharma Jaya")
	pdf.text(137, 531, 11, "F1", "Jl. Raya Penggilingan No 25")
	pdf.text(137, 514, 11, "F1", "Penggilingan, Cakung, Jakarta Timur")
	pdf.text(137, 496, 11, "F2", "13690")
	pdf.textRight(790, 550, 15, "F2", title)
	pdf.textRight(790, 531, 11, "F3", fmt.Sprintf("%s %d", slipMonthLabel(lang, period.Month()), period.Year()))
	pdf.line(137, 480, 790, 480, 1.5)
	pdf.text(50, 442, 9.5, "F1", translate(lang, "slip_member_name"))
	pdf.text(157, 442, 9.5, "F2", ": "+member.FullName)
	pdf.text(450, 442, 9.5, "F1", translate(lang, "slip_workplace"))
	pdf.text(550, 442, 9.5, "F1", ": -")
}

func drawSavingSlip(pdf *slipPDF, data savingSlipData, lang string) {
	drawSlipHeader(pdf, lang, translate(lang, "slip_savings_title"), data.Period, data.Member)
	pdf.text(50, 398, 10, "F2", translate(lang, "slip_principal_savings"))
	pdf.text(188, 398, 10, "F2", "Rp")
	pdf.textRight(335, 398, 10, "F2", slipAmount(data.Current.Pokok))
	pdf.line(50, 382, 790, 382, 1.5)
	pdf.text(50, 358, 9.5, "F2", translate(lang, "slip_month"))
	savedLabel := translate(lang, "slip_savings_stored")
	totalLabel := translate(lang, "slip_savings_total")
	requiredLabel := translate(lang, "slip_required_savings")
	voluntaryLabel := translate(lang, "slip_voluntary_savings")
	pdf.text(190, 358, 9, "F2", fmt.Sprintf("%s %s", savedLabel, requiredLabel))
	pdf.text(315, 358, 9, "F2", fmt.Sprintf("%s %s", totalLabel, requiredLabel))
	pdf.text(445, 358, 9, "F2", fmt.Sprintf("%s %s", savedLabel, voluntaryLabel))
	pdf.text(590, 358, 9, "F2", fmt.Sprintf("%s %s", totalLabel, voluntaryLabel))
	pdf.line(50, 348, 790, 348, 1.5)

	rows := make([]savingSlipMonth, 0, len(data.Months)+1)
	rows = append(rows, savingSlipMonth{Month: time.December, Values: data.Opening})
	rows = append(rows, data.Months...)
	for index, row := range rows {
		y := 332 - float64(index)*13
		rowValues := row.Values
		rowStored := row.Stored
		if index > 0 && int(row.Month) > int(data.Period.Month()) {
			rowValues = slipBalance{}
			rowStored = slipBalance{}
		}
		rowYear := data.Period.Year()
		if index == 0 {
			rowYear--
		}
		pdf.text(50, y, 8.5, "F1", fmt.Sprintf("S/D %s %d", slipMonthLabel(lang, row.Month), rowYear))
		pdf.text(190, y, 8.5, "F1", "Rp")
		pdf.textRight(300, y, 8.5, "F1", slipAmount(rowStored.Wajib))
		pdf.text(315, y, 8.5, "F1", "Rp")
		pdf.textRight(430, y, 8.5, "F1", slipAmount(rowValues.Wajib))
		pdf.text(445, y, 8.5, "F1", "Rp")
		pdf.textRight(565, y, 8.5, "F1", slipAmount(rowStored.Sukarela))
		pdf.text(590, y, 8.5, "F1", "Rp")
		pdf.textRight(790, y, 8.5, "F1", slipAmount(rowValues.Sukarela))
		pdf.dashedLine(50, y-5, 790, y-5, 0.35)
	}

	pdf.line(50, 150, 790, 150, 1.4)
	pdf.line(50, 146, 790, 146, 1.4)
	pdf.text(50, 158, 9.5, "F2", translate(lang, "slip_through_month"))
	pdf.text(315, 158, 9.5, "F2", "Rp")
	pdf.textRight(430, 158, 9.5, "F2", slipAmount(data.Current.Wajib))
	pdf.text(590, 158, 9.5, "F2", "Rp")
	pdf.textRight(790, 158, 9.5, "F2", slipAmount(data.Current.Sukarela))

	pdf.text(50, 130, 10, "F2", translate(lang, "slip_special_savings"))
	pdf.rectangle(185, 118, 275, 24, false, 1.5)
	pdf.text(195, 126, 10, "F2", "Rp")
	pdf.textRight(450, 126, 10, "F2", slipAmount(data.Special))
	pdf.text(50, 90, 10, "F2", translate(lang, "slip_total_savings"))
	pdf.rectangle(185, 78, 275, 24, false, 1.5)
	pdf.text(195, 86, 10, "F2", "Rp")
	pdf.textRight(450, 86, 10, "F2", slipAmount(data.Total))
}

func drawLoanSlip(pdf *slipPDF, data loanSlipData, lang string) {
	drawSlipHeader(pdf, lang, translate(lang, "slip_loan_title"), data.Period, data.Member)
	pdf.text(440, 411, 9.5, "F2", translate(lang, "slip_loan_taken_date"))
	pdf.textRight(790, 411, 9.5, "F1", slipDateLabel(lang, data.Loan.StartDate))
	pdf.line(50, 394, 790, 394, 1.5)
	pdf.text(50, 369, 10, "F2", translate(lang, "slip_installments"))
	pdf.text(575, 369, 9.5, "F2", translate(lang, "slip_principal_plus_admin"))
	pdf.textRight(790, 369, 10, "F2", slipAmount(data.Loan.TotalObligation))
	pdf.line(50, 360, 790, 360, 1.5)

	leftRows := make([]loanSlipMonth, 0, 7)
	leftRows = append(leftRows, loanSlipMonth{Month: time.December, Amount: data.Opening})
	leftRows = append(leftRows, data.Months[:6]...)
	rightRows := data.Months[6:]
	for index := 0; index < 7; index++ {
		y := 344 - float64(index)*17
		left := leftRows[index]
		leftLabel := fmt.Sprintf("S/D %s", slipMonthLabel(lang, left.Month))
		if index > 0 {
			leftLabel = fmt.Sprintf("- %s", slipMonthLabel(lang, left.Month))
		}
		leftAmount := left.Amount
		if index > 0 && int(left.Month) > int(data.Period.Month()) {
			leftAmount = 0
		}
		rowYear := data.Period.Year()
		if index == 0 {
			rowYear--
		}
		pdf.text(50, y, 9, "F1", fmt.Sprintf("%s %d", leftLabel, rowYear))
		pdf.text(270, y, 9, "F1", "Rp")
		pdf.textRight(430, y, 9, "F1", slipAmount(leftAmount))
		pdf.dashedLine(50, y-5, 430, y-5, 0.35)

		if index < len(rightRows) {
			right := rightRows[index]
			rightAmount := right.Amount
			if int(right.Month) > int(data.Period.Month()) {
				rightAmount = 0
			}
			pdf.text(440, y, 9, "F1", fmt.Sprintf("- %s", slipMonthLabel(lang, right.Month)))
			pdf.text(660, y, 9, "F1", "Rp")
			pdf.textRight(790, y, 9, "F1", slipAmount(rightAmount))
			pdf.dashedLine(440, y-5, 790, y-5, 0.35)
		}
	}

	pdf.line(50, 222, 790, 222, 1.4)
	pdf.line(50, 218, 790, 218, 1.4)
	pdf.text(440, 230, 9.5, "F2", translate(lang, "slip_total_installments"))
	pdf.textRight(790, 230, 10, "F2", slipAmount(data.TotalPaidDue))
	pdf.text(50, 182, 10, "F2", translate(lang, "slip_remaining_debt"))
	pdf.rectangle(185, 170, 275, 24, true, 1.5)
	pdf.text(195, 178, 10, "F2", "Rp")
	pdf.textRight(450, 178, 10, "F2", slipAmount(data.Remaining))
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
