package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func adminLoanJSONRequest(fixture testFixture, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	return rec
}

func buildAdminLoanWorkbook(t *testing.T, rows [][]any) []byte {
	t.Helper()
	workbook := excelize.NewFile()
	workbook.SetSheetName(workbook.GetSheetName(0), "Pengajuan Pinjaman")
	if _, err := workbook.NewSheet("Petunjuk"); err != nil {
		t.Fatal(err)
	}
	for cell, value := range map[string]string{"A1": "Versi template", "B1": "1"} {
		if err := workbook.SetCellValue("Petunjuk", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	for column, value := range []string{"NPP Koperasi", "Jenis Pinjaman", "Jumlah Pengajuan", "Tenor (Bulan)", "Tujuan"} {
		cell, _ := excelize.CoordinatesToCellName(column+1, 4)
		if err := workbook.SetCellValue("Pengajuan Pinjaman", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	for rowIndex, row := range rows {
		for column, value := range row {
			cell, _ := excelize.CoordinatesToCellName(column+1, rowIndex+5)
			if err := workbook.SetCellValue("Pengajuan Pinjaman", cell, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	var output bytes.Buffer
	if err := workbook.Write(&output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func adminLoanMultipartRequest(fixture testFixture, token string, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "pengajuan-pinjaman.xlsx")
	if err != nil {
		panic(err)
	}
	if _, err := part.Write(data); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/preview", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	return rec
}

func TestAdminLoanRequestCreationIsScopedAndIdempotent(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	member := fixture.createMember(t, managerToken, `{"member_no":"ADMIN-CREATE-001","full_name":"Admin Created Borrower","join_date":"2026-01-01","status":"active","bank_name":"Bank Test","bank_account":"123456","email":"admin-created-borrower@coop.test","password":"member-password"}`)
	fixture.recordSaving(t, managerToken, member.ID, "deposit", 200000, "ADMIN-CREATE-SAVING", "Loan capacity")
	body := `{"member_no":"ADMIN-CREATE-001","loan_type":"regular","requested_amount":100000,"duration_months":6,"purpose":"Kebutuhan usaha","acknowledge_warnings":true,"idempotency_key":"admin-create-key-001"}`
	response := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", adminToken, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin loan creation status=%d body=%s", response.Code, response.Body.String())
	}
	response = adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", adminToken, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("idempotent admin loan creation status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE member_id=$1 AND creation_source='admin'`, member.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one idempotent admin request, got %d", count)
	}
	var source, creator string
	if err := fixture.db.QueryRow(`SELECT creation_source,created_by FROM loan_requests WHERE member_id=$1`, member.ID).Scan(&source, &creator); err != nil {
		t.Fatal(err)
	}
	if source != "admin" || creator == "" {
		t.Fatalf("missing admin provenance: source=%q creator=%q", source, creator)
	}
	if response := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", managerToken, body); response.Code != http.StatusForbidden {
		t.Fatalf("manager must not create on behalf: %d %s", response.Code, response.Body.String())
	}
}

func TestAdminLoanBulkPreviewCommitIsAtomicAndIdempotent(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	first := fixture.createMember(t, managerToken, `{"member_no":"BULK-0001","full_name":"Bulk One","join_date":"2026-01-01","status":"active","bank_name":"Bank Test","bank_account":"111"}`)
	second := fixture.createMember(t, managerToken, `{"member_no":"BULK-0002","full_name":"Bulk Two","join_date":"2026-01-01","status":"active","bank_name":"Bank Test","bank_account":"222"}`)
	fixture.recordSaving(t, managerToken, first.ID, "deposit", 200000, "BULK-SAVING-1", "Loan capacity")
	fixture.recordSaving(t, managerToken, second.ID, "deposit", 200000, "BULK-SAVING-2", "Loan capacity")
	preview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, [][]any{{"BULK-0001", "Reguler", 100000, 6, "Kebutuhan usaha"}, {"BULK-0002", "Barang Sekunder", 150000, 12, "Pembelian barang"}}))
	if preview.Code != http.StatusOK {
		t.Fatalf("bulk preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var payload struct {
		ID         string `json:"id"`
		ErrorCount int    `json:"error_count"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.ErrorCount != 0 {
		t.Fatalf("unexpected bulk preview: %+v", payload)
	}
	commitBody := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+payload.ID+"/commit", strings.NewReader("acknowledge_warnings=on"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		return rec
	}
	if response := commitBody(); response.Code != http.StatusNoContent {
		t.Fatalf("bulk commit status=%d body=%s", response.Code, response.Body.String())
	}
	var created int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1 AND creation_source='admin'`, payload.ID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("expected two committed requests, got %d", created)
	}
	var batchNotifications int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN notification_events e ON e.id=n.event_id WHERE e.event_type='approval_batch_ready' AND e.request_type='loan_batch' AND e.request_id=$1`, payload.ID).Scan(&batchNotifications); err != nil {
		t.Fatal(err)
	}
	if batchNotifications == 0 {
		t.Fatal("expected one batch summary notification for active Manager/Admin approvers")
	}
	if response := commitBody(); response.Code != http.StatusNoContent {
		t.Fatalf("idempotent bulk commit status=%d body=%s", response.Code, response.Body.String())
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1`, payload.ID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("idempotent commit created duplicate requests: %d", created)
	}
	failedPreview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, [][]any{{"BULK-0001", "Reguler", 100000, 6, "Duplicate pending"}, {"MISSING-0001", "Reguler", 100000, 6, "Unknown member"}}))
	if failedPreview.Code != http.StatusOK {
		t.Fatalf("invalid bulk preview status=%d body=%s", failedPreview.Code, failedPreview.Body.String())
	}
	var failed struct {
		ID         string `json:"id"`
		ErrorCount int    `json:"error_count"`
	}
	if err := json.NewDecoder(failedPreview.Body).Decode(&failed); err != nil {
		t.Fatal(err)
	}
	if failed.ID == "" || failed.ErrorCount != 2 {
		t.Fatalf("expected atomic validation errors, got %+v", failed)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1`, failed.ID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("failed preview created requests: %d", created)
	}
}

func TestAdminLoanBulkCommitAllowsOutstandingBalanceMissingBankAndLimitOverride(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)

	memberNumbers := make([]string, 0, 8)
	var firstMemberID string
	for index := 1; index <= 8; index++ {
		memberNo := fmt.Sprintf("BULK-EDGE-%04d", index)
		email := fmt.Sprintf("bulk-edge-%02d@coop.test", index)
		member := fixture.createMember(t, managerToken, fmt.Sprintf(`{"member_no":%q,"full_name":%q,"join_date":"2026-01-01","status":"active","bank_name":"","bank_account":"","email":%q,"password":"member-password"}`, memberNo, "Bulk Edge Borrower", email))
		memberNumbers = append(memberNumbers, member.MemberNo)
		if index == 1 {
			firstMemberID = member.ID
		}
	}
	if _, err := fixture.db.Exec(`UPDATE members SET bank_name='Test Bank',bank_account='0000000001' WHERE id=$1`, firstMemberID); err != nil {
		t.Fatalf("set bank details for initial member loan: %v", err)
	}

	fixture.recordSaving(t, managerToken, firstMemberID, "deposit", 200000, "BULK-EDGE-SAVING", "Existing loan capacity")
	memberToken := fixture.login(t, "bulk-edge-01@coop.test", "member-password")
	existingRequestID := fixture.createLoanRequest(t, memberToken, 100000, 6)
	openLoan := fixture.approveLoanRequest(t, managerToken, existingRequestID, 100000, 6)
	if openLoan.RemainingBalance <= 0 {
		t.Fatalf("expected disbursed existing loan to retain a balance, got %d", openLoan.RemainingBalance)
	}
	if _, err := fixture.db.Exec(`UPDATE members SET bank_name='',bank_account='' WHERE id=$1`, firstMemberID); err != nil {
		t.Fatalf("clear bank details before Excel batch: %v", err)
	}
	var memberLoanLimit int64
	if err := fixture.db.QueryRow(`SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=$1`, firstMemberID).Scan(&memberLoanLimit); err != nil {
		t.Fatalf("read first borrower's loan limit: %v", err)
	}
	if memberLoanLimit >= 70000000 {
		t.Fatalf("fixture loan limit %d must be below the requested admin amount", memberLoanLimit)
	}

	amounts := []int64{70000000, 50000000, 55000000, 9000000, 12000000, 18000000, 25000000, 30000000}
	rows := make([][]any, 0, len(memberNumbers))
	for index, memberNo := range memberNumbers {
		rows = append(rows, []any{memberNo, "Reguler", amounts[index], 24, "Pengajuan batch admin"})
	}
	preview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, rows))
	if preview.Code != http.StatusOK {
		t.Fatalf("large batch preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var payload struct {
		ID           string `json:"id"`
		ErrorCount   int    `json:"error_count"`
		WarningCount int    `json:"warning_count"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.ErrorCount != 0 || payload.WarningCount != 0 {
		t.Fatalf("expected all eight high-amount rows to preview valid, got %+v", payload)
	}

	commit := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+payload.ID+"/commit", strings.NewReader(""))
	commit.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	commit.Header.Set("Authorization", "Bearer "+adminToken)
	commit.Header.Set("HX-Request", "true")
	committed := httptest.NewRecorder()
	fixture.server.ServeHTTP(committed, commit)
	if committed.Code != http.StatusNoContent {
		t.Fatalf("large batch commit status=%d body=%s", committed.Code, committed.Body.String())
	}
	var created int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1 AND creation_source='admin'`, payload.ID).Scan(&created); err != nil {
		t.Fatalf("count committed batch requests: %v", err)
	}
	if created != 8 {
		t.Fatalf("expected all eight requests to commit atomically, got %d", created)
	}
	var storedBankName, storedBankAccount string
	if err := fixture.db.QueryRow(`SELECT bank_name,bank_account FROM members WHERE id=$1`, firstMemberID).Scan(&storedBankName, &storedBankAccount); err != nil {
		t.Fatalf("verify missing bank details remain absent: %v", err)
	}
	if strings.TrimSpace(storedBankName) != "" || strings.TrimSpace(storedBankAccount) != "" {
		t.Fatalf("expected first borrower to have no bank details, got %q/%q", storedBankName, storedBankAccount)
	}
}

func TestAdminLoanBulkCommitRevalidatesRowsAfterPreviewAtomically(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	first := fixture.createMember(t, managerToken, `{"member_no":"BULK-STALE-0001","full_name":"Stale Preview One","join_date":"2026-01-01","status":"active"}`)
	second := fixture.createMember(t, managerToken, `{"member_no":"BULK-STALE-0002","full_name":"Stale Preview Two","join_date":"2026-01-01","status":"active"}`)

	preview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, [][]any{{first.MemberNo, "Reguler", 100000, 6, "Still available"}, {second.MemberNo, "Reguler", 100000, 6, "Will become pending"}}))
	if preview.Code != http.StatusOK {
		t.Fatalf("stale batch preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var payload struct {
		ID         string `json:"id"`
		ErrorCount int    `json:"error_count"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.ErrorCount != 0 {
		t.Fatalf("expected preview to be valid before member state changes, got %+v", payload)
	}

	newPending := `{"member_no":"` + second.MemberNo + `","loan_type":"regular","requested_amount":100000,"duration_months":6,"purpose":"Created after batch preview","acknowledge_warnings":true,"idempotency_key":"bulk-stale-after-preview"}`
	created := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", adminToken, newPending)
	if created.Code != http.StatusCreated {
		t.Fatalf("create request after preview status=%d body=%s", created.Code, created.Body.String())
	}

	commit := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+payload.ID+"/commit", strings.NewReader("acknowledge_warnings=on"))
	commit.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	commit.Header.Set("Authorization", "Bearer "+adminToken)
	commit.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()
	fixture.server.ServeHTTP(response, commit)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "BATCH_REVALIDATION_FAILED") {
		t.Fatalf("expected commit-time revalidation conflict, got %d %s", response.Code, response.Body.String())
	}
	var batchRequests int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1`, payload.ID).Scan(&batchRequests); err != nil {
		t.Fatalf("count batch requests after revalidation failure: %v", err)
	}
	if batchRequests != 0 {
		t.Fatalf("revalidation failure partially committed %d requests", batchRequests)
	}
	var batchStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM loan_request_batches WHERE id=$1`, payload.ID).Scan(&batchStatus); err != nil {
		t.Fatalf("read failed batch status: %v", err)
	}
	if batchStatus != "validation_failed" {
		t.Fatalf("expected validation_failed status, got %q", batchStatus)
	}
}

func TestAdminLoanBulkPreviewRejectsDuplicateMembersInWorkbook(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	member := fixture.createMember(t, managerToken, `{"member_no":"BULK-DUPLICATE-0001","full_name":"Duplicate Borrower","join_date":"2026-01-01","status":"active"}`)

	preview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, [][]any{{member.MemberNo, "Reguler", 100000, 6, "First row"}, {member.MemberNo, "Reguler", 200000, 12, "Duplicate row"}}))
	if preview.Code != http.StatusOK {
		t.Fatalf("duplicate batch preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var payload struct {
		ID         string `json:"id"`
		ErrorCount int    `json:"error_count"`
		Rows       []struct {
			Status string   `json:"status"`
			Errors []string `json:"errors"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.ErrorCount != 1 || len(payload.Rows) != 2 {
		t.Fatalf("expected one duplicate-row error, got %+v", payload)
	}
	if payload.Rows[0].Status != "valid" || payload.Rows[1].Status != "error" || len(payload.Rows[1].Errors) == 0 {
		t.Fatalf("unexpected duplicate row statuses: %+v", payload.Rows)
	}
	var created int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1`, payload.ID).Scan(&created); err != nil {
		t.Fatalf("count requests after duplicate preview: %v", err)
	}
	if created != 0 {
		t.Fatalf("duplicate preview unexpectedly created %d requests", created)
	}
}

func TestAdminLoanBulkCommitRequiresAcknowledgementForWarnings(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	member := fixture.createMember(t, managerToken, `{"member_no":"BULK-WARNING-0001","full_name":"Warning Borrower","join_date":"2026-01-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET active=FALSE WHERE member_id=$1 AND historical_identity=FALSE`, member.ID); err != nil {
		t.Fatalf("remove member login to produce preview warning: %v", err)
	}

	preview := adminLoanMultipartRequest(fixture, adminToken, buildAdminLoanWorkbook(t, [][]any{{member.MemberNo, "Reguler", 100000, 6, "Warning acknowledgement"}}))
	if preview.Code != http.StatusOK {
		t.Fatalf("warning batch preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var payload struct {
		ID           string `json:"id"`
		ErrorCount   int    `json:"error_count"`
		WarningCount int    `json:"warning_count"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == "" || payload.ErrorCount != 0 || payload.WarningCount == 0 {
		t.Fatalf("expected a warning-only valid batch, got %+v", payload)
	}

	commit := func(acknowledge bool) *httptest.ResponseRecorder {
		body := ""
		if acknowledge {
			body = "acknowledge_warnings=on"
		}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+payload.ID+"/commit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		return rec
	}
	if response := commit(false); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "WARNINGS_REQUIRE_ACKNOWLEDGEMENT") {
		t.Fatalf("unacknowledged warning commit status=%d body=%s", response.Code, response.Body.String())
	}
	var created int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE batch_id=$1`, payload.ID).Scan(&created); err != nil {
		t.Fatalf("count requests before warning acknowledgement: %v", err)
	}
	if created != 0 {
		t.Fatalf("unacknowledged warning commit created %d requests", created)
	}
	if response := commit(true); response.Code != http.StatusNoContent {
		t.Fatalf("acknowledged warning commit status=%d body=%s", response.Code, response.Body.String())
	}
	var acknowledged bool
	if err := fixture.db.QueryRow(`SELECT warnings_acknowledged FROM loan_request_batches WHERE id=$1`, payload.ID).Scan(&acknowledged); err != nil {
		t.Fatalf("read warning acknowledgement state: %v", err)
	}
	if !acknowledged {
		t.Fatal("expected committed batch to record warning acknowledgement")
	}
}
