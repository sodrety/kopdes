package app_test

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

func provisionAdminOfficer(t *testing.T, fixture testFixture, superToken, managerToken string) (string, string) {
	t.Helper()
	member := fixture.createMember(t, managerToken, `{"member_no":"ADMIN-ROLE","full_name":"Tagihan Admin","join_date":"2026-01-01","status":"active","email":"tagihan-admin@coop.test","password":"admin-password"}`)
	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", superToken, `{"member_id":"`+member.ID+`","role":"admin"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create admin Officer: %d %s", response.Code, response.Body.String())
	}
	return member.ID, fixture.login(t, "tagihan-admin@coop.test", "admin-password")
}

func TestAdminRoleHasNoCashAccessAndMemberTagihanConfigIsAdminOnly(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	memberID, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)

	if response := hierarchyRequest(fixture, http.MethodGet, "/api/admin/members", adminToken, ""); response.Code != http.StatusOK {
		t.Fatalf("admin should view members: %d %s", response.Code, response.Body.String())
	}
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin/transactions"},
		{http.MethodGet, "/admin/transactions/categories"},
		{http.MethodGet, "/api/admin/exports/transactions.csv"},
		{http.MethodPost, "/api/admin/transactions"},
	} {
		if response := hierarchyRequest(fixture, route.method, route.path, adminToken, ""); response.Code != http.StatusForbidden {
			t.Fatalf("admin should not access cash route %s: %d %s", route.path, response.Code, response.Body.String())
		}
	}

	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/members/"+memberID+"/tagihan-config", adminToken, `{"simpanan_wajib":125000,"simpanan_manasuka":75000}`)
	if response.Code != http.StatusOK {
		t.Fatalf("admin should update member Tagihan config: %d %s", response.Code, response.Body.String())
	}
	var wajib, manasuka int64
	if err := fixture.db.QueryRow(`SELECT simpanan_wajib,simpanan_manasuka FROM member_tagihan_configs WHERE member_id=$1`, memberID).Scan(&wajib, &manasuka); err != nil {
		t.Fatalf("read member Tagihan config: %v", err)
	}
	if wajib != 125000 || manasuka != 75000 {
		t.Fatalf("unexpected saved member Tagihan config: wajib=%d manasuka=%d", wajib, manasuka)
	}

	for _, token := range []struct {
		name  string
		token string
	}{
		{"manager", managerToken},
		{"super admin", superToken},
	} {
		response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/members/"+memberID+"/tagihan-config", token.token, `{"simpanan_wajib":1,"simpanan_manasuka":2}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s should not edit member Tagihan config: %d %s", token.name, response.Code, response.Body.String())
		}
	}
	for _, body := range []string{`{"simpanan_wajib":-1,"simpanan_manasuka":0}`, `{"simpanan_wajib":1.5,"simpanan_manasuka":0}`} {
		response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/members/"+memberID+"/tagihan-config", adminToken, body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid Tagihan config should be rejected: %d %s", response.Code, response.Body.String())
		}
	}

	secondMember := fixture.createMember(t, managerToken, `{"member_no":"ADMIN-ROLE-2","full_name":"Second Admin Candidate","join_date":"2026-01-01","status":"active","email":"second-admin-candidate@coop.test","password":"admin-password"}`)
	response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", adminToken, `{"member_id":"`+secondMember.ID+`","role":"admin"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("admin should not grant the Admin role: %d %s", response.Code, response.Body.String())
	}

	var role string
	if err := fixture.db.QueryRow(`SELECT role FROM officer_appointments WHERE member_id=$1`, memberID).Scan(&role); err != nil && err != sql.ErrNoRows {
		t.Fatalf("read admin Officer role: %v", err)
	}
	if role != "admin" {
		t.Fatalf("expected exact admin Officer role, got %q", role)
	}
}

func TestAdminRoleApprovesOnlyManagerStageForLoansAndWithdrawals(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	superToken := establishSuperAdmin(t, fixture)
	_, adminToken := provisionAdminOfficer(t, fixture, superToken, managerToken)

	member := fixture.createMember(t, managerToken, `{"member_no":"ADMIN-APPROVAL","full_name":"Admin Approval Member","join_date":"2026-01-01","status":"active","email":"admin-approval-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "admin-approval-member@coop.test", "member-password")
	fixture.recordSaving(t, managerToken, member.ID, "deposit", 200000, "ADMIN-APPROVAL-SAVING", "Approval capacity")

	loanRequestID := fixture.createLoanRequest(t, memberToken, 100000, 6)
	startDate := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+loanRequestID+"/approve", adminToken, `{"approved_amount":100000,"duration_months":6,"start_date":"`+startDate+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("admin Manager-stage loan approval: %d %s", response.Code, response.Body.String())
	}
	var loanStage, officerRole string
	if err := fixture.db.QueryRow(`SELECT current_approval_stage FROM loan_requests WHERE id=$1`, loanRequestID).Scan(&loanStage); err != nil {
		t.Fatalf("read loan stage: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT officer_role FROM loan_request_approvals WHERE request_id=$1 AND stage='manager'`, loanRequestID).Scan(&officerRole); err != nil {
		t.Fatalf("read loan approval role: %v", err)
	}
	if loanStage != "ketua_ii" || officerRole != "admin" {
		t.Fatalf("admin loan approval did not preserve Manager-stage behavior: stage=%q officer_role=%q", loanStage, officerRole)
	}
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+loanRequestID+"/approve", adminToken, `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("admin should not approve a later loan stage: %d %s", response.Code, response.Body.String())
	}

	withdrawalRequestID := fixture.createWithdrawalRequest(t, memberToken, 25000, "Manager-stage withdrawal")
	response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/withdrawal-requests/"+withdrawalRequestID+"/approve", adminToken, `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("admin Manager-stage withdrawal approval: %d %s", response.Code, response.Body.String())
	}
	var withdrawalStage string
	if err := fixture.db.QueryRow(`SELECT current_approval_stage FROM withdrawal_requests WHERE id=$1`, withdrawalRequestID).Scan(&withdrawalStage); err != nil {
		t.Fatalf("read withdrawal stage: %v", err)
	}
	if withdrawalStage != "ketua_i" {
		t.Fatalf("admin withdrawal approval should advance to Ketua I, got %q", withdrawalStage)
	}
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/withdrawal-requests/"+withdrawalRequestID+"/approve", adminToken, `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("admin should not approve a later withdrawal stage: %d %s", response.Code, response.Body.String())
	}
}
