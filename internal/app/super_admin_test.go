package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if response.Loan == nil || response.Loan.ID == "" {
		t.Fatal("expected super admin override to create the loan")
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
	if savingCount != 1 || overrides != 1 {
		t.Fatalf("unexpected withdrawal override state: saving=%d overrides=%d", savingCount, overrides)
	}
	memberReq := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
	memberReq.Header.Set("Authorization", "Bearer "+superToken)
	memberRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(memberRec, memberReq)
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("expected super admin to be excluded from member area, got %d: %s", memberRec.Code, memberRec.Body.String())
	}
}
