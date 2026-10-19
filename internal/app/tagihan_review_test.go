package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/xuri/excelize/v2"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"
)

func postReviewedTagihan(f testFixture, token, month, date string, data []byte, preview string, html bool) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("statement_month", month)
	_ = mw.WriteField("record_date", date)
	_ = mw.WriteField("preview_token", preview)
	p, _ := mw.CreateFormFile("file", "review.xlsx")
	_, _ = p.Write(data)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/admin/tagihan/import", &body)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if html {
		r.Header.Set("Accept", "text/html")
	}
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, r)
	return out
}
func reviewToken(t *testing.T, out *httptest.ResponseRecorder) string {
	t.Helper()
	if out.Code != 200 {
		t.Fatalf("preview failed: %d %s", out.Code, out.Body.String())
	}
	var response struct {
		Token     string `json:"preview_token"`
		Committed bool   `json:"committed"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token == "" || response.Committed {
		t.Fatal("not a preview", out.Body.String())
	}
	return response.Token
}
func TestTagihanReviewedPartialAmountsPreviewAndJournals(t *testing.T) {
	f := newTestFixture(t)
	configureReviewedCollectionMappings(t, f)
	token := f.login(t, "admin@coop.test", "password")
	m := f.createMember(t, token, `{"member_no":"REVIEW-001","full_name":"Reviewed Member","join_date":"2026-01-01","status":"active","email":"review@coop.test","password":"member-password"}`)
	mt := f.login(t, "review@coop.test", "member-password")
	req := f.createLoanRequest(t, mt, 1_000_000, 5)
	start := time.Now().In(time.FixedZone("Asia/Jakarta", 7*3600))
	loan := f.approveLoanRequestWithStartDate(t, token, req, 1_000_000, 5, start.Format("2006-01-02"))
	var first string
	var balance int64
	_ = f.db.QueryRow(`SELECT next_due_date,remaining_balance FROM loans WHERE id=$1`, loan.ID).Scan(&first, &balance)
	month := first[:7]
	date := month + "-28"
	_, err := f.db.Exec(`UPDATE member_tagihan_configs SET simpanan_wajib=100000,simpanan_manasuka=50000 WHERE member_id=$1`, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,loan_type,coa_code,active) VALUES('review-bank','review-bank','repayment','cash_bank','regular','BANK',TRUE)`)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/admin/tagihan/export.xlsx?month="+month, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	w, err := excelize.OpenReader(bytes.NewReader(out.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	rows, _ := w.GetRows("Tagihan")
	target := 0
	for i, row := range rows[1:] {
		_ = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", i+2), "unpaid")
		if row[0] == m.ID {
			target = i + 2
		}
	}
	if target == 0 {
		t.Fatal("member absent")
	}
	for col, val := range map[string]any{"K": "paid", "N": 40000, "O": 0, "P": 100000, "Q": 0, "R": 0, "S": 0} {
		_ = w.SetCellValue("Tagihan", fmt.Sprintf("%s%d", col, target), val)
	}
	detailRow := setLoanDetailReview(t, w, loan.ID, "payment", 100000)
	// Changing reference bill columns must never dictate payment amounts.
	_ = w.SetCellValue("Tagihan", fmt.Sprintf("G%d", target), 999999)
	serialize := func() []byte {
		var b bytes.Buffer
		if err := w.Write(&b); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	data := serialize()
	preview := postReviewedTagihan(f, token, month, date, data, "", false)
	pt := reviewToken(t, preview)
	var plan struct {
		Actual     int64 `json:"actual_total"`
		Components []struct {
			Type                        string `json:"component"`
			Actual, Expected, Remaining int64
		} `json:"components"`
	}
	_ = json.Unmarshal(preview.Body.Bytes(), &plan)
	if plan.Actual != 140000 {
		t.Fatal(preview.Body.String())
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE loan_id=$1`, loan.ID).Scan(&count)
	if count != 0 {
		t.Fatal("preview persisted payment")
	}
	html := postReviewedTagihan(f, token, month, date, data, "", true)
	if html.Code != 200 || !bytes.Contains(html.Body.Bytes(), []byte("preview_token")) {
		t.Fatal("browser preview missing", html.Code, html.Body.String())
	}
	// The signed preview is bound to exact workbook bytes.
	_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("M%d", detailRow), 90000)
	if got := postReviewedTagihan(f, token, month, date, serialize(), pt, false); got.Code != 409 {
		t.Fatal("edited file accepted", got.Code)
	}
	_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("M%d", detailRow), 100000)
	// A changed authoritative configuration requires another preview.
	_, _ = f.db.Exec(`UPDATE member_tagihan_configs SET simpanan_wajib=110000 WHERE member_id=$1`, m.ID)
	if got := postReviewedTagihan(f, token, month, date, data, pt, false); got.Code != 409 {
		t.Fatal("stale preview accepted", got.Code, got.Body.String())
	}
	pt = reviewToken(t, postReviewedTagihan(f, token, month, date, data, "", false))
	commit := postReviewedTagihan(f, token, month, date, data, pt, false)
	if commit.Code != 200 {
		t.Fatal(commit.Code, commit.Body.String())
	}
	var paid, remaining, saved, journal int64
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM loan_repayments WHERE loan_id=$1`, loan.ID).Scan(&paid)
	_ = f.db.QueryRow(`SELECT remaining_balance FROM loans WHERE id=$1`, loan.ID).Scan(&remaining)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM saving_records WHERE member_id=$1 AND reference_no=$2`, m.ID, "TAGIHAN-"+month).Scan(&saved)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(x.amount),0) FROM financial_journal_lines x JOIN financial_journal_entries j ON j.id=x.journal_id WHERE j.transaction_id IN(SELECT id FROM loan_repayments WHERE loan_id=$1) AND x.side='debit'`, loan.ID).Scan(&journal)
	if paid != 100000 || remaining != balance-100000 || saved != 40000 || journal != 100000 {
		t.Fatalf("actuals not respected: paid=%d remaining=%d savings=%d journal=%d", paid, remaining, saved, journal)
	}
	// New preview of same file is idempotent; edited retry cannot overwrite receipts.
	pt = reviewToken(t, postReviewedTagihan(f, token, month, date, data, "", false))
	again := postReviewedTagihan(f, token, month, date, data, pt, false)
	if again.Code != 200 {
		t.Fatal(again.Code, again.Body.String())
	}
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE loan_id=$1`, loan.ID).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate repayment")
	}
	for _, bad := range []any{-1, 1000000, "1.5", ""} {
		_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("M%d", detailRow), bad)
		got := postReviewedTagihan(f, token, month, date, serialize(), "", false)
		if got.Code == 200 {
			t.Fatal("invalid/changed actual accepted", bad)
		}
	}
}

func configureReviewedCollectionMappings(t *testing.T, f testFixture) {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO coa_accounts(id,code,name,account_type,normal_balance,is_group,active) VALUES('review-receivable','REVIEW-LOAN','Loan receivable','asset','D',FALSE,TRUE),('review-liability','REVIEW-SAVINGS','Savings liability','liability','C',FALSE,TRUE)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range [][4]string{{"review-savings-bank", "savings", "cash_bank", "BANK"}, {"review-savings-liability", "savings", "savings_liability", "REVIEW-SAVINGS"}, {"review-repayment-bank", "repayment", "cash_bank", "BANK"}, {"review-repayment-loan", "repayment", "loan_receivable", "REVIEW-LOAN"}} {
		if _, err = f.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,coa_code,active) VALUES($1,$1,$2,$3,$4,TRUE)`, m[0], m[1], m[2], m[3]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTagihanReviewedSuspensionAndPartialPaymentStaySeparate(t *testing.T) {
	f := newTestFixture(t)
	configureReviewedCollectionMappings(t, f)
	token := f.login(t, "admin@coop.test", "password")
	m := f.createMember(t, token, `{"member_no":"REVIEW-MIX","full_name":"Mixed Review","join_date":"2026-01-01","status":"active","email":"review-mix@coop.test","password":"member-password"}`)
	mt := f.login(t, "review-mix@coop.test", "member-password")
	start := time.Now().In(time.FixedZone("Asia/Jakarta", 7*3600)).Format("2006-01-02")
	req := f.createLoanRequest(t, mt, 200000, 5)
	first := f.approveLoanRequestWithStartDate(t, token, req, 200000, 5, start)
	req = f.createLoanRequest(t, mt, 200000, 5)
	second := f.approveLoanRequestWithStartDate(t, token, req, 200000, 5, start)
	_, err := f.db.Exec(`INSERT INTO coa_accounts(id,code,name,account_type,normal_balance,is_group,active) VALUES('mix-fee','40201','Service fee','revenue','C',FALSE,TRUE)`)
	if err != nil {
		t.Fatal(err)
	}
	var due, final string
	var before int64
	_ = f.db.QueryRow(`SELECT next_due_date,final_due_date,remaining_balance FROM loans WHERE id=$1`, first.ID).Scan(&due, &final, &before)
	month := due[:7]
	date := month + "-28"
	r := httptest.NewRequest("GET", "/api/admin/tagihan/export.xlsx?month="+month, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, r)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	w, err := excelize.OpenReader(bytes.NewReader(out.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	rows, _ := w.GetRows("Tagihan")
	target := 0
	for i, row := range rows[1:] {
		_ = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", i+2), "unpaid")
		if row[0] == m.ID {
			target = i + 2
		}
	}
	_ = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", target), "paid")
	for col, val := range map[string]any{"N": 0, "O": 0, "P": 60000, "Q": 0, "R": 0, "S": 0} {
		_ = w.SetCellValue("Tagihan", fmt.Sprintf("%s%d", col, target), val)
	}
	serialize := func() []byte {
		var b bytes.Buffer
		if err := w.Write(&b); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	// Pay the second loan while the first remains unpaid, regardless of ID/due ordering.
	_ = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", target), "unpaid")
	setLoanDetailReview(t, w, first.ID, "payment", 0)
	setLoanDetailReview(t, w, second.ID, "payment", 20000)
	data := serialize()
	plan := postReviewedTagihan(f, token, month, date, data, "", false)
	ptFirst := reviewToken(t, plan)
	committedFirst := postReviewedTagihan(f, token, month, date, data, ptFirst, false)
	if committedFirst.Code != 200 {
		t.Fatal(committedFirst.Code, committedFirst.Body.String())
	}
	var paidFirst, paidSecond int64
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM loan_repayments WHERE loan_id=$1`, first.ID).Scan(&paidFirst)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM loan_repayments WHERE loan_id=$1`, second.ID).Scan(&paidSecond)
	if paidFirst != 0 || paidSecond != 20000 {
		t.Fatalf("wrong loan paid: %d %d", paidFirst, paidSecond)
	}
	// Suspending the first loan remains separate from paying the second loan.
	firstRow := setLoanDetailReview(t, w, first.ID, "suspend", 0)
	_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("L%d", firstRow), "Approved suspension; fee received")
	data = serialize()
	pt := reviewToken(t, postReviewedTagihan(f, token, month, date, data, "", false))
	got := postReviewedTagihan(f, token, month, date, data, pt, false)
	if got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	var unchanged, changed, repayment, fee int64
	var extended string
	_ = f.db.QueryRow(`SELECT remaining_balance,final_due_date FROM loans WHERE id=$1`, first.ID).Scan(&unchanged, &extended)
	_ = f.db.QueryRow(`SELECT remaining_balance FROM loans WHERE id=$1`, second.ID).Scan(&changed)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM loan_repayments WHERE member_id=$1`, m.ID).Scan(&repayment)
	_ = f.db.QueryRow(`SELECT fee_amount FROM loan_suspensions WHERE loan_id=$1`, first.ID).Scan(&fee)
	if unchanged != before || extended == final || changed != before-20000 || repayment != 20000 || fee != 2000 {
		t.Fatalf("mixed operations conflated: %d %s %d %d %d", unchanged, extended, changed, repayment, fee)
	}
	var loanJournal, feeJournal int64
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(x.amount),0) FROM financial_journal_lines x JOIN financial_journal_entries j ON j.id=x.journal_id WHERE j.transaction_id IN(SELECT id FROM loan_repayments WHERE loan_id=$1) AND x.side='debit'`, second.ID).Scan(&loanJournal)
	_ = f.db.QueryRow(`SELECT COALESCE(SUM(x.amount),0) FROM financial_journal_lines x JOIN financial_journal_entries j ON j.id=x.journal_id WHERE j.transaction_id IN(SELECT id FROM loan_suspensions WHERE loan_id=$1) AND x.side='credit' AND x.coa_code='40201'`, first.ID).Scan(&feeJournal)
	if loanJournal != 20000 || feeJournal != 2000 {
		t.Fatal("wrong per-loan journal", loanJournal, feeJournal)
	}
	pt = reviewToken(t, postReviewedTagihan(f, token, month, date, data, "", false))
	again := postReviewedTagihan(f, token, month, date, data, pt, false)
	if again.Code != 200 {
		t.Fatal(again.Code, again.Body.String())
	}
	var receipts, fees int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE member_id=$1`, m.ID).Scan(&receipts)
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM loan_suspensions WHERE member_id=$1`, m.ID).Scan(&fees)
	if receipts != 1 || fees != 1 {
		t.Fatal("duplicate loan collection", receipts, fees)
	}

}

func setLoanDetailReview(t *testing.T, w *excelize.File, loanID, action string, actual int64) int {
	t.Helper()
	rows, err := w.GetRows("Rincian Pinjaman")
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows[1:] {
		if row[0] == loanID {
			r := i + 2
			_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("K%d", r), action)
			_ = w.SetCellFormula("Rincian Pinjaman", fmt.Sprintf("M%d", r), "")
			_ = w.SetCellValue("Rincian Pinjaman", fmt.Sprintf("M%d", r), actual)
			return r
		}
	}
	t.Fatal("loan detail missing", loanID)
	return 0
}
