package app_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMemberDashboardExportsMonthlySavingsAndLoanSlips(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-SLIP-001","full_name":"Slip Member","join_date":"2026-01-01","status":"active","email":"slip-member@coop.test","password":"member-password"}`)
	fixture.recordMemberSlipSavingInCategoryAtDate(t, adminToken, member.ID, "pokok", 5000, "2026-01-01", "SLIP-POKOK", "Pokok")
	fixture.recordMemberSlipSavingInCategoryAtDate(t, adminToken, member.ID, "wajib", 100000, "2026-01-15", "SLIP-WAJIB", "Wajib")
	fixture.recordMemberSlipSavingInCategoryAtDate(t, adminToken, member.ID, "sukarela", 250000, "2026-02-15", "SLIP-SUKARELA", "Sukarela")
	memberToken := fixture.login(t, "slip-member@coop.test", "member-password")
	fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 3), 500000, 3)
	cookie := fixture.browserLogin(t, "slip-member@coop.test", "member-password")

	pageRequest := httptest.NewRequest(http.MethodGet, "/member/dashboard", nil)
	pageRequest.AddCookie(cookie)
	pageResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("member dashboard status=%d body=%s", pageResponse.Code, pageResponse.Body.String())
	}
	for _, text := range []string{"Member slips", "Export Simpanan slip", "Export Pinjaman slip", `href="/member/exports/savings.pdf"`, `href="/member/exports/loans.pdf"`, "Export PDF"} {
		if !strings.Contains(pageResponse.Body.String(), text) {
			t.Fatalf("dashboard missing %q: %s", text, pageResponse.Body.String())
		}
	}
	if strings.Contains(pageResponse.Body.String(), `name="month"`) {
		t.Fatalf("dashboard should not include a month filter: %s", pageResponse.Body.String())
	}

	savingsPeriod := "2026-02"
	savingsRequest := httptest.NewRequest(http.MethodGet, "/member/exports/savings.pdf?month=2026-02", nil)
	savingsRequest.AddCookie(cookie)
	savingsResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(savingsResponse, savingsRequest)
	if savingsResponse.Code != http.StatusOK {
		t.Fatalf("savings slip status=%d body=%s", savingsResponse.Code, savingsResponse.Body.String())
	}
	if got := savingsResponse.Header().Get("Content-Type"); !strings.Contains(got, "application/pdf") {
		t.Fatalf("savings slip content type=%q", got)
	}
	if got := savingsResponse.Header().Get("Content-Disposition"); !strings.Contains(got, `slip-simpanan-M-SLIP-001-`+savingsPeriod+`.pdf`) {
		t.Fatalf("savings slip disposition=%q", got)
	}
	for _, text := range []string{"SLIP Simpanan", "SIMPANAN POKOK", "THROUGH MONTH", "FEBRUARY", "355.000"} {
		if !strings.Contains(savingsResponse.Body.String(), text) {
			t.Fatalf("savings slip missing %q", text)
		}
	}

	loanRequest := httptest.NewRequest(http.MethodGet, "/member/exports/loans.pdf?month=2026-02", nil)
	loanRequest.AddCookie(cookie)
	loanResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(loanResponse, loanRequest)
	if loanResponse.Code != http.StatusOK {
		t.Fatalf("loan slip status=%d body=%s", loanResponse.Code, loanResponse.Body.String())
	}
	if got := loanResponse.Header().Get("Content-Type"); !strings.Contains(got, "application/pdf") {
		t.Fatalf("loan slip content type=%q", got)
	}
	loanPeriod := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01")
	if got := loanResponse.Header().Get("Content-Disposition"); !strings.Contains(got, `slip-pinjaman-M-SLIP-001-`+loanPeriod+`.pdf`) {
		t.Fatalf("loan slip disposition=%q", got)
	}
	for _, text := range []string{"SLIP Pinjaman", "INSTALLMENTS", "PRINCIPAL + ADMIN", "REMAINING DEBT", "JANUARY"} {
		if !strings.Contains(loanResponse.Body.String(), text) {
			t.Fatalf("loan slip missing %q", text)
		}
	}
}

func (f testFixture) recordMemberSlipSavingInCategoryAtDate(t *testing.T, adminToken, memberID, category string, amount int, recordDate, referenceNo, note string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
		"member_id":"`+memberID+`",
		"type":"deposit",
		"category":"`+category+`",
		"amount":`+strconv.Itoa(amount)+`,
		"record_date":"`+recordDate+`",
		"reference_no":"`+referenceNo+`",
		"note":"`+note+`"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected slip saving status 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemberSlipExportsIgnoreMonthParameter(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-SLIP-002","full_name":"Invalid Period","join_date":"2026-01-01","status":"active","email":"invalid-period@coop.test","password":"member-password"}`)
	cookie := fixture.browserLogin(t, "invalid-period@coop.test", "member-password")
	request := httptest.NewRequest(http.MethodGet, "/member/exports/savings.pdf?month=not-a-month", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	fixture.server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected month parameter to be ignored, got %d %s", response.Code, response.Body.String())
	}
	period := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01")
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, `slip-simpanan-M-SLIP-002-`+period+`.pdf`) {
		t.Fatalf("expected current-period savings slip filename, got %q", got)
	}
}
