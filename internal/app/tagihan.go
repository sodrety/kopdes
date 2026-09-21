package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type TagihanConfig struct {
	SavingSource string `json:"saving_source"`
	ReadOnly     bool   `json:"read_only"`
}

func tagihanConfiguration() TagihanConfig {
	return TagihanConfig{
		SavingSource: "latest_member_savings",
		ReadOnly:     true,
	}
}

var errInvalidTagihanMonth = errors.New("invalid tagihan statement month")

type TagihanRow struct {
	MemberID               string `json:"member_id"`
	MemberNo               string `json:"member_no"`
	FullName               string `json:"full_name"`
	SimpananWajib          int64  `json:"simpanan_wajib"`
	SimpananSukarela       int64  `json:"simpanan_sukarela"`
	PinjamanReguler        int64  `json:"pinjaman_reguler"`
	PinjamanBarangSekunder int64  `json:"pinjaman_barang_sekunder"`
	PembelianBarang        int64  `json:"pembelian_barang"`
	PinjamanNonReguler     int64  `json:"pinjaman_non_reguler"`
	Total                  int64  `json:"total"`
	Status                 string `json:"status"`
}

type tagihanStatementMonth struct {
	Value       string
	CutoffDate  string
	DefaultDate string
}

type tagihanLoanDue struct {
	LoanID string
	Amount int64
}

type TagihanImportResult struct {
	StatementMonth string                     `json:"statement_month"`
	RecordDate     string                     `json:"record_date"`
	Rows           []TagihanImportRowResult   `json:"rows"`
	Summary        TagihanImportResultSummary `json:"summary"`
}

type TagihanImportResultSummary struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Invalid  int `json:"invalid"`
}

type TagihanImportRowResult struct {
	ExcelRow       int      `json:"excel_row"`
	MemberID       string   `json:"member_id"`
	MemberNo       string   `json:"member_no,omitempty"`
	FullName       string   `json:"full_name,omitempty"`
	Status         string   `json:"status"`
	Result         string   `json:"result"`
	Messages       []string `json:"messages,omitempty"`
	SavingsCreated int      `json:"savings_created"`
	Repayments     int      `json:"repayments_created"`
}

func (s *Server) requireTagihanManage() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := s.authenticateRequest(c)
		if !ok {
			return
		}
		if user.MustChangePassword {
			respondError(c, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "Password change is required")
			c.Abort()
			return
		}
		if !hasPermission(user.Role, PermissionSavingsRecord) || !hasPermission(user.Role, PermissionRepaymentsRecord) {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "Insufficient permission")
			c.Abort()
			return
		}
		c.Set("user", user)
		s.decorateAuthenticatedContext(c, user)
		c.Next()
	}
}

