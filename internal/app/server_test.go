package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	if loginResponse.User.Email != "admin@coop.test" || loginResponse.User.Role != "manager" {
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
		TotalMembers         int   `json:"total_members"`
		ActiveMembers        int   `json:"active_members"`
		TotalSavings         int   `json:"total_savings"`
		ActiveLoans          int   `json:"active_loans"`
		TotalOutstandingLoan int64 `json:"total_outstanding_loan"`
		PendingLoanRequests  int   `json:"pending_loan_requests"`
	}
	if err := json.Unmarshal(dashboardRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
	if summary.TotalMembers != 5 || summary.ActiveMembers != 5 || summary.TotalSavings != 0 || summary.ActiveLoans != 0 || summary.TotalOutstandingLoan != 0 || summary.PendingLoanRequests != 0 {
		t.Fatalf("expected only the five fixture Members and no financial activity, got %+v", summary)
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

func TestBrowserLanguageSelectionLocalizesLoginAndHtmxErrors(t *testing.T) {
	fixture := newTestFixture(t)

	defaultReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	defaultRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(defaultRec, defaultReq)
	if defaultRec.Code != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d: %s", defaultRec.Code, defaultRec.Body.String())
	}
	if body := defaultRec.Body.String(); !strings.Contains(body, "Language") || !strings.Contains(body, "Log in") || strings.Contains(body, "Kata sandi") {
		t.Fatalf("expected English default login page, got %s", body)
	}

	langCookie := fixture.setLanguage(t, "id", "/login")
	localizedReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	localizedReq.AddCookie(langCookie)
	localizedRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(localizedRec, localizedReq)
	if localizedRec.Code != http.StatusOK {
		t.Fatalf("expected localized login page status 200, got %d: %s", localizedRec.Code, localizedRec.Body.String())
	}
	if body := localizedRec.Body.String(); !strings.Contains(body, "Bahasa") || !strings.Contains(body, "Kata sandi") || !strings.Contains(body, "Masuk") {
		t.Fatalf("expected Bahasa login page, got %s", body)
	}

	failReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("email=admin%40coop.test&password=wrong"))
	failReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	failReq.Header.Set("HX-Request", "true")
	failReq.AddCookie(langCookie)
	failRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected localized login failure status 401, got %d: %s", failRec.Code, failRec.Body.String())
	}
	if body := failRec.Body.String(); body != `<span class="form-error-message">Email atau kata sandi tidak valid</span>` {
		t.Fatalf("expected localized login error fragment, got %s", body)
	}
}

func TestBahasaRenderingForAuthenticatedAdminAndMemberPages(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-LANG","full_name":"Bahasa Member","join_date":"2026-06-18","status":"active","email":"bahasa-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "bahasa-member@coop.test", "member-password")
	fixture.recordSaving(t, adminToken, member.ID, "deposit", 250000, "LANG-DEP", "Bahasa deposit")
	fixture.createLoanRequest(t, memberToken, 1000000, 5)
	langCookie := fixture.setLanguage(t, "id", "/admin/dashboard")

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	adminReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	adminReq.AddCookie(adminCookie)
	adminReq.AddCookie(langCookie)
	adminRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected localized admin dashboard status 200, got %d: %s", adminRec.Code, adminRec.Body.String())
	}
	if body := adminRec.Body.String(); !strings.Contains(body, "Dasbor") || !strings.Contains(body, "Total anggota") || !strings.Contains(body, "Simpanan") || !strings.Contains(body, "Keluar") {
		t.Fatalf("expected Bahasa admin dashboard, got %s", body)
	}

	memberCookie := fixture.browserLogin(t, "bahasa-member@coop.test", "member-password")
	memberReq := httptest.NewRequest(http.MethodGet, "/member/loan-requests", nil)
	memberReq.AddCookie(memberCookie)
	memberReq.AddCookie(langCookie)
	memberRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(memberRec, memberReq)
	if memberRec.Code != http.StatusOK {
		t.Fatalf("expected localized member loan request page status 200, got %d: %s", memberRec.Code, memberRec.Body.String())
	}
	if body := memberRec.Body.String(); !strings.Contains(body, "Permintaan pinjaman") || !strings.Contains(body, "Ajukan permintaan pinjaman") || !strings.Contains(body, "Riwayat permintaan") || !strings.Contains(body, "Pilih jenis pinjaman") || !strings.Contains(body, "Menunggu") {
		t.Fatalf("expected Bahasa member loan request page, got %s", body)
	}
}

func TestMigrateTracksAppliedVersionsAndIsRepeatable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := app.Migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := app.Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var migrationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 17 {
		t.Fatalf("expected seventeen tracked migrations, got %d", migrationCount)
	}

	var latestName string
	if err := db.QueryRow(`SELECT name FROM schema_migrations WHERE version = 17`).Scan(&latestName); err != nil {
		t.Fatalf("read latest migration: %v", err)
	}
	if latestName != "add_member_type" {
		t.Fatalf("expected latest Member Type migration, got %q", latestName)
	}

	if _, err := db.Exec(`INSERT INTO members (id, member_no, full_name, join_date, status) VALUES ('migrate-member', 'M-MIGRATE', 'Migrated Member', '2026-06-18', 'active')`); err != nil {
		t.Fatalf("expected migrated members table to be usable: %v", err)
	}
	var memberType string
	if err := db.QueryRow(`SELECT member_type FROM members WHERE id='migrate-member'`).Scan(&memberType); err != nil {
		t.Fatalf("read migrated Member Type: %v", err)
	}
	if memberType != "employee" {
		t.Fatalf("expected default Member Type employee, got %q", memberType)
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

func TestResponsesIncludeRequestID(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected health status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("expected generated request id response header")
	}
}

func TestResponsesPreserveIncomingRequestID(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "external-request-id")
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "external-request-id" {
		t.Fatalf("expected incoming request id to be preserved, got %q", got)
	}
}

func TestReadinessChecksDatabase(t *testing.T) {
	fixture := newTestFixture(t)

	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected ready status 200, got %d: %s", readyRec.Code, readyRec.Body.String())
	}

	if err := fixture.db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	unreadyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	unreadyRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(unreadyRec, unreadyReq)
	if unreadyRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unready status 503, got %d: %s", unreadyRec.Code, unreadyRec.Body.String())
	}
}

func TestMetricsEndpointExposesHTTPMetrics(t *testing.T) {
	fixture := newTestFixtureWithConfig(t, app.Config{
		JWTSecret:      "0123456789abcdef0123456789abcdef",
		ServiceName:    "kopdes-test",
		ServiceVersion: "test",
		MetricsEnabled: true,
	})

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(healthRec, healthReq)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(metricsRec, metricsReq)

	if metricsRec.Code != http.StatusOK {
		t.Fatalf("expected metrics status 200, got %d: %s", metricsRec.Code, metricsRec.Body.String())
	}
	body := metricsRec.Body.String()
	for _, expected := range []string{
		"http_requests_total",
		`service="kopdes-test"`,
		`version="test"`,
		`route="/health"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected metrics body to contain %q, got %s", expected, body)
		}
	}
}

func TestTracingEnabledKeepsRoutesServing(t *testing.T) {
	fixture := newTestFixtureWithConfig(t, app.Config{
		JWTSecret:      "0123456789abcdef0123456789abcdef",
		ServiceName:    "kopdes-test",
		ServiceVersion: "test",
		TracingEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected traced health status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("expected traced response to include request id")
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

func TestBrowserPageInvalidAuthTokenRedirectsToLogin(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: "not-a-token"})
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status 303, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "auth_token" || cookies[0].MaxAge != -1 {
		t.Fatalf("expected invalid auth cookie to be cleared, got %+v", cookies)
	}
}

func TestAPIInvalidCookieAuthTokenStillReturnsJSONUnauthorized(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: "not-a-token"})
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
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
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RACE-SAV","full_name":"Saving Race","join_date":"2026-06-17","status":"active","email":"saving-race@coop.test","password":"secret-password"}`)
	memberToken := fixture.login(t, "saving-race@coop.test", "secret-password")
	fixture.recordDeposit(t, adminToken, member.ID, 100000)

	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{"amount":80000}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+memberToken)
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
	if balance != 100000 {
		t.Fatalf("expected financial balance unchanged before final approval, got %d", balance)
	}
	var reserved int
	if err := fixture.db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM withdrawal_reservations WHERE member_id=$1 AND status='active'`, member.ID).Scan(&reserved); err != nil || reserved != 80000 {
		t.Fatalf("expected one 80000 reservation, got %d err=%v", reserved, err)
	}
}

func TestConcurrentLoanRequestSubmissionsCreateOnlyOnePendingRequest(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-RACE-LOAN","full_name":"Loan Race","join_date":"2026-06-17","status":"active"}`)
	fixture.createMemberUser(t, adminToken, member.ID, "loan-race@coop.test", "secret-password")
	memberToken := fixture.login(t, "loan-race@coop.test", "secret-password")
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":500000,"duration_months":5,"purpose":"Working capital","loan_type":"regular"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+memberToken)
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
		t.Fatalf("expected one request created and one rejected, got created=%d rejected=%d", created, rejected)
	}

	var pendingRequests int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_requests WHERE member_id = $1 AND status = 'pending'`, member.ID).Scan(&pendingRequests); err != nil {
		t.Fatalf("query pending requests: %v", err)
	}
	if pendingRequests != 1 {
		t.Fatalf("expected one pending request, got %d", pendingRequests)
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
	if remainingBalance != 25000 {
		t.Fatalf("expected remaining balance 25000 including admin fee, got %d", remainingBalance)
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
		if _, err := fixture.db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('member-profile-id','AUTH-MEMBER','Auth Member','2026-01-01','active')`); err != nil {
			t.Fatalf("seed member profile: %v", err)
		}
		if _, err := fixture.db.Exec(`UPDATE users SET member_id='member-profile-id' WHERE id='member-user-id'`); err != nil {
			t.Fatalf("link member profile: %v", err)
		}
		memberToken := fixture.login(t, "member@coop.test", "password")
		req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient permission")
	})
}

func TestBrowserPageWithInsufficientRoleRedirectsToLogin(t *testing.T) {
	fixture := newTestFixture(t)
	memberCookie := fixture.browserLogin(t, "member@coop.test", "password")

	t.Run("regular browser request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		req.AddCookie(memberCookie)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		if cookies := rec.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("expected authenticated Member cookie to be retained, got %+v", cookies)
		}
	})

	t.Run("htmx browser request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(memberCookie)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
		}
		if redirect := rec.Header().Get("HX-Redirect"); redirect != "" {
			t.Fatalf("expected no login redirect for an authenticated Member, got %q", redirect)
		}
	})
}

func TestBrowserAPIInteractionsRedirectExpiredSessionsToLogin(t *testing.T) {
	fixture := newTestFixture(t)

	t.Run("htmx form submission", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/members", strings.NewReader("member_no=M-EXPIRED"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent || rec.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("expected HTMX redirect to login, got status=%d redirect=%q body=%s", rec.Code, rec.Header().Get("HX-Redirect"), rec.Body.String())
		}
	})

	t.Run("download navigation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/exports/loans.csv", nil)
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("expected download navigation redirect to login, got status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
	})
}

