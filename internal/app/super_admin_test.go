package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sodrety/kopdes/internal/app"
	_ "modernc.org/sqlite"
)

func establishSuperAdmin(t *testing.T, fixture testFixture) string {
	t.Helper()
	if err := app.EnsureSuperAdminUser(fixture.db, "super@coop.test", "temporary-super-password", "Platform Super Admin"); err != nil {
		t.Fatalf("bootstrap super admin: %v", err)
	}
	temporaryToken := fixture.login(t, "super@coop.test", "temporary-super-password")
	changeReq := httptest.NewRequest(http.MethodPost, "/api/account/password", bytes.NewBufferString(`{"password":"permanent-super-password"}`))
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.Header.Set("Authorization", "Bearer "+temporaryToken)
	changeRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(changeRec, changeReq)
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change super admin password: %d %s", changeRec.Code, changeRec.Body.String())
	}
	return fixture.login(t, "super@coop.test", "permanent-super-password")
}

func TestSuperAdminIsMemberlessAndOnlyOneActiveAccountIsBootstrapped(t *testing.T) {
	fixture := newTestFixture(t)
	if err := app.EnsureSuperAdminUser(fixture.db, "super@coop.test", "temporary-super-password", "Platform Super Admin"); err != nil {
		t.Fatalf("bootstrap super admin: %v", err)
	}
	var role string
	var memberID sql.NullString
	var activeCount int
	if err := fixture.db.QueryRow(`SELECT role,member_id FROM users WHERE email='super@coop.test'`).Scan(&role, &memberID); err != nil {
		t.Fatalf("read super admin: %v", err)
	}
	if role != "super_admin" || memberID.Valid {
		t.Fatalf("super admin must be memberless: role=%q member_id=%v", role, memberID)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='super_admin' AND active=TRUE AND historical_identity=FALSE`).Scan(&activeCount); err != nil {
		t.Fatalf("count active super admins: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active super admin, got %d", activeCount)
	}
	if err := app.EnsureSuperAdminUser(fixture.db, "other-super@coop.test", "another-password", "Other"); err != nil {
		t.Fatalf("second bootstrap should be a no-op: %v", err)
	}
	if _, err := app.AuthenticateUser(fixture.db, "super@coop.test", "temporary-super-password"); err != nil {
		t.Fatalf("existing super admin credentials were unexpectedly replaced: %v", err)
	}
	if _, err := fixture.db.Exec(`SELECT 1 FROM members WHERE id=(SELECT member_id FROM users WHERE email='super@coop.test')`); err != nil && err != sql.ErrNoRows {
		t.Fatalf("check memberless identity: %v", err)
	}
}

func TestSuperAdminCanOverrideLoanWithoutOfficerApprovals(t *testing.T) {
	fixture := newTestFixture(t)
	superToken := establishSuperAdmin(t, fixture)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"SUPER-LOAN","full_name":"Super Loan Member","join_date":"2026-01-01","status":"active","email":"super-loan-member@coop.test","password":"member-password","bank_name":"Test Bank","bank_account":"123456"}`)
	memberToken := fixture.login(t, "super-loan-member@coop.test", "member-password")
	fixture.recordSaving(t, superToken, member.ID, "deposit", 100000, "SUPER-LOAN-DEPOSIT", "Capacity")
	requestID := fixture.createLoanRequest(t, memberToken, 100000, 6)
	startDate := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
	body := `{"approved_amount":100000,"duration_months":6,"start_date":"` + startDate + `","note":"Emergency platform override"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/override-approve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+superToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("override loan approval: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Loan *struct {
			ID string `json:"id"`
		} `json:"loan"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode override loan response: %v", err)
	}
	if response.Loan != nil {
		t.Fatal("super admin override must wait for Bendahara disbursement before creating a Loan")
	}
	var requestStatus string
	var loanCount int
	if err := fixture.db.QueryRow(`SELECT status FROM loan_requests WHERE id=$1`, requestID).Scan(&requestStatus); err != nil {
		t.Fatalf("read overridden loan request: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE loan_request_id=$1`, requestID).Scan(&loanCount); err != nil {
		t.Fatalf("count loans before disbursement: %v", err)
	}
	if requestStatus != "approved" || loanCount != 0 {
		t.Fatalf("expected approved request without Loan before disbursement, status=%q loans=%d", requestStatus, loanCount)
	}
	loan := fixture.disburseLoanRequest(t, requestID, startDate)
	if loan.ID == "" {
		t.Fatal("expected Bendahara disbursement to create the Loan")
	}
	var approvals, overrides, audits int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id=$1`, requestID).Scan(&approvals); err != nil {
		t.Fatalf("count Officer approvals: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM super_admin_overrides WHERE request_type='loan' AND request_id=$1`, requestID).Scan(&overrides); err != nil {
		t.Fatalf("count super admin overrides: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM admin_audit_events WHERE actor_id=(SELECT id FROM users WHERE email='super@coop.test') AND path LIKE '%override-approve'`).Scan(&audits); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if approvals != 0 || overrides != 1 || audits != 1 {
		t.Fatalf("unexpected override records: Officer approvals=%d overrides=%d audits=%d", approvals, overrides, audits)
	}
}

