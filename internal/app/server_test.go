package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sodrety/kopdes/internal/app"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestAdminCanLoginAndSeeEmptyDashboardSummary(t *testing.T) {
	server := newTestServer(t)

	loginBody := bytes.NewBufferString(`{"email":"admin@coop.test","password":"password"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()

	server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResponse struct {
		Token string `json:"token"`
		User  struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResponse.Token == "" {
		t.Fatal("expected login response to include token")
	}
	if loginResponse.User.Email != "admin@coop.test" || loginResponse.User.Role != "admin" {
		t.Fatalf("unexpected login user: %+v", loginResponse.User)
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	dashboardReq.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	dashboardRec := httptest.NewRecorder()

	server.ServeHTTP(dashboardRec, dashboardReq)

	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", dashboardRec.Code, dashboardRec.Body.String())
	}

	var summary struct {
		TotalMembers         int `json:"total_members"`
		ActiveMembers        int `json:"active_members"`
		TotalSavings         int `json:"total_savings"`
		ActiveLoans          int `json:"active_loans"`
		TotalOutstandingLoan int `json:"total_outstanding_loan"`
		PendingLoanRequests  int `json:"pending_loan_requests"`
	}
	if err := json.Unmarshal(dashboardRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
	if summary != (struct {
		TotalMembers         int `json:"total_members"`
		ActiveMembers        int `json:"active_members"`
		TotalSavings         int `json:"total_savings"`
		ActiveLoans          int `json:"active_loans"`
		TotalOutstandingLoan int `json:"total_outstanding_loan"`
		PendingLoanRequests  int `json:"pending_loan_requests"`
	}{}) {
		t.Fatalf("expected empty dashboard summary, got %+v", summary)
	}
}

func TestLoginRejectsInvalidCredentialsWithStandardError(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"admin@coop.test","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Invalid email or password")
}

func TestHtmxLoginFailureReturnsHTMLFormError(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("email=admin%40coop.test&password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected HTML content type, got %q", contentType)
	}
	if body := rec.Body.String(); body != `<span class="form-error-message">Invalid email or password</span>` {
		t.Fatalf("expected escaped HTML error fragment, got %s", body)
	}
}

func TestResponsesIncludeSecurityHeaders(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	headers := rec.Header()
	if csp := headers.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("expected restrictive CSP, got %q", csp)
	}
	if got := headers.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff header, got %q", got)
	}
	if got := headers.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("expected DENY frame header, got %q", got)
	}
	if got := headers.Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("expected same-origin referrer policy, got %q", got)
	}
}

func TestCookieAuthenticatedMutationRejectsCrossSiteOrigin(t *testing.T) {
	fixture := newTestFixture(t)
	authCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Header.Set("Origin", "https://evil.test")
	req.AddCookie(authCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Same-origin browser request is required")
}

func TestBearerAuthenticatedMutationDoesNotRequireBrowserOrigin(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-CSRF","full_name":"CSRF Check","join_date":"2026-06-17","status":"active"}`)

	id := fixture.recordDeposit(t, adminToken, member.ID, 100000)

	if id == "" {
		t.Fatal("expected bearer-authenticated mutation without Origin to succeed")
	}
}

func TestLoginThrottlesRepeatedInvalidCredentials(t *testing.T) {
	fixture := newTestFixture(t)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"admin@coop.test","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed login %d to return 401, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"admin@coop.test","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected locked login to return 429, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "TOO_MANY_REQUESTS", "Too many failed login attempts. Try again later")
}

func TestTokenIsRejectedAfterUserRecordChanges(t *testing.T) {
	fixture := newTestFixture(t)
	token := fixture.login(t, "admin@coop.test", "password")

	if _, err := fixture.db.Exec(`UPDATE users SET email = $1 WHERE email = $2`, "renamed-admin@coop.test", "admin@coop.test"); err != nil {
		t.Fatalf("update user email: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected stale token status 401, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Invalid authentication token")
}

func TestStatusFilterTreatsSQLInjectionPayloadAsLiteralValue(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-SQLI","full_name":"SQLI Check","join_date":"2026-06-17","status":"active"}`)
	fixture.createMemberUser(t, adminToken, member.ID, "sqli-check@coop.test", "secret-password")
	memberToken := fixture.login(t, "sqli-check@coop.test", "secret-password")
	fixture.createLoanRequest(t, memberToken, 1000000, 5)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/loan-requests?status=pending%27%20OR%201%3D1%20--", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		LoanRequests []struct {
			ID string `json:"id"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode loan requests: %v", err)
	}
	if len(response.LoanRequests) != 0 {
		t.Fatalf("expected SQL injection-like status to match no rows, got %+v", response.LoanRequests)
	}
}

func TestAdminMemberPageEscapesMemberSuppliedHTML(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	authCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-XSS","full_name":"<script>alert(1)</script>","join_date":"2026-06-17","status":"active"}`)

	req := httptest.NewRequest(http.MethodGet, "/admin/members", nil)
	req.AddCookie(authCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected members page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("expected raw script tag to be escaped, got %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected escaped script text, got %s", body)
	}
}

