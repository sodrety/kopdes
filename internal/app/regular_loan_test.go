package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegularLoanSnapshotsFixedAdminFeeTermsEndToEnd(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, managerToken, `{"member_no":"M-REGULAR","full_name":"Regular Borrower","join_date":"2026-06-16","status":"active","email":"regular@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "regular@coop.test", "member-password")

	requestID := fixture.createLoanRequest(t, memberToken, 30_000_000, 24)
	loan := fixture.approveLoanRequest(t, managerToken, requestID, 30_000_000, 24)
	if loan.AdminFeePolicy != "regular_tiered_monthly_v1" || loan.MonthlyAdminFee != 325_000 || loan.TotalAdminFee != 7_800_000 || loan.TotalObligation != 37_800_000 || loan.RemainingBalance != 37_800_000 {
		t.Fatalf("unexpected Regular Loan terms: %+v", loan)
	}

	var policy string
	var monthlyFee, totalFee, obligation int64
	if err := fixture.db.QueryRow(`SELECT proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation FROM loan_requests WHERE id=$1`, requestID).Scan(&policy, &monthlyFee, &totalFee, &obligation); err != nil {
		t.Fatal(err)
	}
	if policy != "regular_tiered_monthly_v1" || monthlyFee != 325_000 || totalFee != 7_800_000 || obligation != 37_800_000 {
		t.Fatalf("unexpected snapshotted terms: policy=%q monthly=%d total=%d obligation=%d", policy, monthlyFee, totalFee, obligation)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/member/loans/active", nil)
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("active Loan status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"interest_rate_bps", "total_interest"} {
		if _, exists := payload[removed]; exists || strings.Contains(rec.Body.String(), `"`+removed+`"`) {
			t.Fatalf("deprecated field %q remains in Loan JSON: %s", removed, rec.Body.String())
		}
	}
}

func TestSecondaryGoodsLoanUsesOneTimeAdminFee(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, managerToken, `{"member_no":"M-SECONDARY","full_name":"Secondary Goods Borrower","join_date":"2026-06-16","status":"active","email":"secondary@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "secondary@coop.test", "member-password")

	requestID := fixture.createLoanRequestWithType(t, memberToken, "secondary_goods", 10_000_000, 12)
	loan := fixture.approveLoanRequest(t, managerToken, requestID, 10_000_000, 12)
	if loan.AdminFeePolicy != "secondary_goods_one_time_v1" || loan.MonthlyAdminFee != 0 || loan.TotalAdminFee != 2_000_000 || loan.TotalObligation != 12_000_000 || loan.MonthlyInstallment != 1_000_000 {
		t.Fatalf("unexpected Secondary Goods Loan terms: %+v", loan)
	}

	for _, body := range []string{
		`{"loan_type":"secondary_goods","requested_amount":1000000,"duration_months":13,"purpose":"Too long"}`,
		`{"loan_type":"secondary_goods","requested_amount":1000000,"duration_months":12,"purpose":""}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected validation status 400 for %s, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestPaylaterLoanUsesOneMonthFivePercentAdminFee(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, managerToken, `{"member_no":"M-PAYLATER","full_name":"Paylater Borrower","join_date":"2026-06-16","status":"active","email":"paylater@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "paylater@coop.test", "member-password")

	requestID := fixture.createLoanRequestWithType(t, memberToken, "goods_purchase_paylater", 1_000_001, 1)
	loan := fixture.approveLoanRequest(t, managerToken, requestID, 1_000_001, 1)
	if loan.AdminFeePolicy != "goods_purchase_paylater_one_time_v1" || loan.MonthlyAdminFee != 0 || loan.TotalAdminFee != 50_000 || loan.TotalObligation != 1_050_001 || loan.MonthlyInstallment != 1_050_001 {
		t.Fatalf("unexpected Paylater Loan terms: %+v", loan)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", strings.NewReader(`{"loan_type":"goods_purchase_paylater","requested_amount":1000000,"duration_months":2,"purpose":"Too long"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected validation status 400 for Paylater tenor, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegularLoanRequestRequiresPurposeAndMaximumTwentyFourMonths(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, managerToken, `{"member_no":"M-REGULAR-RULES","full_name":"Regular Rules","join_date":"2026-06-16","status":"active","email":"regular-rules@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "regular-rules@coop.test", "member-password")

	for _, body := range []string{
		`{"loan_type":"regular","requested_amount":1000000,"duration_months":24,"purpose":""}`,
		`{"loan_type":"regular","requested_amount":1000000,"duration_months":25,"purpose":"Working capital"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected validation status 400 for %s, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestLoanApprovalErrorsAreLocalizedForJSONAndHTMX(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, managerToken, `{"member_no":"M-LOCALE","full_name":"Localized Borrower","join_date":"2026-06-16","status":"active","email":"locale@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "locale@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 1_000_000, 12)

	for _, testCase := range []struct {
		name, contentType, body string
		htmx                    bool
	}{
		{"JSON", "application/json", `{}`, false},
		{"HTMX", "application/x-www-form-urlencoded", `approved_amount=&duration_months=&start_date=`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", strings.NewReader(testCase.body))
			req.Header.Set("Authorization", "Bearer "+managerToken)
			req.Header.Set("Content-Type", testCase.contentType)
			req.Header.Set("Accept-Language", "id")
			if testCase.htmx {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			fixture.server.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Jumlah persetujuan") {
				t.Fatalf("localized %s error status=%d body=%s", testCase.name, rec.Code, rec.Body.String())
			}
			if strings.Contains(strings.ToLower(rec.Body.String()), "interest") || strings.Contains(strings.ToLower(rec.Body.String()), "bunga") {
				t.Fatalf("obsolete interest wording in %s error: %s", testCase.name, rec.Body.String())
			}
		})
	}
}
