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
		SavingSource: "member_tagihan_config",
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
	Source                 string `json:"source"`
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
	Committed      bool                       `json:"committed"`
	Message        string                     `json:"message,omitempty"`
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
	Source         string   `json:"source,omitempty"`
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

	result, err := s.importTagihan(statementMonth, recordDate, file, user.ID, languageFromRequest(c))
	var rejected tagihanImportRejectedError
	if errors.As(err, &rejected) {
		c.JSON(http.StatusUnprocessableEntity, rejected.result)
		return
	}
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
	config, err := s.memberTagihanConfig(memberID)
	if err != nil {
		return tagihanSavingAmounts{}, err
	}
	wajib := config.SimpananWajib
	manasuka := config.SimpananManasuka
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

func (s *Server) tagihanMappingCode(transactionType, component, loanType string) (string, error) {
	return tagihanMappingCodeWithQueryer(s.db, transactionType, component, loanType)
}

func (s *Server) tagihanMappingCodeTx(tx *sql.Tx, transactionType, component, loanType string) (string, error) {
	return tagihanMappingCodeWithQueryer(tx, transactionType, component, loanType)
}

type sqlRowQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func tagihanMappingCodeWithQueryer(queryer sqlRowQueryer, transactionType, component, loanType string) (string, error) {
	var code string
	err := queryer.QueryRow(`SELECT COALESCE(coa_code,'') FROM accounting_mappings WHERE transaction_type=$1 AND component=$2 AND loan_type=$3 AND active=TRUE AND (effective_from IS NULL OR effective_from <= CURRENT_DATE) AND (effective_to IS NULL OR effective_to >= CURRENT_DATE) ORDER BY COALESCE(effective_from,'0000-00-00') DESC LIMIT 1`, transactionType, component, loanType).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) || strings.TrimSpace(code) == "" {
		return "", errors.New("required accounting mapping is missing")
	}
	var active, isGroup bool
	if err := queryer.QueryRow(`SELECT active,is_group FROM coa_accounts WHERE code=$1`, strings.TrimSpace(code)).Scan(&active, &isGroup); errors.Is(err, sql.ErrNoRows) {
		return "", errCOAAccountNotFound
	} else if err != nil {
		return "", err
	} else if !active {
		return "", errCOAAccountInactive
	} else if isGroup {
		return "", errCOAAccountGroup
	}
	return strings.TrimSpace(code), nil
}

func (s *Server) loanTypeForTagihanDue(loanID string) (string, error) {
	var loanType string
	err := s.db.QueryRow(`SELECT COALESCE(loan_type,'') FROM loans WHERE id=$1`, loanID).Scan(&loanType)
	return loanType, err
}

func (s *Server) loanTypeForTagihanDueTx(tx *sql.Tx, loanID string) (string, error) {
	var loanType string
	err := tx.QueryRow(`SELECT COALESCE(loan_type,'') FROM loans WHERE id=$1`, loanID).Scan(&loanType)
	return loanType, err
}

