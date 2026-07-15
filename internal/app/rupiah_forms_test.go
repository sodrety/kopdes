package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGroupedRupiahBrowserFormsPersistExactAmounts(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RUPIAH","full_name":"Rupiah Forms","join_date":"2026-07-15","status":"active","email":"rupiah-forms@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "rupiah-forms@coop.test", "member-password")

	postForm := func(path, token string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		fixture.server.ServeHTTP(response, request)
		return response
	}

	savingResponse := postForm("/api/admin/savings", adminToken, url.Values{
		"member_id": {member.ID}, "type": {"deposit"}, "category": {"sukarela"},
		"amount": {"Rp 1.250.000"}, "record_date": {"2026-07-15"},
	})
	if savingResponse.Code != http.StatusSeeOther {
		t.Fatalf("record grouped saving: status=%d body=%s", savingResponse.Code, savingResponse.Body.String())
	}
	var savingAmount int
	if err := fixture.db.QueryRow(`SELECT amount FROM saving_records WHERE member_id=$1`, member.ID).Scan(&savingAmount); err != nil || savingAmount != 1250000 {
		t.Fatalf("saved amount = %d, %v; want 1250000", savingAmount, err)
	}

	withdrawalResponse := postForm("/api/member/withdrawal-requests", memberToken, url.Values{"amount": {"Rp 250,000"}, "note": {"Grouped withdrawal"}})
	if withdrawalResponse.Code != http.StatusSeeOther {
		t.Fatalf("record grouped withdrawal: status=%d body=%s", withdrawalResponse.Code, withdrawalResponse.Body.String())
	}
	var withdrawalAmount int
	if err := fixture.db.QueryRow(`SELECT amount FROM withdrawal_requests WHERE member_id=$1`, member.ID).Scan(&withdrawalAmount); err != nil || withdrawalAmount != 250000 {
		t.Fatalf("withdrawal amount = %d, %v; want 250000", withdrawalAmount, err)
	}

	loanRequestResponse := postForm("/api/member/loan-requests", memberToken, url.Values{
		"loan_type": {"regular"}, "requested_amount": {"Rp 3.000.000.000"},
		"duration_months": {"5"}, "purpose": {"Grouped browser Loan request"},
	})
	if loanRequestResponse.Code != http.StatusSeeOther {
		t.Fatalf("record grouped Loan request: status=%d body=%s", loanRequestResponse.Code, loanRequestResponse.Body.String())
	}
	var loanRequestID string
	var requestedAmount int64
	if err := fixture.db.QueryRow(`SELECT id,requested_amount FROM loan_requests WHERE member_id=$1 AND status='pending'`, member.ID).Scan(&loanRequestID, &requestedAmount); err != nil || requestedAmount != 3_000_000_000 {
		t.Fatalf("Loan requested amount = %d, %v; want 3000000000", requestedAmount, err)
	}
	loan := fixture.approveLoanRequest(t, adminToken, loanRequestID, 3_000_000_000, 5)
	repaymentResponse := postForm("/api/admin/loans/"+loan.ID+"/repayments", adminToken, url.Values{
		"amount": {"Rp 200.000"}, "record_date": {"2026-07-15"},
	})
	if repaymentResponse.Code != http.StatusSeeOther {
		t.Fatalf("record grouped repayment: status=%d body=%s", repaymentResponse.Code, repaymentResponse.Body.String())
	}
	var repaymentAmount int
	if err := fixture.db.QueryRow(`SELECT amount FROM loan_repayments WHERE loan_id=$1`, loan.ID).Scan(&repaymentAmount); err != nil || repaymentAmount != 200000 {
		t.Fatalf("repayment amount = %d, %v; want 200000", repaymentAmount, err)
	}
}