func TestStandardBrowserFormsRedirectOrRenderHTML(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	t.Run("successful submission redirects", func(t *testing.T) {
		body := "member_no=M-FORM-001&full_name=Form+Member&join_date=2026-07-13&status=active&email=form-member%40coop.test&password=member-password"
		req := httptest.NewRequest(http.MethodPost, "/api/admin/members", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		req.AddCookie(adminCookie)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/members/") {
			t.Fatalf("expected member page redirect, got status=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
	})

	t.Run("failed submission renders localized page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/members", strings.NewReader("member_no="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Referer", "http://example.com/admin/members/new")
		req.AddCookie(adminCookie)
		req.AddCookie(&http.Cookie{Name: "kopdes_lang", Value: "id"})
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("expected HTML validation page, got status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, "Permintaan tidak dapat diselesaikan") || !strings.Contains(body, `href="/admin/members/new"`) {
			t.Fatalf("expected localized error page with safe return link, got %s", body)
		}
	})
}

func TestUnknownBrowserRouteRendersLocalizedErrorPage(t *testing.T) {
	fixture := newTestFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "kopdes_lang", Value: "id"})
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected HTML 404 page, got status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Halaman yang diminta tidak ditemukan") || !strings.Contains(body, "<!doctype html>") {
		t.Fatalf("expected localized browser error page instead of JSON, got %s", body)
	}
}

