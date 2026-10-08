package app_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
