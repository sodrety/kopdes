package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

const (
	adminLoanRequestTemplateVersion = "1"
	adminLoanRequestHeaderRow       = 4
	adminLoanRequestMaxRows         = 1000
	adminLoanRequestMaxFileBytes    = 12 << 20
)

var adminLoanRequestHeaders = []string{
	"NPP Koperasi",
	"Jenis Pinjaman",
	"Jumlah Pengajuan",
	"Tenor (Bulan)",
	"Tujuan",
}

var adminLoanTypeLabels = map[string]string{
	"Reguler":                   "regular",
	"Barang Sekunder":           "secondary_goods",
	"Pembelian Barang/Paylater": "goods_purchase_paylater",
}

var adminLoanTypeCodes = map[string]string{
	"regular":                 "Reguler",
	"secondary_goods":         "Barang Sekunder",
	"goods_purchase_paylater": "Pembelian Barang/Paylater",
}

var (
	errInvalidAdminLoanWorkbook = errors.New("invalid admin loan workbook")
	errAdminLoanBatchNotFound   = errors.New("admin loan batch not found")
	errAdminLoanBatchForbidden  = errors.New("admin loan batch forbidden")
	errAdminLoanBatchExpired    = errors.New("admin loan batch preview expired")
	errAdminLoanBatchWarnings   = errors.New("admin loan batch warnings require acknowledgement")
	errAdminLoanBatchErrors     = errors.New("admin loan batch validation failed")
	errAdminLoanCancelReason    = errors.New("admin loan cancellation reason required")
	errAdminLoanCancelForbidden = errors.New("admin loan cancellation forbidden")
	errAdminLoanMemberNotFound  = errors.New("admin loan member not found")
	errAdminLoanDuplicateMember = errors.New("admin loan duplicate member")
)

type adminLoanRequestInput struct {
	MemberNo            string `json:"member_no" form:"member_no"`
	LoanType            string `json:"loan_type" form:"loan_type"`
	RequestedAmount     int64  `json:"requested_amount" form:"requested_amount"`
	DurationMonths      int    `json:"duration_months" form:"duration_months"`
	Purpose             string `json:"purpose" form:"purpose"`
	AcknowledgeWarnings bool   `json:"acknowledge_warnings" form:"acknowledge_warnings"`
	IdempotencyKey      string `json:"idempotency_key" form:"idempotency_key"`
}

type adminLoanRequestRow struct {
	ExcelRow        int
	MemberNo        string
	FullName        string
	LoanType        string
	RequestedAmount int64
	DurationMonths  int
	Purpose         string
	Errors          []string
	Warnings        []string
	RequestID       string
}

type AdminLoanRequestBatchRow struct {
	ID              string   `json:"id"`
	ExcelRow        int      `json:"excel_row"`
	MemberNo        string   `json:"member_no"`
	FullName        string   `json:"full_name"`
	LoanType        string   `json:"loan_type"`
	RequestedAmount int64    `json:"requested_amount"`
	DurationMonths  int      `json:"duration_months"`
	Purpose         string   `json:"purpose"`
	Status          string   `json:"status"`
	Errors          []string `json:"errors,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	RequestID       string   `json:"request_id,omitempty"`
}

type AdminLoanRequestBatch struct {
	ID                   string                     `json:"id"`
	CreatedByID          string                     `json:"created_by_id"`
	CreatedByName        string                     `json:"created_by_name"`
	FileHash             string                     `json:"file_hash"`
	TemplateVersion      string                     `json:"template_version"`
	Status               string                     `json:"status"`
	SourceFileName       string                     `json:"source_file_name"`
	RowCount             int                        `json:"row_count"`
	CreatedCount         int                        `json:"created_count"`
	ErrorCount           int                        `json:"error_count"`
	WarningCount         int                        `json:"warning_count"`
	PendingCount         int                        `json:"pending_count"`
	ApprovedCount        int                        `json:"approved_count"`
	RejectedCount        int                        `json:"rejected_count"`
	CancelledCount       int                        `json:"cancelled_count"`
	BatchWarnings        []string                   `json:"batch_warnings,omitempty"`
	WarningsAcknowledged bool                       `json:"warnings_acknowledged"`
	PreviewExpiresAt     string                     `json:"preview_expires_at,omitempty"`
	ConfirmedBy          string                     `json:"confirmed_by,omitempty"`
	ConfirmedAt          string                     `json:"confirmed_at,omitempty"`
	CreatedAt            string                     `json:"created_at"`
	UpdatedAt            string                     `json:"updated_at"`
	Rows                 []AdminLoanRequestBatchRow `json:"rows"`
	CanConfirm           bool                       `json:"can_confirm"`
}

func maxLoanDurationForType(loanType string) int {
	switch loanType {
	case "regular":
		return maxRegularLoanDurationMonths
	case "secondary_goods":
		return maxSecondaryGoodsDuration
	case "goods_purchase_paylater":
		return 1
	default:
		return 0
	}
}

func validateLoanRequestInput(req loanRequestInput) error {
	if maxLoanDurationForType(strings.TrimSpace(req.LoanType)) == 0 || req.RequestedAmount <= 0 || req.DurationMonths <= 0 || req.DurationMonths > maxLoanDurationForType(strings.TrimSpace(req.LoanType)) || strings.TrimSpace(req.Purpose) == "" {
		return errInvalidLoanRequest
	}
	return nil
}

func (s *Server) downloadAdminLoanRequestTemplate(c *gin.Context) {
	c.Header("Content-Disposition", `attachment; filename="template-pengajuan-pinjaman.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", adminLoanRequestTemplateXLSX)
}