func TestConcurrentWithdrawalsCannotOverdrawSavings(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RACE-SAV","full_name":"Saving Race","join_date":"2026-06-17","status":"active"}`)
	fixture.recordDeposit(t, adminToken, member.ID, 100000)

	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
				"member_id":"`+member.ID+`",
				"type":"withdrawal",
				"amount":80000,
				"record_date":"2026-06-17"
			}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()

			fixture.server.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	wg.Wait()
	close(statuses)

	var created, rejected int
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		}
		if status == http.StatusBadRequest {
			rejected++
		}
	}
	if created != 1 || rejected != 1 {
		t.Fatalf("expected one withdrawal created and one rejected, got created=%d rejected=%d", created, rejected)
	}

	var balance int
	if err := fixture.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE -amount END), 0) FROM saving_records WHERE member_id = $1`, member.ID).Scan(&balance); err != nil {
		t.Fatalf("query saving balance: %v", err)
	}
	if balance != 20000 {
		t.Fatalf("expected balance 20000 after one withdrawal, got %d", balance)
	}
}

func TestConcurrentLoanApprovalsCreateOnlyOneActiveLoan(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RACE-LOAN","full_name":"Loan Race","join_date":"2026-06-17","status":"active"}`)
	fixture.createMemberUser(t, adminToken, member.ID, "loan-race@coop.test", "secret-password")
	memberToken := fixture.login(t, "loan-race@coop.test", "secret-password")
	firstRequestID := fixture.createLoanRequest(t, memberToken, 1000000, 5)
	secondRequestID := "second-race-request"
	if _, err := fixture.db.Exec(
		`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, purpose, status) VALUES ($1, $2, $3, $4, $5, 'pending')`,
		secondRequestID,
		member.ID,
		900000,
		5,
		"Concurrent approval test",
	); err != nil {
		t.Fatalf("seed second pending loan request: %v", err)
	}

	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, requestID := range []string{firstRequestID, secondRequestID} {
		wg.Add(1)
		go func(requestID string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":500000,"duration_months":5}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()

			fixture.server.ServeHTTP(rec, req)
			statuses <- rec.Code
		}(requestID)
	}
	wg.Wait()
	close(statuses)

	var created, rejected int
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		}
		if status == http.StatusBadRequest {
			rejected++
		}
	}
	if created != 1 || rejected != 1 {
		t.Fatalf("expected one approval created and one rejected, got created=%d rejected=%d", created, rejected)
	}

	var activeLoans int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE member_id = $1 AND status = 'active'`, member.ID).Scan(&activeLoans); err != nil {
		t.Fatalf("query active loans: %v", err)
	}
	if activeLoans != 1 {
		t.Fatalf("expected one active loan, got %d", activeLoans)
	}
}

func TestConcurrentRepaymentsCannotOverpayLoan(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RACE-REPAY","full_name":"Repayment Race","join_date":"2026-06-17","status":"active"}`)
	fixture.createMemberUser(t, adminToken, member.ID, "repay-race@coop.test", "secret-password")
	memberToken := fixture.login(t, "repay-race@coop.test", "secret-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 100000, 5), 100000, 5)

	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loan.ID+"/repayments", bytes.NewBufferString(`{"amount":80000,"record_date":"2026-06-17"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()

			fixture.server.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	wg.Wait()
	close(statuses)

	var created, rejected int
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		}
		if status == http.StatusBadRequest {
			rejected++
		}
	}
	if created != 1 || rejected != 1 {
		t.Fatalf("expected one repayment created and one rejected, got created=%d rejected=%d", created, rejected)
	}

	var remainingBalance int
	if err := fixture.db.QueryRow(`SELECT remaining_balance FROM loans WHERE id = $1`, loan.ID).Scan(&remainingBalance); err != nil {
		t.Fatalf("query remaining loan balance: %v", err)
	}
	if remainingBalance != 20000 {
		t.Fatalf("expected remaining balance 20000, got %d", remainingBalance)
	}
}

func TestAdminDashboardRequiresValidAdminToken(t *testing.T) {
	fixture := newTestFixture(t)

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Authentication token is required")
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer not-a-token")
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Invalid authentication token")
	})

	t.Run("member token", func(t *testing.T) {
		memberToken := fixture.login(t, "member@coop.test", "password")
		req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient role")
	})
}

func TestAdminCanUseBrowserLoginAndSeeDashboardPage(t *testing.T) {
	fixture := newTestFixture(t)

	loginBody := strings.NewReader("email=admin%40coop.test&password=password")
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected browser login status 303, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var authCookie *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name == "auth_token" {
			authCookie = cookie
		}
	}
	if authCookie == nil || authCookie.Value == "" {
		t.Fatal("expected browser login to set auth_token cookie")
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	dashboardReq.AddCookie(authCookie)
	dashboardRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(dashboardRec, dashboardReq)

	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard page status 200, got %d: %s", dashboardRec.Code, dashboardRec.Body.String())
	}
	body := dashboardRec.Body.String()
	for _, text := range []string{`data-lucide="layout-dashboard"`, `data-lucide="users"`, `data-lucide="piggy-bank"`, "Admin menu", "Toggle sidebar", "Logout", "Dashboard", "Members", "Total members", "Pending requests"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected dashboard page to include %q, got %s", text, body)
		}
	}
}

func TestBrowserLoginCookieUsesStagingSecuritySettings(t *testing.T) {
	fixture := newTestFixtureWithConfig(t, app.Config{
		JWTSecret:    "0123456789abcdef0123456789abcdef",
		CookieSecure: true,
	})

	loginBody := strings.NewReader("email=admin%40coop.test&password=password")
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected browser login status 303, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var authCookie *http.Cookie
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name == "auth_token" {
			authCookie = cookie
		}
	}
	if authCookie == nil {
		t.Fatal("expected auth_token cookie")
	}
	if !authCookie.HttpOnly {
		t.Fatal("expected auth_token cookie to be HttpOnly")
	}
	if !authCookie.Secure {
		t.Fatal("expected auth_token cookie to be Secure")
	}
	if authCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected auth_token cookie SameSite=Lax, got %v", authCookie.SameSite)
	}
}

func TestHtmxLoginRedirectsWithoutSwappingDashboardIntoFormTarget(t *testing.T) {
	fixture := newTestFixture(t)

	loginBody := strings.NewReader("email=admin%40coop.test&password=password")
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("HX-Request", "true")
	loginRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusNoContent {
		t.Fatalf("expected htmx login status 204, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	if redirect := loginRec.Header().Get("HX-Redirect"); redirect != "/admin/dashboard" {
		t.Fatalf("expected HX-Redirect to /admin/dashboard, got %q", redirect)
	}
	if loginRec.Body.Len() != 0 {
		t.Fatalf("expected empty htmx redirect body, got %s", loginRec.Body.String())
	}
}

func TestStaticCSSIncludesMobileAdminResponsiveRules(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected css status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	css := rec.Body.String()
	for _, text := range []string{"@media (max-width: 760px)", ".admin-sidebar", "overflow-x: auto", ".page-shell", ".summary-grid", "grid-template-columns: repeat(2, minmax(0, 1fr))", ".inline-approval-form", ".inline-repayment-form", ".table-scroll td:last-child"} {
		if !strings.Contains(css, text) {
			t.Fatalf("expected css to include %q, got %s", text, css)
		}
	}
}

func TestBrowserPagesUseLocalPinnedFrontendAssets(t *testing.T) {
	fixture := newTestFixture(t)

	pageReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	pageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d: %s", pageRec.Code, pageRec.Body.String())
	}
	body := pageRec.Body.String()
	for _, text := range []string{`src="/static/vendor/htmx-2.0.10.min.js"`, `src="/static/vendor/lucide-0.468.0.min.js"`} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected login page to reference %q, got %s", text, body)
		}
	}
	for _, text := range []string{"cdn.jsdelivr.net", "unpkg.com"} {
		if strings.Contains(body, text) {
			t.Fatalf("expected login page not to reference %q, got %s", text, body)
		}
	}

	for _, path := range []string{"/static/vendor/htmx-2.0.10.min.js", "/static/vendor/lucide-0.468.0.min.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s status 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("expected %s to return asset content", path)
		}
	}
}

func TestBrowserPagesRenderSharedLayoutsAndHtmxForms(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0100","full_name":"Render Member","join_date":"2026-06-16","status":"active","email":"render-member@coop.test","password":"member-password"}`)

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	memberCookie := fixture.browserLogin(t, "render-member@coop.test", "member-password")

	tests := []struct {
		name     string
		path     string
		cookie   *http.Cookie
		contains []string
	}{
		{
			name:   "admin members page",
			path:   "/admin/members",
			cookie: adminCookie,
			contains: []string{
				`<aside class="admin-sidebar" aria-label="Admin menu">`,
				`src="/static/vendor/htmx-2.0.10.min.js"`,
				`src="/static/vendor/lucide-0.468.0.min.js"`,
				`hx-post="/api/admin/members"`,
				`hx-target="#member-form-error"`,
				`id="member-form-error" class="form-error"`,
				`<span class="status-badge">active</span>`,
				member.MemberNo,
			},
		},
		{
			name:   "member loan request page",
			path:   "/member/loan-requests",
			cookie: memberCookie,
			contains: []string{
				`class="member-shell member-loan-requests-shell"`,
				`src="/static/vendor/htmx-2.0.10.min.js"`,
				`hx-post="/api/member/loan-requests"`,
				`hx-target="#loan-request-error"`,
				`id="loan-request-error" class="form-error"`,
				`<p class="empty-state">No loan requests yet.</p>`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.AddCookie(tt.cookie)
			rec := httptest.NewRecorder()

			fixture.server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected page status 200, got %d: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, text := range tt.contains {
				if !strings.Contains(body, text) {
					t.Fatalf("expected page to include %q, got %s", text, body)
				}
			}
		})
	}
}

func TestLogoutClearsBrowserSessionAndReturnsToLogin(t *testing.T) {
	fixture := newTestFixture(t)
	authCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	logoutReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logoutReq.Header.Set("Origin", "http://example.com")
	logoutReq.AddCookie(authCookie)
	logoutRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusSeeOther {
		t.Fatalf("expected logout status 303, got %d: %s", logoutRec.Code, logoutRec.Body.String())
	}
	if location := logoutRec.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected logout redirect to /login, got %q", location)
	}

	var cleared bool
	for _, cookie := range logoutRec.Result().Cookies() {
		if cookie.Name == "auth_token" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("expected logout to clear auth_token cookie, got %+v", logoutRec.Result().Cookies())
	}
}

func TestAdminCanCreateAndViewMember(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")

	createBody := bytes.NewBufferString(`{
		"member_no":"M-0001",
		"full_name":"Siti Aminah",
		"phone":"08123456789",
		"address":"Jakarta",
		"join_date":"2026-06-15",
		"status":"active"
	}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/members", createBody)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create member status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var created struct {
		ID       string `json:"id"`
		MemberNo string `json:"member_no"`
		FullName string `json:"full_name"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create member response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected created member to include id")
	}
	if created.MemberNo != "M-0001" || created.FullName != "Siti Aminah" || created.Status != "active" {
		t.Fatalf("unexpected created member: %+v", created)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/admin/members/"+created.ID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+adminToken)
	detailRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(detailRec, detailReq)

	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected detail status 200, got %d: %s", detailRec.Code, detailRec.Body.String())
	}

	var detail struct {
		ID       string `json:"id"`
		MemberNo string `json:"member_no"`
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
		Address  string `json:"address"`
		JoinDate string `json:"join_date"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode member detail: %v", err)
	}
	if detail.ID != created.ID || detail.MemberNo != "M-0001" || detail.FullName != "Siti Aminah" || detail.Phone != "08123456789" || detail.Address != "Jakarta" || detail.JoinDate != "2026-06-15" || detail.Status != "active" {
		t.Fatalf("unexpected member detail: %+v", detail)
	}
}

func TestAdminCanCreateMemberWithLoginInOneForm(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{
		"member_no":"M-0011",
		"full_name":"One Form Member",
		"phone":"0844444444",
		"address":"Medan",
		"join_date":"2026-06-16",
		"status":"active",
		"email":"one-form@coop.test",
		"password":"member-password"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create member status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID           string `json:"id"`
		MemberNo     string `json:"member_no"`
		FullName     string `json:"full_name"`
		LoginCreated bool   `json:"login_created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create member response: %v", err)
	}
	if created.ID == "" || created.MemberNo != "M-0011" || created.FullName != "One Form Member" || !created.LoginCreated {
		t.Fatalf("unexpected create member response: %+v", created)
	}

	memberToken := fixture.login(t, "one-form@coop.test", "member-password")
	profileReq := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+memberToken)
	profileRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected profile status 200, got %d: %s", profileRec.Code, profileRec.Body.String())
	}
}

func TestAdminCanListMembers(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	created := fixture.createMember(t, adminToken, `{"member_no":"M-0002","full_name":"Budi Santoso","join_date":"2026-06-15","status":"inactive"}`)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/members", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var response struct {
		Members []struct {
			ID       string `json:"id"`
			MemberNo string `json:"member_no"`
			FullName string `json:"full_name"`
			Status   string `json:"status"`
		} `json:"members"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode member list: %v", err)
	}
	if len(response.Members) != 1 {
		t.Fatalf("expected one member, got %+v", response.Members)
	}
	if response.Members[0].ID != created.ID || response.Members[0].MemberNo != "M-0002" || response.Members[0].FullName != "Budi Santoso" || response.Members[0].Status != "inactive" {
		t.Fatalf("unexpected member list: %+v", response.Members[0])
	}
}

func TestAdminDashboardCountsMembers(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0007","full_name":"Active Member","join_date":"2026-06-15","status":"active"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0008","full_name":"Inactive Member","join_date":"2026-06-15","status":"inactive"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var summary struct {
		TotalMembers  int `json:"total_members"`
		ActiveMembers int `json:"active_members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode dashboard summary: %v", err)
	}
	if summary.TotalMembers != 2 || summary.ActiveMembers != 1 {
		t.Fatalf("unexpected member counts: %+v", summary)
	}
}