func (s *Server) recordPaidTagihanRowTx(tx *sql.Tx, memberID string, statementMonth tagihanStatementMonth, recordDate, source, recordedBy, language string, savingAmounts tagihanSavingAmounts, dues []tagihanLoanDue) (int, int, []string, error) {
	reference := tagihanReference(statementMonth)
	note := tagihanNote(statementMonth, memberID)
	savingsCreated := 0
	messages := []string{}
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
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1 AND category=$2 AND type='deposit' AND reference_no=$3`, memberID, saving.category, reference).Scan(&existing); err != nil {
			return 0, 0, nil, err
		}
		if existing > 0 {
			messages = append(messages, fmt.Sprintf(translate(language, "tagihan_saving_already_recorded"), saving.category))
			continue
		}
		coaCode, err := s.tagihanMappingCodeTx(tx, "savings", saving.category, "")
		if err != nil {
			return 0, 0, nil, err
		}
		id := newID()
		if _, err := tx.Exec(`INSERT INTO saving_records (id,member_id,type,category,source,coa_code,amount,record_date,reference_no,note,recorded_by) VALUES ($1,$2,'deposit',$3,$4,$5,$6,$7,$8,$9,$10)`, id, memberID, saving.category, source, coaCode, saving.amount, recordDate, reference, note, recordedBy); err != nil {
			return 0, 0, nil, err
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{ReferenceNo: reference, TransactionID: id, TransactionType: "savings", TransactionDate: recordDate, Source: source, Amount: saving.amount, Direction: accountingDirectionDebit, COACode: coaCode, Description: "Tagihan Simpanan " + saving.category, RecordedBy: recordedBy, BatchID: reference}); err != nil {
			return 0, 0, nil, err
		}
		savingsCreated++
	}

	repaymentsCreated := 0
	for _, due := range dues {
		loanType, err := s.loanTypeForTagihanDueTx(tx, due.LoanID)
		if err != nil {
			return 0, 0, nil, err
		}
		coaCode, err := s.tagihanMappingCodeTx(tx, "repayment", "principal", loanType)
		if err != nil {
			return 0, 0, nil, err
		}
		if err := s.recordTagihanRepaymentTx(tx, due.LoanID, due.Amount, recordDate, reference, note, source, coaCode, recordedBy); err != nil {
			return 0, 0, nil, err
		}
		repaymentsCreated++
	}
	return savingsCreated, repaymentsCreated, messages, nil
}

func (s *Server) recordTagihanRepaymentTx(tx *sql.Tx, loanID string, amount int64, recordDate, reference, note, source, coaCode, recordedBy string) error {
	var loan struct {
		MemberID         string
		RemainingBalance int64
		Status           string
	}
	if err := tx.QueryRow(`SELECT member_id,remaining_balance,status FROM loans WHERE id=$1`+rowLockClause(s.db), loanID).Scan(&loan.MemberID, &loan.RemainingBalance, &loan.Status); err != nil {
		return err
	}
	if loan.Status != "active" && loan.Status != "adjustment_due" || amount <= 0 || amount > loan.RemainingBalance {
		return errRepaymentOverBalance
	}
	remaining := amount
	rows, err := tx.Query(`SELECT id,scheduled_amount,paid_amount FROM loan_installments WHERE loan_id=$1 AND paid_amount < scheduled_amount ORDER BY installment_no`, loanID)
	if err != nil {
		return err
	}
	type unpaid struct {
		id              string
		scheduled, paid int64
	}
	var installments []unpaid
	for rows.Next() {
		var item unpaid
		if err := rows.Scan(&item.id, &item.scheduled, &item.paid); err != nil {
			rows.Close()
			return err
		}
		installments = append(installments, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range installments {
		if remaining == 0 {
			break
		}
		applied := item.scheduled - item.paid
		if applied > remaining {
			applied = remaining
		}
		if _, err := tx.Exec(`UPDATE loan_installments SET paid_amount=paid_amount+$1 WHERE id=$2`, applied, item.id); err != nil {
			return err
		}
		remaining -= applied
	}
	if remaining != 0 {
		return errRepaymentOverBalance
	}
	newBalance := loan.RemainingBalance - amount
	newStatus := loan.Status
	if newBalance == 0 {
		newStatus = "paid"
	}
	var nextDue string
	if newBalance > 0 {
		if err := tx.QueryRow(`SELECT due_date FROM loan_installments WHERE loan_id=$1 AND paid_amount < scheduled_amount ORDER BY installment_no LIMIT 1`, loanID).Scan(&nextDue); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE loans SET remaining_balance=$1,status=$2,next_due_date=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4 AND status=$5 AND remaining_balance=$6`, newBalance, newStatus, nextDue, loanID, loan.Status, loan.RemainingBalance); err != nil {
		return err
	}
	id := newID()
	if _, err := tx.Exec(`INSERT INTO loan_repayments (id,loan_id,member_id,source,coa_code,amount,record_date,reference_no,note,recorded_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, loanID, loan.MemberID, source, coaCode, amount, recordDate, reference, note, recordedBy); err != nil {
		return err
	}
	return s.createFinancialJournalTx(tx, accountingJournalInput{ReferenceNo: reference, TransactionID: id, TransactionType: "repayment", TransactionDate: recordDate, Source: source, Amount: amount, Direction: accountingDirectionDebit, COACode: coaCode, Description: "Tagihan Angsuran", RecordedBy: recordedBy, BatchID: reference})
}

func buildTagihanWorkbook(statementMonth tagihanStatementMonth, rows []TagihanRow) (*excelize.File, error) {
	const sheet = "Tagihan"
	workbook := excelize.NewFile()
	defaultSheet := workbook.GetSheetName(0)
	if err := workbook.SetSheetName(defaultSheet, sheet); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	headers := []interface{}{"Member ID", "NPP", "Nama", "Simpanan Wajib", "Simpanan Manasuka", "Pinjaman Reguler", "Pinjaman Barang Sekunder", "Pembelian Barang", "Total Tagihan", "Source", "Status"}
	if err := workbook.SetSheetRow(sheet, "A1", &headers); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	for index, row := range rows {
		values := []interface{}{row.MemberID, row.MemberNo, row.FullName, row.SimpananWajib, row.SimpananSukarela, row.PinjamanReguler, row.PinjamanBarangSekunder, row.PembelianBarang, row.Total, row.Source, row.Status}
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
		_ = workbook.SetCellStyle(sheet, "A1", "K1", style)
	}
	_ = workbook.SetColWidth(sheet, "A", "A", 28)
	_ = workbook.SetColWidth(sheet, "B", "C", 22)
	_ = workbook.SetColWidth(sheet, "D", "K", 18)
	_ = workbook.SetDocProps(&excelize.DocProperties{
		Title:   "Tagihan " + statementMonth.Value,
		Subject: "KKSUK PD Dharma Jaya Tagihan",
	})
	return workbook, nil
}

func (s *Server) importTagihan(statementMonth tagihanStatementMonth, recordDate string, reader io.Reader, recordedBy, language string) (TagihanImportResult, error) {
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
	sourceColumn, hasSource := headers["source"]
	if !hasSource {
		sourceColumn, hasSource = headers["sumber"]
	}
	if !hasMemberID || !hasStatus || !hasSource {
		return TagihanImportResult{}, errors.New("missing required tagihan headers")
	}

	result := TagihanImportResult{StatementMonth: statementMonth.Value, RecordDate: recordDate, Message: translate(language, "tagihan_import_no_writes")}
	type validatedTagihanRow struct {
		result  TagihanImportRowResult
		member  Member
		status  string
		source  string
		savings tagihanSavingAmounts
		dues    []tagihanLoanDue
	}
	validated := make([]validatedTagihanRow, 0, len(rows)-1)
	seenMembers := map[string]bool{}
	mappingCache := map[string]string{}
	lookupMapping := func(transactionType, component, loanType string) (string, error) {
		key := transactionType + ":" + component + ":" + loanType
		if code, ok := mappingCache[key]; ok {
			return code, nil
		}
		code, err := s.tagihanMappingCode(transactionType, component, loanType)
		if err == nil {
			mappingCache[key] = code
		}
		return code, err
	}
	invalidRows := 0
	for index, row := range rows[1:] {
		rowResult := TagihanImportRowResult{ExcelRow: index + 2}
		memberID := tagihanCell(row, memberIDColumn)
		statusValue := tagihanCell(row, statusColumn)
		sourceValue := tagihanCell(row, sourceColumn)
		rowResult.MemberID = memberID
		rowResult.Status = statusValue
		rowResult.Source = strings.ToLower(strings.TrimSpace(sourceValue))
		status, ok := parseTagihanStatus(statusValue)
		if strings.TrimSpace(statusValue) == "" {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_status_required"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if !ok {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_unknown_status"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if strings.TrimSpace(memberID) == "" {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, "missing member id")
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if seenMembers[memberID] {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_duplicate_member"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		seenMembers[memberID] = true
		member, err := s.tagihanMember(memberID)
		if errors.Is(err, sql.ErrNoRows) {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_member_not_eligible"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if err != nil {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_member_lookup_failed"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		rowResult.MemberNo = member.MemberNo
		rowResult.FullName = member.FullName
		if status == "unpaid" {
			if rowResult.Source != "" && !validTransactionSource(rowResult.Source) {
				rowResult.Result = "invalid"
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_source_invalid"))
				invalidRows++
				result.Rows = append(result.Rows, rowResult)
				continue
			}
			rowResult.Result = "skipped"
			rowResult.Messages = append(rowResult.Messages, "unpaid")
			result.Summary.Skipped++
			result.Rows = append(result.Rows, rowResult)
			validated = append(validated, validatedTagihanRow{result: rowResult, member: member, status: status, source: rowResult.Source})
			continue
		}
		savings, err := s.tagihanSavingAmounts(member.ID, statementMonth)
		if err != nil {
			rowResult.Result = "invalid"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_saving_lookup_failed"))
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		dues := []tagihanLoanDue{}
		for _, regular := range []bool{true, false} {
			loanDues, duesErr := s.tagihanLoanDues(member.ID, statementMonth, regular)
			if duesErr != nil {
				rowResult.Result = "invalid"
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_loan_due_lookup_failed"))
				invalidRows++
				break
			}
			dues = append(dues, loanDues...)
		}
		hasDue := savings.Wajib > 0 || savings.Manasuka > 0 || len(dues) > 0
		if hasDue {
			if rowResult.Source == "" {
				rowResult.Messages = append(rowResult.Messages, "source is required for paid rows with transactions")
			} else if !validTransactionSource(rowResult.Source) {
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_source_invalid"))
			}
		}
		if rowResult.Source != "" && !validTransactionSource(rowResult.Source) {
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_source_invalid"))
		}
		if savings.Wajib > 0 {
			if _, mappingErr := lookupMapping("savings", "wajib", ""); mappingErr != nil {
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_mapping_missing_savings_wajib"))
			}
		}
		if savings.Manasuka > 0 {
			if _, mappingErr := lookupMapping("savings", "sukarela", ""); mappingErr != nil {
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_mapping_missing_savings_sukarela"))
			}
		}
		for _, due := range dues {
			loanType, loanErr := s.loanTypeForTagihanDue(due.LoanID)
			if loanErr != nil {
				rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_loan_type_lookup_failed"))
				continue
			}
			if _, mappingErr := lookupMapping("repayment", "principal", loanType); mappingErr != nil {
				rowResult.Messages = append(rowResult.Messages, fmt.Sprintf(translate(language, "tagihan_mapping_missing_repayment"), loanType))
			}
		}
		if len(rowResult.Messages) > 0 {
			rowResult.Result = "invalid"
			invalidRows++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		if !hasDue {
			rowResult.Result = "skipped"
			rowResult.Messages = append(rowResult.Messages, translate(language, "tagihan_nothing_due"))
			result.Summary.Skipped++
		}
		result.Rows = append(result.Rows, rowResult)
		validated = append(validated, validatedTagihanRow{result: rowResult, member: member, status: status, source: rowResult.Source, savings: savings, dues: dues})
	}
	if invalidRows > 0 {
		result.Summary.Invalid = invalidRows
		result.Message = translate(language, "tagihan_import_rejected")
		return result, tagihanImportRejectedError{result: result}
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TagihanImportResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, row := range validated {
		if row.status != "paid" || (row.savings.Wajib <= 0 && row.savings.Manasuka <= 0 && len(row.dues) == 0) {
			continue
		}
		savingsCreated, repaymentCreated, messages, writeErr := s.recordPaidTagihanRowTx(tx, row.member.ID, statementMonth, recordDate, row.source, recordedBy, language, row.savings, row.dues)
		if writeErr != nil {
			return TagihanImportResult{}, writeErr
		}
		for index := range result.Rows {
			if result.Rows[index].ExcelRow != row.result.ExcelRow {
				continue
			}
			result.Rows[index].SavingsCreated = savingsCreated
			result.Rows[index].Repayments = repaymentCreated
			result.Rows[index].Messages = append(result.Rows[index].Messages, messages...)
			if savingsCreated == 0 && repaymentCreated == 0 {
				result.Rows[index].Result = "skipped"
				result.Rows[index].Messages = append(result.Rows[index].Messages, translate(language, "tagihan_nothing_due"))
				result.Summary.Skipped++
			} else {
				result.Rows[index].Result = "imported"
				result.Summary.Imported++
			}
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return TagihanImportResult{}, err
	}
	result.Committed = true
	result.Message = translate(language, "tagihan_import_committed")
	return result, nil
}

type tagihanImportRejectedError struct {
	result TagihanImportResult
}

func (e tagihanImportRejectedError) Error() string {
	return e.result.Message
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