func (s *Server) parseAdminLoanWorkbookFile(c *gin.Context) ([]byte, string, string, error) {
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size <= 0 || fileHeader.Size > adminLoanRequestMaxFileBytes || !strings.EqualFold(filepath.Ext(fileHeader.Filename), ".xlsx") {
		return nil, "", "", errInvalidAdminLoanWorkbook
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", "", errInvalidAdminLoanWorkbook
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, adminLoanRequestMaxFileBytes+1))
	if err != nil || len(data) == 0 || len(data) > adminLoanRequestMaxFileBytes {
		return nil, "", "", errInvalidAdminLoanWorkbook
	}
	hash := sha256.Sum256(data)
	return data, fmt.Sprintf("%x", hash[:]), fileHeader.Filename, nil
}

func cellValue(workbook *excelize.File, sheet, cell string) string {
	value, err := workbook.GetCellValue(sheet, cell)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func parseAdminLoanWorkbook(data []byte) ([]adminLoanRequestRow, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidAdminLoanWorkbook
	}
	defer workbook.Close()
	inputSheet, inputErr := workbook.GetSheetIndex("Pengajuan Pinjaman")
	guideSheet, guideErr := workbook.GetSheetIndex("Petunjuk")
	if inputErr != nil || guideErr != nil || inputSheet < 0 || guideSheet < 0 {
		return nil, errInvalidAdminLoanWorkbook
	}
	if cellValue(workbook, "Petunjuk", "B1") != adminLoanRequestTemplateVersion {
		return nil, errInvalidAdminLoanWorkbook
	}
	rows, err := workbook.GetRows("Pengajuan Pinjaman")
	if err != nil || len(rows) < adminLoanRequestHeaderRow {
		return nil, errInvalidAdminLoanWorkbook
	}
	headers := rows[adminLoanRequestHeaderRow-1]
	if len(headers) != len(adminLoanRequestHeaders) {
		return nil, errInvalidAdminLoanWorkbook
	}
	for index, expected := range adminLoanRequestHeaders {
		if headers[index] != expected {
			return nil, errInvalidAdminLoanWorkbook
		}
	}

	result := make([]adminLoanRequestRow, 0, minInt(len(rows)-adminLoanRequestHeaderRow, adminLoanRequestMaxRows))
	for index, values := range rows[adminLoanRequestHeaderRow:] {
		excelRow := index + adminLoanRequestHeaderRow + 1
		cells := make([]string, len(adminLoanRequestHeaders))
		for column := range cells {
			if column < len(values) {
				cells[column] = strings.TrimSpace(values[column])
			}
		}
		extraValue := false
		for _, value := range values[len(adminLoanRequestHeaders):] {
			if strings.TrimSpace(value) != "" {
				extraValue = true
				break
			}
		}
		allEmpty := !extraValue
		for _, value := range cells {
			if value != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}
		row := adminLoanRequestRow{ExcelRow: excelRow, MemberNo: cells[0], FullName: "", Purpose: cells[4]}
		if extraValue {
			row.Errors = append(row.Errors, "admin_issue_extra_column")
		}
		if cells[0] == "" || cells[1] == "" || cells[2] == "" || cells[3] == "" || cells[4] == "" {
			row.Errors = append(row.Errors, "admin_issue_partial_row")
		}
		if code, ok := adminLoanTypeLabels[cells[1]]; ok {
			row.LoanType = code
		} else if cells[1] != "" {
			row.Errors = append(row.Errors, "admin_issue_unsupported_loan_type")
		}
		if cells[2] != "" {
			amount, parseErr := strconv.ParseInt(cells[2], 10, 64)
			if parseErr != nil || amount <= 0 {
				row.Errors = append(row.Errors, "admin_issue_amount_numeric")
			} else {
				row.RequestedAmount = amount
			}
		}
		if cells[3] != "" {
			duration, parseErr := strconv.Atoi(cells[3])
			if parseErr != nil || duration <= 0 {
				row.Errors = append(row.Errors, "admin_issue_duration_numeric")
			} else {
				row.DurationMonths = duration
			}
		}
		result = append(result, row)
		if len(result) > adminLoanRequestMaxRows {
			return nil, errInvalidAdminLoanWorkbook
		}
	}
	if len(result) == 0 {
		return nil, errInvalidAdminLoanWorkbook
	}
	return result, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func splitAdminLoanIssues(value string) []string {
	parts := strings.Split(value, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return result
}

func joinAdminLoanIssues(values []string) string {
	return strings.Join(values, "\n")
}

func adminLoanIssueMessage(key string) string {
	return translate(bahasaLanguage, key)
}

func (s *Server) validateAdminLoanRow(tx *sql.Tx, row *adminLoanRequestRow, lock bool) (string, []string, error) {
	if len(row.Errors) > 0 {
		return "", nil, nil
	}
	if err := validateLoanRequestInput(loanRequestInput{LoanType: row.LoanType, RequestedAmount: row.RequestedAmount, DurationMonths: row.DurationMonths, Purpose: row.Purpose}); err != nil {
		row.Errors = append(row.Errors, "admin_issue_invalid_loan_fields")
		return "", nil, nil
	}
	memberID := ""
	status := ""
	bankName := ""
	bankAccount := ""
	if err := tx.QueryRow(`SELECT id,status,COALESCE(bank_name,''),COALESCE(bank_account,''),full_name FROM members WHERE member_no=$1`+func() string {
		if lock {
			return rowLockClause(s.db)
		}
		return ""
	}(), row.MemberNo).Scan(&memberID, &status, &bankName, &bankAccount, &row.FullName); errors.Is(err, sql.ErrNoRows) {
		row.Errors = append(row.Errors, "admin_issue_member_not_found")
		return "", nil, nil
	} else if err != nil {
		return "", nil, err
	}
	if status != "active" {
		row.Errors = append(row.Errors, "admin_issue_inactive_member")
	}
	if strings.TrimSpace(bankName) == "" || strings.TrimSpace(bankAccount) == "" {
		row.Errors = append(row.Errors, "admin_issue_bank_details")
	}
	summary, err := savingSummary(tx, memberID)
	if err != nil {
		return "", nil, err
	}
	if row.RequestedAmount > maxLoanAmountForSavingBalance(summary.CurrentBalance) {
		row.Errors = append(row.Errors, "admin_issue_amount_limit")
	}
	var pendingID string
	if err := tx.QueryRow(`SELECT id FROM loan_requests WHERE member_id=$1 AND status='pending' LIMIT 1`, memberID).Scan(&pendingID); err == nil {
		row.Errors = append(row.Errors, "admin_issue_pending")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", nil, err
	}
	var outstanding int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(remaining_balance),0) FROM loans WHERE member_id=$1 AND status<>'cancelled' AND remaining_balance>0`, memberID).Scan(&outstanding); err != nil {
		return "", nil, err
	}
	if outstanding > 0 {
		row.Errors = append(row.Errors, "admin_issue_outstanding")
	}
	var loginCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE member_id=$1 AND historical_identity=FALSE AND active=TRUE`, memberID).Scan(&loginCount); err != nil {
		return "", nil, err
	}
	warnings := []string{}
	if loginCount == 0 {
		warnings = append(warnings, "admin_issue_no_member_login")
	}
	row.Warnings = append(row.Warnings, warnings...)
	return memberID, warnings, nil
}