func (s *Server) adminTagihan(c *gin.Context) {
	statementMonth, err := parseTagihanStatementMonth(c.Query("month"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_month"))
		return
	}
	rows, err := s.tagihanRows(statementMonth)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	c.JSON(http.StatusOK, gin.H{"statement_month": statementMonth.Value, "cutoff_date": statementMonth.CutoffDate, "saving_config": tagihanConfiguration(), "rows": rows})
}

func (s *Server) exportTagihanXLSX(c *gin.Context) {
	statementMonth, err := parseTagihanStatementMonth(c.Query("month"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_month"))
		return
	}
	rows, err := s.tagihanRows(statementMonth)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	workbook, err := buildTagihanWorkbook(statementMonth, rows)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	defer workbook.Close()

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="tagihan-%s.xlsx"`, statementMonth.Value))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err := workbook.Write(c.Writer); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
}

func (s *Server) importTagihanXLSX(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication token is required")
		return
	}
	statementMonth, err := parseTagihanStatementMonth(c.PostForm("statement_month"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_month"))
		return
	}
	recordDate := strings.TrimSpace(c.PostForm("record_date"))
	if recordDate == "" {
		recordDate = statementMonth.DefaultDate
	}
	if _, err := parseLoanDate(recordDate); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_record_date"))
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_file"))
		return
	}
	defer file.Close()

	result, err := s.importTagihan(statementMonth, recordDate, file, user.ID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_file"))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) adminTagihanPage(c *gin.Context) {
	now := time.Now().In(jakartaLocation)
	month := c.Query("month")
	if strings.TrimSpace(month) == "" {
		month = now.Format("2006-01")
	}
	statementMonth, err := parseTagihanStatementMonth(month)
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(languageFromRequest(c), "error_invalid_tagihan_month"))
		return
	}
	rows, err := s.tagihanRows(statementMonth)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Internal server error")
		return
	}
	renderPage(c, "admin-tagihan", pageData(c, "Tagihan - KKSUK PD Dharma Jaya", "tagihan", "tagihan", "tagihan_description", gin.H{
		"StatementMonth": statementMonth.Value,
		"DefaultDate":    statementMonth.DefaultDate,
		"TagihanConfig":  tagihanConfiguration(),
		"Rows":           rows,
	}))
}

func parseTagihanStatementMonth(value string) (tagihanStatementMonth, error) {
	value = strings.TrimSpace(value)
	parsed, err := time.ParseInLocation("2006-01", value, jakartaLocation)
	if err != nil || parsed.Format("2006-01") != value {
		return tagihanStatementMonth{}, errInvalidTagihanMonth
	}
	cutoff := time.Date(parsed.Year(), parsed.Month()+1, 0, 0, 0, 0, 0, jakartaLocation)
	return tagihanStatementMonth{
		Value:       value,
		CutoffDate:  cutoff.Format("2006-01-02"),
		DefaultDate: cutoff.Format("2006-01-02"),
	}, nil
}

func tagihanReference(statementMonth tagihanStatementMonth) string {
	return "TAGIHAN-" + statementMonth.Value
}

func tagihanNote(statementMonth tagihanStatementMonth, memberID string) string {
	return fmt.Sprintf("Tagihan %s member %s", statementMonth.Value, memberID)
}

func (s *Server) tagihanRows(statementMonth tagihanStatementMonth) ([]TagihanRow, error) {
	rows, err := s.db.Query(`SELECT id, member_no, full_name FROM members WHERE member_type='employee' AND status='active' ORDER BY member_no`)
	if err != nil {
		return nil, err
	}
	var members []TagihanRow
	for rows.Next() {
		var row TagihanRow
		if err := rows.Scan(&row.MemberID, &row.MemberNo, &row.FullName); err != nil {
			_ = rows.Close()
			return nil, err
		}
		members = append(members, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var tagihanRows []TagihanRow
	for _, row := range members {
		savingAmounts, err := s.tagihanSavingAmounts(row.MemberID, statementMonth)
		if err != nil {
			return nil, err
		}
		row.SimpananWajib = savingAmounts.Wajib
		row.SimpananSukarela = savingAmounts.Manasuka
		regularDue, err := s.tagihanLoanDueTotal(row.MemberID, statementMonth, true)
		if err != nil {
			return nil, err
		}
		secondaryGoodsDue, err := s.tagihanLoanDueTotalForType(row.MemberID, statementMonth, "secondary_goods")
		if err != nil {
			return nil, err
		}
		goodsPurchaseDue, err := s.tagihanLoanDueTotalForType(row.MemberID, statementMonth, "goods_purchase_paylater")
		if err != nil {
			return nil, err
		}
		row.PinjamanReguler = regularDue
		row.PinjamanBarangSekunder = secondaryGoodsDue
		row.PembelianBarang = goodsPurchaseDue
		row.PinjamanNonReguler = row.PinjamanBarangSekunder + row.PembelianBarang
		row.Total = row.SimpananWajib + row.SimpananSukarela + row.PinjamanReguler + row.PinjamanNonReguler
		if row.Total > 0 {
			tagihanRows = append(tagihanRows, row)
		}
	}
	return tagihanRows, nil
}

func (s *Server) tagihanSavingAlreadyRecorded(memberID, category string, statementMonth tagihanStatementMonth) bool {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1 AND category=$2 AND type='deposit' AND reference_no=$3`, memberID, category, tagihanReference(statementMonth)).Scan(&count)
	return err == nil && count > 0
}

func (s *Server) tagihanSavingRecordedForStatementMonth(memberID, category string, statementMonth tagihanStatementMonth) bool {
	if s.tagihanSavingAlreadyRecorded(memberID, category, statementMonth) {
		return true
	}

	parsed, err := time.ParseInLocation("2006-01", statementMonth.Value, jakartaLocation)
	if err != nil {
		return false
	}
	monthStart := parsed.Format("2006-01-02")
	var latestRecordDate string
	err = s.db.QueryRow(`
		SELECT record_date
		FROM saving_records
		WHERE member_id=$1
		  AND category=$2
		  AND type='deposit'
		  AND record_date >= $3
		  AND record_date <= $4
		ORDER BY record_date DESC, created_at DESC
		LIMIT 1`, memberID, category, monthStart, statementMonth.CutoffDate).Scan(&latestRecordDate)
	return err == nil && latestRecordDate != ""
}

type tagihanSavingAmounts struct {
	Wajib    int64
	Manasuka int64
}

func (s *Server) tagihanSavingAmounts(memberID string, statementMonth tagihanStatementMonth) (tagihanSavingAmounts, error) {
	wajib, err := s.tagihanLatestSavingAmount(memberID, "wajib", statementMonth.CutoffDate)
	if err != nil {
		return tagihanSavingAmounts{}, err
	}
	manasuka, err := s.tagihanLatestSavingAmount(memberID, "sukarela", statementMonth.CutoffDate)
	if err != nil {
		return tagihanSavingAmounts{}, err
	}
	if wajib > 0 && s.tagihanSavingRecordedForStatementMonth(memberID, "wajib", statementMonth) {
		wajib = 0
	}
	if manasuka > 0 && s.tagihanSavingRecordedForStatementMonth(memberID, "sukarela", statementMonth) {
		manasuka = 0
	}
	return tagihanSavingAmounts{Wajib: wajib, Manasuka: manasuka}, nil
}

func (s *Server) tagihanLatestSavingAmount(memberID, category, cutoffDate string) (int64, error) {
	var recordType string
	var amount int64
	err := s.db.QueryRow(`
		SELECT type, amount
		FROM saving_records
		WHERE member_id=$1
		  AND category=$2
		  AND record_date <= $3
		ORDER BY record_date DESC, created_at DESC
		LIMIT 1`, memberID, category, cutoffDate).Scan(&recordType, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err == nil && recordType != "deposit" {
		return 0, nil
	}
	return amount, err
}

func (s *Server) tagihanLoanDueTotal(memberID string, statementMonth tagihanStatementMonth, regular bool) (int64, error) {
	dues, err := s.tagihanLoanDues(memberID, statementMonth, regular)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, due := range dues {
		total += due.Amount
	}
	return total, nil
}

func (s *Server) tagihanLoanDueTotalForType(memberID string, statementMonth tagihanStatementMonth, loanType string) (int64, error) {
	var total int64
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(li.scheduled_amount-li.paid_amount),0)
		FROM loans l
		INNER JOIN loan_installments li ON li.loan_id=l.id
		WHERE l.member_id=$1
		  AND l.status IN ('active','adjustment_due')
		  AND l.remaining_balance>0
		  AND li.due_date <= $2
		  AND li.paid_amount < li.scheduled_amount
		  AND l.loan_type=$3`, memberID, statementMonth.CutoffDate, loanType).Scan(&total)
	return total, err
}

func (s *Server) tagihanLoanDues(memberID string, statementMonth tagihanStatementMonth, regular bool) ([]tagihanLoanDue, error) {
	loanTypeCondition := "l.loan_type <> 'regular'"
	if regular {
		loanTypeCondition = "l.loan_type = 'regular'"
	}
	rows, err := s.db.Query(`
		SELECT l.id, COALESCE(SUM(li.scheduled_amount-li.paid_amount),0)
		FROM loans l
		INNER JOIN loan_installments li ON li.loan_id=l.id
		WHERE l.member_id=$1
		  AND l.status IN ('active','adjustment_due')
		  AND l.remaining_balance>0
		  AND li.due_date <= $2
		  AND li.paid_amount < li.scheduled_amount
		  AND `+loanTypeCondition+`
		GROUP BY l.id
		ORDER BY MIN(li.due_date), l.created_at`, memberID, statementMonth.CutoffDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dues []tagihanLoanDue
	for rows.Next() {
		var due tagihanLoanDue
		if err := rows.Scan(&due.LoanID, &due.Amount); err != nil {
			return nil, err
		}
		if due.Amount > 0 {
			dues = append(dues, due)
		}
	}
	return dues, rows.Err()
}

func buildTagihanWorkbook(statementMonth tagihanStatementMonth, rows []TagihanRow) (*excelize.File, error) {
	const sheet = "Tagihan"
	workbook := excelize.NewFile()
	defaultSheet := workbook.GetSheetName(0)
	if err := workbook.SetSheetName(defaultSheet, sheet); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	headers := []interface{}{"Member ID", "NPP", "Nama", "Simpanan Wajib", "Simpanan Manasuka", "Pinjaman Reguler", "Pinjaman Barang Sekunder", "Pembelian Barang", "Total Tagihan", "Status"}
	if err := workbook.SetSheetRow(sheet, "A1", &headers); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	for index, row := range rows {
		values := []interface{}{row.MemberID, row.MemberNo, row.FullName, row.SimpananWajib, row.SimpananSukarela, row.PinjamanReguler, row.PinjamanBarangSekunder, row.PembelianBarang, row.Total, row.Status}
		cell, err := excelize.CoordinatesToCellName(1, index+2)
		if err != nil {
			_ = workbook.Close()
			return nil, err
		}
		if err := workbook.SetSheetRow(sheet, cell, &values); err != nil {
			_ = workbook.Close()
			return nil, err
		}
	}
	style, err := workbook.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err == nil {
		_ = workbook.SetCellStyle(sheet, "A1", "J1", style)
	}
	_ = workbook.SetColWidth(sheet, "A", "A", 28)
	_ = workbook.SetColWidth(sheet, "B", "C", 22)
	_ = workbook.SetColWidth(sheet, "D", "J", 18)
	_ = workbook.SetDocProps(&excelize.DocProperties{
		Title:   "Tagihan " + statementMonth.Value,
		Subject: "KKSUK PD Dharma Jaya Tagihan",
	})
	return workbook, nil
}

func (s *Server) importTagihan(statementMonth tagihanStatementMonth, recordDate string, reader io.Reader, recordedBy string) (TagihanImportResult, error) {
	workbook, err := excelize.OpenReader(reader)
	if err != nil {
		return TagihanImportResult{}, err
	}
	defer workbook.Close()

	sheet := workbook.GetSheetName(0)
	rows, err := workbook.GetRows(sheet)
	if err != nil {
		return TagihanImportResult{}, err
	}
	if len(rows) == 0 {
		return TagihanImportResult{}, errors.New("empty tagihan workbook")
	}
	headers := tagihanHeaderIndexes(rows[0])
	memberIDColumn, hasMemberID := headers["member id"]
	statusColumn, hasStatus := headers["status"]
	if !hasMemberID || !hasStatus {
		return TagihanImportResult{}, errors.New("missing required tagihan headers")
	}

	result := TagihanImportResult{StatementMonth: statementMonth.Value, RecordDate: recordDate}
	for index, row := range rows[1:] {
		rowResult := TagihanImportRowResult{ExcelRow: index + 2}
		memberID := tagihanCell(row, memberIDColumn)
		statusValue := tagihanCell(row, statusColumn)
		rowResult.MemberID = memberID
		rowResult.Status = statusValue
		status, ok := parseTagihanStatus(statusValue)
		if strings.TrimSpace(statusValue) == "" {
			rowResult.Result = "skipped"
			rowResult.Messages = append(rowResult.Messages, "blank status")
			result.Summary.Skipped++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if !ok {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, "unknown status")
			result.Summary.Invalid++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if strings.TrimSpace(memberID) == "" {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, "missing member id")
			result.Summary.Invalid++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		member, err := s.tagihanMember(memberID)
		if errors.Is(err, sql.ErrNoRows) {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, "member not eligible")
			result.Summary.Invalid++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if err != nil {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, "member lookup failed")
			result.Summary.Invalid++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		rowResult.MemberNo = member.MemberNo
		rowResult.FullName = member.FullName
		if status == "unpaid" {
			rowResult.Result = "skipped"
			rowResult.Messages = append(rowResult.Messages, "unpaid")
			result.Summary.Skipped++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		savingsCreated, repaymentCreated, messages := s.recordPaidTagihanRow(member.ID, statementMonth, recordDate, recordedBy)
		rowResult.SavingsCreated = savingsCreated
		rowResult.Repayments = repaymentCreated
		rowResult.Messages = append(rowResult.Messages, messages...)
		if savingsCreated == 0 && repaymentCreated == 0 {
			rowResult.Result = "skipped"
			if len(rowResult.Messages) == 0 {
				rowResult.Messages = append(rowResult.Messages, "nothing due")
			}
			result.Summary.Skipped++
		} else {
			rowResult.Result = "imported"
			result.Summary.Imported++
		}
		result.Rows = append(result.Rows, rowResult)
	}
	return result, nil
}

func tagihanHeaderIndexes(row []string) map[string]int {
	headers := map[string]int{}
	for index, value := range row {
		headers[strings.ToLower(strings.TrimSpace(value))] = index
	}
	return headers
}

func tagihanCell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func parseTagihanStatus(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "paid", "lunas":
		return "paid", true
	case "unpaid", "belum lunas":
		return "unpaid", true
	default:
		return "", false
	}
}

func (s *Server) tagihanMember(memberID string) (Member, error) {
	var member Member
	err := s.db.QueryRow(`SELECT id, member_no, full_name, status, member_type FROM members WHERE id=$1 AND status='active' AND member_type='employee'`, memberID).Scan(&member.ID, &member.MemberNo, &member.FullName, &member.Status, &member.MemberType)
	member.MemberTypeLabel = memberTypeLabel(member.MemberType)
	return member, err
}

func (s *Server) recordPaidTagihanRow(memberID string, statementMonth tagihanStatementMonth, recordDate, recordedBy string) (int, int, []string) {
	reference := tagihanReference(statementMonth)
	note := tagihanNote(statementMonth, memberID)
	savingsCreated := 0
	messages := []string{}
	savingAmounts, err := s.tagihanSavingAmounts(memberID, statementMonth)
	if err != nil {
		messages = append(messages, "saving amount lookup failed")
	} else {
		for _, saving := range []struct {
			category string
			amount   int64
		}{
			{"wajib", savingAmounts.Wajib},
			{"sukarela", savingAmounts.Manasuka},
		} {
			if saving.amount <= 0 {
				continue
			}
			if s.tagihanSavingRecordedForStatementMonth(memberID, saving.category, statementMonth) {
				messages = append(messages, "saving "+saving.category+" already recorded")
				continue
			}
			_, err := s.insertSaving(savingRequest{
				MemberID:    memberID,
				Type:        "deposit",
				Category:    saving.category,
				Amount:      saving.amount,
				RecordDate:  recordDate,
				ReferenceNo: reference,
				Note:        note,
			}, recordedBy)
			if err != nil {
				messages = append(messages, "saving "+saving.category+" failed")
				continue
			}
			savingsCreated++
		}
	}

	repaymentsCreated := 0
	for _, regular := range []bool{true, false} {
		dues, err := s.tagihanLoanDues(memberID, statementMonth, regular)
		if err != nil {
			messages = append(messages, "loan due lookup failed")
			continue
		}
		for _, due := range dues {
			if due.Amount <= 0 {
				continue
			}
			_, err := s.recordRepayment(due.LoanID, recordedBy, repaymentInput{
				Amount:      due.Amount,
				RecordDate:  recordDate,
				ReferenceNo: reference,
				Note:        note + " loan " + due.LoanID + " amount " + strconv.FormatInt(due.Amount, 10),
			})
			if err != nil {
				messages = append(messages, "repayment failed for loan "+due.LoanID)
				continue
			}
			repaymentsCreated++
		}
	}
	return savingsCreated, repaymentsCreated, messages
}