func TestCreateMemberRejectsDuplicateMemberNumber(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0003","full_name":"Dewi Lestari","join_date":"2026-06-15","status":"active"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{"member_no":"M-0003","full_name":"Dewi Other","join_date":"2026-06-16","status":"active"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate status 409, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "DUPLICATE_DATA", "Member number already exists")
}

func TestCreateMemberValidatesRequiredFieldsAndStatus(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{"member_no":"M-0004","full_name":"Rina","join_date":"2026-06-15","status":"archived"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected validation status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Member number, full name, join date, and valid status are required")
}

func TestMemberManagementRequiresAdminRole(t *testing.T) {
	fixture := newTestFixture(t)
	memberToken := fixture.login(t, "member@coop.test", "password")

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/members", nil)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Authentication token is required")
	})

	t.Run("member token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{"member_no":"M-0005","full_name":"Not Allowed","join_date":"2026-06-15","status":"active"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient role")
	})
}

func TestAdminMemberPagesRenderListCreateAndDetailFlows(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	adminToken := fixture.login(t, "admin@coop.test", "password")
	created := fixture.createMember(t, adminToken, `{"member_no":"M-0006","full_name":"Agus Wijaya","join_date":"2026-06-15","status":"suspended"}`)

	listReq := httptest.NewRequest(http.MethodGet, "/admin/members", nil)
	listReq.AddCookie(adminCookie)
	listRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected member page status 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	listBody := listRec.Body.String()
	for _, text := range []string{`data-lucide="layout-dashboard"`, `data-lucide="users"`, `data-lucide="piggy-bank"`, "Admin menu", "Toggle sidebar", "Logout", "Dashboard", "Members", "Create member", "Member login", "table-scroll", `name="email"`, `name="password"`, "Agus Wijaya", "M-0006", "suspended"} {
		if !strings.Contains(listBody, text) {
			t.Fatalf("expected members page to include %q, got %s", text, listBody)
		}
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/admin/members/"+created.ID, nil)
	detailReq.AddCookie(adminCookie)
	detailRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(detailRec, detailReq)

	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected member detail page status 200, got %d: %s", detailRec.Code, detailRec.Body.String())
	}
	detailBody := detailRec.Body.String()
	for _, text := range []string{"Admin menu", "Members", "Member detail", "Agus Wijaya", "M-0006", "2026-06-15"} {
		if !strings.Contains(detailBody, text) {
			t.Fatalf("expected member detail page to include %q, got %s", text, detailBody)
		}
	}
}

func TestAdminCanCreateMemberLoginAndMemberCanViewOwnProfile(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0009","full_name":"Member Profile","phone":"0811111111","address":"Bandung","join_date":"2026-06-16","status":"active"}`)

	accountReq := httptest.NewRequest(http.MethodPost, "/api/admin/members/"+member.ID+"/user", bytes.NewBufferString(`{"email":"profile@coop.test","password":"secret-password"}`))
	accountReq.Header.Set("Content-Type", "application/json")
	accountReq.Header.Set("Authorization", "Bearer "+adminToken)
	accountRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(accountRec, accountReq)

	if accountRec.Code != http.StatusCreated {
		t.Fatalf("expected member user status 201, got %d: %s", accountRec.Code, accountRec.Body.String())
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"profile@coop.test","password":"secret-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected member login status 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResponse struct {
		Token string `json:"token"`
		User  struct {
			Email    string `json:"email"`
			Role     string `json:"role"`
			MemberID string `json:"member_id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResponse); err != nil {
		t.Fatalf("decode member login response: %v", err)
	}
	if loginResponse.Token == "" || loginResponse.User.Email != "profile@coop.test" || loginResponse.User.Role != "member" || loginResponse.User.MemberID != member.ID {
		t.Fatalf("unexpected member login response: %+v", loginResponse)
	}

	profileReq := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	profileRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected profile status 200, got %d: %s", profileRec.Code, profileRec.Body.String())
	}

	var profile struct {
		ID       string `json:"id"`
		MemberNo string `json:"member_no"`
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
		Address  string `json:"address"`
		JoinDate string `json:"join_date"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(profileRec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode member profile: %v", err)
	}
	if profile.ID != member.ID || profile.MemberNo != "M-0009" || profile.FullName != "Member Profile" || profile.Phone != "0811111111" || profile.Address != "Bandung" || profile.JoinDate != "2026-06-16" || profile.Status != "active" {
		t.Fatalf("unexpected member profile: %+v", profile)
	}
}

func TestMemberProfileRequiresMemberRoleAndLinkedIdentity(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	unlinkedMemberToken := fixture.login(t, "member@coop.test", "password")

	t.Run("admin cannot use member profile route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient role")
	})

	t.Run("member token must be linked to a member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
		req.Header.Set("Authorization", "Bearer "+unlinkedMemberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Invalid authentication token")
	})
}

func TestMemberCanUseBrowserLoginAndSeeProfilePage(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0010","full_name":"Browser Member","phone":"0822222222","address":"Surabaya","join_date":"2026-06-16","status":"active"}`)
	fixture.createMemberUser(t, adminToken, member.ID, "browser-member@coop.test", "secret-password")
	memberCookie := fixture.browserLogin(t, "browser-member@coop.test", "secret-password")

	req := httptest.NewRequest(http.MethodGet, "/member/profile", nil)
	req.AddCookie(memberCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected member profile page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{"member-profile-shell", "Member profile", "Browser Member", "M-0010", "Surabaya", "Logout"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected member profile page to include %q, got %s", text, body)
		}
	}
}