func (s *Server) activeManagerApproverWarning(tx *sql.Tx) (bool, error) {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE WHERE oa.role IN ('manager','admin') AND oa.active=TRUE AND m.status='active' AND u.active=TRUE`).Scan(&count)
	return count == 0, err
}

func (s *Server) validateAdminLoanBatchRows(rows []adminLoanRequestRow) ([]adminLoanRequestRow, []string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	noApprover, err := s.activeManagerApproverWarning(tx)
	if err != nil {
		return nil, nil, err
	}
	batchWarnings := []string{}
	if noApprover {
		batchWarnings = append(batchWarnings, "admin_issue_no_manager_approver")
	}
	seen := map[string]bool{}
	for index := range rows {
		row := &rows[index]
		memberNo := strings.TrimSpace(row.MemberNo)
		if memberNo != "" && seen[memberNo] {
			row.Errors = append(row.Errors, "admin_issue_duplicate_member")
		}
		if memberNo != "" {
			seen[memberNo] = true
		}
		if len(row.Errors) == 0 {
			if _, _, err := s.validateAdminLoanRow(tx, row, false); err != nil {
				return nil, nil, err
			}
		}
	}
	return rows, batchWarnings, nil
}

func (s *Server) persistAdminLoanBatch(actor User, fileName, fileHash string, rows []adminLoanRequestRow, batchWarnings []string, forcedError string) (AdminLoanRequestBatch, error) {
	status := "preview"
	if forcedError != "" {
		status = "validation_failed"
		batchWarnings = append(batchWarnings, forcedError)
	}
	errorCount := 0
	warningCount := len(batchWarnings)
	for index := range rows {
		if len(rows[index].Errors) > 0 {
			errorCount++
		}
		warningCount += len(rows[index].Warnings)
	}
	if errorCount > 0 {
		status = "validation_failed"
	}
	previewExpiresAt := any(nil)
	if status == "preview" {
		previewExpiresAt = time.Now().Add(15 * time.Minute)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	batchID := newID()
	if _, err := tx.Exec(`INSERT INTO loan_request_batches (id,created_by,file_hash,template_version,status,source_file_name,row_count,error_count,warning_count,batch_warnings,preview_expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, batchID, actor.ID, fileHash, adminLoanRequestTemplateVersion, status, strings.TrimSpace(fileName), len(rows), errorCount, warningCount, joinAdminLoanIssues(batchWarnings), previewExpiresAt); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	for _, row := range rows {
		rowStatus := "valid"
		if len(row.Errors) > 0 {
			rowStatus = "error"
		} else if len(row.Warnings) > 0 {
			rowStatus = "warning"
		}
		if _, err := tx.Exec(`INSERT INTO loan_request_batch_rows (id,batch_id,excel_row,member_no,full_name,loan_type,requested_amount,duration_months,purpose,status,errors,warnings) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, newID(), batchID, row.ExcelRow, row.MemberNo, row.FullName, row.LoanType, nullableInt64(row.RequestedAmount), nullableInt(row.DurationMonths), row.Purpose, rowStatus, joinAdminLoanIssues(row.Errors), joinAdminLoanIssues(row.Warnings)); err != nil {
			return AdminLoanRequestBatch{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	return s.adminLoanBatchByID(batchID)
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func (s *Server) adminLoanRequestBatchPreview(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "error_authentication_required")
		return
	}
	data, hash, fileName, err := s.parseAdminLoanWorkbookFile(c)
	if err != nil {
		batch, persistErr := s.persistAdminLoanBatch(actor, fileName, hash, nil, nil, adminLoanIssueMessage("admin_issue_invalid_template"))
		if persistErr == nil {
			respondOKOrHXRedirect(c, "/admin/loan-request-batches/"+batch.ID, batch)
			return
		}
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_invalid_admin_loan_template")
		return
	}
	rows, err := parseAdminLoanWorkbook(data)
	if err != nil {
		batch, persistErr := s.persistAdminLoanBatch(actor, fileName, hash, nil, nil, adminLoanIssueMessage("admin_issue_invalid_template"))
		if persistErr == nil {
			respondOKOrHXRedirect(c, "/admin/loan-request-batches/"+batch.ID, batch)
			return
		}
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_invalid_admin_loan_template")
		return
	}
	rows, batchWarnings, err := s.validateAdminLoanBatchRows(rows)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	batch, err := s.persistAdminLoanBatch(actor, fileName, hash, rows, batchWarnings, "")
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	respondOKOrHXRedirect(c, "/admin/loan-request-batches/"+batch.ID, batch)
}

func (s *Server) adminLoanRequestBatchCommit(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "error_authentication_required")
		return
	}
	acknowledge := c.PostForm("acknowledge_warnings") != ""
	batch, err := s.commitAdminLoanRequestBatch(actor, c.Param("id"), acknowledge)
	if errors.Is(err, errAdminLoanBatchNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_admin_loan_batch_not_found")
		return
	}
	if errors.Is(err, errAdminLoanBatchForbidden) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "error_admin_loan_batch_forbidden")
		return
	}
	if errors.Is(err, errAdminLoanBatchExpired) {
		respondError(c, http.StatusConflict, "PREVIEW_EXPIRED", "error_admin_loan_preview_expired")
		return
	}
	if errors.Is(err, errAdminLoanBatchWarnings) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "WARNINGS_REQUIRE_ACKNOWLEDGEMENT", "message": translate(languageFromRequest(c), "error_admin_loan_warnings_ack"), "batch": batch}})
		return
	}
	if errors.Is(err, errAdminLoanBatchErrors) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "BATCH_REVALIDATION_FAILED", "message": translate(languageFromRequest(c), "error_admin_loan_revalidation_failed"), "batch": batch}})
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	respondOKOrHXRedirect(c, "/admin/loan-requests?batch_id="+batch.ID, batch)
}

func (s *Server) createAdminLoanRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "error_authentication_required")
		return
	}
	var input adminLoanRequestInput
	if err := bindRequestWithRupiahAmount(c, &input, "requested_amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_invalid_admin_loan_request")
		return
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = c.GetHeader("Idempotency-Key")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_admin_loan_idempotency_required")
		return
	}
	result, err := s.createAdminLoanRequestFor(actor, input)
	if errors.Is(err, errAdminLoanBatchWarnings) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "WARNINGS_REQUIRE_ACKNOWLEDGEMENT", "message": translate(languageFromRequest(c), "error_admin_loan_warnings_ack"), "warnings": result.Warnings}})
		return
	}
	if errors.Is(err, errAdminLoanMemberNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_admin_loan_member_not_found")
		return
	}
	if errors.Is(err, errPendingLoanRequestExists) {
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", "error_pending_loan_request")
		return
	}
	var rowErr adminLoanRowError
	if errors.As(err, &rowErr) {
		messages := make([]string, 0, len(rowErr))
		for _, key := range rowErr {
			messages = append(messages, translate(languageFromRequest(c), key))
		}
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "BUSINESS_RULE_VIOLATION", "message": strings.Join(messages, "; "), "errors": messages}})
		return
	}
	if errors.Is(err, errOutstandingLoanBalance) {
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", "error_outstanding_loan_request")
		return
	}
	if errors.Is(err, errInvalidLoanRequest) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_loan_request_fields")
		return
	}
	if errors.Is(err, errMemberBankDetailsRequired) || errors.Is(err, errInactiveLoanMember) || errors.Is(err, errLoanAmountLimitExceeded) {
		key := "error_invalid_admin_loan_request"
		if errors.Is(err, errMemberBankDetailsRequired) {
			key = "error_member_bank_details_required"
		} else if errors.Is(err, errInactiveLoanMember) {
			key = "error_inactive_loan_member"
		} else if errors.Is(err, errLoanAmountLimitExceeded) {
			key = "error_loan_amount_limit"
		}
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", key)
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	respondCreatedOrHXRedirect(c, "/admin/loan-requests", gin.H{"loan_request": result.Request, "warnings": result.Warnings})
}

type adminLoanCreateResult struct {
	Request  LoanRequest
	Warnings []string
}

func (s *Server) createAdminLoanRequestFor(actor User, input adminLoanRequestInput) (adminLoanCreateResult, error) {
	input.MemberNo = strings.TrimSpace(input.MemberNo)
	input.LoanType = strings.TrimSpace(input.LoanType)
	input.Purpose = strings.TrimSpace(input.Purpose)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey != "" {
		var existingID string
		if err := s.db.QueryRow(`SELECT id FROM loan_requests WHERE created_by=$1 AND creation_idempotency_key=$2`, actor.ID, input.IdempotencyKey).Scan(&existingID); err == nil {
			request, requestErr := s.loanRequestByID(existingID)
			return adminLoanCreateResult{Request: request}, requestErr
		}
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return adminLoanCreateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row := adminLoanRequestRow{MemberNo: input.MemberNo, LoanType: input.LoanType, RequestedAmount: input.RequestedAmount, DurationMonths: input.DurationMonths, Purpose: input.Purpose}
	memberID, _, err := s.validateAdminLoanRow(tx, &row, true)
	if err != nil {
		return adminLoanCreateResult{}, err
	}
	if len(row.Errors) > 0 {
		return adminLoanCreateResult{}, adminLoanRowError(row.Errors)
	}
	noApprover, err := s.activeManagerApproverWarning(tx)
	if err != nil {
		return adminLoanCreateResult{}, err
	}
	warnings := append([]string(nil), row.Warnings...)
	if noApprover {
		warnings = append(warnings, "admin_issue_no_manager_approver")
	}
	if len(warnings) > 0 && !input.AcknowledgeWarnings {
		return adminLoanCreateResult{Warnings: warnings}, errAdminLoanBatchWarnings
	}
	request, err := s.insertAdminLoanRequestTx(tx, actor, memberID, input, "")
	if err != nil {
		if isUniqueViolation(err) {
			var existingID string
			if lookupErr := tx.QueryRow(`SELECT id FROM loan_requests WHERE created_by=$1 AND creation_idempotency_key=$2`, actor.ID, input.IdempotencyKey).Scan(&existingID); lookupErr == nil {
				if commitErr := tx.Commit(); commitErr != nil {
					return adminLoanCreateResult{}, commitErr
				}
				existing, existingErr := s.loanRequestByID(existingID)
				return adminLoanCreateResult{Request: existing}, existingErr
			}
		}
		return adminLoanCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return adminLoanCreateResult{}, err
	}
	return adminLoanCreateResult{Request: request, Warnings: warnings}, nil
}

type adminLoanRowError []string

func (e adminLoanRowError) Error() string { return strings.Join(e, ",") }

func (s *Server) insertAdminLoanRequestTx(tx *sql.Tx, actor User, memberID string, input adminLoanRequestInput, batchID string) (LoanRequest, error) {
	request := LoanRequest{ID: newID(), MemberID: memberID, RequestedAmount: input.RequestedAmount, DurationMonths: input.DurationMonths, Purpose: strings.TrimSpace(input.Purpose), Status: "pending", LoanType: strings.TrimSpace(input.LoanType), CurrentApprovalStage: approvalStageManager}
	_, err := tx.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,created_by,creation_source,batch_id,creation_idempotency_key) VALUES ($1,$2,$3,$4,$5,'pending',$6,'manager',$7,'admin',$8,$9)`, request.ID, request.MemberID, request.RequestedAmount, request.DurationMonths, request.Purpose, request.LoanType, actor.ID, nullableString(batchID), nullableString(input.IdempotencyKey))
	if err != nil {
		return LoanRequest{}, err
	}
	if batchID == "" {
		if err := createStageNotification(tx, "loan", request.ID, approvalStageManager, "/admin/loan-requests"); err != nil {
			return LoanRequest{}, err
		}
	} else if err := createAdminLoanStageEvent(tx, request.ID); err != nil {
		return LoanRequest{}, err
	}
	if err := createAdminCreatedMemberNotification(tx, request.ID, memberID); err != nil {
		return LoanRequest{}, err
	}
	return request, nil
}

func createAdminLoanStageEvent(tx *sql.Tx, requestID string) error {
	_, err := tx.Exec(`INSERT INTO notification_events (id,event_type,request_type,request_id,payload) VALUES ($1,'approval_stage_ready','loan',$2,'{"stage":"manager","delivery":"batch_summary"}')`, newID(), requestID)
	return err
}

func createAdminLoanBatchNotification(tx *sql.Tx, batchID string, createdCount int) error {
	eventID := newID()
	if _, err := tx.Exec(`INSERT INTO notification_events (id,event_type,request_type,request_id,payload) VALUES ($1,'approval_batch_ready','loan_batch',$2,$3)`, eventID, batchID, fmt.Sprintf(`{"count":%d,"stage":"manager"}`, createdCount)); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT u.id FROM officer_appointments oa JOIN members m ON m.id=oa.member_id JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE WHERE oa.role IN ('manager','admin') AND oa.active=TRUE AND m.status='active' AND u.active=TRUE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link,audience) VALUES ($1,$2,$3,'notification_admin_loan_batch_ready_title','notification_admin_loan_batch_ready_body',$4,'officer')`, newID(), eventID, userID, "/admin/loan-request-batches/"+batchID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func createAdminCreatedMemberNotification(tx *sql.Tx, requestID, memberID string) error {
	var userID string
	if err := tx.QueryRow(`SELECT id FROM users WHERE member_id=$1 AND historical_identity=FALSE AND active=TRUE LIMIT 1`, memberID).Scan(&userID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	eventID := newID()
	if _, err := tx.Exec(`INSERT INTO notification_events (id,event_type,request_type,request_id,payload) VALUES ($1,'loan_request_created','loan',$2,'{}')`, eventID, requestID); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link,audience) VALUES ($1,$2,$3,'notification_admin_loan_created_title','notification_admin_loan_created_body',$4,'member')`, newID(), eventID, userID, "/member/loan-requests")
	return err
}

