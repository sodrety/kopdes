package app_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCashTransactionCategoryTemplateImportsMultiLevelTree(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	workbook := excelize.NewFile()
	defer workbook.Close()
	rows := [][]interface{}{
		{"Category Key", "Account Code", "Category Name", "Direction", "Parent Key", "Normal Balance", "Is Group", "Active"},
		{"test-root", "90000", "Test root", "cash_in", "", "D", "TRUE", "TRUE"},
		{"test-child", "90001", "Test child", "cash_in", "test-root", "D", "TRUE", "TRUE"},
		{"test-leaf", "90002", "Test leaf", "cash_in", "test-child", "D", "FALSE", "TRUE"},
	}
	if err := workbook.SetSheetRow("Sheet1", "A1", &rows[0]); err != nil {
		t.Fatalf("write category headers: %v", err)
	}
	for index := 1; index < len(rows); index++ {
		cell, err := excelize.CoordinatesToCellName(1, index+1)
		if err != nil {
			t.Fatalf("category row cell: %v", err)
		}
		if err := workbook.SetSheetRow("Sheet1", cell, &rows[index]); err != nil {
			t.Fatalf("write category row: %v", err)
		}
	}
	var file bytes.Buffer
	if err := workbook.Write(&file); err != nil {
		t.Fatalf("encode category workbook: %v", err)
	}
	requestBody := &bytes.Buffer{}
	form := multipart.NewWriter(requestBody)
	part, err := form.CreateFormFile("file", "categories.xlsx")
	if err != nil {
		t.Fatalf("create upload part: %v", err)
	}
	if _, err := part.Write(file.Bytes()); err != nil {
		t.Fatalf("write upload part: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close upload form: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/transaction-categories/import", requestBody)
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"imported":3`) {
		t.Fatalf("expected category import success, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var rootID, childID, leafID string
	if err := fixture.db.QueryRow(`SELECT id FROM cash_transaction_categories WHERE category_key='test-root'`).Scan(&rootID); err != nil {
		t.Fatalf("read root category: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT id FROM cash_transaction_categories WHERE category_key='test-child'`).Scan(&childID); err != nil {
		t.Fatalf("read child category: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT id FROM cash_transaction_categories WHERE category_key='test-leaf'`).Scan(&leafID); err != nil {
		t.Fatalf("read leaf category: %v", err)
	}
	var parentID string
	if err := fixture.db.QueryRow(`SELECT parent_id FROM cash_transaction_categories WHERE id=$1`, childID).Scan(&parentID); err != nil {
		t.Fatalf("read child parent: %v", err)
	}
	if parentID != rootID {
		t.Fatalf("expected child parent %q, got %q", rootID, parentID)
	}
	if err := fixture.db.QueryRow(`SELECT parent_id FROM cash_transaction_categories WHERE id=$1`, leafID).Scan(&parentID); err != nil {
		t.Fatalf("read leaf parent: %v", err)
	}
	if parentID != childID {
		t.Fatalf("expected leaf parent %q, got %q", childID, parentID)
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/admin/transactions/categories", nil)
	pageRequest.Header.Set("Authorization", "Bearer "+adminToken)
	pageRequest.Header.Set("Accept", "text/html")
	pageRecorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(pageRecorder, pageRequest)
	if pageRecorder.Code != http.StatusOK {
		t.Fatalf("expected category page status 200, got %d: %s", pageRecorder.Code, pageRecorder.Body.String())
	}
	for _, name := range []string{"Test root", "Test child", "Test leaf", "Download Excel template", "Import Excel template"} {
		if !strings.Contains(pageRecorder.Body.String(), name) {
			t.Fatalf("expected category page to contain %q, got %s", name, pageRecorder.Body.String())
		}
	}

	transactionRequest := httptest.NewRequest(http.MethodPost, "/api/admin/transactions", strings.NewReader(`{"direction":"cash_in","category_id":"`+rootID+`","description":"Group should fail","amount":1000,"transaction_date":"2026-06-16"}`))
	transactionRequest.Header.Set("Authorization", "Bearer "+adminToken)
	transactionRequest.Header.Set("Content-Type", "application/json")
	transactionRecorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(transactionRecorder, transactionRequest)
	if transactionRecorder.Code != http.StatusBadRequest || !strings.Contains(transactionRecorder.Body.String(), "Group categories cannot be used directly") {
		t.Fatalf("expected group category rejection, got %d: %s", transactionRecorder.Code, transactionRecorder.Body.String())
	}
}

func TestCashTransactionCategoryTemplateDownloadContainsCOAReview(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	request := httptest.NewRequest(http.MethodGet, "/api/admin/transaction-categories/template.xlsx", nil)
	request.Header.Set("Authorization", "Bearer "+adminToken)
	recorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected template download status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet") {
		t.Fatalf("expected XLSX content type, got %q", contentType)
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("open downloaded category template: %v", err)
	}
	defer workbook.Close()
	if sheets := workbook.GetSheetList(); len(sheets) != 2 || sheets[0] != "Petunjuk" || sheets[1] != "Kategori Kas" {
		t.Fatalf("unexpected template sheets: %v", sheets)
	}
	rows, err := workbook.GetRows("Kategori Kas")
	if err != nil {
		t.Fatalf("read downloaded category rows: %v", err)
	}
	if len(rows) != 142 || rows[2][0] != "Kunci Kategori" || rows[2][3] != "Arah Akun" || rows[2][5] != "Arah Kas" {
		t.Fatalf("expected 139 COA rows and category headers, got %d rows", len(rows))
	}
	if !strings.Contains(strings.Join(rows[3], "|"), "KAS BESAR") {
		t.Fatalf("expected COA row in downloaded template, got %v", rows[3])
	}
	uploadBody := &bytes.Buffer{}
	uploadForm := multipart.NewWriter(uploadBody)
	uploadPart, err := uploadForm.CreateFormFile("file", "cash-transaction-category-template.xlsx")
	if err != nil {
		t.Fatalf("create COA upload part: %v", err)
	}
	if _, err := uploadPart.Write(recorder.Body.Bytes()); err != nil {
		t.Fatalf("write COA upload part: %v", err)
	}
	if err := uploadForm.Close(); err != nil {
		t.Fatalf("close COA upload form: %v", err)
	}
	importRequest := httptest.NewRequest(http.MethodPost, "/api/admin/transaction-categories/import", uploadBody)
	importRequest.Header.Set("Authorization", "Bearer "+adminToken)
	importRequest.Header.Set("Content-Type", uploadForm.FormDataContentType())
	importRecorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(importRecorder, importRequest)
	if importRecorder.Code != http.StatusOK || !strings.Contains(importRecorder.Body.String(), `"imported":139`) {
		t.Fatalf("expected reviewed COA import success, got %d: %s", importRecorder.Code, importRecorder.Body.String())
	}
}
