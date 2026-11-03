package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestGeneralLedgerExportsFilteredXLSX(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	if _, err := fixture.db.Exec(`INSERT INTO coa_accounts (id,code,name,account_type,normal_balance,is_group,active) VALUES
		('ledger-export-bank','LEDGER-BANK','Ledger Export Bank','asset','D',FALSE,TRUE),
		('ledger-export-savings','LEDGER-SUKARELA','Ledger Export Savings','liability','C',FALSE,TRUE)`); err != nil {
		t.Fatalf("seed ledger export accounts: %v", err)
	}
	if _, err := fixture.db.Exec(`INSERT INTO accounting_mappings (id,mapping_key,transaction_type,component,category,loan_type,coa_code,active) VALUES
		('ledger-export-bank-map','ledger-export-bank-map','savings','cash_bank','','','LEDGER-BANK',TRUE),
		('ledger-export-savings-map','ledger-export-savings-map','savings','savings_liability','sukarela','','LEDGER-SUKARELA',TRUE)`); err != nil {
		t.Fatalf("map ledger export accounts: %v", err)
	}
	member := fixture.createMember(t, adminToken, `{"member_no":"LEDGER-XLSX","full_name":"Ledger Export Member","join_date":"2026-01-01","status":"active"}`)
	fixture.recordSavingInCategoryAtDate(t, adminToken, member.ID, "sukarela", 100000, "2026-06-16", "LEDGER-EXPORT", "Ledger export fixture")

	path := "/admin/journals?view=ledger&account=LEDGER-SUKARELA&date_from=2026-06-01&date_to=2026-06-30&export=xlsx"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.AddCookie(fixture.setLanguage(t, "en", request.URL.Path))
	response := httptest.NewRecorder()
	fixture.server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("export General Ledger workbook: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), `filename="buku-besar.xlsx"`) {
		t.Fatalf("expected Buku Besar attachment, got %q", response.Header().Get("Content-Disposition"))
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("open General Ledger workbook: %v", err)
	}
	defer workbook.Close()
	sheet := workbook.GetSheetName(0)
	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("read General Ledger workbook: %v", err)
	}
	if sheet != "General ledger" || len(rows) < 6 {
		t.Fatalf("unexpected General Ledger sheet or row count: sheet=%q rows=%#v", sheet, rows)
	}
	if rows[0][0] != "Account code" || rows[0][1] != "LEDGER-SUKARELA" || rows[1][1] != "2026-06-01" || rows[1][3] != "2026-06-30" {
		t.Fatalf("workbook metadata did not preserve the selected account and date filters: %#v", rows[:3])
	}
	if strings.Join(rows[4], ",") != "Date,Reference,Description,Debit,Credit,Balance" {
		t.Fatalf("unexpected General Ledger columns: %#v", rows[4])
	}
	if rows[5][1] != "LEDGER-EXPORT" || rows[5][4] != "100000" || rows[5][5] != "100000" {
		t.Fatalf("export did not include the filtered ledger entry: %#v", rows[5])
	}
}