func TestAdminCanRecordDepositAndMemberCanSeeSavingHistoryAndSummary(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{
		"member_no":"M-0012",
		"full_name":"Saving Member",
		"phone":"0855555555",
		"address":"Bogor",
		"join_date":"2026-06-16",
		"status":"active",
		"email":"saving-member@coop.test",
		"password":"member-password"
	}`)
	memberToken := fixture.login(t, "saving-member@coop.test", "member-password")

	depositReq := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
		"member_id":"`+member.ID+`",
		"type":"deposit",
		"amount":500000,
		"record_date":"2026-06-16",
		"reference_no":"TRF-001",
		"note":"Initial saving"
	}`))
	depositReq.Header.Set("Content-Type", "application/json")
	depositReq.Header.Set("Authorization", "Bearer "+adminToken)
	depositRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(depositRec, depositReq)

	if depositRec.Code != http.StatusCreated {
		t.Fatalf("expected deposit status 201, got %d: %s", depositRec.Code, depositRec.Body.String())
	}

	var deposit struct {
		ID          string `json:"id"`
		MemberID    string `json:"member_id"`
		Type        string `json:"type"`
		Amount      int    `json:"amount"`
		RecordDate  string `json:"record_date"`
		ReferenceNo string `json:"reference_no"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal(depositRec.Body.Bytes(), &deposit); err != nil {
		t.Fatalf("decode deposit response: %v", err)
	}
	if deposit.ID == "" || deposit.MemberID != member.ID || deposit.Type != "deposit" || deposit.Amount != 500000 || deposit.RecordDate != "2026-06-16" || deposit.ReferenceNo != "TRF-001" || deposit.Note != "Initial saving" {
		t.Fatalf("unexpected deposit response: %+v", deposit)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/member/savings", nil)
	historyReq.Header.Set("Authorization", "Bearer "+memberToken)
	historyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected saving history status 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}

	var history struct {
		Savings []struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			Amount      int    `json:"amount"`
			RecordDate  string `json:"record_date"`
			ReferenceNo string `json:"reference_no"`
			Note        string `json:"note"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode saving history: %v", err)
	}
	if len(history.Savings) != 1 || history.Savings[0].ID != deposit.ID || history.Savings[0].Amount != 500000 || history.Savings[0].Type != "deposit" {
		t.Fatalf("unexpected saving history: %+v", history)
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/member/savings/summary", nil)
	summaryReq.Header.Set("Authorization", "Bearer "+memberToken)
	summaryRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(summaryRec, summaryReq)

	if summaryRec.Code != http.StatusOK {
		t.Fatalf("expected saving summary status 200, got %d: %s", summaryRec.Code, summaryRec.Body.String())
	}

	var summary struct {
		TotalDeposit    int `json:"total_deposit"`
		TotalWithdrawal int `json:"total_withdrawal"`
		CurrentBalance  int `json:"current_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode saving summary: %v", err)
	}
	if summary.TotalDeposit != 500000 || summary.TotalWithdrawal != 0 || summary.CurrentBalance != 500000 {
		t.Fatalf("unexpected saving summary: %+v", summary)
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	dashboardReq.Header.Set("Authorization", "Bearer "+adminToken)
	dashboardRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(dashboardRec, dashboardReq)

	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", dashboardRec.Code, dashboardRec.Body.String())
	}
	var dashboard struct {
		TotalSavings int `json:"total_savings"`
	}
	if err := json.Unmarshal(dashboardRec.Body.Bytes(), &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if dashboard.TotalSavings != 500000 {
		t.Fatalf("expected dashboard total savings 500000, got %+v", dashboard)
	}
}

func TestRecordDepositValidatesPositiveAmountAndActiveMember(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	inactive := fixture.createMember(t, adminToken, `{"member_no":"M-0013","full_name":"Inactive Saver","join_date":"2026-06-16","status":"inactive"}`)

	t.Run("amount must be positive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{"member_id":"`+inactive.ID+`","type":"deposit","amount":0,"record_date":"2026-06-16"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Member, deposit amount, and record date are required")
	})

	t.Run("member must be active", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{"member_id":"`+inactive.ID+`","type":"deposit","amount":100000,"record_date":"2026-06-16"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Savings can only be recorded for active members")
	})
}

func TestHtmxAdminFormFailureReturnsHTMLFormError(t *testing.T) {
	fixture := newTestFixture(t)
	authCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", strings.NewReader("member_id=&amount=&record_date="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(authCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected HTML content type, got %q", contentType)
	}
	if body := rec.Body.String(); body != `<span class="form-error-message">Member, deposit amount, and record date are required</span>` {
		t.Fatalf("expected escaped HTML error fragment, got %s", body)
	}
}

func TestAdminCanRecordWithdrawalAndMemberBalanceCannotGoNegative(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{
		"member_no":"M-0015",
		"full_name":"Withdrawal Member",
		"join_date":"2026-06-16",
		"status":"active",
		"email":"withdrawal-member@coop.test",
		"password":"member-password"
	}`)
	memberToken := fixture.login(t, "withdrawal-member@coop.test", "member-password")
	fixture.recordDeposit(t, adminToken, member.ID, 300000)

	withdrawalID := fixture.recordSaving(t, adminToken, member.ID, "withdrawal", 125000, "WD-001", "Cash withdrawal")

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/member/savings/summary", nil)
	summaryReq.Header.Set("Authorization", "Bearer "+memberToken)
	summaryRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(summaryRec, summaryReq)

	if summaryRec.Code != http.StatusOK {
		t.Fatalf("expected summary status 200, got %d: %s", summaryRec.Code, summaryRec.Body.String())
	}
	var summary struct {
		TotalDeposit    int `json:"total_deposit"`
		TotalWithdrawal int `json:"total_withdrawal"`
		CurrentBalance  int `json:"current_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TotalDeposit != 300000 || summary.TotalWithdrawal != 125000 || summary.CurrentBalance != 175000 {
		t.Fatalf("unexpected summary after withdrawal: %+v", summary)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/member/savings", nil)
	historyReq.Header.Set("Authorization", "Bearer "+memberToken)
	historyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}
	var history struct {
		Savings []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Amount int    `json:"amount"`
			Note   string `json:"note"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	var sawWithdrawal bool
	for _, record := range history.Savings {
		if record.ID == withdrawalID && record.Type == "withdrawal" && record.Amount == 125000 && record.Note == "Cash withdrawal" {
			sawWithdrawal = true
		}
	}
	if !sawWithdrawal {
		t.Fatalf("expected withdrawal in history, got %+v", history.Savings)
	}

	overReq := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
		"member_id":"`+member.ID+`",
		"type":"withdrawal",
		"amount":200000,
		"record_date":"2026-06-16",
		"reference_no":"WD-OVER",
		"note":"Too much"
	}`))
	overReq.Header.Set("Content-Type", "application/json")
	overReq.Header.Set("Authorization", "Bearer "+adminToken)
	overRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(overRec, overReq)

	if overRec.Code != http.StatusBadRequest {
		t.Fatalf("expected over-withdrawal status 400, got %d: %s", overRec.Code, overRec.Body.String())
	}
	assertError(t, overRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed current saving balance")

	summaryAfterRejectReq := httptest.NewRequest(http.MethodGet, "/api/member/savings/summary", nil)
	summaryAfterRejectReq.Header.Set("Authorization", "Bearer "+memberToken)
	summaryAfterRejectRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(summaryAfterRejectRec, summaryAfterRejectReq)

	if summaryAfterRejectRec.Code != http.StatusOK {
		t.Fatalf("expected summary after reject status 200, got %d: %s", summaryAfterRejectRec.Code, summaryAfterRejectRec.Body.String())
	}
	var summaryAfterReject struct {
		TotalDeposit    int `json:"total_deposit"`
		TotalWithdrawal int `json:"total_withdrawal"`
		CurrentBalance  int `json:"current_balance"`
	}
	if err := json.Unmarshal(summaryAfterRejectRec.Body.Bytes(), &summaryAfterReject); err != nil {
		t.Fatalf("decode summary after reject: %v", err)
	}
	if summaryAfterReject.TotalDeposit != 300000 || summaryAfterReject.TotalWithdrawal != 125000 || summaryAfterReject.CurrentBalance != 175000 {
		t.Fatalf("expected rejected withdrawal to leave balance unchanged, got %+v", summaryAfterReject)
	}
}

func TestSavingPagesRenderDepositFormAndMemberBalance(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{
		"member_no":"M-0014",
		"full_name":"Saving Page Member",
		"phone":"0866666666",
		"address":"Depok",
		"join_date":"2026-06-16",
		"status":"active",
		"email":"saving-page@coop.test",
		"password":"member-password"
	}`)
	fixture.recordDeposit(t, adminToken, member.ID, 750000)

	adminPageReq := httptest.NewRequest(http.MethodGet, "/admin/savings", nil)
	adminPageReq.AddCookie(adminCookie)
	adminPageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminPageRec, adminPageReq)

	if adminPageRec.Code != http.StatusOK {
		t.Fatalf("expected admin savings page status 200, got %d: %s", adminPageRec.Code, adminPageRec.Body.String())
	}
	adminBody := adminPageRec.Body.String()
	for _, text := range []string{"Record saving", "Saving Page Member", "deposit", "withdrawal", `name="amount"`, `name="record_date"`} {
		if !strings.Contains(adminBody, text) {
			t.Fatalf("expected admin savings page to include %q, got %s", text, adminBody)
		}
	}

	memberCookie := fixture.browserLogin(t, "saving-page@coop.test", "member-password")
	profileReq := httptest.NewRequest(http.MethodGet, "/member/profile", nil)
	profileReq.AddCookie(memberCookie)
	profileRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected member profile page status 200, got %d: %s", profileRec.Code, profileRec.Body.String())
	}
	profileBody := profileRec.Body.String()
	for _, text := range []string{"Saving balance", "750000", "Saving history", "table-scroll", "TRF-PAGE", "Page deposit"} {
		if !strings.Contains(profileBody, text) {
			t.Fatalf("expected member profile page to include %q, got %s", text, profileBody)
		}
	}
}

func TestMemberCanSubmitAndTrackLoanRequest(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{
		"member_no":"M-0016",
		"full_name":"Loan Member",
		"join_date":"2026-06-16",
		"status":"active",
		"email":"loan-member@coop.test",
		"password":"member-password"
	}`)
	memberToken := fixture.login(t, "loan-member@coop.test", "member-password")

	createReq := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{
		"requested_amount":3000000,
		"duration_months":6,
		"purpose":"Small business capital"
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+memberToken)
	createRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected loan request status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var created struct {
		ID              string `json:"id"`
		MemberID        string `json:"member_id"`
		RequestedAmount int    `json:"requested_amount"`
		DurationMonths  int    `json:"duration_months"`
		Purpose         string `json:"purpose"`
		Status          string `json:"status"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode loan request: %v", err)
	}
	if created.ID == "" || created.MemberID != member.ID || created.RequestedAmount != 3000000 || created.DurationMonths != 6 || created.Purpose != "Small business capital" || created.Status != "pending" {
		t.Fatalf("unexpected loan request: %+v", created)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/member/loan-requests", nil)
	historyReq.Header.Set("Authorization", "Bearer "+memberToken)
	historyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected loan request history status 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}

	var history struct {
		LoanRequests []struct {
			ID              string `json:"id"`
			RequestedAmount int    `json:"requested_amount"`
			DurationMonths  int    `json:"duration_months"`
			Status          string `json:"status"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode loan request history: %v", err)
	}
	if len(history.LoanRequests) != 1 || history.LoanRequests[0].ID != created.ID || history.LoanRequests[0].Status != "pending" {
		t.Fatalf("unexpected loan request history: %+v", history)
	}
}

func TestLoanRequestValidationAndEligibility(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0017","full_name":"Active Loan","join_date":"2026-06-16","status":"active","email":"active-loan@coop.test","password":"member-password"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0018","full_name":"Inactive Loan","join_date":"2026-06-16","status":"inactive","email":"inactive-loan@coop.test","password":"member-password"}`)
	activeToken := fixture.login(t, "active-loan@coop.test", "member-password")
	inactiveToken := fixture.login(t, "inactive-loan@coop.test", "member-password")

	t.Run("amount and duration must be positive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":0,"duration_months":0}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+activeToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Requested amount and duration months must be greater than zero")
	})

	t.Run("member must be active", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":1000000,"duration_months":4}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+inactiveToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Only active members can request loans")
	})

	t.Run("member can have only one pending request", func(t *testing.T) {
		fixture.createLoanRequest(t, activeToken, 1000000, 4)
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":2000000,"duration_months":8}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+activeToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Member already has a pending loan request")
	})
}

