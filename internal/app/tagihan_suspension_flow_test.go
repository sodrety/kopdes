package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestTagihanSuspensionExcelPreservesBalanceAndPosts40201(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	m := f.createMember(t, token, `{"member_no":"SUSP-001","full_name":"Suspension Member","join_date":"2026-01-01","status":"active","email":"susp@coop.test","password":"member-password"}`)
	if m.ID == "" {
		t.Fatal("missing member")
	}
	if _, err := f.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,loan_type,coa_code,active) VALUES('susp-test-bank','susp-test-bank','repayment','cash_bank','regular','BANK',TRUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO coa_accounts(id,code,name,account_type,normal_balance,is_group,active) VALUES('susp-income','40201','Loan service income','revenue','C',FALSE,TRUE) ON CONFLICT(code) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	mt := f.login(t, "susp@coop.test", "member-password")
	reqID := f.createLoanRequest(t, mt, 1_000_000, 5)
	start := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60))
	loan := f.approveLoanRequestWithStartDate(t, token, reqID, 1_000_000, 5, start.Format("2006-01-02"))
	var first, final string
	var original int64
	if err := f.db.QueryRow(`SELECT next_due_date,final_due_date,remaining_balance FROM loans WHERE id=$1`, loan.ID).Scan(&first, &final, &original); err != nil {
		t.Fatal(err)
	}
	month := first[:7]
	date := month + "-28"
	r := httptest.NewRequest("GET", "/api/admin/tagihan/export.xlsx?month="+month, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	w, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	main, err := w.GetRows("Tagihan")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(main); i++ {
		if err = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", i+1), "unpaid"); err != nil {
			t.Fatal(err)
		}
	}
	susp, err := w.GetRows("Rincian Pinjaman")
	if err != nil || len(susp) != 2 {
		t.Fatalf("suspension rows: %v %v", susp, err)
	}
	fee, err := strconv.ParseInt(susp[1][9], 10, 64)
	if err != nil || fee <= 0 {
		t.Fatal(fee, err)
	}
	_ = w.SetCellValue("Rincian Pinjaman", "K2", "suspend")
	// An omitted reason must work through preview, confirmation and retry.
	_ = w.SetCellValue("Rincian Pinjaman", "L2", "")
	buf, err := w.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	importFile := func(data []byte) *httptest.ResponseRecorder {
		t.Helper()
		preview := postReviewedTagihan(f, token, month, date, data, "", false)
		if preview.Code != 200 {
			return preview
		}
		var plan struct {
			Token string `json:"preview_token"`
		}
		if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil {
			t.Fatal(err)
		}
		return postReviewedTagihan(f, token, month, date, data, plan.Token, false)
	}

	// Inactive revenue account must roll back both schedule and fee, atomically.
	_, _ = f.db.Exec(`UPDATE coa_accounts SET active=FALSE WHERE code='40201'`)
	if out := importFile(buf.Bytes()); out.Code == 200 {
		t.Fatal("inactive income account accepted")
	}
	var unchanged string
	var written int
	_ = f.db.QueryRow(`SELECT final_due_date FROM loans WHERE id=$1`, loan.ID).Scan(&unchanged)
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM loan_suspensions`).Scan(&written)
	if unchanged != final || written != 0 {
		t.Fatal("partial suspension persisted")
	}
	_, _ = f.db.Exec(`UPDATE coa_accounts SET active=TRUE WHERE code='40201'`)
	for n := 0; n < 2; n++ {
		out := importFile(buf.Bytes())
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		var result struct {
			Created int `json:"suspensions_created"`
			Skipped int `json:"suspensions_skipped"`
		}
		_ = json.Unmarshal(out.Body.Bytes(), &result)
		if n == 0 && result.Created != 1 || n == 1 && result.Skipped != 1 {
			t.Fatal(result)
		}
	}
	var balance, paid, credit, debit int64
	var count, base int
	var after string
	_ = f.db.QueryRow(`SELECT remaining_balance,duration_months,final_due_date FROM loans WHERE id=$1`, loan.ID).Scan(&balance, &base, &after)
	_ = f.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount),0) FROM loan_repayments WHERE loan_id=$1`, loan.ID).Scan(&count, &paid)
	if balance != original || base != 5 || after == final || count != 0 || paid != 0 {
		t.Fatalf("balance %d original %d tenor %d final %s -> %s repayments %d", balance, original, base, final, after, count)
	}
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(j.amount),0) FROM financial_journal_lines j JOIN financial_journal_entries e ON e.id=j.journal_id WHERE e.transaction_type='loan_suspension_fee' AND e.status='posted' AND j.side='credit' AND j.coa_code='40201'`).Scan(&credit)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(j.amount),0) FROM financial_journal_lines j JOIN financial_journal_entries e ON e.id=j.journal_id WHERE e.transaction_type='loan_suspension_fee' AND e.status='posted' AND j.side='debit'`).Scan(&debit)
	if credit != fee || debit != fee {
		t.Fatalf("fee %d credit %d debit %d", fee, credit, debit)
	}
	var due int64
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(scheduled_amount-paid_amount),0) FROM loan_installments WHERE loan_id=$1 AND due_date<=$2`, loan.ID, month+"-31").Scan(&due)
	if due != 0 {
		t.Fatal("suspended month still due", due)
	}
	// Changing a fee in the returned file is rejected atomically, even on retry.
	_ = w.SetCellValue("Rincian Pinjaman", "J2", fee+1)
	tampered, _ := w.WriteToBuffer()
	if out := importFile(tampered.Bytes()); out.Code == 200 {
		t.Fatal("tampered fee accepted")
	}
	// Effective tenor is exposed without repricing the immutable fee snapshot.
	r = httptest.NewRequest(http.MethodGet, "/api/admin/loans", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	f.server.ServeHTTP(rec, r)
	var payload struct {
		Loans []struct {
			ID       string `json:"id"`
			Duration int    `json:"duration_months"`
		}
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range payload.Loans {
		if l.ID == loan.ID {
			found = true
			if l.Duration != 6 {
				t.Fatal("effective tenor", l.Duration)
			}
		}
	}
	if !found {
		t.Fatal("missing loan")
	}
	// The bank movement must also appear in the transaction report.
	r = httptest.NewRequest("GET", "/admin/transactions?type=loan_suspension_fee", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	f.server.ServeHTTP(rec, r)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("Suspension Member")) {
		t.Fatal("missing suspension cash report", rec.Code)
	}
}

func TestTagihanSuspensionAppAuthorizationAndConsecutiveMonths(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	m := f.createMember(t, token, `{"member_no":"SUSP-APP","full_name":"Suspension App","join_date":"2026-01-01","status":"active","email":"susp-app@coop.test","password":"member-password"}`)
	mt := f.login(t, "susp-app@coop.test", "member-password")
	reqID := f.createLoanRequest(t, mt, 1_000_000, 5)
	loan := f.approveLoanRequestWithStartDate(t, token, reqID, 1_000_000, 5, time.Now().Format("2006-01-02"))
	_, err := f.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,loan_type,coa_code,active) VALUES('susp-app-bank','susp-app-bank','repayment','cash_bank','regular','BANK',TRUE)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`INSERT INTO coa_accounts(id,code,name,account_type,normal_balance,is_group,active) VALUES('susp-app-income','40201','Loan service income','revenue','C',FALSE,TRUE) ON CONFLICT(code) DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	var first string
	var balance int64
	_ = f.db.QueryRow(`SELECT next_due_date,remaining_balance FROM loans WHERE id=$1`, loan.ID).Scan(&first, &balance)
	post := func(auth, month, member string, fee int64) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"loan_id": loan.ID, "member_id": member, "statement_month": month, "fee_amount": fee, "record_date": month + "-28"})
		r := httptest.NewRequest("POST", "/api/admin/tagihan/suspensions", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		f.server.ServeHTTP(out, r)
		return out
	}
	if out := post(mt, first[:7], m.ID, 10000); out.Code != 403 {
		t.Fatal("member may suspend", out.Code)
	}
	if out := post(token, first[:7], "another-member", 10000); out.Code == 200 {
		t.Fatal("wrong owner accepted")
	}
	page := httptest.NewRequest("GET", "/admin/tagihan?month="+first[:7], nil)
	page.Header.Set("Authorization", "Bearer "+token)
	pageRec := httptest.NewRecorder()
	f.server.ServeHTTP(pageRec, page)
	if pageRec.Code != 200 || !bytes.Contains(pageRec.Body.Bytes(), []byte("/api/admin/tagihan/suspensions")) {
		t.Fatal("missing Tagihan action", pageRec.Code)
	}
	for n := 0; n < 2; n++ {
		var due string
		_ = f.db.QueryRow(`SELECT next_due_date FROM loans WHERE id=$1`, loan.ID).Scan(&due)
		if out := post(token, due[:7], m.ID, 10000); out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	var count int
	var fees, after int64
	_ = f.db.QueryRow(`SELECT COUNT(*),SUM(fee_amount) FROM loan_suspensions WHERE loan_id=$1`, loan.ID).Scan(&count, &fees)
	_ = f.db.QueryRow(`SELECT remaining_balance FROM loans WHERE id=$1`, loan.ID).Scan(&after)
	if count != 2 || fees != 20000 || after != balance {
		t.Fatal("consecutive suspension repriced or reduced loan", count, fees, after)
	}
	// A partial installment cannot be silently reclassified as a suspension.
	f.recordRepayment(t, token, loan.ID, 1)
	var due string
	_ = f.db.QueryRow(`SELECT next_due_date FROM loans WHERE id=$1`, loan.ID).Scan(&due)
	if out := post(token, due[:7], m.ID, 10000); out.Code == 200 {
		t.Fatal("partially paid installment suspended")
	}
}
