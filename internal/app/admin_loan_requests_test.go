package app_test

import (
	"bytes"
	"encoding/json"
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