func TestMemberLoanRequestPageRendersFormAndHistory(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0019","full_name":"Loan Page","join_date":"2026-06-16","status":"active","email":"loan-page@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "loan-page@coop.test", "member-password")
	fixture.createLoanRequest(t, memberToken, 1500000, 5)
	memberCookie := fixture.browserLogin(t, "loan-page@coop.test", "member-password")

	req := httptest.NewRequest(http.MethodGet, "/member/loan-requests", nil)
	req.AddCookie(memberCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected loan request page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{"member-loan-requests-shell", "Loan requests", "Submit loan request", `name="requested_amount"`, `name="duration_months"`, "table-scroll", "1500000", "pending"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected loan request page to include %q, got %s", text, body)
		}
	}
}

func TestAdminCanListPendingLoanRequestsForReview(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0020","full_name":"Review Member","join_date":"2026-06-16","status":"active","email":"review-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "review-member@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 2500000, 10)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/loan-requests?status=pending", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin loan request status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		LoanRequests []struct {
			ID              string `json:"id"`
			MemberID        string `json:"member_id"`
			MemberNo        string `json:"member_no"`
			FullName        string `json:"full_name"`
			RequestedAmount int    `json:"requested_amount"`
			DurationMonths  int    `json:"duration_months"`
			Purpose         string `json:"purpose"`
			Status          string `json:"status"`
			CreatedAt       string `json:"created_at"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode admin loan requests: %v", err)
	}
	if len(response.LoanRequests) != 1 {
		t.Fatalf("expected one pending request, got %+v", response.LoanRequests)
	}
	got := response.LoanRequests[0]
	if got.ID != requestID || got.MemberNo != "M-0020" || got.FullName != "Review Member" || got.RequestedAmount != 2500000 || got.DurationMonths != 10 || got.Purpose != "Test loan" || got.Status != "pending" || got.CreatedAt == "" {
		t.Fatalf("unexpected admin loan request: %+v", got)
	}
}

func TestAdminLoanRequestReviewRequiresAdminRole(t *testing.T) {
	fixture := newTestFixture(t)
	memberToken := fixture.login(t, "member@coop.test", "password")

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/loan-requests?status=pending", nil)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Authentication token is required")
	})

	t.Run("member token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/loan-requests?status=pending", nil)
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient role")
	})
}

func TestAdminLoanRequestReviewPageRendersPendingQueue(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0021","full_name":"Queue Member","join_date":"2026-06-16","status":"active","email":"queue-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "queue-member@coop.test", "member-password")
	fixture.createLoanRequest(t, memberToken, 3200000, 12)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodGet, "/admin/loan-requests", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin loan request page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{"Loan request review", "table-scroll", "Queue Member", "M-0021", "3200000", "12", "Test loan", "pending", "Approve", "Reject"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected admin loan request page to include %q, got %s", text, body)
		}
	}
}

func TestAdminLoanRequestReviewPageShowsEmptyPendingState(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	req := httptest.NewRequest(http.MethodGet, "/admin/loan-requests", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin loan request page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "No pending loan requests.") {
		t.Fatalf("expected empty pending state, got %s", body)
	}
}

func TestAdminCanApproveLoanRequestAndExposeActiveLoan(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0022","full_name":"Approved Borrower","join_date":"2026-06-16","status":"active","email":"approved-borrower@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "approved-borrower@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 2400000, 12)

	loan := fixture.approveLoanRequest(t, adminToken, requestID, 1200000, 6)
	if loan.ID == "" || loan.LoanRequestID != requestID || loan.MemberID != member.ID || loan.ApprovedAmount != 1200000 || loan.DurationMonths != 6 || loan.MonthlyInstallment != 200000 || loan.RemainingBalance != 1200000 || loan.Status != "active" {
		t.Fatalf("unexpected approved loan: %+v", loan)
	}

	requestsReq := httptest.NewRequest(http.MethodGet, "/api/member/loan-requests", nil)
	requestsReq.Header.Set("Authorization", "Bearer "+memberToken)
	requestsRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(requestsRec, requestsReq)

	if requestsRec.Code != http.StatusOK {
		t.Fatalf("expected loan request history status 200, got %d: %s", requestsRec.Code, requestsRec.Body.String())
	}
	var requestHistory struct {
		LoanRequests []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(requestsRec.Body.Bytes(), &requestHistory); err != nil {
		t.Fatalf("decode loan request history: %v", err)
	}
	if len(requestHistory.LoanRequests) != 1 || requestHistory.LoanRequests[0].ID != requestID || requestHistory.LoanRequests[0].Status != "approved" {
		t.Fatalf("expected approved request history, got %+v", requestHistory.LoanRequests)
	}

	activeReq := httptest.NewRequest(http.MethodGet, "/api/member/loans/active", nil)
	activeReq.Header.Set("Authorization", "Bearer "+memberToken)
	activeRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(activeRec, activeReq)

	if activeRec.Code != http.StatusOK {
		t.Fatalf("expected member active loan status 200, got %d: %s", activeRec.Code, activeRec.Body.String())
	}
	var activeLoan struct {
		ID                 string `json:"id"`
		ApprovedAmount     int    `json:"approved_amount"`
		DurationMonths     int    `json:"duration_months"`
		MonthlyInstallment int    `json:"monthly_installment"`
		RemainingBalance   int    `json:"remaining_balance"`
		Status             string `json:"status"`
	}
	if err := json.Unmarshal(activeRec.Body.Bytes(), &activeLoan); err != nil {
		t.Fatalf("decode member active loan: %v", err)
	}
	if activeLoan.ID != loan.ID || activeLoan.ApprovedAmount != 1200000 || activeLoan.MonthlyInstallment != 200000 || activeLoan.RemainingBalance != 1200000 || activeLoan.Status != "active" {
		t.Fatalf("unexpected member active loan: %+v", activeLoan)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/admin/loans?status=active", nil)
	adminReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminRec, adminReq)

	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin active loan list status 200, got %d: %s", adminRec.Code, adminRec.Body.String())
	}
	var adminLoans struct {
		Loans []struct {
			ID             string `json:"id"`
			MemberNo       string `json:"member_no"`
			FullName       string `json:"full_name"`
			ApprovedAmount int    `json:"approved_amount"`
			Status         string `json:"status"`
		} `json:"loans"`
	}
	if err := json.Unmarshal(adminRec.Body.Bytes(), &adminLoans); err != nil {
		t.Fatalf("decode admin active loans: %v", err)
	}
	if len(adminLoans.Loans) != 1 || adminLoans.Loans[0].ID != loan.ID || adminLoans.Loans[0].MemberNo != "M-0022" || adminLoans.Loans[0].FullName != "Approved Borrower" || adminLoans.Loans[0].ApprovedAmount != 1200000 || adminLoans.Loans[0].Status != "active" {
		t.Fatalf("unexpected admin active loans: %+v", adminLoans.Loans)
	}
}

func TestLoanApprovalValidationAndConflicts(t *testing.T) {
	t.Run("amount and duration must be positive", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0023","full_name":"Approval Rules","join_date":"2026-06-16","status":"active","email":"approval-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "approval-rules@coop.test", "member-password")
		requestID := fixture.createLoanRequest(t, memberToken, 1000000, 5)
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":0,"duration_months":0}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Approved amount and duration months must be greater than zero")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
			t.Fatalf("expected validation failure to create no loans, got %+v", loans)
		}
	})

	t.Run("approved amount cannot exceed requested amount", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0023","full_name":"Approval Rules","join_date":"2026-06-16","status":"active","email":"approval-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "approval-rules@coop.test", "member-password")
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+fixture.pendingLoanRequestID(t, memberToken)+"/approve", bytes.NewBufferString(`{"approved_amount":1500000,"duration_months":5}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Approved amount cannot exceed requested amount")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
			t.Fatalf("expected exceeded amount failure to create no loans, got %+v", loans)
		}
	})

	t.Run("approval is pending only and one active loan per member", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0023","full_name":"Approval Rules","join_date":"2026-06-16","status":"active","email":"approval-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "approval-rules@coop.test", "member-password")
		requestID := fixture.pendingLoanRequestID(t, memberToken)
		firstLoan := fixture.approveLoanRequest(t, adminToken, requestID, 800000, 4)

		reapproveReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":800000,"duration_months":4}`))
		reapproveReq.Header.Set("Content-Type", "application/json")
		reapproveReq.Header.Set("Authorization", "Bearer "+adminToken)
		reapproveRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(reapproveRec, reapproveReq)

		if reapproveRec.Code != http.StatusBadRequest {
			t.Fatalf("expected reapproval status 400, got %d: %s", reapproveRec.Code, reapproveRec.Body.String())
		}
		assertError(t, reapproveRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be approved")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].ID != firstLoan.ID {
			t.Fatalf("expected reapproval failure to keep one active loan, got %+v", loans)
		}

		secondRequestID := fixture.createLoanRequest(t, memberToken, 600000, 3)
		conflictReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+secondRequestID+"/approve", bytes.NewBufferString(`{"approved_amount":600000,"duration_months":3}`))
		conflictReq.Header.Set("Content-Type", "application/json")
		conflictReq.Header.Set("Authorization", "Bearer "+adminToken)
		conflictRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(conflictRec, conflictReq)

		if conflictRec.Code != http.StatusBadRequest {
			t.Fatalf("expected active loan conflict status 400, got %d: %s", conflictRec.Code, conflictRec.Body.String())
		}
		assertError(t, conflictRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Member already has an active loan")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].ID != firstLoan.ID {
			t.Fatalf("expected active loan conflict to keep one active loan, got %+v", loans)
		}
	})
}