func TestGeneralJournalAllowsNonCashAccrualEntry(t *testing.T) {
	fixture := newTestFixture(t)
	bendaharaEmail := "journal-bendahara@coop.test"
	seedUser(t, fixture.db, "journal-bendahara-user-id", bendaharaEmail, "password", "bendahara")
	bendaharaToken := fixture.login(t, bendaharaEmail, "password")
	ketuaIToken := fixture.login(t, "ketua-i@coop.test", "password")

	if _, err := fixture.db.Exec(`INSERT INTO coa_accounts (id,code,name,account_type,subtype,normal_balance,is_group,active) VALUES
		('test-journal-receivable','TEST-JOURNAL-AR','Piutang Usaha','asset','receivable','D',FALSE,TRUE),
		('test-journal-revenue','TEST-JOURNAL-REV','Pendapatan Usaha','revenue','Laba Rugi','C',FALSE,TRUE),
		('test-journal-cogs','TEST-JOURNAL-COGS','Harga Pokok Penjualan','expense','Laba Rugi','D',FALSE,TRUE),
		('test-journal-inventory','TEST-JOURNAL-INV','Persediaan','asset','inventory','D',FALSE,TRUE)`); err != nil {
		t.Fatalf("seed non-cash accrual COA accounts: %v", err)
	}

	journalBody := `{"transaction_date":"2026-08-31","reference_no":"001/KOKA/IX/2026","description":"Pencatatan Pendapatan Periode Jan sd Agts 2026 KOKA MART","note":"Accrual sales and cost of goods sold","lines":[{"coa_code":"TEST-JOURNAL-AR","debit":298013000},{"coa_code":"TEST-JOURNAL-REV","credit":298013000},{"coa_code":"TEST-JOURNAL-COGS","debit":271006590},{"coa_code":"TEST-JOURNAL-INV","credit":271006590}]}`
	record := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(journalBody))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		fixture.server.ServeHTTP(response, req)
		return response
	}
	var draft struct {
		ID string `json:"id"`
	}
	draftResponse := record("/api/admin/journals", bendaharaToken)
	if draftResponse.Code != http.StatusCreated {
		t.Fatalf("expected accrual journal draft status 201, got %d: %s", draftResponse.Code, draftResponse.Body.String())
	}
	if err := json.Unmarshal(draftResponse.Body.Bytes(), &draft); err != nil || draft.ID == "" {
		t.Fatalf("decode accrual journal draft: %v %s", err, draftResponse.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/journals/drafts/"+draft.ID+"/approve", nil)
	approveReq.Header.Set("Authorization", "Bearer "+ketuaIToken)
	approveResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(approveResponse, approveReq)
	if approveResponse.Code != http.StatusCreated {
		t.Fatalf("expected non-cash accrual approval status 201, got %d: %s", approveResponse.Code, approveResponse.Body.String())
	}
	var approved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(approveResponse.Body.Bytes(), &approved); err != nil || approved.ID == "" {
		t.Fatalf("decode accrual journal approval: %v %s", err, approveResponse.Body.String())
	}

	var journalStatus, transactionType string
	var amount int64
	if err := fixture.db.QueryRow(`SELECT status,transaction_type,amount FROM financial_journal_entries WHERE transaction_id=$1`, approved.ID).Scan(&journalStatus, &transactionType, &amount); err != nil {
		t.Fatalf("read posted accrual journal: %v", err)
	}
	if journalStatus != "posted" || transactionType != "manual" || amount != 569019590 {
		t.Fatalf("expected posted balanced journal, got status=%q type=%q amount=%d", journalStatus, transactionType, amount)
	}
	var journalID string
	if err := fixture.db.QueryRow(`SELECT id FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='manual'`, approved.ID).Scan(&journalID); err != nil {
		t.Fatalf("read posted journal entry ID: %v", err)
	}
	printReq := httptest.NewRequest(http.MethodGet, "/admin/journals/"+journalID+"/print", nil)
	printReq.Header.Set("Authorization", "Bearer "+bendaharaToken)
	printReq.AddCookie(fixture.setLanguage(t, "id", printReq.URL.Path))
	printResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(printResponse, printReq)
	if printResponse.Code != http.StatusOK {
		t.Fatalf("expected journal receipt page status 200, got %d: %s", printResponse.Code, printResponse.Body.String())
	}
	for _, expected := range []string{"BUKTI TRANSAKSI", "001/KOKA/IX/2026", "Pencatatan Pendapatan Periode Jan sd Agts 2026 KOKA MART", "TEST-JOURNAL-AR — Piutang Usaha", "298.013.000", "Disetorkan/Diterima", "window.print()"} {
		if !strings.Contains(printResponse.Body.String(), expected) {
			t.Fatalf("expected journal receipt to include %q, got %s", expected, printResponse.Body.String())
		}
	}
	var transactionCount int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM manual_cash_transactions WHERE id=$1`, approved.ID).Scan(&transactionCount); err != nil {
		t.Fatalf("check cash transaction ledger: %v", err)
	}
	if transactionCount != 0 {
		t.Fatalf("non-cash accrual should not create a cash transaction, found %d", transactionCount)
	}
	var draftTransactionID sql.NullString
	if err := fixture.db.QueryRow(`SELECT transaction_id FROM manual_cash_transaction_drafts WHERE id=$1`, draft.ID).Scan(&draftTransactionID); err != nil {
		t.Fatalf("read approved draft link: %v", err)
	}
	if draftTransactionID.Valid {
		t.Fatalf("non-cash journal draft should not link to a cash transaction, got %q", draftTransactionID.String)
	}

	// Cash transaction approvals still require an actual cash/bank movement.
	cashDraftResponse := record("/api/admin/transactions", bendaharaToken)
	if cashDraftResponse.Code != http.StatusCreated {
		t.Fatalf("expected cash transaction draft status 201, got %d: %s", cashDraftResponse.Code, cashDraftResponse.Body.String())
	}
	var cashDraft struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(cashDraftResponse.Body.Bytes(), &cashDraft); err != nil || cashDraft.ID == "" {
		t.Fatalf("decode cash transaction draft: %v %s", err, cashDraftResponse.Body.String())
	}
	cashApproveReq := httptest.NewRequest(http.MethodPost, "/api/admin/transactions/drafts/"+cashDraft.ID+"/approve", nil)
	cashApproveReq.Header.Set("Authorization", "Bearer "+ketuaIToken)
	cashApproveResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(cashApproveResponse, cashApproveReq)
	if cashApproveResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected zero-cash movement approval to remain rejected, got %d: %s", cashApproveResponse.Code, cashApproveResponse.Body.String())
	}
}