func TestUnknownOrUnsupportedAPIRouteKeepsJSONContract(t *testing.T) {
	fixture := newTestFixture(t)
	tests := []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{name: "unknown route", method: http.MethodGet, path: "/api/does-not-exist", status: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "unsupported method", method: http.MethodDelete, path: "/api/auth/login", status: http.StatusMethodNotAllowed, code: "METHOD_NOT_ALLOWED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()

			fixture.server.ServeHTTP(rec, req)

			if rec.Code != tt.status || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("expected JSON status %d, got status=%d content-type=%q body=%s", tt.status, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			}
			assertError(t, rec.Body.Bytes(), tt.code, map[string]string{"NOT_FOUND": "Page not found", "METHOD_NOT_ALLOWED": "Method not allowed"}[tt.code])
		})
	}
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
	for _, text := range []string{`data-lucide="layout-dashboard"`, `data-lucide="users"`, `data-lucide="piggy-bank"`, "Admin menu", "Toggle sidebar", "Logout", "Dashboard", "Members", "Total members", "Pending requests", "Recent activity", "Quick actions", "Balance report"} {
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
	if redirect := loginRec.Header().Get("HX-Redirect"); redirect != "/member/dashboard" {
		t.Fatalf("expected HX-Redirect to /member/dashboard, got %q", redirect)
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
	for _, text := range []string{"--primary: #0056b3", "--warning: #ffc107", "--negative: #dc2626", ".public-header", ".public-hero", ".public-feature-card", ".auth-visual", "linear-gradient(180deg, #4e73df", ".admin-topbar", ".summary-card", ".dashboard-home-grid", ".dashboard-quick-list", ".brand-logo", ".sidebar-group-label", ".status-pending", "@media (max-width: 760px)", ".admin-sidebar", "overflow-x: auto", ".page-shell", ".summary-grid", "grid-template-columns: repeat(2, minmax(0, 1fr))", ".inline-approval-form", ".inline-repayment-form", ".review-modal:target", ".review-modal-card", ".table-scroll td:last-child"} {
		if !strings.Contains(css, text) {
			t.Fatalf("expected css to include %q, got %s", text, css)
		}
	}
	for _, text := range []string{".balance-report-card-header {\n  min-height: 60px;", ".balance-report-card-header h2 {\n  color: var(--primary);", ".profit-card-header,\n.profit-insights-header {\n  min-height: 52px;", ".profit-tabs-header {\n  display: flex;", ".profit-tabs-header span.active {\n  background: var(--primary-pale);"} {
		if !strings.Contains(css, text) {
			t.Fatalf("expected normalized report component css to include %q, got %s", text, css)
		}
	}
}

func TestRootRedirectsToLoginWithoutSessionAndDashboardWithSession(t *testing.T) {
	fixture := newTestFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected root without session to redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected root without session to redirect to /login, got %q", location)
	}

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	authReq := httptest.NewRequest(http.MethodGet, "/", nil)
	authReq.AddCookie(adminCookie)
	authRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(authRec, authReq)

	if authRec.Code != http.StatusSeeOther {
		t.Fatalf("expected root with admin session to redirect, got %d: %s", authRec.Code, authRec.Body.String())
	}
	if location := authRec.Header().Get("Location"); location != "/member/dashboard" {
		t.Fatalf("expected root with admin session to redirect to dashboard, got %q", location)
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
	for _, text := range []string{`src="/static/vendor/htmx-2.0.10.min.js"`, `src="/static/vendor/lucide-0.468.0.min.js"`, `src="/static/images/lambang-koperasi.png"`, `class="brand-logo"`, `class="auth-visual"`} {
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

	logoReq := httptest.NewRequest(http.MethodGet, "/static/images/lambang-koperasi.png", nil)
	logoRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(logoRec, logoReq)

	if logoRec.Code != http.StatusOK {
		t.Fatalf("expected logo status 200, got %d: %s", logoRec.Code, logoRec.Body.String())
	}
	if contentType := logoRec.Header().Get("Content-Type"); !strings.Contains(contentType, "image/png") {
		t.Fatalf("expected logo content type image/png, got %q", contentType)
	}
	if logoRec.Body.Len() == 0 {
		t.Fatal("expected logo to return asset content")
	}
}

func TestBrowserPagesRenderSharedLayoutsAndHtmxForms(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0100","full_name":"Render Member","join_date":"2026-06-16","status":"active","email":"render-member@coop.test","password":"member-password"}`)

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	memberCookie := fixture.browserLogin(t, "render-member@coop.test", "member-password")

	tests := []struct {
		name     string
		path     string
		cookie   *http.Cookie
		contains []string
	}{
		{
			name:   "admin member create page",
			path:   "/admin/members/new",
			cookie: adminCookie,
			contains: []string{
				`<aside class="admin-sidebar" aria-label="Admin menu">`,
				`src="/static/images/lambang-koperasi.png"`,
				`src="/static/vendor/htmx-2.0.10.min.js"`,
				`src="/static/vendor/lucide-0.468.0.min.js"`,
				`hx-post="/api/admin/members"`,
				`hx-target="#member-form-error"`,
				`id="member-form-error" class="form-error"`,
				`name="email"`,
				`name="password"`,
			},
		},
		{
			name:   "member loan request page",
			path:   "/member/loan-requests",
			cookie: memberCookie,
			contains: []string{
				`class="member-shell member-loan-requests-shell"`,
				`src="/static/images/lambang-koperasi.png"`,
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
		"status":"active",
		"email":"siti@coop.test",
		"password":"member-password"
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
	if len(response.Members) != 6 {
		t.Fatalf("expected five fixture Members and one created Member, got %+v", response.Members)
	}
	var listed bool
	for _, member := range response.Members {
		if member.ID == created.ID && member.MemberNo == "M-0002" && member.FullName == "Budi Santoso" && member.Status == "inactive" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("created Member missing from list: %+v", response.Members)
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
	if summary.TotalMembers != 7 || summary.ActiveMembers != 6 {
		t.Fatalf("unexpected member counts: %+v", summary)
	}
}

func TestCreateMemberRejectsDuplicateMemberNumber(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0003","full_name":"Dewi Lestari","join_date":"2026-06-15","status":"active"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{"member_no":"M-0003","full_name":"Dewi Other","join_date":"2026-06-16","status":"active","email":"dewi-other@coop.test","password":"member-password"}`))
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

	req := httptest.NewRequest(http.MethodPost, "/api/admin/members", bytes.NewBufferString(`{"member_no":"M-0004","full_name":"Rina","join_date":"2026-06-15","status":"archived","email":"rina@coop.test","password":"member-password"}`))
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
	if _, err := fixture.db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('member-role-id','ROLE-MEMBER','Role Member','2026-01-01','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`UPDATE users SET member_id='member-role-id' WHERE id='member-user-id'`); err != nil {
		t.Fatal(err)
	}
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
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient permission")
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
	for _, text := range []string{`data-lucide="layout-dashboard"`, `data-lucide="users"`, `data-lucide="piggy-bank"`, "Admin menu", "Toggle sidebar", "Logout", "Dashboard", "Members", `href="/admin/members/new"`, "Create member", "table-scroll", "Agus Wijaya", "M-0006", "Ditangguhkan"} {
		if !strings.Contains(listBody, text) {
			t.Fatalf("expected members page to include %q, got %s", text, listBody)
		}
	}
	for _, text := range []string{`hx-post="/api/admin/members"`, `name="email"`, `name="password"`} {
		if strings.Contains(listBody, text) {
			t.Fatalf("expected members list page not to include create form marker %q, got %s", text, listBody)
		}
	}

	createReq := httptest.NewRequest(http.MethodGet, "/admin/members/new", nil)
	createReq.AddCookie(adminCookie)
	createRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusOK {
		t.Fatalf("expected member create page status 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	createBody := createRec.Body.String()
	for _, text := range []string{"Create member", "Member login", `hx-post="/api/admin/members"`, `name="email"`, `name="password"`} {
		if !strings.Contains(createBody, text) {
			t.Fatalf("expected member create page to include %q, got %s", text, createBody)
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
	for _, text := range []string{"Admin menu", "Members", "Member detail", "Agus Wijaya", "M-0006", "2026-06-15", "Saving balance", "No saving records yet.", "No loan requests yet.", "No active loan.", "No repayment records yet."} {
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
	memberToken := fixture.login(t, "member@coop.test", "password")

	t.Run("Officer can use Member profile route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("session is invalidated when User loses Member identity", func(t *testing.T) {
		if _, err := fixture.db.Exec(`UPDATE users SET member_id=NULL WHERE email='member@coop.test'`); err != nil {
			t.Fatalf("unlink Member identity: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
		req.Header.Set("Authorization", "Bearer "+memberToken)
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

	loginBody := strings.NewReader("email=browser-member%40coop.test&password=secret-password")
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected member browser login redirect status 303, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	if location := loginRec.Header().Get("Location"); location != "/member/dashboard" {
		t.Fatalf("expected member browser login to redirect to dashboard, got %q", location)
	}

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
		"category":"wajib",
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
		Category    string `json:"category"`
		Amount      int    `json:"amount"`
		RecordDate  string `json:"record_date"`
		ReferenceNo string `json:"reference_no"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal(depositRec.Body.Bytes(), &deposit); err != nil {
		t.Fatalf("decode deposit response: %v", err)
	}
	if deposit.ID == "" || deposit.MemberID != member.ID || deposit.Type != "deposit" || deposit.Category != "wajib" || deposit.Amount != 500000 || deposit.RecordDate != "2026-06-16" || deposit.ReferenceNo != "TRF-001" || deposit.Note != "Initial saving" {
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
			Category    string `json:"category"`
			Amount      int    `json:"amount"`
			RecordDate  string `json:"record_date"`
			ReferenceNo string `json:"reference_no"`
			Note        string `json:"note"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode saving history: %v", err)
	}
	if len(history.Savings) != 1 || history.Savings[0].ID != deposit.ID || history.Savings[0].Amount != 500000 || history.Savings[0].Type != "deposit" || history.Savings[0].Category != "wajib" {
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
		PokokBalance    int `json:"pokok_balance"`
		WajibBalance    int `json:"wajib_balance"`
		SukarelaBalance int `json:"sukarela_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode saving summary: %v", err)
	}
	if summary.TotalDeposit != 500000 || summary.TotalWithdrawal != 0 || summary.CurrentBalance != 500000 || summary.PokokBalance != 0 || summary.WajibBalance != 500000 || summary.SukarelaBalance != 0 {
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
		req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{"member_id":"`+inactive.ID+`","type":"deposit","category":"sukarela","amount":0,"record_date":"2026-06-16"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Member, category, amount, and record date are required")
	})

	t.Run("member must be active", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{"member_id":"`+inactive.ID+`","type":"deposit","category":"sukarela","amount":100000,"record_date":"2026-06-16"}`))
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
	if body := rec.Body.String(); body != `<span class="form-error-message">Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807</span>` {
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

	requestID := fixture.createWithdrawalRequest(t, memberToken, 125000, "Cash withdrawal")
	fixture.approveWithdrawalRequest(t, adminToken, requestID)
	var withdrawalID string
	if err := fixture.db.QueryRow(`SELECT saving_record_id FROM withdrawal_requests WHERE id=$1`, requestID).Scan(&withdrawalID); err != nil {
		t.Fatalf("read approved saving record: %v", err)
	}

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
		PokokBalance    int `json:"pokok_balance"`
		WajibBalance    int `json:"wajib_balance"`
		SukarelaBalance int `json:"sukarela_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TotalDeposit != 300000 || summary.TotalWithdrawal != 125000 || summary.CurrentBalance != 175000 || summary.SukarelaBalance != 175000 {
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
			ID       string `json:"id"`
			Type     string `json:"type"`
			Category string `json:"category"`
			Amount   int    `json:"amount"`
			Note     string `json:"note"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	var sawWithdrawal bool
	for _, record := range history.Savings {
		if record.ID == withdrawalID && record.Type == "withdrawal" && record.Category == "sukarela" && record.Amount == 125000 && record.Note == "Cash withdrawal" {
			sawWithdrawal = true
		}
	}
	if !sawWithdrawal {
		t.Fatalf("expected withdrawal in history, got %+v", history.Savings)
	}

	overReq := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{"amount":200000,"note":"Too much"}`))
	overReq.Header.Set("Content-Type", "application/json")
	overReq.Header.Set("Authorization", "Bearer "+memberToken)
	overRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(overRec, overReq)

	if overRec.Code != http.StatusBadRequest {
		t.Fatalf("expected over-withdrawal status 400, got %d: %s", overRec.Code, overRec.Body.String())
	}
	assertError(t, overRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed Simpanan Sukarela balance")

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
		PokokBalance    int `json:"pokok_balance"`
		WajibBalance    int `json:"wajib_balance"`
		SukarelaBalance int `json:"sukarela_balance"`
	}
	if err := json.Unmarshal(summaryAfterRejectRec.Body.Bytes(), &summaryAfterReject); err != nil {
		t.Fatalf("decode summary after reject: %v", err)
	}
	if summaryAfterReject.TotalDeposit != 300000 || summaryAfterReject.TotalWithdrawal != 125000 || summaryAfterReject.CurrentBalance != 175000 || summaryAfterReject.SukarelaBalance != 175000 {
		t.Fatalf("expected rejected withdrawal to leave balance unchanged, got %+v", summaryAfterReject)
	}
}

func TestSavingPagesRenderDepositFormAndMemberBalance(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	emptyAdminPageReq := httptest.NewRequest(http.MethodGet, "/admin/savings", nil)
	emptyAdminPageReq.AddCookie(adminCookie)
	emptyAdminPageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(emptyAdminPageRec, emptyAdminPageReq)

	if emptyAdminPageRec.Code != http.StatusOK {
		t.Fatalf("expected empty admin savings page status 200, got %d: %s", emptyAdminPageRec.Code, emptyAdminPageRec.Body.String())
	}
	if body := emptyAdminPageRec.Body.String(); !strings.Contains(body, "Saving records") || !strings.Contains(body, `href="/admin/savings/new"`) || strings.Contains(body, `hx-post="/api/admin/savings"`) {
		t.Fatalf("expected savings list page to show records and create link without insert form, got %s", body)
	}

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
	for _, text := range []string{"Saving records", `href="/admin/savings/new"`, "Saving Page Member", "Simpanan Sukarela", "TRF-PAGE", "deposit", "Apply filters"} {
		if !strings.Contains(adminBody, text) {
			t.Fatalf("expected admin savings page to include %q, got %s", text, adminBody)
		}
	}
	for _, text := range []string{`hx-post="/api/admin/savings"`, `data-saving-submit`, `name="amount"`} {
		if strings.Contains(adminBody, text) {
			t.Fatalf("expected admin savings list page not to include insert form marker %q, got %s", text, adminBody)
		}
	}

	newSavingReq := httptest.NewRequest(http.MethodGet, "/admin/savings/new", nil)
	newSavingReq.AddCookie(adminCookie)
	newSavingRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(newSavingRec, newSavingReq)

	if newSavingRec.Code != http.StatusOK {
		t.Fatalf("expected new saving page status 200, got %d: %s", newSavingRec.Code, newSavingRec.Body.String())
	}
	newSavingBody := newSavingRec.Body.String()
	for _, text := range []string{"Record saving", "Saving Page Member", "Simpanan Pokok", "Simpanan Wajib", "Simpanan Sukarela", `hx-post="/api/admin/savings"`, `name="category"`, "deposit", "withdrawal", `name="amount"`, `name="record_date"`, `data-saving-submit`, `value="`} {
		if !strings.Contains(newSavingBody, text) {
			t.Fatalf("expected new saving page to include %q, got %s", text, newSavingBody)
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
	for _, text := range []string{"Saving balance", "750.000", "Simpanan Sukarela", "Saving history", "table-scroll", "TRF-PAGE", "Page deposit"} {
		if !strings.Contains(profileBody, text) {
			t.Fatalf("expected member profile page to include %q, got %s", text, profileBody)
		}
	}
}

func TestAdminCanFilterSimpananHistoryByCategory(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-FILTER-1","full_name":"Filter One","join_date":"2026-06-16","status":"active"}`)
	other := fixture.createMember(t, adminToken, `{"member_no":"M-FILTER-2","full_name":"Filter Two","join_date":"2026-06-16","status":"active"}`)
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 100000, "WJB-001", "Wajib saving")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 200000, "SUK-001", "Sukarela saving")
	fixture.recordSavingInCategory(t, adminToken, other.ID, "deposit", "wajib", 300000, "WJB-OTHER", "Other wajib saving")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/savings?member_id="+member.ID+"&category=wajib", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin savings filter status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Savings []struct {
			MemberID    string `json:"member_id"`
			MemberNo    string `json:"member_no"`
			FullName    string `json:"full_name"`
			Type        string `json:"type"`
			Category    string `json:"category"`
			Amount      int    `json:"amount"`
			ReferenceNo string `json:"reference_no"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode admin savings filter: %v", err)
	}
	if len(response.Savings) != 1 || response.Savings[0].MemberID != member.ID || response.Savings[0].MemberNo != "M-FILTER-1" || response.Savings[0].FullName != "Filter One" || response.Savings[0].Type != "deposit" || response.Savings[0].Category != "wajib" || response.Savings[0].Amount != 100000 || response.Savings[0].ReferenceNo != "WJB-001" {
		t.Fatalf("unexpected filtered savings: %+v", response.Savings)
	}

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	pageReq := httptest.NewRequest(http.MethodGet, "/admin/savings?member_id="+member.ID+"&category=wajib", nil)
	pageReq.AddCookie(adminCookie)
	pageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("expected admin savings page status 200, got %d: %s", pageRec.Code, pageRec.Body.String())
	}
	pageBody := pageRec.Body.String()
	for _, text := range []string{"Saving records", "Filter One", "M-FILTER-1", "Simpanan Wajib", "WJB-001", `option value="wajib" selected`} {
		if !strings.Contains(pageBody, text) {
			t.Fatalf("expected filtered admin savings page to include %q, got %s", text, pageBody)
		}
	}
	for _, text := range []string{"SUK-001", "WJB-OTHER"} {
		if strings.Contains(pageBody, text) {
			t.Fatalf("expected filtered admin savings page not to include %q, got %s", text, pageBody)
		}
	}
}

func TestAdminCanExportFilteredSimpananCSV(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-EXP-1","full_name":"Export One","join_date":"2026-06-16","status":"active"}`)
	other := fixture.createMember(t, adminToken, `{"member_no":"M-EXP-2","full_name":"Export Two","join_date":"2026-06-16","status":"active"}`)
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 100000, "EXP-WJB", "Export wajib")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 200000, "EXP-SUK", "Export sukarela")
	fixture.recordSavingInCategory(t, adminToken, other.ID, "deposit", "wajib", 300000, "EXP-OTHER", "Other export")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/exports/savings.csv?member_id="+member.ID+"&category=wajib", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected simpanan export status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("expected CSV content type, got %q", contentType)
	}
	if disposition := rec.Header().Get("Content-Disposition"); !strings.Contains(disposition, "simpanan-export.csv") {
		t.Fatalf("expected stable simpanan export filename, got %q", disposition)
	}
	body := rec.Body.String()
	for _, text := range []string{"member_no,member,Member type,category,type,amount,date,reference_no,note,recorded_by", "M-EXP-1,Export One,Karyawan,wajib,deposit,100000,2026-06-16,EXP-WJB,Export wajib"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected simpanan export to include %q, got %s", text, body)
		}
	}
	for _, text := range []string{"EXP-SUK", "EXP-OTHER"} {
		if strings.Contains(body, text) {
			t.Fatalf("expected filtered simpanan export not to include %q, got %s", text, body)
		}
	}
}

func TestAdminCanExportPinjamanAndAngsuranCSV(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-LOAN-EXP","full_name":"Loan Export","join_date":"2026-06-16","status":"active","email":"loan-export@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "loan-export@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 900000, 9), 900000, 9)
	fixture.recordRepayment(t, adminToken, loan.ID, 100000)

	loanReq := httptest.NewRequest(http.MethodGet, "/api/admin/exports/loans.csv?status=active", nil)
	loanReq.Header.Set("Authorization", "Bearer "+adminToken)
	loanRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(loanRec, loanReq)

	if loanRec.Code != http.StatusOK {
		t.Fatalf("expected pinjaman export status 200, got %d: %s", loanRec.Code, loanRec.Body.String())
	}
	loanBody := loanRec.Body.String()
	for _, text := range []string{"member_no,member,Member type,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_at,start_date,admin_fee_policy,monthly_admin_fee,total_admin_fee,total_obligation,next_due_date,final_due_date", "M-LOAN-EXP,Loan Export,Karyawan,900000,9,109000,881000,active,", "regular_tiered_monthly_v1,9000,81000,981000"} {
		if !strings.Contains(loanBody, text) {
			t.Fatalf("expected pinjaman export to include %q, got %s", text, loanBody)
		}
	}

	repaymentReq := httptest.NewRequest(http.MethodGet, "/api/admin/exports/repayments.csv", nil)
	repaymentReq.Header.Set("Authorization", "Bearer "+adminToken)
	repaymentRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(repaymentRec, repaymentReq)

	if repaymentRec.Code != http.StatusOK {
		t.Fatalf("expected angsuran export status 200, got %d: %s", repaymentRec.Code, repaymentRec.Body.String())
	}
	repaymentBody := repaymentRec.Body.String()
	for _, text := range []string{"member_no,member,Member type,loan_id,amount,date,reference_no,note", "M-LOAN-EXP,Loan Export,Karyawan," + loan.ID + ",100000,2026-06-16,RPY-TEST,Test repayment"} {
		if !strings.Contains(repaymentBody, text) {
			t.Fatalf("expected angsuran export to include %q, got %s", text, repaymentBody)
		}
	}
}

func TestAdminCanExportFilteredPenarikanCSV(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-WD-EXP","full_name":"Withdrawal Export","join_date":"2026-06-16","status":"active","email":"wd-export@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "wd-export@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 500000, "WD-EXP-SAV", "Export balance")
	fixture.createWithdrawalRequest(t, memberToken, 100000, "Pending export")
	rejectedID := fixture.createWithdrawalRequest(t, memberToken, 125000, "Rejected export")

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/admin/withdrawal-requests/"+rejectedID+"/reject", bytes.NewBufferString(`{"rejection_reason":"Missing approval"}`))
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectReq.Header.Set("Authorization", "Bearer "+adminToken)
	rejectRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected withdrawal reject status 200, got %d: %s", rejectRec.Code, rejectRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/exports/withdrawal-requests.csv?status=pending", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected penarikan export status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("expected CSV content type, got %q", contentType)
	}
	if disposition := rec.Header().Get("Content-Disposition"); !strings.Contains(disposition, "penarikan-export.csv") {
		t.Fatalf("expected stable penarikan export filename, got %q", disposition)
	}
	body := rec.Body.String()
	for _, text := range []string{"member_no,member,Member type,amount,status,requested_at,reviewed_at,note,review_note,saving_record_id", "M-WD-EXP,Withdrawal Export,Karyawan,100000,pending"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected penarikan export to include %q, got %s", text, body)
		}
	}
	if !strings.Contains(body, "Pending export") {
		t.Fatalf("expected pending export row, got %s", body)
	}
	for _, text := range []string{"Rejected export", "Missing approval"} {
		if strings.Contains(body, text) {
			t.Fatalf("expected filtered penarikan export not to include %q, got %s", text, body)
		}
	}
}

func TestAdminReportsRenderOperationalChartsAndEmptyStates(t *testing.T) {
	emptyFixture := newTestFixture(t)
	emptyCookie := emptyFixture.browserLogin(t, "admin@coop.test", "password")
	emptyReq := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	emptyReq.AddCookie(emptyCookie)
	emptyRec := httptest.NewRecorder()

	emptyFixture.server.ServeHTTP(emptyRec, emptyReq)

	if emptyRec.Code != http.StatusOK {
		t.Fatalf("expected empty reports page status 200, got %d: %s", emptyRec.Code, emptyRec.Body.String())
	}
	if body := emptyRec.Body.String(); !strings.Contains(body, "Reports") || !strings.Contains(body, "No report data yet.") || !strings.Contains(body, "Simpanan by category") {
		t.Fatalf("expected empty reports page to render chart empty states, got %s", body)
	}

	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-CHART","full_name":"Chart Member","join_date":"2026-06-16","status":"active","email":"chart-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "chart-member@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "pokok", 50000, "CH-POK", "Pokok")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 150000, "CH-WAJ", "Wajib")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 300000, "CH-SUK", "Sukarela")
	fixture.createWithdrawalRequest(t, memberToken, 100000, "Chart withdrawal")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 600000, 6), 600000, 6)
	fixture.recordRepayment(t, adminToken, loan.ID, 200000)

	req := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected reports page status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{"Simpanan by category", "Penarikan by status", "Pinjaman exposure", "Angsuran progress", "Simpanan report", "Penarikan report", "Pinjaman report", "Angsuran report", "Chart Member", "M-CHART", "Simpanan Pokok", "50.000", "Simpanan Wajib", "150.000", "Simpanan Sukarela", "300.000", "Menunggu", "Approved principal", "600.000", "Remaining balance", "436.000", "Actual repayment", "200.000"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected reports page to include %q, got %s", text, body)
		}
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	dashboardReq.AddCookie(adminCookie)
	dashboardRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(dashboardRec, dashboardReq)

	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("expected dashboard page status 200, got %d: %s", dashboardRec.Code, dashboardRec.Body.String())
	}
	if dashboardBody := dashboardRec.Body.String(); !strings.Contains(dashboardBody, "Perbandingan Simpanan &amp; Pinjaman") || !strings.Contains(dashboardBody, "Neraca Trend (6 Months)") || !strings.Contains(dashboardBody, `class="line-chart-path chart-line-simpanan"`) {
		t.Fatalf("expected dashboard page to include operational charts, got %s", dashboardBody)
	}
}

func TestAdminBalanceReportRendersOperationalBalance(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-BAL","full_name":"Balance Member","join_date":"2026-06-16","status":"active","email":"balance-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "balance-member@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "pokok", 100000, "BAL-POK", "Pokok")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 200000, "BAL-WAJ", "Wajib")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 300000, "BAL-SUK", "Sukarela")
	fixture.createWithdrawalRequest(t, memberToken, 50000, "Pending balance withdrawal")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 240000, 6), 240000, 6)
	fixture.recordRepayment(t, adminToken, loan.ID, 40000)

	req := httptest.NewRequest(http.MethodGet, "/admin/reports/balance", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected balance report status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{`<h1>Balance report</h1>`, `href="/admin/reports/balance" title="Balance report"`, `<span class="sidebar-group-label">Reports</span>`, "Financial balance report", "Export CSV", "Export PDF", "Financial health indicator", "Liability to asset ratio", "Total assets", "Total liabilities", "Total equity", "Balance detail", "Assets", "Cash (in - out)", "Loan receivable", "Liabilities", "Member savings", "Equity", "TOTAL ASSETS = TOTAL LIABILITIES &#43; EQUITY", "Information", "Composition", "Rp 764.400", "Rp 600.000", "Rp 214.400", "Rp 164.400"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected balance report to include %q, got %s", text, body)
		}
	}
	if strings.Contains(body, `name="date_from"`) {
		t.Fatal("balance report must not render a filter that is not applied to its calculations")
	}
}