func TestLoanApprovalPagesRenderReviewAndActiveLoanViews(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0024","full_name":"Approval Page","join_date":"2026-06-16","status":"active","email":"approval-page@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "approval-page@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 1800000, 9)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	reviewReq := httptest.NewRequest(http.MethodGet, "/admin/loan-requests", nil)
	reviewReq.AddCookie(adminCookie)
	reviewRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(reviewRec, reviewReq)

	if reviewRec.Code != http.StatusOK {
		t.Fatalf("expected review page status 200, got %d: %s", reviewRec.Code, reviewRec.Body.String())
	}
	reviewBody := reviewRec.Body.String()
	for _, text := range []string{"/api/admin/loan-requests/" + requestID + "/approve", `name="approved_amount"`, `name="duration_months"`, "Approve"} {
		if !strings.Contains(reviewBody, text) {
			t.Fatalf("expected review page to include %q, got %s", text, reviewBody)
		}
	}

	fixture.approveLoanRequest(t, adminToken, requestID, 900000, 9)

	memberCookie := fixture.browserLogin(t, "approval-page@coop.test", "member-password")
	profileReq := httptest.NewRequest(http.MethodGet, "/member/profile", nil)
	profileReq.AddCookie(memberCookie)
	profileRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected member profile status 200, got %d: %s", profileRec.Code, profileRec.Body.String())
	}
	profileBody := profileRec.Body.String()
	for _, text := range []string{"Active loan", "900000", "100000", "active"} {
		if !strings.Contains(profileBody, text) {
			t.Fatalf("expected member profile to include %q, got %s", text, profileBody)
		}
	}

	adminLoansReq := httptest.NewRequest(http.MethodGet, "/admin/loans", nil)
	adminLoansReq.AddCookie(adminCookie)
	adminLoansRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminLoansRec, adminLoansReq)

	if adminLoansRec.Code != http.StatusOK {
		t.Fatalf("expected admin loans page status 200, got %d: %s", adminLoansRec.Code, adminLoansRec.Body.String())
	}
	adminLoansBody := adminLoansRec.Body.String()
	for _, text := range []string{"Active loans", "table-scroll", member.FullName, member.MemberNo, "900000", "100000", "active"} {
		if !strings.Contains(adminLoansBody, text) {
			t.Fatalf("expected admin loans page to include %q, got %s", text, adminLoansBody)
		}
	}
}

func TestAdminCanRejectLoanRequestWithReason(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0025","full_name":"Rejected Borrower","join_date":"2026-06-16","status":"active","email":"rejected-borrower@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "rejected-borrower@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 1400000, 7)

	rejected := fixture.rejectLoanRequest(t, adminToken, requestID, "Cash flow does not support installment")
	if rejected.ID != requestID || rejected.Status != "rejected" || rejected.RejectionReason != "Cash flow does not support installment" {
		t.Fatalf("unexpected rejected request: %+v", rejected)
	}
	if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
		t.Fatalf("expected rejected request to create no active loans, got %+v", loans)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/member/loan-requests", nil)
	historyReq.Header.Set("Authorization", "Bearer "+memberToken)
	historyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected member loan request history status 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}
	var history struct {
		LoanRequests []struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			RejectionReason string `json:"rejection_reason"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode loan request history: %v", err)
	}
	if len(history.LoanRequests) != 1 || history.LoanRequests[0].ID != requestID || history.LoanRequests[0].Status != "rejected" || history.LoanRequests[0].RejectionReason != "Cash flow does not support installment" {
		t.Fatalf("expected member-visible rejected request, got %+v", history.LoanRequests)
	}
}

func TestLoanRejectionValidationAndReviewedRequestRules(t *testing.T) {
	t.Run("reason is required", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0026","full_name":"Reject Rules","join_date":"2026-06-16","status":"active","email":"reject-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "reject-rules@coop.test", "member-password")
		requestID := fixture.createLoanRequest(t, memberToken, 1000000, 5)

		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/reject", bytes.NewBufferString(`{"rejection_reason":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Rejection reason is required")
	})

	t.Run("only pending requests can be rejected", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0026","full_name":"Reject Rules","join_date":"2026-06-16","status":"active","email":"reject-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "reject-rules@coop.test", "member-password")
		requestID := fixture.createLoanRequest(t, memberToken, 1000000, 5)
		fixture.approveLoanRequest(t, adminToken, requestID, 800000, 4)

		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/reject", bytes.NewBufferString(`{"rejection_reason":"Reviewed after approval"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be rejected")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 {
			t.Fatalf("expected rejected approved request to leave existing loan only, got %+v", loans)
		}
	})

	t.Run("already rejected request cannot be approved or rejected again", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0026","full_name":"Reject Rules","join_date":"2026-06-16","status":"active","email":"reject-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "reject-rules@coop.test", "member-password")
		requestID := fixture.createLoanRequest(t, memberToken, 1000000, 5)
		fixture.rejectLoanRequest(t, adminToken, requestID, "Does not meet policy")

		approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":800000,"duration_months":4}`))
		approveReq.Header.Set("Content-Type", "application/json")
		approveReq.Header.Set("Authorization", "Bearer "+adminToken)
		approveRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(approveRec, approveReq)

		if approveRec.Code != http.StatusBadRequest {
			t.Fatalf("expected approval status 400, got %d: %s", approveRec.Code, approveRec.Body.String())
		}
		assertError(t, approveRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be approved")

		rejectReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/reject", bytes.NewBufferString(`{"rejection_reason":"Second reason"}`))
		rejectReq.Header.Set("Content-Type", "application/json")
		rejectReq.Header.Set("Authorization", "Bearer "+adminToken)
		rejectRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rejectRec, rejectReq)

		if rejectRec.Code != http.StatusBadRequest {
			t.Fatalf("expected rejection status 400, got %d: %s", rejectRec.Code, rejectRec.Body.String())
		}
		assertError(t, rejectRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Only pending loan requests can be rejected")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
			t.Fatalf("expected repeatedly reviewed request to create no loans, got %+v", loans)
		}
	})
}

func TestLoanRejectionPageRendersReasonForm(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0027","full_name":"Reject Page","join_date":"2026-06-16","status":"active","email":"reject-page@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "reject-page@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 1200000, 6)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	reviewReq := httptest.NewRequest(http.MethodGet, "/admin/loan-requests", nil)
	reviewReq.AddCookie(adminCookie)
	reviewRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(reviewRec, reviewReq)

	if reviewRec.Code != http.StatusOK {
		t.Fatalf("expected review page status 200, got %d: %s", reviewRec.Code, reviewRec.Body.String())
	}
	reviewBody := reviewRec.Body.String()
	for _, text := range []string{"/api/admin/loan-requests/" + requestID + "/reject", `name="rejection_reason"`, "Reject"} {
		if !strings.Contains(reviewBody, text) {
			t.Fatalf("expected review page to include %q, got %s", text, reviewBody)
		}
	}
}

