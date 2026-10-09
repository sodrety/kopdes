package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func voucherWorkbook(t *testing.T, fixture testFixture, token string, rows [][]any) []byte {
	t.Helper()
	response := adminLoanJSONRequest(fixture, http.MethodGet, "/api/admin/loan-requests/template.xlsx", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("download template: %d %s", response.Code, response.Body.String())
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer workbook.Close()
	version, _ := workbook.GetCellValue("Petunjuk", "B1")
	header, _ := workbook.GetCellValue("Pengajuan Pinjaman", "F4")
	if version != "2" || header != "Biaya Admin (Nominal)" {
		t.Fatalf("unexpected template: version=%q fee header=%q", version, header)
	}
	for index, row := range rows {
		if err := workbook.SetSheetRow("Pengajuan Pinjaman", fmt.Sprintf("A%d", index+5), &row); err != nil {
			t.Fatal(err)
		}
	}
	output, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func commitVoucherBatch(t *testing.T, fixture testFixture, token, batchID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+batchID+"/commit", strings.NewReader("acknowledge_warnings=on"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("commit voucher batch: %d %s", rec.Code, rec.Body.String())
	}
}

func TestVoucherBulkNominalFeeApprovalAndPrimerJournals(t *testing.T) {
	for _, fee := range []int64{12345, 0} {
		t.Run(fmt.Sprint(fee), func(t *testing.T) {
			fixture := newTestFixture(t)
			manager := fixture.login(t, "admin@coop.test", "password")
			super := establishSuperAdmin(t, fixture)
			_, admin := provisionAdminOfficer(t, fixture, super, manager)
			member := fixture.createMember(t, manager, `{"member_no":"V-001","full_name":"Voucher Borrower","join_date":"2026-01-01","status":"active","bank_name":"Bank Test","bank_account":"123","email":"voucher@coop.test","password":"member-password"}`)
			fixture.recordSaving(t, manager, member.ID, "deposit", 200000, "V-SAVING", "Capacity")
			body := fmt.Sprintf(`{"member_no":"V-001","loan_type":"voucher","requested_amount":100000,"duration_months":1,"purpose":"Voucher","admin_fee":%d,"idempotency_key":"voucher-single","acknowledge_warnings":true}`, fee)
			if response := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", admin, body); response.Code != http.StatusBadRequest {
				t.Fatalf("individual intake must reject Voucher: %d %s", response.Code, response.Body.String())
			}
			memberToken := fixture.login(t, "voucher@coop.test", "member-password")
			if response := adminLoanJSONRequest(fixture, http.MethodPost, "/api/member/loan-requests", memberToken, body); response.Code != http.StatusBadRequest {
				t.Fatalf("member intake must reject Voucher: %d %s", response.Code, response.Body.String())
			}
			preview := adminLoanMultipartRequest(fixture, admin, voucherWorkbook(t, fixture, admin, [][]any{{"V-001", "Voucher", 100000, 1, "Voucher", fee}}))
			var batch struct {
				ID         string
				ErrorCount int `json:"error_count"`
				Rows       []struct {
					AdminFee *int64 `json:"admin_fee"`
				}
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &batch); err != nil || preview.Code != http.StatusOK || batch.ErrorCount != 0 || len(batch.Rows) != 1 || batch.Rows[0].AdminFee == nil || *batch.Rows[0].AdminFee != fee {
				t.Fatalf("preview lost nominal fee: %v %d %s", err, preview.Code, preview.Body.String())
			}
			commitVoucherBatch(t, fixture, admin, batch.ID)
			commitVoucherBatch(t, fixture, admin, batch.ID)
			var requestID string
			var count int
			if err := fixture.db.QueryRow(`SELECT MIN(id),COUNT(*) FROM loan_requests WHERE batch_id=$1`, batch.ID).Scan(&requestID, &count); err != nil || count != 1 {
				t.Fatalf("idempotent Voucher commit: count=%d err=%v", count, err)
			}
			if _, err := fixture.db.Exec(`UPDATE loan_requests SET requested_admin_fee=requested_admin_fee+1 WHERE id=$1`, requestID); err == nil {
				t.Fatal("Voucher intake fee must be immutable")
			}
			if _, err := fixture.db.Exec(`UPDATE loan_requests SET batch_id=NULL WHERE id=$1`, requestID); err == nil {
				t.Fatal("Voucher must retain bulk provenance")
			}
			// Configure Paylater mappings only; Voucher must resolve these accounts.
			for _, code := range []string{"11101", "40101", "BANK"} {
				if _, err := fixture.db.Exec(`INSERT INTO coa_accounts(id,code,name,account_type,normal_balance,active,is_group) VALUES($1,$1,$1,'asset','D',TRUE,FALSE) ON CONFLICT(code) DO UPDATE SET active=TRUE,is_group=FALSE`, code); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.db.Exec(`UPDATE accounting_mappings SET coa_code='BANK' WHERE component='cash_bank'`); err != nil {
				t.Fatal(err)
			}
			for _, mapping := range []struct{ transaction, component, loanType, code string }{
				{"loan", "cash_bank", "", "BANK"},
				{"loan", "loan_receivable", "goods_purchase_paylater", "11101"},
				{"loan", "admin_fee_income", "goods_purchase_paylater", "40101"},
				{"repayment", "cash_bank", "", "BANK"},
				{"repayment", "loan_receivable", "goods_purchase_paylater", "11101"},
			} {
				id := "voucher-test-" + mapping.transaction + "-" + mapping.component
				if _, err := fixture.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,loan_type,coa_code,active) VALUES($1,$1,$2,$3,$4,$5,TRUE)`, id, mapping.transaction, mapping.component, mapping.loanType, mapping.code); err != nil {
					t.Fatal(err)
				}
			}
			loan := fixture.approveLoanRequest(t, manager, requestID, 120000, 1)
			if loan.AdminFeePolicy != "voucher_nominal_one_time_v1" || loan.TotalAdminFee != fee || loan.TotalObligation != 120000+fee || loan.MonthlyInstallment != 120000+fee {
				t.Fatalf("fee must remain nominal after principal changes: %+v", loan)
			}
			var loanStatus, receivable string
			if err := fixture.db.QueryRow(`SELECT e.status,l.coa_code FROM financial_journal_entries e JOIN financial_journal_lines l ON l.journal_id=e.id WHERE e.transaction_type='loan' AND e.loan_type='voucher' AND l.component='loan_receivable'`).Scan(&loanStatus, &receivable); err != nil || loanStatus != "posted" || receivable != "11101" {
				t.Fatalf("Voucher disbursement must use Primer: %s %s %v", loanStatus, receivable, err)
			}
			if fee > 0 {
				var income string
				if err := fixture.db.QueryRow(`SELECT l.coa_code FROM financial_journal_entries e JOIN financial_journal_lines l ON l.journal_id=e.id WHERE e.transaction_type='loan' AND e.loan_type='voucher' AND l.component='admin_fee_income' AND l.amount=$1`, fee).Scan(&income); err != nil || income != "40101" {
					t.Fatalf("Voucher fee must use Primer income: %s %v", income, err)
				}
			}
			// Tagihan exposes the entire one-month obligation in its Voucher column.
			month := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).AddDate(0, 1, 0).Format("2006-01")
			export := adminLoanJSONRequest(fixture, http.MethodGet, "/api/admin/tagihan/export.xlsx?month="+month, manager, "")
			if export.Code != http.StatusOK {
				t.Fatalf("Voucher Tagihan export: %d %s", export.Code, export.Body.String())
			}
			workbook, err := excelize.OpenReader(bytes.NewReader(export.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := workbook.GetRows(workbook.GetSheetName(0), excelize.Options{RawCellValue: true})
			workbook.Close()
			found := false
			for _, row := range rows[1:] {
				if row[0] == member.ID {
					found = len(row) > 11 && row[11] == fmt.Sprint(loan.TotalObligation)
				}
			}
			if err != nil || !found {
				t.Fatalf("Voucher Tagihan lost obligation: %v %#v", err, rows)
			}
			// A repayment resolves its own journal immediately with Primer mapping.
			date := time.Now().Format("2006-01-02")
			response := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loans/"+loan.ID+"/repayments", manager, fmt.Sprintf(`{"amount":%d,"record_date":"%s","reference_no":"V-REPAY"}`, loan.TotalObligation, date))
			if response.Code != http.StatusCreated {
				t.Fatalf("Voucher repayment: %d %s", response.Code, response.Body.String())
			}
			var status, coa string
			if err := fixture.db.QueryRow(`SELECT e.status,l.coa_code FROM financial_journal_entries e JOIN financial_journal_lines l ON l.journal_id=e.id WHERE e.transaction_type='repayment' AND e.loan_type='voucher' AND l.component='loan_receivable'`).Scan(&status, &coa); err != nil || status != "posted" || coa != "11101" {
				t.Fatalf("Voucher repayment must use Primer: status=%q coa=%q err=%v", status, coa, err)
			}
		})
	}
}

func TestVoucherWorkbookFeeValidation(t *testing.T) {
	fixture := newTestFixture(t)
	manager := fixture.login(t, "admin@coop.test", "password")
	super := establishSuperAdmin(t, fixture)
	_, admin := provisionAdminOfficer(t, fixture, super, manager)
	rows := [][]any{
		{"V-MISSING", "Voucher", 100000, 1, "Voucher", ""},
		{"V-NEGATIVE", "Voucher", 100000, 1, "Voucher", -1},
		{"V-FRACTION", "Voucher", 100000, 1, "Voucher", 1.5},
		{"V-OVERFLOW", "Voucher", 100000, 1, "Voucher", "9223372036854775807"},
		{"V-REGULAR", "Reguler", 100000, 1, "Regular", 1000},
		{"V-TENOR", "Voucher", 100000, 2, "Voucher", 0},
	}
	preview := adminLoanMultipartRequest(fixture, admin, voucherWorkbook(t, fixture, admin, rows))
	var batch struct {
		ErrorCount int `json:"error_count"`
		Rows       []struct{ Errors []string }
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &batch); err != nil || preview.Code != http.StatusOK || batch.ErrorCount != len(rows) {
		t.Fatalf("invalid Voucher rows must fail preview: %v %d %s", err, preview.Code, preview.Body.String())
	}
	for index, row := range batch.Rows[:5] {
		if !strings.Contains(strings.Join(row.Errors, ","), "admin_issue_voucher_fee") {
			t.Fatalf("row %d missing fee error: %+v", index, row)
		}
	}
}