func (s *Server) commitAdminLoanRequestBatch(actor User, batchID string, acknowledge bool) (AdminLoanRequestBatch, error) {
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var stored AdminLoanRequestBatch
	var batchWarnings string
	var expires sql.NullString
	var confirmedBy sql.NullString
	var confirmedAt sql.NullString
	err = tx.QueryRow(`SELECT b.id,b.created_by,COALESCE(u.full_name,u.email,''),b.file_hash,b.template_version,b.status,b.source_file_name,b.row_count,b.created_count,b.error_count,b.warning_count,b.batch_warnings,b.warnings_acknowledged,b.preview_expires_at,b.confirmed_by,b.confirmed_at,b.created_at,b.updated_at FROM loan_request_batches b LEFT JOIN users u ON u.id=b.created_by WHERE b.id=$1`, batchID).Scan(&stored.ID, &stored.CreatedByID, &stored.CreatedByName, &stored.FileHash, &stored.TemplateVersion, &stored.Status, &stored.SourceFileName, &stored.RowCount, &stored.CreatedCount, &stored.ErrorCount, &stored.WarningCount, &batchWarnings, &stored.WarningsAcknowledged, &expires, &confirmedBy, &confirmedAt, &stored.CreatedAt, &stored.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminLoanRequestBatch{}, errAdminLoanBatchNotFound
	}
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	if stored.CreatedByID != actor.ID {
		return AdminLoanRequestBatch{}, errAdminLoanBatchForbidden
	}
	var committedID string
	if err := tx.QueryRow(`SELECT id FROM loan_request_batches WHERE file_hash=$1 AND status='committed' LIMIT 1`, stored.FileHash).Scan(&committedID); err == nil {
		_ = tx.Rollback()
		return s.adminLoanBatchByID(committedID)
	}
	if stored.Status == "committed" {
		_ = tx.Rollback()
		return s.adminLoanBatchByID(stored.ID)
	}
	if stored.Status != "preview" {
		return AdminLoanRequestBatch{}, errAdminLoanBatchErrors
	}
	if expires.Valid && parseDBTime(expires.String).Before(time.Now()) {
		return AdminLoanRequestBatch{}, errAdminLoanBatchExpired
	}
	if confirmedBy.Valid {
		stored.ConfirmedBy = confirmedBy.String
	}
	if confirmedAt.Valid {
		stored.ConfirmedAt = confirmedAt.String
	}
	stored.BatchWarnings = splitAdminLoanIssues(batchWarnings)
	rows, err := adminLoanBatchRowsTx(tx, batchID)
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	validationRows := make([]adminLoanRequestRow, len(rows))
	for index, row := range rows {
		validationRows[index] = adminLoanRequestRow{ExcelRow: row.ExcelRow, MemberNo: row.MemberNo, FullName: row.FullName, LoanType: row.LoanType, RequestedAmount: row.RequestedAmount, DurationMonths: row.DurationMonths, Purpose: row.Purpose, Warnings: row.Warnings}
	}
	noApprover, err := s.activeManagerApproverWarning(tx)
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	newBatchWarnings := []string{}
	if noApprover {
		newBatchWarnings = append(newBatchWarnings, "admin_issue_no_manager_approver")
	}
	seen := map[string]bool{}
	for index := range validationRows {
		row := &validationRows[index]
		row.Errors = nil
		row.Warnings = nil
		if seen[row.MemberNo] {
			row.Errors = append(row.Errors, "admin_issue_duplicate_member")
		}
		seen[row.MemberNo] = true
		if len(row.Errors) == 0 {
			if _, _, err := s.validateAdminLoanRow(tx, row, true); err != nil {
				return AdminLoanRequestBatch{}, err
			}
		}
	}
	for _, row := range validationRows {
		rowStatus := "valid"
		if len(row.Errors) > 0 {
			rowStatus = "error"
		} else if len(row.Warnings) > 0 {
			rowStatus = "warning"
		}
		if _, err := tx.Exec(`UPDATE loan_request_batch_rows SET full_name=$1,errors=$2,warnings=$3,status=$4,request_id=NULL WHERE batch_id=$5 AND excel_row=$6`, row.FullName, joinAdminLoanIssues(row.Errors), joinAdminLoanIssues(row.Warnings), rowStatus, batchID, row.ExcelRow); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		if len(row.Errors) > 0 {
			stored.ErrorCount++
		}
	}
	stored.BatchWarnings = newBatchWarnings
	stored.WarningCount = len(newBatchWarnings)
	for _, row := range validationRows {
		stored.WarningCount += len(row.Warnings)
	}
	if stored.ErrorCount > 0 {
		stored.Status = "validation_failed"
		stored.CreatedCount = 0
		if _, err := tx.Exec(`UPDATE loan_request_batches SET status='validation_failed',error_count=$1,warning_count=$2,batch_warnings=$3,updated_at=CURRENT_TIMESTAMP WHERE id=$4`, stored.ErrorCount, stored.WarningCount, joinAdminLoanIssues(stored.BatchWarnings), batchID); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		if err := tx.Commit(); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		batch, loadErr := s.adminLoanBatchByID(batchID)
		return batch, errors.Join(errAdminLoanBatchErrors, loadErr)
	}
	if stored.WarningCount > 0 && !acknowledge {
		if _, err := tx.Exec(`UPDATE loan_request_batches SET warning_count=$1,batch_warnings=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$3`, stored.WarningCount, joinAdminLoanIssues(stored.BatchWarnings), batchID); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		if err := tx.Commit(); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		batch, loadErr := s.adminLoanBatchByID(batchID)
		return batch, errors.Join(errAdminLoanBatchWarnings, loadErr)
	}
	created := 0
	for index, row := range validationRows {
		input := adminLoanRequestInput{MemberNo: row.MemberNo, LoanType: row.LoanType, RequestedAmount: row.RequestedAmount, DurationMonths: row.DurationMonths, Purpose: row.Purpose, AcknowledgeWarnings: acknowledge}
		memberID, _, err := s.validateAdminLoanRow(tx, &row, true)
		if err != nil {
			return AdminLoanRequestBatch{}, err
		}
		request, err := s.insertAdminLoanRequestTx(tx, actor, memberID, input, batchID)
		if err != nil {
			return AdminLoanRequestBatch{}, err
		}
		if _, err := tx.Exec(`UPDATE loan_request_batch_rows SET status='created',request_id=$1,errors='',warnings=$2 WHERE batch_id=$3 AND excel_row=$4`, request.ID, joinAdminLoanIssues(row.Warnings), batchID, validationRows[index].ExcelRow); err != nil {
			return AdminLoanRequestBatch{}, err
		}
		created++
	}
	if err := createAdminLoanBatchNotification(tx, batchID, created); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	if _, err := tx.Exec(`UPDATE loan_request_batches SET status='committed',created_count=$1,error_count=0,warning_count=$2,batch_warnings=$3,warnings_acknowledged=$4,confirmed_by=$5,confirmed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP,preview_expires_at=NULL WHERE id=$6`, created, stored.WarningCount, joinAdminLoanIssues(stored.BatchWarnings), acknowledge, actor.ID, batchID); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	return s.adminLoanBatchByID(batchID)
}

func parseDBTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Now().Add(-time.Hour)
}

func adminLoanBatchRowsTx(tx *sql.Tx, batchID string) ([]AdminLoanRequestBatchRow, error) {
	rows, err := tx.Query(`SELECT id,excel_row,member_no,full_name,loan_type,COALESCE(requested_amount,0),COALESCE(duration_months,0),purpose,status,errors,warnings,COALESCE(request_id,'') FROM loan_request_batch_rows WHERE batch_id=$1 ORDER BY excel_row`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdminLoanRequestBatchRow{}
	for rows.Next() {
		var row AdminLoanRequestBatchRow
		var errorsText, warningsText string
		if err := rows.Scan(&row.ID, &row.ExcelRow, &row.MemberNo, &row.FullName, &row.LoanType, &row.RequestedAmount, &row.DurationMonths, &row.Purpose, &row.Status, &errorsText, &warningsText, &row.RequestID); err != nil {
			return nil, err
		}
		row.Errors = splitAdminLoanIssues(errorsText)
		row.Warnings = splitAdminLoanIssues(warningsText)
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Server) adminLoanBatchByID(batchID string) (AdminLoanRequestBatch, error) {
	var batch AdminLoanRequestBatch
	var batchWarnings string
	var expires, confirmedBy, confirmedAt sql.NullString
	err := s.db.QueryRow(`SELECT b.id,b.created_by,COALESCE(u.full_name,u.email,''),b.file_hash,b.template_version,b.status,b.source_file_name,b.row_count,b.created_count,b.error_count,b.warning_count,b.batch_warnings,b.warnings_acknowledged,b.preview_expires_at,b.confirmed_by,b.confirmed_at,b.created_at,b.updated_at FROM loan_request_batches b LEFT JOIN users u ON u.id=b.created_by WHERE b.id=$1`, batchID).Scan(&batch.ID, &batch.CreatedByID, &batch.CreatedByName, &batch.FileHash, &batch.TemplateVersion, &batch.Status, &batch.SourceFileName, &batch.RowCount, &batch.CreatedCount, &batch.ErrorCount, &batch.WarningCount, &batchWarnings, &batch.WarningsAcknowledged, &expires, &confirmedBy, &confirmedAt, &batch.CreatedAt, &batch.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminLoanRequestBatch{}, errAdminLoanBatchNotFound
	}
	if err != nil {
		return AdminLoanRequestBatch{}, err
	}
	batch.BatchWarnings = splitAdminLoanIssues(batchWarnings)
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='approved' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='rejected' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END),0) FROM loan_requests WHERE batch_id=$1`, batchID).Scan(&batch.PendingCount, &batch.ApprovedCount, &batch.RejectedCount, &batch.CancelledCount); err != nil {
		return AdminLoanRequestBatch{}, err
	}
	if expires.Valid {
		batch.PreviewExpiresAt = expires.String
	}
	if confirmedBy.Valid {
		batch.ConfirmedBy = confirmedBy.String
	}
	if confirmedAt.Valid {
		batch.ConfirmedAt = confirmedAt.String
	}
	batch.Rows, err = func() ([]AdminLoanRequestBatchRow, error) {
		rows, queryErr := s.db.Query(`SELECT id,excel_row,member_no,full_name,loan_type,COALESCE(requested_amount,0),COALESCE(duration_months,0),purpose,status,errors,warnings,COALESCE(request_id,'') FROM loan_request_batch_rows WHERE batch_id=$1 ORDER BY excel_row`, batchID)
		if queryErr != nil {
			return nil, queryErr
		}
		defer rows.Close()
		result := []AdminLoanRequestBatchRow{}
		for rows.Next() {
			var row AdminLoanRequestBatchRow
			var errorsText, warningsText string
			if scanErr := rows.Scan(&row.ID, &row.ExcelRow, &row.MemberNo, &row.FullName, &row.LoanType, &row.RequestedAmount, &row.DurationMonths, &row.Purpose, &row.Status, &errorsText, &warningsText, &row.RequestID); scanErr != nil {
				return nil, scanErr
			}
			row.Errors = splitAdminLoanIssues(errorsText)
			row.Warnings = splitAdminLoanIssues(warningsText)
			result = append(result, row)
		}
		return result, rows.Err()
	}()
	return batch, err
}

func (s *Server) adminLoanRequestBatchAPI(c *gin.Context) {
	batch, err := s.adminLoanBatchByID(c.Param("id"))
	if errors.Is(err, errAdminLoanBatchNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_admin_loan_batch_not_found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	if actor, ok := currentUser(c); ok {
		batch.CanConfirm = batch.Status == "preview" && actor.ID == batch.CreatedByID && !parseDBTime(batch.PreviewExpiresAt).Before(time.Now())
	}
	c.JSON(http.StatusOK, gin.H{"batch": batch})
}

func (s *Server) adminLoanRequestBatchPage(c *gin.Context) {
	batch, err := s.adminLoanBatchByID(c.Param("id"))
	if errors.Is(err, errAdminLoanBatchNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_admin_loan_batch_not_found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	actor, _ := currentUser(c)
	batch.CanConfirm = batch.Status == "preview" && actor.ID == batch.CreatedByID && batch.ErrorCount == 0 && !parseDBTime(batch.PreviewExpiresAt).Before(time.Now())
	renderPage(c, "admin-loan-request-batch", pageData(c, translate(languageFromRequest(c), "admin_loan_batch_page_title"), "loan-requests", "admin_loan_batch_heading", "admin_loan_batch_description", gin.H{"Batch": batch}))
}

func (s *Server) exportAdminLoanRequestBatch(c *gin.Context) {
	batch, err := s.adminLoanBatchByID(c.Param("id"))
	if errors.Is(err, errAdminLoanBatchNotFound) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_admin_loan_batch_not_found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	_ = workbook.SetSheetName(sheet, "Hasil")
	headers := []string{"Baris Excel", "NPP Koperasi", "Nama Anggota", "Jenis Pinjaman", "Jumlah Pengajuan", "Tenor (Bulan)", "Tujuan", "Status", "Kesalahan", "Peringatan", "ID Permintaan"}
	for column, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(column+1, 1)
		_ = workbook.SetCellValue("Hasil", cell, header)
	}
	for index, row := range batch.Rows {
		values := []any{row.ExcelRow, row.MemberNo, row.FullName, adminLoanTypeCodes[row.LoanType], row.RequestedAmount, row.DurationMonths, row.Purpose, row.Status, strings.Join(row.Errors, "; "), strings.Join(row.Warnings, "; "), row.RequestID}
		for column, value := range values {
			cell, _ := excelize.CoordinatesToCellName(column+1, index+2)
			_ = workbook.SetCellValue("Hasil", cell, value)
		}
	}
	_ = workbook.SetColWidth("Hasil", "A", "A", 12)
	_ = workbook.SetColWidth("Hasil", "B", "K", 24)
	var output bytes.Buffer
	if err := workbook.Write(&output); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="hasil-batch-pengajuan-pinjaman-%s.xlsx"`, batch.ID))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", output.Bytes())
}