func TestAdminCanRecordRepaymentAndMemberCanViewHistory(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0028","full_name":"Repayment Member","join_date":"2026-06-16","status":"active","email":"repayment-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "repayment-member@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 1000000, 5), 1000000, 5)

	repayment := fixture.recordRepayment(t, adminToken, loan.ID, 250000)
	if repayment.ID == "" || repayment.LoanID != loan.ID || repayment.MemberID != loan.MemberID || repayment.Amount != 250000 || repayment.RecordDate != "2026-06-16" || repayment.ReferenceNo != "RPY-TEST" || repayment.Note != "Test repayment" {
		t.Fatalf("unexpected repayment: %+v", repayment)
	}

	activeReq := httptest.NewRequest(http.MethodGet, "/api/member/loans/active", nil)
	activeReq.Header.Set("Authorization", "Bearer "+memberToken)
	activeRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(activeRec, activeReq)

	if activeRec.Code != http.StatusOK {
		t.Fatalf("expected member active loan status 200, got %d: %s", activeRec.Code, activeRec.Body.String())
	}
	var activeLoan struct {
		ID               string `json:"id"`
		RemainingBalance int    `json:"remaining_balance"`
		Status           string `json:"status"`
	}
	if err := json.Unmarshal(activeRec.Body.Bytes(), &activeLoan); err != nil {
		t.Fatalf("decode active loan: %v", err)
	}
	if activeLoan.ID != loan.ID || activeLoan.RemainingBalance != 750000 || activeLoan.Status != "active" {
		t.Fatalf("expected updated active loan balance, got %+v", activeLoan)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/member/repayments", nil)
	historyReq.Header.Set("Authorization", "Bearer "+memberToken)
	historyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected repayment history status 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}
	var history struct {
		Repayments []testRepayment `json:"repayments"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode repayment history: %v", err)
	}
	if len(history.Repayments) != 1 || history.Repayments[0].ID != repayment.ID || history.Repayments[0].Amount != 250000 {
		t.Fatalf("unexpected repayment history: %+v", history.Repayments)
	}
}

func TestRepaymentValidationBalanceAndPaidTransition(t *testing.T) {
	t.Run("amount must be positive", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0029","full_name":"Repayment Rules","join_date":"2026-06-16","status":"active","email":"repayment-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "repayment-rules@coop.test", "member-password")
		loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)

		req := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loan.ID+"/repayments", bytes.NewBufferString(`{"amount":0,"record_date":"2026-06-16"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Repayment amount and record date are required")
		if repayments := fixture.memberRepayments(t, memberToken); len(repayments) != 0 {
			t.Fatalf("expected invalid repayment to create no records, got %+v", repayments)
		}
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].RemainingBalance != 500000 {
			t.Fatalf("expected invalid repayment to keep loan balance, got %+v", loans)
		}
	})

	t.Run("overpayment is rejected without changing balance", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0029","full_name":"Repayment Rules","join_date":"2026-06-16","status":"active","email":"repayment-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "repayment-rules@coop.test", "member-password")
		loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)

		req := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loan.ID+"/repayments", bytes.NewBufferString(`{"amount":600000,"record_date":"2026-06-16"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Repayment amount cannot exceed remaining loan balance")
		if repayments := fixture.memberRepayments(t, memberToken); len(repayments) != 0 {
			t.Fatalf("expected overpayment to create no records, got %+v", repayments)
		}
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].RemainingBalance != 500000 {
			t.Fatalf("expected overpayment to keep loan balance, got %+v", loans)
		}
	})

	t.Run("full repayment marks loan paid", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0029","full_name":"Repayment Rules","join_date":"2026-06-16","status":"active","email":"repayment-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "repayment-rules@coop.test", "member-password")
		loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)

		fixture.recordRepayment(t, adminToken, loan.ID, 500000)

		activeReq := httptest.NewRequest(http.MethodGet, "/api/member/loans/active", nil)
		activeReq.Header.Set("Authorization", "Bearer "+memberToken)
		activeRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(activeRec, activeReq)

		if activeRec.Code != http.StatusNotFound {
			t.Fatalf("expected paid loan to no longer be active, got %d: %s", activeRec.Code, activeRec.Body.String())
		}
		allLoans := fixture.loansByStatus(t, adminToken, "")
		if len(allLoans) != 1 || allLoans[0].ID != loan.ID || allLoans[0].RemainingBalance != 0 || allLoans[0].Status != "paid" {
			t.Fatalf("expected paid loan with zero balance, got %+v", allLoans)
		}
	})
}

func TestRepaymentHistoryIsMemberIsolatedAndPagesRender(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0030","full_name":"First Borrower","join_date":"2026-06-16","status":"active","email":"first-borrower@coop.test","password":"member-password"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0031","full_name":"Second Borrower","join_date":"2026-06-16","status":"active","email":"second-borrower@coop.test","password":"member-password"}`)
	firstToken := fixture.login(t, "first-borrower@coop.test", "member-password")
	secondToken := fixture.login(t, "second-borrower@coop.test", "member-password")
	firstLoan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, firstToken, 800000, 4), 800000, 4)
	fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, secondToken, 600000, 3), 600000, 3)
	fixture.recordRepayment(t, adminToken, firstLoan.ID, 200000)

	firstHistory := fixture.memberRepayments(t, firstToken)
	if len(firstHistory) != 1 || firstHistory[0].Amount != 200000 {
		t.Fatalf("expected first borrower repayment history, got %+v", firstHistory)
	}
	if secondHistory := fixture.memberRepayments(t, secondToken); len(secondHistory) != 0 {
		t.Fatalf("expected second borrower not to see first repayment, got %+v", secondHistory)
	}

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	adminLoansReq := httptest.NewRequest(http.MethodGet, "/admin/loans", nil)
	adminLoansReq.AddCookie(adminCookie)
	adminLoansRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminLoansRec, adminLoansReq)

	if adminLoansRec.Code != http.StatusOK {
		t.Fatalf("expected admin loans page status 200, got %d: %s", adminLoansRec.Code, adminLoansRec.Body.String())
	}
	adminLoansBody := adminLoansRec.Body.String()
	for _, text := range []string{"/api/admin/loans/" + firstLoan.ID + "/repayments", `name="record_date"`, `name="reference_no"`, "Record repayment", "600000"} {
		if !strings.Contains(adminLoansBody, text) {
			t.Fatalf("expected admin loans page to include %q, got %s", text, adminLoansBody)
		}
	}

	firstCookie := fixture.browserLogin(t, "first-borrower@coop.test", "member-password")
	profileReq := httptest.NewRequest(http.MethodGet, "/member/profile", nil)
	profileReq.AddCookie(firstCookie)
	profileRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected member profile status 200, got %d: %s", profileRec.Code, profileRec.Body.String())
	}
	profileBody := profileRec.Body.String()
	for _, text := range []string{"Repayment history", "200000", "RPY-TEST", "Test repayment", "600000"} {
		if !strings.Contains(profileBody, text) {
			t.Fatalf("expected member profile to include %q, got %s", text, profileBody)
		}
	}
}

func TestAdminRepaymentsMenuLinksToActiveRepaymentsPage(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0037","full_name":"Repayment Menu","join_date":"2026-06-16","status":"active","email":"repayment-menu@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "repayment-menu@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)
	fixture.recordRepayment(t, adminToken, loan.ID, 100000)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	loansReq := httptest.NewRequest(http.MethodGet, "/admin/loans", nil)
	loansReq.AddCookie(adminCookie)
	loansRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loansRec, loansReq)

	if loansRec.Code != http.StatusOK {
		t.Fatalf("expected loans page status 200, got %d: %s", loansRec.Code, loansRec.Body.String())
	}
	if body := loansRec.Body.String(); !strings.Contains(body, `href="/admin/repayments"`) || strings.Contains(body, `title="Repayments"><i class="sidebar-icon" data-lucide="receipt"></i><span class="sidebar-label">Repayments</span></span>`) {
		t.Fatalf("expected repayments sidebar item to be an active link, got %s", body)
	}

	repaymentsReq := httptest.NewRequest(http.MethodGet, "/admin/repayments", nil)
	repaymentsReq.AddCookie(adminCookie)
	repaymentsRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(repaymentsRec, repaymentsReq)

	if repaymentsRec.Code != http.StatusOK {
		t.Fatalf("expected repayments page status 200, got %d: %s", repaymentsRec.Code, repaymentsRec.Body.String())
	}
	repaymentsBody := repaymentsRec.Body.String()
	for _, text := range []string{`class="sidebar-link active" href="/admin/repayments"`, "table-scroll", "Repayment Menu", "M-0037", "100000", "RPY-TEST"} {
		if !strings.Contains(repaymentsBody, text) {
			t.Fatalf("expected repayments page to include %q, got %s", text, repaymentsBody)
		}
	}
}

func TestAdminDashboardAggregatesOperationalTotals(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	active := fixture.createMember(t, adminToken, `{"member_no":"M-0032","full_name":"Dashboard Active","join_date":"2026-06-16","status":"active","email":"dashboard-active@coop.test","password":"member-password"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0033","full_name":"Dashboard Inactive","join_date":"2026-06-16","status":"inactive"}`)
	memberToken := fixture.login(t, "dashboard-active@coop.test", "member-password")
	fixture.recordSaving(t, adminToken, active.ID, "deposit", 1000000, "DASH-DEP", "Dashboard deposit")
	fixture.recordSaving(t, adminToken, active.ID, "withdrawal", 200000, "DASH-WD", "Dashboard withdrawal")
	activeLoan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 600000, 6), 600000, 6)
	fixture.recordRepayment(t, adminToken, activeLoan.ID, 150000)
	fixture.createLoanRequest(t, memberToken, 300000, 3)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var summary struct {
		TotalMembers         int `json:"total_members"`
		ActiveMembers        int `json:"active_members"`
		TotalSavings         int `json:"total_savings"`
		ActiveLoans          int `json:"active_loans"`
		TotalOutstandingLoan int `json:"total_outstanding_loan"`
		PendingLoanRequests  int `json:"pending_loan_requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode dashboard summary: %v", err)
	}
	if summary.TotalMembers != 2 || summary.ActiveMembers != 1 || summary.TotalSavings != 800000 || summary.ActiveLoans != 1 || summary.TotalOutstandingLoan != 450000 || summary.PendingLoanRequests != 1 {
		t.Fatalf("unexpected dashboard summary: %+v", summary)
	}
}

func TestMemberDashboardIsIsolatedAndIncludesLatestActivity(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	first := fixture.createMember(t, adminToken, `{"member_no":"M-0034","full_name":"Dashboard First","join_date":"2026-06-16","status":"active","email":"dashboard-first@coop.test","password":"member-password"}`)
	second := fixture.createMember(t, adminToken, `{"member_no":"M-0035","full_name":"Dashboard Second","join_date":"2026-06-16","status":"active","email":"dashboard-second@coop.test","password":"member-password"}`)
	firstToken := fixture.login(t, "dashboard-first@coop.test", "member-password")
	secondToken := fixture.login(t, "dashboard-second@coop.test", "member-password")
	fixture.recordSaving(t, adminToken, first.ID, "deposit", 700000, "FIRST-DEP", "First deposit")
	fixture.recordSaving(t, adminToken, second.ID, "deposit", 900000, "SECOND-DEP", "Second deposit")
	firstLoan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, firstToken, 500000, 5), 500000, 5)
	fixture.recordRepayment(t, adminToken, firstLoan.ID, 100000)

	req := httptest.NewRequest(http.MethodGet, "/api/member/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+firstToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected member dashboard status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashboard struct {
		SavingBalance        int       `json:"saving_balance"`
		RemainingLoanBalance int       `json:"remaining_loan_balance"`
		ActiveLoan           *testLoan `json:"active_loan"`
		LatestSavings        []struct {
			MemberID    string `json:"member_id"`
			ReferenceNo string `json:"reference_no"`
		} `json:"latest_savings"`
		LatestRepayments []testRepayment `json:"latest_repayments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dashboard); err != nil {
		t.Fatalf("decode member dashboard: %v", err)
	}
	if dashboard.SavingBalance != 700000 || dashboard.RemainingLoanBalance != 400000 || dashboard.ActiveLoan == nil || dashboard.ActiveLoan.ID != firstLoan.ID {
		t.Fatalf("unexpected member dashboard balances: %+v", dashboard)
	}
	if len(dashboard.LatestSavings) != 1 || dashboard.LatestSavings[0].MemberID != first.ID || dashboard.LatestSavings[0].ReferenceNo != "FIRST-DEP" {
		t.Fatalf("expected isolated latest savings, got %+v", dashboard.LatestSavings)
	}
	if len(dashboard.LatestRepayments) != 1 || dashboard.LatestRepayments[0].MemberID != first.ID {
		t.Fatalf("expected isolated latest repayments, got %+v", dashboard.LatestRepayments)
	}

	secondDashboardReq := httptest.NewRequest(http.MethodGet, "/api/member/dashboard", nil)
	secondDashboardReq.Header.Set("Authorization", "Bearer "+secondToken)
	secondDashboardRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(secondDashboardRec, secondDashboardReq)

	if secondDashboardRec.Code != http.StatusOK {
		t.Fatalf("expected second member dashboard status 200, got %d: %s", secondDashboardRec.Code, secondDashboardRec.Body.String())
	}
	if body := secondDashboardRec.Body.String(); strings.Contains(body, "FIRST-DEP") || strings.Contains(body, firstLoan.ID) {
		t.Fatalf("expected second dashboard not to expose first member data, got %s", body)
	}
}

