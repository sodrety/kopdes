package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRepaymentEditModalsOnLoanDetailAndRepaymentPages(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-MODAL","full_name":"Modal Borrower","join_date":"2026-06-16","status":"active","email":"modal@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "modal@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)
	repayment := fixture.recordRepayment(t, adminToken, loan.ID, 100000)
	establishSuperAdmin(t, fixture)
	cookie := fixture.browserLogin(t, "super@coop.test", "permanent-super-password")
	loanPath := "/admin/loans/" + loan.ID
	readPage := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %s: status %d: %s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	for _, path := range []string{"/admin/repayments", loanPath} {
		body := readPage(path)
		for _, fragment := range []string{`href="#repayment-edit-` + repayment.ID + `"`, `id="repayment-edit-` + repayment.ID + `"`, `action="/api/admin/repayments/` + repayment.ID + `/update"`} {
			if !strings.Contains(body, fragment) {
				t.Fatalf("page %s missing modal control %q", path, fragment)
			}
		}
		if strings.Contains(body, `action="/api/admin/repayments"`) {
			t.Fatalf("page %s unexpectedly contains a manual-add form", path)
		}
		if path == loanPath && strings.Count(body, `name="reason"`) != 1 {
			t.Fatal("loan detail must show a single correction reason in its edit modal")
		}
	}
	for _, returnTo := range []string{loanPath, "https://example.com/", "/admin/loans/../repayments"} {
		form := url.Values{"amount": {"100000"}, "record_date": {"2026-06-16"}, "reason": {"Manual audit from loan details"}, "return_to": {returnTo}, "note": {returnTo}}
		req := httptest.NewRequest(http.MethodPost, "/api/admin/repayments/"+repayment.ID+"/update", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		wantRedirect := "/admin/repayments"
		if returnTo == loanPath {
			wantRedirect = loanPath
		}
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != wantRedirect {
			t.Fatalf("return path %q: expected redirect to %s, got %d %s: %s", returnTo, wantRedirect, rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
	}
	cookie = fixture.browserLogin(t, "admin@coop.test", "password")
	for _, path := range []string{"/admin/repayments", loanPath} {
		if strings.Contains(readPage(path), `id="repayment-edit-`) {
			t.Fatalf("non-Super Admin should not see edit modals on %s", path)
		}
	}
}