func TestMalformedRupiahBrowserFormIsLocalizedAndCreatesNoSaving(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RUPIAH-BAD","full_name":"Invalid Rupiah","join_date":"2026-07-15","status":"active"}`)
	values := url.Values{
		"member_id": {member.ID}, "type": {"deposit"}, "category": {"sukarela"},
		"amount": {"Rp 123,50"}, "record_date": {"2026-07-15"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/savings", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.AddCookie(fixture.setLanguage(t, "id", "/admin/savings/new"))
	response := httptest.NewRecorder()
	fixture.server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807") {
		t.Fatalf("expected localized invalid Rupiah response, status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1`, member.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid amount created %d savings, err=%v", count, err)
	}
}

func TestSavingAggregateCapacityErrorIsLocalizedForHTMX(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RUPIAH-CAP","full_name":"Rupiah Capacity","join_date":"2026-07-15","status":"active"}`)
	post := func(amount string, language string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/admin/savings", strings.NewReader(url.Values{
			"member_id": {member.ID}, "type": {"deposit"}, "category": {"sukarela"},
			"amount": {amount}, "record_date": {"2026-07-15"},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Authorization", "Bearer "+adminToken)
		if language != "" {
			request.Header.Set("HX-Request", "true")
			request.AddCookie(fixture.setLanguage(t, language, "/admin/savings/new"))
		}
		response := httptest.NewRecorder()
		fixture.server.ServeHTTP(response, request)
		return response
	}

	if response := post("Rp 9.223.372.036.854.775.807", ""); response.Code != http.StatusSeeOther {
		t.Fatalf("persist MaxInt64 saving: status=%d body=%s", response.Code, response.Body.String())
	}
	response := post("Rp 1", "id")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Jumlah ini akan melebihi total moneter aman sistem") {
		t.Fatalf("expected localized aggregate capacity error, status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1`, member.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("capacity failure persisted a row: count=%d err=%v", count, err)
	}
}

func TestRupiahBrowserEndpointsRenderLocalizedAmountErrors(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RUPIAH-ERRORS","full_name":"Rupiah Errors","join_date":"2026-07-15","status":"active","email":"rupiah-errors@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "rupiah-errors@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 1000000, 5), 1000000, 5)

	workflows := []struct {
		name, path, token string
		values            url.Values
	}{
		{
			name: "Simpanan", path: "/api/admin/savings", token: adminToken,
			values: url.Values{"member_id": {member.ID}, "type": {"deposit"}, "category": {"sukarela"}, "record_date": {"2026-07-15"}},
		},
		{
			name: "Penarikan", path: "/api/member/withdrawal-requests", token: memberToken,
			values: url.Values{"note": {"Invalid amount boundary"}},
		},
		{
			name: "Angsuran", path: "/api/admin/loans/" + loan.ID + "/repayments", token: adminToken,
			values: url.Values{"record_date": {"2026-07-15"}},
		},
		{
			name: "Pinjaman", path: "/api/member/loan-requests", token: memberToken,
			values: url.Values{"loan_type": {"regular"}, "duration_months": {"5"}, "purpose": {"Invalid grouped amount"}},
		},
	}
	amounts := []struct{ name, value string }{
		{name: "invalid", value: "abc"},
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
		{name: "malformed", value: "Rp 123,50"},
		{name: "integer_max_plus_one", value: "Rp 9.223.372.036.854.775.808"},
	}
	languages := []struct{ code, message string }{
		{code: "en", message: "Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807"},
		{code: "id", message: "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807"},
	}

	for _, workflow := range workflows {
		for _, language := range languages {
			for _, amount := range amounts {
				t.Run(workflow.name+"_"+language.code+"_"+amount.name, func(t *testing.T) {
					values := url.Values{}
					for key, entries := range workflow.values {
						values[key] = append([]string(nil), entries...)
					}
					values.Set("amount", amount.value)
					request := httptest.NewRequest(http.MethodPost, workflow.path, strings.NewReader(values.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					request.Header.Set("Authorization", "Bearer "+workflow.token)
					request.Header.Set("HX-Request", "true")
					request.AddCookie(fixture.setLanguage(t, language.code, "/"))
					response := httptest.NewRecorder()
					fixture.server.ServeHTTP(response, request)

					if response.Code != http.StatusBadRequest {
						t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
					}
					if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
						t.Fatalf("content type=%q, want localized HTML fragment", contentType)
					}
					if body := response.Body.String(); !strings.Contains(body, language.message) || !strings.Contains(body, `class="form-error-message"`) {
						t.Fatalf("expected localized rendered error %q, got %s", language.message, body)
					}
				})
			}
		}
	}
}

func TestRupiahInputsRenderLocaleAwareAccessibleEnhancement(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	for _, test := range []struct {
		name, language, separator, hint, rangeError string
	}{
		{name: "English", language: "en", separator: ",", hint: "Enter a whole Rupiah amount", rangeError: "Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807"},
		{name: "Bahasa", language: "id", separator: ".", hint: "Masukkan jumlah Rupiah bulat", rangeError: "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin/savings/new", nil)
			request.AddCookie(adminCookie)
			request.AddCookie(fixture.setLanguage(t, test.language, "/admin/savings/new"))
			response := httptest.NewRecorder()
			fixture.server.ServeHTTP(response, request)
			body := response.Body.String()
			for _, expected := range []string{`data-rupiah-input`, `inputmode="numeric"`, `data-rupiah-group="` + test.separator + `"`, `aria-describedby="saving-amount-hint"`, test.hint, test.rangeError} {
				if !strings.Contains(body, expected) {
					t.Fatalf("expected %q in localized Rupiah input, got %s", expected, body)
				}
			}
		})
	}
}