func (s *Server) cancelAdminLoanRequest(c *gin.Context) {
	actor, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "error_authentication_required")
		return
	}
	reason := strings.TrimSpace(c.PostForm("cancellation_reason"))
	if reason == "" {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "error_admin_loan_cancellation_reason")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	defer func() { _ = tx.Rollback() }()
	var status, source, createdBy, memberID string
	err = tx.QueryRow(`SELECT status,creation_source,COALESCE(created_by,''),member_id FROM loan_requests WHERE id=$1`+rowLockClause(s.db), c.Param("id")).Scan(&status, &source, &createdBy, &memberID)
	if errors.Is(err, sql.ErrNoRows) {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "error_loan_request_not_found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	if source != "admin" || (actor.Role != "super_admin" && (actor.Role != "admin" || createdBy != actor.ID)) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", "error_admin_loan_cancel_forbidden")
		return
	}
	if status != "pending" {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "error_loan_request_not_pending")
		return
	}
	result, err := tx.Exec(`UPDATE loan_requests SET status='cancelled',current_approval_stage=NULL,cancellation_reason=$1,cancelled_by=$2,cancelled_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$3 AND status='pending'`, reason, actor.ID, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		respondError(c, http.StatusBadRequest, "BUSINESS_RULE_VIOLATION", "error_loan_request_not_pending")
		return
	}
	if err := resolveRequestNotifications(tx, "loan", c.Param("id")); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	request, err := s.loanRequestByID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "error_internal_server")
		return
	}
	respondOKOrHXRedirect(c, "/admin/loan-requests", request)
}