func TestSuperAdminCanOverrideAdminOriginLoanAboveSavingsLimit(t *testing.T) {
	fixture := newTestFixture(t)
	superToken := establishSuperAdmin(t, fixture)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)
	member := fixture.createMember(t, managerToken, `{"member_no":"SUPER-ADMIN-LOAN-LIMIT","full_name":"Admin Loan Limit Member","join_date":"2026-01-01","status":"active","email":"admin-limit-member@coop.test","password":"member-password","bank_name":"Test Bank","bank_account":"123456"}`)
	created := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/on-behalf", adminToken, `{"member_no":"SUPER-ADMIN-LOAN-LIMIT","loan_type":"regular","requested_amount":55000000,"duration_months":24,"purpose":"Admin request above savings limit","acknowledge_warnings":true,"idempotency_key":"super-admin-limit-override"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create admin loan request: %d %s", created.Code, created.Body.String())
	}
	var createdRequest struct {
		Request struct {
			ID string `json:"id"`
		} `json:"loan_request"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdRequest); err != nil || createdRequest.Request.ID == "" {
		t.Fatalf("decode created admin loan request: %s (%v)", created.Body.String(), err)
	}
	requestID := createdRequest.Request.ID
	var limit int64
	if err := fixture.db.QueryRow(`SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=$1`, member.ID).Scan(&limit); err != nil {
		t.Fatalf("read member savings limit: %v", err)
	}
	if limit >= 55000000 {
		t.Fatalf("fixture must put request above savings limit: limit=%d", limit)
	}

	cookie := fixture.browserLogin(t, "super@coop.test", "permanent-super-password")
	pageReq := httptest.NewRequest(http.MethodGet, "/admin/loan-requests", nil)
	pageReq.AddCookie(cookie)
	pageRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("admin loan request page: %d %s", pageRec.Code, pageRec.Body.String())
	}
	page := pageRec.Body.String()
	formStart := strings.Index(page, `action="/api/admin/loan-requests/`+requestID+`/override-approve"`)
	if formStart < 0 {
		t.Fatalf("Super Admin override form missing for admin request %s", requestID)
	}
	formEnd := strings.Index(page[formStart:], "</form>")
	if formEnd < 0 || strings.Contains(page[formStart:formStart+formEnd], `data-rupiah-max=`) {
		t.Fatalf("admin-origin Super Admin override form still applies the savings limit: %s", page[formStart:])
	}

	override := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/override-approve", superToken, `{"approved_amount":55000000,"duration_months":24,"note":"Approved above savings limit"}`)
	if override.Code != http.StatusOK {
		t.Fatalf("override admin-origin loan above savings limit: %d %s", override.Code, override.Body.String())
	}
	var requestStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM loan_requests WHERE id=$1`, requestID).Scan(&requestStatus); err != nil {
		t.Fatalf("read overridden request status: %v", err)
	}
	if requestStatus != "approved" {
		t.Fatalf("expected admin-origin request to be approved, got %q", requestStatus)
	}
}