func TestDashboardPagesRenderRealAndEmptyStates(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	emptyReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	emptyReq.AddCookie(adminCookie)
	emptyRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(emptyRec, emptyReq)

	if emptyRec.Code != http.StatusOK {
		t.Fatalf("expected empty dashboard page status 200, got %d: %s", emptyRec.Code, emptyRec.Body.String())
	}
	for _, text := range []string{"Total members", "Active loans", "Pending requests", ">0</strong>"} {
		if !strings.Contains(emptyRec.Body.String(), text) {
			t.Fatalf("expected empty dashboard page to include %q, got %s", text, emptyRec.Body.String())
		}
	}

	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-0036","full_name":"Dashboard Page","join_date":"2026-06-16","status":"active","email":"dashboard-page@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "dashboard-page@coop.test", "member-password")
	fixture.recordDeposit(t, adminToken, member.ID, 400000)
	fixture.createLoanRequest(t, memberToken, 200000, 2)

	pageReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	pageReq.AddCookie(adminCookie)
	pageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard page status 200, got %d: %s", pageRec.Code, pageRec.Body.String())
	}
	for _, text := range []string{"Dashboard", "400000", "Pending requests", ">1</strong>"} {
		if !strings.Contains(pageRec.Body.String(), text) {
			t.Fatalf("expected dashboard page to include %q, got %s", text, pageRec.Body.String())
		}
	}
}

type testFixture struct {
	server http.Handler
	db     *sql.DB
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return newTestFixture(t).server
}

func newTestFixture(t *testing.T) testFixture {
	t.Helper()
	return newTestFixtureWithConfig(t, app.Config{
		JWTSecret: "0123456789abcdef0123456789abcdef",
	})
}

func newTestFixtureWithConfig(t *testing.T, cfg app.Config) testFixture {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := app.Migrate(db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if err := app.EnsureAdminUser(db, "admin@coop.test", "password"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	seedUser(t, db, "member-user-id", "member@coop.test", "password", "member")

	return testFixture{server: app.NewServer(cfg, db), db: db}
}

func (f testFixture) login(t *testing.T, email, password string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"`+email+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if response.Token == "" {
		t.Fatal("expected token")
	}
	return response.Token
}

func (f testFixture) browserLogin(t *testing.T, email, password string) *http.Cookie {
	t.Helper()

	body := strings.NewReader("email=" + strings.ReplaceAll(email, "@", "%40") + "&password=" + password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected browser login status 303, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "auth_token" {
			return cookie
		}
	}
	t.Fatal("expected auth_token cookie")
	return nil
}

func (f testFixture) createMember(t *testing.T, adminToken, body string) struct {
	ID       string `json:"id"`
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	Status   string `json:"status"`
} {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create member status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created struct {
		ID       string `json:"id"`
		MemberNo string `json:"member_no"`
		FullName string `json:"full_name"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create member response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected created member id")
	}
	return created
}

func (f testFixture) createMemberUser(t *testing.T, adminToken, memberID, email, password string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members/"+memberID+"/user", bytes.NewBufferString(`{"email":"`+email+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected member user status 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func (f testFixture) recordDeposit(t *testing.T, adminToken, memberID string, amount int) string {
	t.Helper()
	return f.recordSaving(t, adminToken, memberID, "deposit", amount, "TRF-PAGE", "Page deposit")
}

func (f testFixture) recordSaving(t *testing.T, adminToken, memberID, recordType string, amount int, referenceNo, note string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
		"member_id":"`+memberID+`",
		"type":"`+recordType+`",
		"amount":`+strconv.Itoa(amount)+`,
		"record_date":"2026-06-16",
		"reference_no":"`+referenceNo+`",
		"note":"`+note+`"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected deposit status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode deposit response: %v", err)
	}
	return response.ID
}

func (f testFixture) createLoanRequest(t *testing.T, memberToken string, amount, durationMonths int) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{
		"requested_amount":`+strconv.Itoa(amount)+`,
		"duration_months":`+strconv.Itoa(durationMonths)+`,
		"purpose":"Test loan"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected loan request status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode loan request response: %v", err)
	}
	return response.ID
}

type testLoan struct {
	ID                 string `json:"id"`
	LoanRequestID      string `json:"loan_request_id"`
	MemberID           string `json:"member_id"`
	MemberNo           string `json:"member_no"`
	FullName           string `json:"full_name"`
	ApprovedAmount     int    `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int    `json:"monthly_installment"`
	RemainingBalance   int    `json:"remaining_balance"`
	Status             string `json:"status"`
}

type testRepayment struct {
	ID          string `json:"id"`
	LoanID      string `json:"loan_id"`
	MemberID    string `json:"member_id"`
	Amount      int    `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
}

func (f testFixture) approveLoanRequest(t *testing.T, adminToken, requestID string, approvedAmount, durationMonths int) testLoan {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{
		"approved_amount":`+strconv.Itoa(approvedAmount)+`,
		"duration_months":`+strconv.Itoa(durationMonths)+`
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected approve loan request status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response testLoan
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode loan approval response: %v", err)
	}
	return response
}

func (f testFixture) rejectLoanRequest(t *testing.T, adminToken, requestID, reason string) struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	RejectionReason string `json:"rejection_reason"`
} {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/reject", bytes.NewBufferString(`{"rejection_reason":"`+reason+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected reject loan request status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		RejectionReason string `json:"rejection_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode loan rejection response: %v", err)
	}
	return response
}

func (f testFixture) activeLoans(t *testing.T, adminToken string) []testLoan {
	t.Helper()
	return f.loansByStatus(t, adminToken, "active")
}

func (f testFixture) loansByStatus(t *testing.T, adminToken, status string) []testLoan {
	t.Helper()

	path := "/api/admin/loans"
	if status != "" {
		path += "?status=" + status
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected active loans status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Loans []testLoan `json:"loans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode active loans response: %v", err)
	}
	return response.Loans
}

func (f testFixture) recordRepayment(t *testing.T, adminToken, loanID string, amount int) testRepayment {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loanID+"/repayments", bytes.NewBufferString(`{
		"amount":`+strconv.Itoa(amount)+`,
		"record_date":"2026-06-16",
		"reference_no":"RPY-TEST",
		"note":"Test repayment"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected repayment status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response testRepayment
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode repayment response: %v", err)
	}
	return response
}

func (f testFixture) memberRepayments(t *testing.T, memberToken string) []testRepayment {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/member/repayments", nil)
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected member repayment history status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Repayments []testRepayment `json:"repayments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode member repayment history: %v", err)
	}
	return response.Repayments
}

func (f testFixture) pendingLoanRequestID(t *testing.T, memberToken string) string {
	t.Helper()
	return f.createLoanRequest(t, memberToken, 1000000, 5)
}

func seedUser(t *testing.T, db *sql.DB, id, email, password, role string) {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, $4)`,
		id,
		email,
		string(hash),
		role,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func assertError(t *testing.T, data []byte, code, message string) {
	t.Helper()

	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Error.Code != code || response.Error.Message != message {
		t.Fatalf("expected error %s %q, got %+v", code, message, response.Error)
	}
}