func TestAdminProfitLossReportMimicsKopkarlytaReport(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-PL","full_name":"Profit Loss Member","join_date":"2026-06-16","status":"active","email":"profit-loss-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "profit-loss-member@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "pokok", 100000, "PL-POK", "Pendapatan simpanan")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 300000, "PL-SUK", "Pendapatan sukarela")
	withdrawalID := fixture.createWithdrawalRequest(t, memberToken, 75000, "Biaya penarikan")
	fixture.approveWithdrawalRequest(t, adminToken, withdrawalID)
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 240000, 6), 240000, 6)
	fixture.recordRepayment(t, adminToken, loan.ID, 40000)

	req := httptest.NewRequest(http.MethodGet, "/admin/reports/profit-loss", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected profit/loss report status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, text := range []string{`href="/admin/reports/profit-loss" title="Profit/loss report"`, `<h1>Profit/loss report</h1>`, `<span class="sidebar-group-label">Reports</span>`, `name="date_from"`, `name="date_to"`, "Apply filters", "Reset", "Total income", "Total cost", "Net profit", "Data export", "Export CSV", "Export PDF", "Print report", "Income detail", "Cost detail", "Monthly breakdown", "Income breakdown", "Cost breakdown", "Insights &amp; analysis", "Financial composition", "Monthly performance", "Rp 440.000", "Rp 75.000", "Rp 365.000"} {
		if !strings.Contains(body, text) {
			t.Fatalf("expected profit/loss report to include %q, got %s", text, body)
		}
	}
}

func TestAdminReportExportsReturnRealDownloadFormats(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	tests := []struct {
		path        string
		contentType string
		filename    string
		prefix      string
		contains    string
	}{
		{path: "/admin/reports/balance?export=csv", contentType: "text/csv; charset=utf-8", filename: `filename="balance-report.csv"`, contains: "metric,amount"},
		{path: "/admin/reports/balance?export=pdf", contentType: "application/pdf", filename: `filename="balance-report.pdf"`, prefix: "%PDF-1.4"},
		{path: "/admin/reports/profit-loss?export=csv", contentType: "text/csv; charset=utf-8", filename: `filename="profit-loss-report.csv"`, contains: "period_start,period_end,total_income"},
		{path: "/admin/reports/profit-loss?export=pdf", contentType: "application/pdf", filename: `filename="profit-loss-report.pdf"`, prefix: "%PDF-1.4"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.AddCookie(adminCookie)
			rec := httptest.NewRecorder()
			fixture.server.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tt.contentType {
				t.Fatalf("expected content type %q, got %q", tt.contentType, got)
			}
			if disposition := rec.Header().Get("Content-Disposition"); !strings.Contains(disposition, tt.filename) {
				t.Fatalf("expected download filename %q, got %q", tt.filename, disposition)
			}
			body := rec.Body.String()
			if tt.prefix != "" && !strings.HasPrefix(body, tt.prefix) {
				t.Fatalf("expected body prefix %q, got %q", tt.prefix, body[:min(len(body), 20)])
			}
			if tt.contains != "" && !strings.Contains(body, tt.contains) {
				t.Fatalf("expected body to contain %q, got %q", tt.contains, body)
			}
		})
	}
}

func TestProfitLossPeriodMatchesAllIncludedActivity(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-PERIOD","full_name":"Period Member","join_date":"2025-01-01","status":"active"}`)
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "pokok", 120000, "PERIOD-OLD", "Historical activity")
	if _, err := fixture.db.Exec(`UPDATE saving_records SET record_date = '2025-01-15' WHERE reference_no = 'PERIOD-OLD'`); err != nil {
		t.Fatalf("move saving into historical period: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/reports/profit-loss", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected report status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	now := time.Now()
	months := (now.Year()-2025)*12 + int(now.Month()-time.January) + 1
	body := rec.Body.String()
	for _, expected := range []string{
		"Period: 15/01/2025 - " + now.Format("02/01/2006"),
		"Monthly average:",
		"Rp " + dottedTestNominal(120000/months),
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected period-aware report to include %q, got %s", expected, body)
		}
	}
}

