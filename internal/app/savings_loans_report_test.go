package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestSavingsLoansReportFiltersAndAccess(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	member := f.createMember(t, token, `{"member_no":"REPORT-001","full_name":"Report Filter Member","join_date":"2026-01-01","status":"active"}`)
	for _, record := range []struct{ id, date string }{{"report-1", "2026-06-01"}, {"report-2", "2026-06-30"}, {"report-3", "2026-07-01"}} {
		if _, err := f.db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,recorded_by) VALUES ($1,$2,'deposit','wajib',123456,$3,'admin-user-id')`, record.id, member.ID, record.date); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query     string
		status    int
		hasMember bool
	}{
		{"?search=filter&date_from=2026-06-01&date_to=2026-06-30", 200, true},
		{"?search=REPORT-001&date_from=2026-06-01&date_to=2026-06-01", 200, true},
		{"?search=REPORT-001&date_from=2026-06-30&date_to=2026-06-30", 200, true},
		{"?search=REPORT-001&date_from=2026-06-02&date_to=2026-06-29", 200, false},
		{"?search=REPORT-001&date_from=2026-07-01", 200, true},
		{"?search=REPORT-001&date_to=2026-05-31", 200, false},
		{"?search=unmatched", 200, false}, {"?search=%25", 200, false}, {"?search=_", 200, false},
		{"?date_from=2026-02-30", 400, false}, {"?date_from=2026-07-01&date_to=2026-06-01", 400, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := savingsLoansReportResponse(f, token, tc.query, "")
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if tc.status != 200 {
				return
			}
			rows := savingsLoansReportCells(t, rec.Body.String(), member.MemberNo)
			if tc.hasMember {
				expected := [][]string{{member.MemberNo, "Report Filter Member", "0", "370.368", "0", "0", "0", "370.368", "0", "0"}}
				if !reflect.DeepEqual(rows, expected) {
					t.Fatalf("current member balances=%v want %v", rows, expected)
				}
			} else if len(rows) != 0 || !strings.Contains(rec.Body.String(), "No members match") {
				t.Fatalf("unexpected results: %v", rows)
			}
		})
	}
	memberToken := f.login(t, "member@coop.test", "password")
	if rec := savingsLoansReportResponse(f, memberToken, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member access status %d", rec.Code)
	}
}

func TestSavingsLoansReportSummarizesAllSavingsCategories(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	member := f.createMember(t, token, `{"member_no":"SUMMARY-001","full_name":"Summary Member","join_date":"2026-01-01","status":"active"}`)
	for i, record := range []struct {
		category, kind string
		amount         int64
	}{
		{"pokok", "deposit", 100000}, {"wajib", "deposit", 3000000000}, {"sukarela", "deposit", 500000}, {"sukarela", "withdrawal", 125000}, {"shu", "deposit", 75000}, {"khusus", "deposit", 200000},
	} {
		if _, err := f.db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,recorded_by) VALUES ($1,$2,$3,$4,$5,'2026-06-01','admin-user-id')`, fmt.Sprintf("summary-%d", i), member.ID, record.kind, record.category, record.amount); err != nil {
			t.Fatal(err)
		}
	}
	rec := savingsLoansReportResponse(f, token, "?search=SUMMARY-001", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	expected := [][]string{{member.MemberNo, "Summary Member", "100.000", "3.000.000.000", "375.000", "75.000", "200.000", "3.000.750.000", "0", "0"}}
	if rows := savingsLoansReportCells(t, rec.Body.String(), member.MemberNo); !reflect.DeepEqual(rows, expected) {
		t.Fatalf("summary=%v want %v", rows, expected)
	}
	if !strings.Contains(rec.Body.String(), `sidebar-link active" href="/admin/reports/savings-loans"`) {
		t.Fatal("report sidebar link is not active")
	}
	for _, lang := range []string{"en", "id"} {
		rec := savingsLoansReportResponse(f, token, "?search=SUMMARY-001", lang)
		if rec.Code != 200 {
			t.Fatalf("%s status %d", lang, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "savings_loans_") {
			t.Fatalf("untranslated report text for %s", lang)
		}
		if lang == "id" && !strings.Contains(html.UnescapeString(rec.Body.String()), "Total pinjaman berjalan + biaya") {
			t.Fatal("missing Bahasa heading")
		}
	}
	empty := f.createMember(t, token, `{"member_no":"EMPTY-001","full_name":"Empty Member","join_date":"2026-01-01","status":"inactive"}`)
	rec = savingsLoansReportResponse(f, token, "?search=EMPTY-001", "")
	expected = [][]string{{empty.MemberNo, "Empty Member", "0", "0", "0", "0", "0", "0", "0", "0"}}
	if rows := savingsLoansReportCells(t, rec.Body.String(), empty.MemberNo); !reflect.DeepEqual(rows, expected) {
		t.Fatalf("empty member balances=%v", rows)
	}
}

func TestSavingsLoansReportRunningLoansIncludeFeesOnce(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	member := f.createMember(t, token, `{"member_no":"LOAN-SUMMARY","full_name":"Loan Summary Member","join_date":"2026-01-01","status":"active","email":"report-loans@coop.test","password":"member-password"}`)
	memberToken := f.login(t, "report-loans@coop.test", "member-password")
	regular := f.approveLoanRequest(t, token, f.createLoanRequest(t, memberToken, 800000, 4), 800000, 4)
	secondary := f.approveLoanRequest(t, token, f.createLoanRequestWithType(t, memberToken, "secondary_goods", 300000, 3), 300000, 3)
	if regular.TotalAdminFee == 0 || secondary.TotalAdminFee == 0 {
		t.Fatal("fixture must exercise loan fees")
	}
	if _, err := f.db.Exec(`UPDATE loans SET remaining_balance=$1 WHERE id=$2`, regular.TotalObligation-100, regular.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE loans SET status='adjustment_due',remaining_balance=50 WHERE id=$1`, secondary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO loan_repayments (id,loan_id,member_id,amount,record_date,recorded_by) VALUES ('report-repaid',$1,$2,100,'2026-06-20','admin-user-id')`, regular.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO loan_repayment_events (id,loan_id,member_id,event_type,amount,record_date,recorded_by,source_key) VALUES ('report-historical',$1,$2,'adjustment',-200,'2026-05-20','admin-user-id','report-historical')`, regular.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	assertAmounts := func(query, total, remaining string) {
		t.Helper()
		rec := savingsLoansReportResponse(f, token, query, "")
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		rows := savingsLoansReportCells(t, rec.Body.String(), member.MemberNo)
		if len(rows) != 1 || len(rows[0]) != 10 || rows[0][8] != total || rows[0][9] != remaining {
			t.Fatalf("loan summary=%v want total %s remaining %s", rows, total, remaining)
		}
	}
	// 800,000 + 32,000 regular fees + 300,000 + 60,000 one-time goods fees.
	assertAmounts("?search=LOAN-SUMMARY", "1.192.000", "831.950")
	assertAmounts("?search=LOAN-SUMMARY&date_from=2026-06-20&date_to=2026-06-20", "1.192.000", "831.950")
	assertAmounts("?search=LOAN-SUMMARY&date_from=2026-05-20&date_to=2026-05-20", "1.192.000", "831.950")
	if _, err := f.db.Exec(`UPDATE loans SET status='paid',remaining_balance=0 WHERE id=$1`, secondary.ID); err != nil {
		t.Fatal(err)
	}
	assertAmounts("?search=LOAN-SUMMARY", "832.000", "831.900")
	if _, err := f.db.Exec(`UPDATE loans SET status='cancelled' WHERE id=$1`, regular.ID); err != nil {
		t.Fatal(err)
	}
	assertAmounts("?search=LOAN-SUMMARY", "0", "0")
}

func savingsLoansReportResponse(f testFixture, token, query, lang string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/admin/reports/savings-loans"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if lang != "" {
		req.AddCookie(&http.Cookie{Name: "kopdes_lang", Value: lang})
	}
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

func savingsLoansReportCells(t *testing.T, body, memberNo string) [][]string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var textContent func(*html.Node) string
	textContent = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var text string
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			text += textContent(child)
		}
		return text
	}
	var rows [][]string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []string
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "td" {
					cells = append(cells, strings.TrimSpace(textContent(child)))
				}
			}
			if len(cells) > 0 && cells[0] == memberNo {
				rows = append(rows, cells)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	return rows
}