func TestSuperAdminOverrideKeepsSavingsLimitForMemberOriginLoan(t *testing.T) {
	fixture := newTestFixture(t)
	superToken := establishSuperAdmin(t, fixture)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"SUPER-MEMBER-LOAN-LIMIT","full_name":"Member Loan Limit","join_date":"2026-01-01","status":"active","email":"member-limit@coop.test","password":"member-password"}`)
	fixture.recordSaving(t, superToken, member.ID, "deposit", 100000, "SUPER-MEMBER-LOAN-SAVING", "Loan capacity")
	var limit int64
	if err := fixture.db.QueryRow(`SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=$1`, member.ID).Scan(&limit); err != nil {
		t.Fatalf("read member savings limit: %v", err)
	}
	if limit <= 0 {
		t.Fatalf("expected a positive member loan limit after saving, got %d", limit)
	}
	memberToken := fixture.login(t, "member-limit@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, int(limit), 6)
	tooHigh := strconv.FormatInt(limit+1, 10)
	body := `{"approved_amount":` + tooHigh + `,"duration_months":6,"note":"Must retain member savings limit"}`
	override := adminLoanJSONRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/override-approve", superToken, body)
	if override.Code != http.StatusBadRequest {
		t.Fatalf("member-origin override above savings limit should fail: %d %s", override.Code, override.Body.String())
	}
	var requestStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM loan_requests WHERE id=$1`, requestID).Scan(&requestStatus); err != nil {
		t.Fatalf("read member request status: %v", err)
	}
	if requestStatus != "pending" {
		t.Fatalf("expected rejected override to leave member request pending, got %q", requestStatus)
	}
}

func TestSuperAdminCanOverrideWithdrawalAndCannotUseMemberArea(t *testing.T) {
	fixture := newTestFixture(t)
	superToken := establishSuperAdmin(t, fixture)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"SUPER-WITHDRAWAL","full_name":"Super Withdrawal Member","join_date":"2026-01-01","status":"active","email":"super-withdrawal-member@coop.test","password":"member-password","bank_name":"Test Bank","bank_account":"654321"}`)
	memberToken := fixture.login(t, "super-withdrawal-member@coop.test", "member-password")
	fixture.recordSaving(t, superToken, member.ID, "deposit", 100000, "SUPER-WITHDRAWAL-DEPOSIT", "Balance")
	requestID := fixture.createWithdrawalRequest(t, memberToken, 25000, "Need cash")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/withdrawal-requests/"+requestID+"/override-approve", bytes.NewBufferString(`{"note":"Platform override"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+superToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("override withdrawal approval: %d %s", rec.Code, rec.Body.String())
	}
	var savingCount, overrides int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1 AND type='withdrawal' AND amount=$2`, member.ID, 25000).Scan(&savingCount); err != nil {
		t.Fatalf("count withdrawal saving record: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM super_admin_overrides WHERE request_type='withdrawal' AND request_id=$1`, requestID).Scan(&overrides); err != nil {
		t.Fatalf("count withdrawal override: %v", err)
	}
	if savingCount != 0 || overrides != 1 {
		t.Fatalf("unexpected withdrawal override state: saving=%d overrides=%d", savingCount, overrides)
	}
	fixture.disburseWithdrawalRequest(t, requestID)
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id=$1 AND type='withdrawal' AND amount=$2`, member.ID, 25000).Scan(&savingCount); err != nil {
		t.Fatalf("count disbursed withdrawal saving record: %v", err)
	}
	if savingCount != 1 {
		t.Fatalf("expected one withdrawal saving record after Bendahara disbursement, got %d", savingCount)
	}
	memberReq := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
	memberReq.Header.Set("Authorization", "Bearer "+superToken)
	memberRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(memberRec, memberReq)
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("expected super admin to be excluded from member area, got %d: %s", memberRec.Code, memberRec.Body.String())
	}
}