func TestProfitLossPeriodFilterChangesTotalsAndExports(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-PL-FILTER","full_name":"Profit Filter Member","join_date":"2026-01-01","status":"active","email":"profit-filter@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "profit-filter@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "pokok", 100000, "PLF-OLD", "Old income")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 250000, "PLF-NEW", "Filtered income")
	withdrawalID := fixture.createWithdrawalRequest(t, memberToken, 50000, "Filtered cost")
	fixture.approveWithdrawalRequest(t, adminToken, withdrawalID)
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 300000, 6), 300000, 6)
	fixture.recordRepayment(t, adminToken, loan.ID, 75000)
	if _, err := fixture.db.Exec(`UPDATE saving_records SET record_date = '2026-01-10' WHERE reference_no = 'PLF-OLD'`); err != nil {
		t.Fatalf("move old saving out of selected period: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE saving_records SET record_date = '2026-02-10' WHERE reference_no = 'PLF-NEW' OR id=(SELECT saving_record_id FROM withdrawal_requests WHERE id=$1)`, withdrawalID); err != nil {
		t.Fatalf("move saving records into selected period: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE loan_repayments SET record_date = '2026-02-11' WHERE loan_id = $1`, loan.ID); err != nil {
		t.Fatalf("move repayment into selected period: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/reports/profit-loss?date_from=2026-02-01&date_to=2026-02-28", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected filtered report status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, expected := range []string{
		"Period: 01/02/2026 - 28/02/2026",
		`value="2026-02-01"`,
		`value="2026-02-28"`,
		`href="/admin/reports/profit-loss?date_from=2026-02-01&amp;date_to=2026-02-28&amp;export=csv"`,
		"Rp 325.000",
		"Rp 50.000",
		"Rp 275.000",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected filtered profit/loss report to include %q, got %s", expected, body)
		}
	}
	if strings.Contains(body, "Rp 425.000") {
		t.Fatalf("expected filtered report to exclude old income, got %s", body)
	}

	csvReq := httptest.NewRequest(http.MethodGet, "/admin/reports/profit-loss?date_from=2026-02-01&date_to=2026-02-28&export=csv", nil)
	csvReq.AddCookie(adminCookie)
	csvRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(csvRec, csvReq)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("expected filtered csv status 200, got %d: %s", csvRec.Code, csvRec.Body.String())
	}
	if body := csvRec.Body.String(); !strings.Contains(body, "01/02/2026,28/02/2026,325000,50000,275000") {
		t.Fatalf("expected filtered CSV to include selected totals, got %s", body)
	}
}

func TestMemberCanRequestPenarikanAndAdminApproveCreatesSukarelaWithdrawal(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-WD-REQ","full_name":"Withdrawal Request","join_date":"2026-06-16","status":"active","email":"wd-request@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "wd-request@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 300000, "SUK-WD", "Sukarela balance")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 500000, "WJB-WD", "Wajib balance")

	createReq := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{"amount":125000,"note":"Need cash"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+memberToken)
	createRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected withdrawal request status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID       string `json:"id"`
		MemberID string `json:"member_id"`
		Amount   int    `json:"amount"`
		Note     string `json:"note"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode withdrawal request: %v", err)
	}
	if created.ID == "" || created.MemberID != member.ID || created.Amount != 125000 || created.Note != "Need cash" || created.Status != "pending" {
		t.Fatalf("unexpected withdrawal request: %+v", created)
	}

	adminListReq := httptest.NewRequest(http.MethodGet, "/api/admin/withdrawal-requests?status=pending", nil)
	adminListReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminListRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminListRec, adminListReq)

	if adminListRec.Code != http.StatusOK {
		t.Fatalf("expected admin withdrawal request list status 200, got %d: %s", adminListRec.Code, adminListRec.Body.String())
	}
	var adminList struct {
		WithdrawalRequests []struct {
			ID       string `json:"id"`
			MemberNo string `json:"member_no"`
			FullName string `json:"full_name"`
			Amount   int    `json:"amount"`
			Status   string `json:"status"`
		} `json:"withdrawal_requests"`
	}
	if err := json.Unmarshal(adminListRec.Body.Bytes(), &adminList); err != nil {
		t.Fatalf("decode admin withdrawal requests: %v", err)
	}
	if len(adminList.WithdrawalRequests) != 1 || adminList.WithdrawalRequests[0].ID != created.ID || adminList.WithdrawalRequests[0].MemberNo != "M-WD-REQ" || adminList.WithdrawalRequests[0].FullName != "Withdrawal Request" || adminList.WithdrawalRequests[0].Amount != 125000 || adminList.WithdrawalRequests[0].Status != "pending" {
		t.Fatalf("unexpected admin withdrawal requests: %+v", adminList.WithdrawalRequests)
	}

	fixture.approveWithdrawalRequest(t, adminToken, created.ID)
	var approved struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		SavingRecordID string `json:"saving_record_id"`
	}
	if err := fixture.db.QueryRow(`SELECT id,status,saving_record_id FROM withdrawal_requests WHERE id=$1`, created.ID).Scan(&approved.ID, &approved.Status, &approved.SavingRecordID); err != nil {
		t.Fatalf("read approved withdrawal request: %v", err)
	}
	if approved.ID != created.ID || approved.Status != "approved" || approved.SavingRecordID == "" {
		t.Fatalf("unexpected approved withdrawal request: %+v", approved)
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/member/savings/summary", nil)
	summaryReq.Header.Set("Authorization", "Bearer "+memberToken)
	summaryRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(summaryRec, summaryReq)

	if summaryRec.Code != http.StatusOK {
		t.Fatalf("expected saving summary status 200, got %d: %s", summaryRec.Code, summaryRec.Body.String())
	}
	var summary struct {
		TotalWithdrawal int `json:"total_withdrawal"`
		CurrentBalance  int `json:"current_balance"`
		WajibBalance    int `json:"wajib_balance"`
		SukarelaBalance int `json:"sukarela_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode saving summary: %v", err)
	}
	if summary.TotalWithdrawal != 125000 || summary.CurrentBalance != 675000 || summary.WajibBalance != 500000 || summary.SukarelaBalance != 175000 {
		t.Fatalf("unexpected summary after approved withdrawal: %+v", summary)
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
			ID       string `json:"id"`
			Type     string `json:"type"`
			Category string `json:"category"`
			Amount   int    `json:"amount"`
			Note     string `json:"note"`
		} `json:"savings"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode saving history: %v", err)
	}
	var sawWithdrawal bool
	for _, record := range history.Savings {
		if record.ID == approved.SavingRecordID && record.Type == "withdrawal" && record.Category == "sukarela" && record.Amount == 125000 && record.Note == "Need cash" {
			sawWithdrawal = true
		}
	}
	if !sawWithdrawal {
		t.Fatalf("expected approved withdrawal saving record, got %+v", history.Savings)
	}
}

func TestPenarikanValidationAndRejectionKeepsBalances(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-WD-RULE","full_name":"Withdrawal Rules","join_date":"2026-06-16","status":"active","email":"wd-rules@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "wd-rules@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "wajib", 500000, "WJB-ONLY", "Wajib only")

	t.Run("request cannot exceed sukarela balance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{"amount":100000,"note":"No sukarela"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected over-sukarela request status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Withdrawal cannot exceed Simpanan Sukarela balance")
	})

	t.Run("direct withdrawal cannot use wajib", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
			"member_id":"`+member.ID+`",
			"type":"withdrawal",
			"category":"wajib",
			"amount":100000,
			"record_date":"2026-06-16"
		}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected wajib withdrawal status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Sukarela withdrawals must use the approval chain")
	})

	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 200000, "SUK-REJ", "Sukarela for reject")
	createReq := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{"amount":150000,"note":"Rejected request"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+memberToken)
	createRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected withdrawal request status 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode withdrawal request: %v", err)
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/admin/withdrawal-requests/"+created.ID+"/reject", bytes.NewBufferString(`{"rejection_reason":"Insufficient documentation"}`))
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectReq.Header.Set("Authorization", "Bearer "+adminToken)
	rejectRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rejectRec, rejectReq)

	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected withdrawal reject status 200, got %d: %s", rejectRec.Code, rejectRec.Body.String())
	}
	var rejected struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		RejectionReason string `json:"rejection_reason"`
	}
	if err := json.Unmarshal(rejectRec.Body.Bytes(), &rejected); err != nil {
		t.Fatalf("decode rejected withdrawal request: %v", err)
	}
	if rejected.ID != created.ID || rejected.Status != "rejected" || rejected.RejectionReason != "Insufficient documentation" {
		t.Fatalf("unexpected rejected withdrawal request: %+v", rejected)
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/member/savings/summary", nil)
	summaryReq.Header.Set("Authorization", "Bearer "+memberToken)
	summaryRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(summaryRec, summaryReq)

	if summaryRec.Code != http.StatusOK {
		t.Fatalf("expected saving summary status 200, got %d: %s", summaryRec.Code, summaryRec.Body.String())
	}
	var summary struct {
		TotalWithdrawal int `json:"total_withdrawal"`
		SukarelaBalance int `json:"sukarela_balance"`
	}
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode saving summary: %v", err)
	}
	if summary.TotalWithdrawal != 0 || summary.SukarelaBalance != 200000 {
		t.Fatalf("expected rejected withdrawal to leave balances unchanged, got %+v", summary)
	}
}

func TestPenarikanPagesRenderMemberAndAdminFlows(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-WD-PAGE","full_name":"Withdrawal Page","join_date":"2026-06-16","status":"active","email":"wd-page@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "wd-page@coop.test", "member-password")
	fixture.recordSavingInCategory(t, adminToken, member.ID, "deposit", "sukarela", 400000, "SUK-PAGE", "Page sukarela")
	requestID := fixture.createWithdrawalRequest(t, memberToken, 100000, "Page withdrawal")

	memberCookie := fixture.browserLogin(t, "wd-page@coop.test", "member-password")
	memberReq := httptest.NewRequest(http.MethodGet, "/member/withdrawal-requests", nil)
	memberReq.AddCookie(memberCookie)
	memberRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(memberRec, memberReq)

	if memberRec.Code != http.StatusOK {
		t.Fatalf("expected member withdrawal page status 200, got %d: %s", memberRec.Code, memberRec.Body.String())
	}
	memberBody := memberRec.Body.String()
	for _, text := range []string{"Penarikan", "Submit withdrawal request", `hx-post="/api/member/withdrawal-requests"`, "Simpanan Sukarela", "Page withdrawal", "Menunggu"} {
		if !strings.Contains(memberBody, text) {
			t.Fatalf("expected member withdrawal page to include %q, got %s", text, memberBody)
		}
	}

	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")
	adminReq := httptest.NewRequest(http.MethodGet, "/admin/withdrawal-requests", nil)
	adminReq.AddCookie(adminCookie)
	adminRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminRec, adminReq)

	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin withdrawal page status 200, got %d: %s", adminRec.Code, adminRec.Body.String())
	}
	adminBody := adminRec.Body.String()
	for _, text := range []string{"Penarikan review", "Withdrawal Page", "M-WD-PAGE", "100.000", "Page withdrawal", "/api/admin/withdrawal-requests/" + requestID + "/approve", "/api/admin/withdrawal-requests/" + requestID + "/reject", "/api/admin/exports/withdrawal-requests.csv?status=pending", "Menunggu"} {
		if !strings.Contains(adminBody, text) {
			t.Fatalf("expected admin withdrawal page to include %q, got %s", text, adminBody)
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
		"loan_type":" regular ",
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
		RequestedAmount int64  `json:"requested_amount"`
		DurationMonths  int    `json:"duration_months"`
		Purpose         string `json:"purpose"`
		Status          string `json:"status"`
		LoanType        string `json:"loan_type"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode loan request: %v", err)
	}
	if created.ID == "" || created.MemberID != member.ID || created.RequestedAmount != 3000000 || created.DurationMonths != 6 || created.Purpose != "Small business capital" || created.Status != "pending" || created.LoanType != "regular" {
		t.Fatalf("unexpected loan request: %+v", created)
	}
	var persistedLoanType string
	if err := fixture.db.QueryRow(`SELECT loan_type FROM loan_requests WHERE id=$1`, created.ID).Scan(&persistedLoanType); err != nil {
		t.Fatalf("read persisted loan type: %v", err)
	}
	if persistedLoanType != created.LoanType {
		t.Fatalf("persisted loan type=%q, returned loan type=%q", persistedLoanType, created.LoanType)
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
			RequestedAmount int64  `json:"requested_amount"`
			DurationMonths  int    `json:"duration_months"`
			Status          string `json:"status"`
			LoanType        string `json:"loan_type"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(historyRec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode loan request history: %v", err)
	}
	if len(history.LoanRequests) != 1 || history.LoanRequests[0].ID != created.ID || history.LoanRequests[0].Status != "pending" || history.LoanRequests[0].LoanType != created.LoanType {
		t.Fatalf("unexpected loan request history: %+v", history)
	}
}

func TestLoanRequestValidationAndEligibility(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0017","full_name":"Active Loan","join_date":"2026-06-16","status":"active","email":"active-loan@coop.test","password":"member-password"}`)
	inactive := fixture.createMember(t, adminToken, `{"member_no":"M-0018","full_name":"Inactive Loan","join_date":"2026-06-16","status":"active","email":"inactive-loan@coop.test","password":"member-password"}`)
	activeToken := fixture.login(t, "active-loan@coop.test", "member-password")
	inactiveToken := fixture.login(t, "inactive-loan@coop.test", "member-password")
	if _, err := fixture.db.Exec(`UPDATE members SET status='inactive' WHERE id=$1`, inactive.ID); err != nil {
		t.Fatalf("deactivate ineligible Member: %v", err)
	}

	t.Run("loan type is explicit and known", func(t *testing.T) {
		for name, payload := range map[string]string{
			"missing":     `{"requested_amount":1000000,"duration_months":4}`,
			"unsupported": `{"requested_amount":1000000,"duration_months":4,"loan_type":"unknown_type"}`,
		} {
			t.Run(name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+activeToken)
				rec := httptest.NewRecorder()

				fixture.server.ServeHTTP(rec, req)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
				}
				assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Loan Type is required and requested amount and duration months must be greater than zero")
			})
		}
	})

	t.Run("loan type validation is localized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":1000000,"duration_months":4}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+activeToken)
		req.Header.Set("Accept-Language", "id")
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Jenis Pinjaman wajib dipilih serta jumlah permintaan dan durasi bulan harus lebih besar dari nol")
	})

	t.Run("amount and duration must be positive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":0,"duration_months":0}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+activeToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Loan Type is required and requested amount and duration months must be greater than zero")
	})

	t.Run("member must be active", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":1000000,"duration_months":4}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+inactiveToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, rec.Body.Bytes(), "UNAUTHORIZED", "Invalid authentication token")
	})

	t.Run("member can have only one pending request", func(t *testing.T) {
		fixture.createLoanRequest(t, activeToken, 1000000, 4)
		req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":2000000,"duration_months":8,"purpose":"Working capital","loan_type":"regular"}`))
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
	for _, text := range []string{"member-loan-requests-shell", "Loan requests", "Submit loan request", `name="loan_type"`, `value="regular"`, `name="requested_amount"`, `name="duration_months"`, "table-scroll", "1.500.000", "Menunggu"} {
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
			RequestedAmount int64  `json:"requested_amount"`
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
	if _, err := fixture.db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('loan-role-id','LOAN-ROLE','Loan Role Member','2026-01-01','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(`UPDATE users SET member_id='loan-role-id' WHERE id='member-user-id'`); err != nil {
		t.Fatal(err)
	}
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
		assertError(t, rec.Body.Bytes(), "FORBIDDEN", "Insufficient permission")
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
	for _, text := range []string{"Loan request review", "table-scroll", "Queue Member", "M-0021", "3.200.000", "12", "Test loan", "Menunggu", `href="#loan-request-review-`, `class="review-modal"`, "Approve", "Reject"} {
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
	if loan.ID == "" || loan.LoanRequestID != requestID || loan.MemberID != member.ID || loan.ApprovedAmount != 1200000 || loan.DurationMonths != 6 || loan.MonthlyInstallment != 212000 || loan.RemainingBalance != 1272000 || loan.Status != "active" {
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
		ApprovedAmount     int64  `json:"approved_amount"`
		DurationMonths     int    `json:"duration_months"`
		MonthlyInstallment int64  `json:"monthly_installment"`
		RemainingBalance   int64  `json:"remaining_balance"`
		Status             string `json:"status"`
	}
	if err := json.Unmarshal(activeRec.Body.Bytes(), &activeLoan); err != nil {
		t.Fatalf("decode member active loan: %v", err)
	}
	if activeLoan.ID != loan.ID || activeLoan.ApprovedAmount != 1200000 || activeLoan.MonthlyInstallment != 212000 || activeLoan.RemainingBalance != 1272000 || activeLoan.Status != "active" {
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
			ApprovedAmount int64  `json:"approved_amount"`
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
		assertError(t, rec.Body.Bytes(), "VALIDATION_ERROR", "Approved amount, duration of 1 to 24 months, and start date are required")
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
			t.Fatalf("expected validation failure to create no loans, got %+v", loans)
		}
	})

	t.Run("manager may increase requested amount", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0023","full_name":"Approval Rules","join_date":"2026-06-16","status":"active","email":"approval-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "approval-rules@coop.test", "member-password")
		requestID := fixture.pendingLoanRequestID(t, memberToken)
		body := fmt.Sprintf(`{"approved_amount":1500000,"duration_months":5,"start_date":%q}`, time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02"))
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()

		fixture.server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var result struct {
			Request struct {
				ProposedApprovedAmount int64  `json:"proposed_approved_amount"`
				ProposedAdminFeePolicy string `json:"proposed_admin_fee_policy"`
			} `json:"loan_request"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Request.ProposedApprovedAmount != 1_500_000 || result.Request.ProposedAdminFeePolicy != "regular_tiered_monthly_v1" {
			t.Fatalf("unexpected increased proposal: %s", rec.Body.String())
		}
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 0 {
			t.Fatalf("expected only Manager proposal to create no loans, got %+v", loans)
		}
	})

	t.Run("approval is pending only and one active loan per member", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0023","full_name":"Approval Rules","join_date":"2026-06-16","status":"active","email":"approval-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "approval-rules@coop.test", "member-password")
		requestID := fixture.pendingLoanRequestID(t, memberToken)
		firstLoan := fixture.approveLoanRequest(t, adminToken, requestID, 800000, 4)

		reapproveReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":800000,"duration_months":4,"start_date":"2026-07-11"}`))
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

		conflictReq := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":600000,"duration_months":3,"loan_type":"regular","purpose":"blocked"}`))
		conflictReq.Header.Set("Content-Type", "application/json")
		conflictReq.Header.Set("Authorization", "Bearer "+memberToken)
		conflictRec := httptest.NewRecorder()

		fixture.server.ServeHTTP(conflictRec, conflictReq)

		if conflictRec.Code != http.StatusBadRequest {
			t.Fatalf("expected active loan conflict status 400, got %d: %s", conflictRec.Code, conflictRec.Body.String())
		}
		assertError(t, conflictRec.Body.Bytes(), "BUSINESS_RULE_VIOLATION", "Outstanding loan balance must be fully paid before requesting another loan")
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
	for _, text := range []string{`href="#loan-request-review-` + requestID + `"`, `id="loan-request-review-` + requestID + `"`, "/api/admin/loan-requests/" + requestID + "/approve", `name="approved_amount"`, `name="duration_months"`, "Approved amount", "Duration", "Approve"} {
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
	for _, text := range []string{"Active loan", "900.000", "109.000", "active"} {
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
	for _, text := range []string{"Active loans", "table-scroll", member.FullName, member.MemberNo, "900.000", "109.000", "Aktif"} {
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

		approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{"approved_amount":800000,"duration_months":4,"start_date":"2026-07-11"}`))
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
	for _, text := range []string{"/api/admin/loan-requests/" + requestID + "/reject", `name="rejection_reason"`, "Rejection reason", "Reject"} {
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
		RemainingBalance int64  `json:"remaining_balance"`
		Status           string `json:"status"`
	}
	if err := json.Unmarshal(activeRec.Body.Bytes(), &activeLoan); err != nil {
		t.Fatalf("decode active loan: %v", err)
	}
	if activeLoan.ID != loan.ID || activeLoan.RemainingBalance != 800000 || activeLoan.Status != "active" {
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
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].RemainingBalance != 525000 {
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
		if loans := fixture.activeLoans(t, adminToken); len(loans) != 1 || loans[0].RemainingBalance != 525000 {
			t.Fatalf("expected overpayment to keep loan balance, got %+v", loans)
		}
	})

	t.Run("full repayment marks loan paid", func(t *testing.T) {
		fixture := newTestFixture(t)
		adminToken := fixture.login(t, "admin@coop.test", "password")
		fixture.createMember(t, adminToken, `{"member_no":"M-0029","full_name":"Repayment Rules","join_date":"2026-06-16","status":"active","email":"repayment-rules@coop.test","password":"member-password"}`)
		memberToken := fixture.login(t, "repayment-rules@coop.test", "member-password")
		loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)

		fixture.recordRepayment(t, adminToken, loan.ID, loan.RemainingBalance)

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
	firstMember := fixture.createMember(t, adminToken, `{"member_no":"M-0030","full_name":"First Borrower","join_date":"2026-06-16","status":"active","email":"first-borrower@coop.test","password":"member-password"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0031","full_name":"Second Borrower","join_date":"2026-06-16","status":"active","email":"second-borrower@coop.test","password":"member-password"}`)
	firstToken := fixture.login(t, "first-borrower@coop.test", "member-password")
	secondToken := fixture.login(t, "second-borrower@coop.test", "member-password")
	fixture.recordSaving(t, adminToken, firstMember.ID, "deposit", 350000, "DETAIL-SAVE", "Detail saving")
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
	for _, text := range []string{"/api/admin/loans/" + firstLoan.ID + "/repayments", `name="record_date"`, `name="reference_no"`, "Record repayment", "600.000"} {
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
	for _, text := range []string{"Repayment history", "200.000", "RPY-TEST", "Test repayment", "632.000"} {
		if !strings.Contains(profileBody, text) {
			t.Fatalf("expected member profile to include %q, got %s", text, profileBody)
		}
	}

	adminMemberDetailReq := httptest.NewRequest(http.MethodGet, "/admin/members/"+firstMember.ID, nil)
	adminMemberDetailReq.AddCookie(adminCookie)
	adminMemberDetailRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(adminMemberDetailRec, adminMemberDetailReq)

	if adminMemberDetailRec.Code != http.StatusOK {
		t.Fatalf("expected admin member detail status 200, got %d: %s", adminMemberDetailRec.Code, adminMemberDetailRec.Body.String())
	}
	detailBody := adminMemberDetailRec.Body.String()
	for _, text := range []string{"Saving balance", "350.000", "Saving records", "DETAIL-SAVE", "Loan requests", "800.000", "Active loan", "632.000", "Repayment records", "RPY-TEST"} {
		if !strings.Contains(detailBody, text) {
			t.Fatalf("expected admin member detail to include %q, got %s", text, detailBody)
		}
	}
	if strings.Contains(detailBody, "Second Borrower") {
		t.Fatalf("expected admin member detail not to include another member's activity, got %s", detailBody)
	}
}

func TestAdminRepaymentsMenuLinksToActiveRepaymentsPage(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-0037","full_name":"Repayment Menu","join_date":"2026-06-16","status":"active","email":"repayment-menu@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "repayment-menu@coop.test", "member-password")
	loan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 500000, 5), 500000, 5)
	fixture.recordRepayment(t, adminToken, loan.ID, 100000)
	fixture.createMember(t, adminToken, `{"member_no":"M-0038","full_name":"Hidden Repayment","join_date":"2026-06-16","status":"active","email":"hidden-repayment@coop.test","password":"member-password"}`)
	hiddenToken := fixture.login(t, "hidden-repayment@coop.test", "member-password")
	hiddenLoan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, hiddenToken, 400000, 4), 400000, 4)
	fixture.recordRepayment(t, adminToken, hiddenLoan.ID, 50000)
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
	for _, text := range []string{`class="sidebar-link active" href="/admin/repayments"`, "table-scroll", `data-enhanced-table`, `data-page-label="Page"`, "table-pagination", "table-sort-button", "Repayment Menu", "M-0037", "100.000", "RPY-TEST"} {
		if !strings.Contains(repaymentsBody, text) {
			t.Fatalf("expected repayments page to include %q, got %s", text, repaymentsBody)
		}
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/admin/repayments?search=Repayment+Menu", nil)
	searchReq.AddCookie(adminCookie)
	searchRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(searchRec, searchReq)

	if searchRec.Code != http.StatusOK {
		t.Fatalf("expected searched repayments page status 200, got %d: %s", searchRec.Code, searchRec.Body.String())
	}
	searchBody := searchRec.Body.String()
	for _, text := range []string{`name="search" value="Repayment Menu"`, "Repayment Menu", "M-0037", "100.000"} {
		if !strings.Contains(searchBody, text) {
			t.Fatalf("expected searched repayments page to include %q, got %s", text, searchBody)
		}
	}
	if strings.Contains(searchBody, "Hidden Repayment") || strings.Contains(searchBody, "M-0038") {
		t.Fatalf("expected searched repayments page to exclude other members, got %s", searchBody)
	}
}

func TestAdminDashboardAggregatesOperationalTotals(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	active := fixture.createMember(t, adminToken, `{"member_no":"M-0032","full_name":"Dashboard Active","join_date":"2026-06-16","status":"active","email":"dashboard-active@coop.test","password":"member-password"}`)
	fixture.createMember(t, adminToken, `{"member_no":"M-0033","full_name":"Dashboard Inactive","join_date":"2026-06-16","status":"inactive"}`)
	memberToken := fixture.login(t, "dashboard-active@coop.test", "member-password")
	fixture.recordSaving(t, adminToken, active.ID, "deposit", 1000000, "DASH-DEP", "Dashboard deposit")
	withdrawalID := fixture.createWithdrawalRequest(t, memberToken, 200000, "Dashboard withdrawal")
	fixture.approveWithdrawalRequest(t, adminToken, withdrawalID)
	activeLoan := fixture.approveLoanRequest(t, adminToken, fixture.createLoanRequest(t, memberToken, 600000, 6), 600000, 6)
	fixture.recordRepayment(t, adminToken, activeLoan.ID, 150000)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()

	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var summary struct {
		TotalMembers         int   `json:"total_members"`
		ActiveMembers        int   `json:"active_members"`
		TotalSavings         int   `json:"total_savings"`
		ActiveLoans          int   `json:"active_loans"`
		TotalOutstandingLoan int64 `json:"total_outstanding_loan"`
		PendingLoanRequests  int   `json:"pending_loan_requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode dashboard summary: %v", err)
	}
	if summary.TotalMembers != 7 || summary.ActiveMembers != 6 || summary.TotalSavings != 800000 || summary.ActiveLoans != 1 || summary.TotalOutstandingLoan != 486000 || summary.PendingLoanRequests != 0 {
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
		RemainingLoanBalance int64     `json:"remaining_loan_balance"`
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
	if dashboard.SavingBalance != 700000 || dashboard.RemainingLoanBalance != 425000 || dashboard.ActiveLoan == nil || dashboard.ActiveLoan.ID != firstLoan.ID {
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

	firstCookie := fixture.browserLogin(t, "dashboard-first@coop.test", "member-password")
	dashboardPageReq := httptest.NewRequest(http.MethodGet, "/member/dashboard", nil)
	dashboardPageReq.AddCookie(firstCookie)
	dashboardPageRec := httptest.NewRecorder()

	fixture.server.ServeHTTP(dashboardPageRec, dashboardPageReq)

	if dashboardPageRec.Code != http.StatusOK {
		t.Fatalf("expected member dashboard page status 200, got %d: %s", dashboardPageRec.Code, dashboardPageRec.Body.String())
	}
	pageBody := dashboardPageRec.Body.String()
	for _, text := range []string{"member-dashboard-shell", "Saving balance", "700.000", "Remaining loan", "425.000", "Loan request status", "FIRST-DEP", "Latest repayment records", "100.000"} {
		if !strings.Contains(pageBody, text) {
			t.Fatalf("expected member dashboard page to include %q, got %s", text, pageBody)
		}
	}
	if strings.Contains(pageBody, "SECOND-DEP") {
		t.Fatalf("expected member dashboard page not to expose second member data, got %s", pageBody)
	}
}

func TestLoanScheduleDetailCorrectionAndOutstandingRules(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"M-SCHEDULE","full_name":"Schedule Member","join_date":"2026-06-16","status":"active","email":"schedule@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "schedule@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 1000000, 3)
	startDate := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
	body := fmt.Sprintf(`{"approved_amount":900000,"duration_months":3,"start_date":%q}`, startDate)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve scheduled loan: %d %s", rec.Code, rec.Body.String())
	}
	var loan testLoan
	for _, credentials := range []struct{ email, password string }{
		{"ketua-i@coop.test", "password"},
		{"ketua-ii@coop.test", "password"},
		{"ketua-utama@coop.test", "password"},
	} {
		token := fixture.login(t, credentials.email, credentials.password)
		approvalReq := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(`{}`))
		approvalReq.Header.Set("Content-Type", "application/json")
		approvalReq.Header.Set("Authorization", "Bearer "+token)
		approvalRec := httptest.NewRecorder()
		fixture.server.ServeHTTP(approvalRec, approvalReq)
		if approvalRec.Code != http.StatusOK {
			t.Fatalf("advance scheduled loan: %d %s", approvalRec.Code, approvalRec.Body.String())
		}
		var result struct {
			Loan *testLoan `json:"loan"`
		}
		if err := json.Unmarshal(approvalRec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Loan != nil {
			loan = *result.Loan
		}
	}
	if loan.ID == "" {
		t.Fatal("expected Ketua Utama approval to create the scheduled loan")
	}
	if loan.RemainingBalance != 927000 {
		t.Fatalf("expected obligation 927000, got %+v", loan)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/admin/loans/"+loan.ID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+adminToken)
	detailRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", detailRec.Code, detailRec.Body.String())
	}
	var detail struct {
		Installments []struct {
			ScheduledAmount int64  `json:"scheduled_amount"`
			DueDate         string `json:"due_date"`
		} `json:"installments"`
	}
	if json.Unmarshal(detailRec.Body.Bytes(), &detail) != nil || len(detail.Installments) != 3 {
		t.Fatalf("unexpected schedule detail %s", detailRec.Body.String())
	}
	sum := 0
	for _, i := range detail.Installments {
		sum += int(i.ScheduledAmount)
	}
	if sum != 927000 {
		t.Fatalf("schedule sum %d", sum)
	}

	memberPageReq := httptest.NewRequest(http.MethodGet, "/member/loan-requests", nil)
	memberPageReq.Header.Set("Accept-Language", "id")
	memberPageReq.AddCookie(fixture.browserLogin(t, "schedule@coop.test", "member-password"))
	memberPageRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(memberPageRec, memberPageReq)
	for _, text := range []string{"Pinjaman belum lunas", "Sisa saldo", "Tenggat angsuran berikutnya", "Tenggat pelunasan akhir"} {
		if memberPageRec.Code != http.StatusOK || !strings.Contains(memberPageRec.Body.String(), text) {
			t.Fatalf("expected Indonesian outstanding UI %q, got %d %s", text, memberPageRec.Code, memberPageRec.Body.String())
		}
	}

	adminDetailPageReq := httptest.NewRequest(http.MethodGet, "/admin/loans/"+loan.ID, nil)
	adminDetailPageReq.Header.Set("Accept-Language", "id")
	adminDetailPageReq.AddCookie(fixture.browserLogin(t, "admin@coop.test", "password"))
	adminDetailPageRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(adminDetailPageRec, adminDetailPageReq)
	for _, text := range []string{"Detail pinjaman", "Jadwal angsuran", "Biaya admin bulanan", "Total biaya admin", startDate, "9.000"} {
		if adminDetailPageRec.Code != http.StatusOK || !strings.Contains(adminDetailPageRec.Body.String(), text) {
			t.Fatalf("expected Indonesian detail UI %q, got %d %s", text, adminDetailPageRec.Code, adminDetailPageRec.Body.String())
		}
	}
	if strings.Contains(adminDetailPageRec.Body.String(), "Bunga") || strings.Contains(adminDetailPageRec.Body.String(), "bunga") {
		t.Fatalf("expected preserved charge labels to use Biaya Admin terminology, got %s", adminDetailPageRec.Body.String())
	}

	correctReq := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loan.ID+"/start-date", bytes.NewBufferString(fmt.Sprintf(`{"start_date":%q}`, startDate)))
	correctReq.Header.Set("Content-Type", "application/json")
	correctReq.Header.Set("Authorization", "Bearer "+adminToken)
	correctReq.Header.Set("HX-Request", "true")
	correctRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(correctRec, correctReq)
	if correctRec.Code != http.StatusNotFound {
		t.Fatalf("expected routine start-date correction route to be removed, got %d %s", correctRec.Code, correctRec.Body.String())
	}
	var audits int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_start_date_audits WHERE loan_id=$1`, loan.ID).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}

	blockedReq := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":100000,"duration_months":1,"purpose":"Another need","loan_type":"regular"}`))
	blockedReq.Header.Set("Content-Type", "application/json")
	blockedReq.Header.Set("Authorization", "Bearer "+memberToken)
	blockedRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusBadRequest {
		t.Fatalf("expected outstanding block, got %d %s", blockedRec.Code, blockedRec.Body.String())
	}
	blockedIDReq := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{"requested_amount":100000,"duration_months":1,"purpose":"Kebutuhan lain","loan_type":"regular"}`))
	blockedIDReq.Header.Set("Content-Type", "application/json")
	blockedIDReq.Header.Set("Authorization", "Bearer "+memberToken)
	blockedIDReq.Header.Set("Accept-Language", "id")
	blockedIDReq.Header.Set("HX-Request", "true")
	blockedIDRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(blockedIDRec, blockedIDReq)
	if blockedIDRec.Code != http.StatusBadRequest || !strings.Contains(blockedIDRec.Body.String(), "Saldo pinjaman yang belum lunas") {
		t.Fatalf("expected localized outstanding error, got %d %s", blockedIDRec.Code, blockedIDRec.Body.String())
	}

	if _, err := fixture.db.Exec(`UPDATE loans SET status='adjustment_due' WHERE id=$1`, loan.ID); err != nil {
		t.Fatal(err)
	}
	fixture.recordRepayment(t, adminToken, loan.ID, 1000)
	lockedReq := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loan.ID+"/start-date", bytes.NewBufferString(fmt.Sprintf(`{"start_date":%q}`, startDate)))
	lockedReq.Header.Set("Content-Type", "application/json")
	lockedReq.Header.Set("Authorization", "Bearer "+adminToken)
	lockedReq.Header.Set("Accept-Language", "id")
	lockedReq.Header.Set("HX-Request", "true")
	lockedRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(lockedRec, lockedReq)
	if lockedRec.Code != http.StatusNotFound {
		t.Fatalf("expected correction route to remain unavailable, got %d %s", lockedRec.Code, lockedRec.Body.String())
	}
	var total int
	if err := fixture.db.QueryRow(`SELECT COALESCE(SUM(remaining_balance),0) FROM loans WHERE member_id=$1 AND remaining_balance>0`, member.ID).Scan(&total); err != nil || total != 926000 {
		t.Fatalf("adjusted total=%d err=%v", total, err)
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
	for _, text := range []string{"Dashboard", "400.000", "Pending requests", ">1</strong>"} {
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
	seedUser(t, db, "ketua-i-user-id", "ketua-i@coop.test", "password", "ketua_i")
	seedUser(t, db, "ketua-ii-user-id", "ketua-ii@coop.test", "password", "ketua_ii")
	seedUser(t, db, "ketua-utama-user-id", "ketua-utama@coop.test", "password", "ketua_utama")
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

func (f testFixture) setLanguage(t *testing.T, lang, redirectPath string) *http.Cookie {
	t.Helper()

	body := strings.NewReader("lang=" + lang + "&redirect=" + redirectPath)
	req := httptest.NewRequest(http.MethodPost, "/language", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected language redirect status 303, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "kopdes_lang" {
			return cookie
		}
	}
	t.Fatal("expected kopdes_lang cookie")
	return nil
}

func (f testFixture) createMember(t *testing.T, adminToken, body string) struct {
	ID       string `json:"id"`
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	Status   string `json:"status"`
} {
	t.Helper()
	var payload map[string]any
	hadEmail := false
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		memberNo, _ := payload["member_no"].(string)
		email, _ := payload["email"].(string)
		hadEmail = strings.TrimSpace(email) != ""
		if strings.TrimSpace(email) == "" {
			payload["email"] = strings.ToLower(memberNo) + "@test.local"
			payload["password"] = "member-password"
			encoded, encodeErr := json.Marshal(payload)
			if encodeErr != nil {
				t.Fatalf("encode test member: %v", encodeErr)
			}
			body = string(encoded)
		}
	}

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
	if _, err := f.db.Exec(`UPDATE users SET must_change_password=FALSE WHERE member_id=$1`, created.ID); err != nil {
		t.Fatalf("mark test member password as established: %v", err)
	}
	if !hadEmail {
		if _, err := f.db.Exec(`DELETE FROM users WHERE member_id=$1 AND id<>'member-user-id'`, created.ID); err != nil {
			t.Fatalf("remove generated test login: %v", err)
		}
		if _, err := f.db.Exec(`UPDATE users SET member_id=$1,full_name=$2 WHERE id='member-user-id'`, created.ID, created.FullName); err != nil {
			t.Fatalf("link shared test login: %v", err)
		}
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
	if _, err := f.db.Exec(`UPDATE users SET must_change_password=FALSE WHERE member_id=$1`, memberID); err != nil {
		t.Fatalf("mark test member password as established: %v", err)
	}
}

func (f testFixture) recordDeposit(t *testing.T, adminToken, memberID string, amount int) string {
	t.Helper()
	return f.recordSaving(t, adminToken, memberID, "deposit", amount, "TRF-PAGE", "Page deposit")
}

func (f testFixture) recordSaving(t *testing.T, adminToken, memberID, recordType string, amount int, referenceNo, note string) string {
	t.Helper()
	return f.recordSavingInCategory(t, adminToken, memberID, recordType, "sukarela", amount, referenceNo, note)
}

func (f testFixture) recordSavingInCategory(t *testing.T, adminToken, memberID, recordType, category string, amount int, referenceNo, note string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{
		"member_id":"`+memberID+`",
		"type":"`+recordType+`",
		"category":"`+category+`",
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
	return f.createLoanRequestWithType(t, memberToken, "regular", amount, durationMonths)
}

func (f testFixture) createLoanRequestWithType(t *testing.T, memberToken, loanType string, amount, durationMonths int) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", bytes.NewBufferString(`{
		"requested_amount":`+strconv.Itoa(amount)+`,
		"duration_months":`+strconv.Itoa(durationMonths)+`,
		"loan_type":"`+loanType+`",
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

func (f testFixture) createWithdrawalRequest(t *testing.T, memberToken string, amount int, note string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/member/withdrawal-requests", bytes.NewBufferString(`{
		"amount":`+strconv.Itoa(amount)+`,
		"note":"`+note+`"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()

	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected withdrawal request status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode withdrawal request response: %v", err)
	}
	return response.ID
}

func (f testFixture) approveWithdrawalRequest(t *testing.T, managerToken, requestID string) {
	t.Helper()
	for _, token := range []string{
		managerToken,
		f.login(t, "ketua-i@coop.test", "password"),
		f.login(t, "ketua-ii@coop.test", "password"),
		f.login(t, "ketua-utama@coop.test", "password"),
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/withdrawal-requests/"+requestID+"/approve", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		f.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected withdrawal approval status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}

type testLoan struct {
	ID                 string `json:"id"`
	LoanRequestID      string `json:"loan_request_id"`
	MemberID           string `json:"member_id"`
	MemberNo           string `json:"member_no"`
	FullName           string `json:"full_name"`
	ApprovedAmount     int64  `json:"approved_amount"`
	DurationMonths     int    `json:"duration_months"`
	MonthlyInstallment int64  `json:"monthly_installment"`
	RemainingBalance   int64  `json:"remaining_balance"`
	AdminFeePolicy     string `json:"admin_fee_policy"`
	MonthlyAdminFee    int64  `json:"monthly_admin_fee"`
	TotalAdminFee      int64  `json:"total_admin_fee"`
	TotalObligation    int64  `json:"total_obligation"`
	Status             string `json:"status"`
}

type testRepayment struct {
	ID          string `json:"id"`
	LoanID      string `json:"loan_id"`
	MemberID    string `json:"member_id"`
	Amount      int64  `json:"amount"`
	RecordDate  string `json:"record_date"`
	ReferenceNo string `json:"reference_no"`
	Note        string `json:"note"`
}

func (f testFixture) approveLoanRequest(t *testing.T, adminToken, requestID string, approvedAmount, durationMonths int) testLoan {
	t.Helper()

	managerBody := `{
		"approved_amount":` + strconv.Itoa(approvedAmount) + `,
		"duration_months":` + strconv.Itoa(durationMonths) + `,
		"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `"
	}`
	stages := []struct {
		token string
		body  string
	}{
		{adminToken, managerBody},
		{f.login(t, "ketua-i@coop.test", "password"), `{}`},
		{f.login(t, "ketua-ii@coop.test", "password"), `{}`},
		{f.login(t, "ketua-utama@coop.test", "password"), `{}`},
	}

	var response struct {
		Loan *testLoan `json:"loan"`
	}
	for _, stage := range stages {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", bytes.NewBufferString(stage.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+stage.token)
		rec := httptest.NewRecorder()
		f.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected approve loan request status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode loan approval response: %v", err)
		}
	}
	if response.Loan == nil {
		t.Fatal("expected final approval to create loan")
	}
	return *response.Loan
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

func (f testFixture) recordRepayment(t *testing.T, adminToken, loanID string, amount int64) testRepayment {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/loans/"+loanID+"/repayments", bytes.NewBufferString(`{
		"amount":`+strconv.FormatInt(amount, 10)+`,
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
	memberID := id + "-member"
	memberNo := "TEST-" + strings.ToUpper(id)
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ($1,$2,$3,'2026-01-01','active')`, memberID, memberNo, email); err != nil {
		t.Fatalf("seed Member: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (id,email,password_hash,role,member_id,full_name,historical_identity) VALUES ($1,$2,$3,'member',$4,$5,FALSE)`,
		id, email, string(hash), memberID, email,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if role != "member" {
		if _, err := db.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,$3,TRUE)`, id, memberID, role); err != nil {
			t.Fatalf("seed Officer Appointment: %v", err)
		}
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

func dottedTestNominal(value int) string {
	raw := strconv.Itoa(value)
	if len(raw) <= 3 {
		return raw
	}
	sign := ""
	if raw[0] == '-' {
		sign = "-"
		raw = raw[1:]
	}
	result := make([]byte, 0, len(raw)+(len(raw)-1)/3)
	firstGroup := len(raw) % 3
	if firstGroup == 0 {
		firstGroup = 3
	}
	result = append(result, raw[:firstGroup]...)
	for i := firstGroup; i < len(raw); i += 3 {
		result = append(result, '.')
		result = append(result, raw[i:i+3]...)
	}
	return sign + string(result)
}
