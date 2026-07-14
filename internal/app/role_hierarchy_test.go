package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLoanApprovalRequiresEveryOfficerStageAndCreatesLoanOnlyAtFinalStage(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	ketuaIToken := fixture.login(t, "ketua-i@coop.test", "password")
	ketuaIIToken := fixture.login(t, "ketua-ii@coop.test", "password")
	ketuaUtamaToken := fixture.login(t, "ketua-utama@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-001","full_name":"Hierarchy Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	memberToken := fixture.login(t, "member@coop.test", "password")
	requestID := fixture.createLoanRequest(t, memberToken, 1_000_000, 10)

	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", ketuaIToken, `{}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected wrong-stage approval status 403, got %d: %s", response.Code, response.Body.String())
	}

	managerBody := `{"approved_amount":900000,"duration_months":9,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `","interest_rate_bps":125,"note":"Terms verified"}`
	stages := []struct {
		token     string
		body      string
		nextStage string
	}{
		{managerToken, managerBody, "ketua_i"},
		{ketuaIToken, `{"note":"Ketua I review"}`, "ketua_ii"},
		{ketuaIIToken, `{}`, "ketua_utama"},
		{ketuaUtamaToken, `{"note":"Externally completed"}`, ""},
	}
	for index, stage := range stages {
		response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", stage.token, stage.body)
		if response.Code != http.StatusOK {
			t.Fatalf("stage %d approval status: %d: %s", index, response.Code, response.Body.String())
		}
		var status, currentStage string
		if err := fixture.db.QueryRow(`SELECT status,COALESCE(current_approval_stage,'') FROM loan_requests WHERE id=$1`, requestID).Scan(&status, &currentStage); err != nil {
			t.Fatalf("read loan request: %v", err)
		}
		if index < len(stages)-1 && (status != "pending" || currentStage != stage.nextStage) {
			t.Fatalf("stage %d left request at status=%s stage=%s", index, status, currentStage)
		}
		var loanCount int
		if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE loan_request_id=$1`, requestID).Scan(&loanCount); err != nil {
			t.Fatalf("count loans: %v", err)
		}
		expected := 0
		if index == len(stages)-1 {
			expected = 1
		}
		if loanCount != expected {
			t.Fatalf("stage %d expected %d loans, got %d", index, expected, loanCount)
		}
	}

	var decisions, approvedAmount, interestRateBPS int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id=$1`, requestID).Scan(&decisions); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT approved_amount,interest_rate_bps FROM loans WHERE loan_request_id=$1`, requestID).Scan(&approvedAmount, &interestRateBPS); err != nil {
		t.Fatalf("read final loan: %v", err)
	}
	if decisions != 4 || approvedAmount != 900_000 || interestRateBPS != 125 {
		t.Fatalf("unexpected final loan decisions=%d amount=%d rate=%d", decisions, approvedAmount, interestRateBPS)
	}
}

func TestWithdrawalReservationsReleaseAndFinalApprovalCreatesOneSavingRecord(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-002","full_name":"Reserved Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	memberToken := fixture.login(t, "member@coop.test", "password")
	fixture.recordDeposit(t, managerToken, member.ID, 1_000)

	firstID := fixture.createWithdrawalRequest(t, memberToken, 700, "First reservation")
	response := hierarchyRequest(fixture, http.MethodPost, "/api/member/withdrawal-requests", memberToken, `{"amount":400,"note":"Too much reserved"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected reservation to prevent overdraw, got %d: %s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(fixture, http.MethodPost, "/api/member/withdrawal-requests/"+firstID+"/cancel", memberToken, `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel withdrawal: %d: %s", response.Code, response.Body.String())
	}
	secondID := fixture.createWithdrawalRequest(t, memberToken, 400, "Released balance")

	stages := []string{
		managerToken,
		fixture.login(t, "ketua-i@coop.test", "password"),
		fixture.login(t, "ketua-ii@coop.test", "password"),
		fixture.login(t, "ketua-utama@coop.test", "password"),
	}
	for index, token := range stages {
		response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/withdrawal-requests/"+secondID+"/approve", token, `{"note":"verified"}`)
		if response.Code != http.StatusOK {
			t.Fatalf("withdrawal stage %d: %d: %s", index, response.Code, response.Body.String())
		}
		var count int
		if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE type='withdrawal' AND member_id=$1`, member.ID).Scan(&count); err != nil {
			t.Fatalf("count withdrawal records: %v", err)
		}
		expected := 0
		if index == len(stages)-1 {
			expected = 1
		}
		if count != expected {
			t.Fatalf("stage %d expected %d withdrawal records, got %d", index, expected, count)
		}
	}

	var firstReservation, secondReservation string
	if err := fixture.db.QueryRow(`SELECT status FROM withdrawal_reservations WHERE request_id=$1`, firstID).Scan(&firstReservation); err != nil {
		t.Fatalf("read released reservation: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT status FROM withdrawal_reservations WHERE request_id=$1`, secondID).Scan(&secondReservation); err != nil {
		t.Fatalf("read consumed reservation: %v", err)
	}
	if firstReservation != "released" || secondReservation != "consumed" {
		t.Fatalf("unexpected reservation states: first=%s second=%s", firstReservation, secondReservation)
	}
}

func TestOfficerPermissionsForcedPasswordChangeAndImmediateSessionInvalidation(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	ketuaIToken := fixture.login(t, "ketua-i@coop.test", "password")
	ketuaUtamaToken := fixture.login(t, "ketua-utama@coop.test", "password")

	if response := hierarchyRequest(fixture, http.MethodGet, "/api/admin/members", ketuaIToken, ""); response.Code != http.StatusOK {
		t.Fatalf("Ketua I should view members: %d", response.Code)
	}
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/members", ketuaIToken, `{"member_no":"DENIED","full_name":"Denied","join_date":"2026-07-01","status":"active"}`); response.Code != http.StatusForbidden {
		t.Fatalf("Ketua I member mutation should be forbidden, got %d", response.Code)
	}
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", managerToken, `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("Manager Officer administration should be forbidden, got %d", response.Code)
	}

	member := fixture.createMember(t, managerToken, `{"member_no":"OFF-001","full_name":"New Manager","join_date":"2026-07-01","status":"active"}`)
	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", ketuaUtamaToken, `{"member_id":"`+member.ID+`","email":"new-manager@coop.test","role":"manager","password":"temporary123"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create officer: %d: %s", response.Code, response.Body.String())
	}
	var officer struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &officer); err != nil || officer.ID == "" {
		t.Fatalf("decode officer: %v body=%s", err, response.Body.String())
	}
	newToken := fixture.login(t, "new-manager@coop.test", "temporary123")
	response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", newToken, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("temporary password should gate dashboard, got %d", response.Code)
	}
	response = hierarchyRequest(fixture, http.MethodPost, "/api/account/password", newToken, `{"password":"permanent123"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("change password: %d: %s", response.Code, response.Body.String())
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", newToken, ""); response.Code != http.StatusOK {
		t.Fatalf("changed password should unlock dashboard, got %d: %s", response.Code, response.Body.String())
	}

	response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers/"+officer.ID+"/update", ketuaUtamaToken, `{"full_name":"New Manager","role":"manager","active":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("deactivate officer: %d: %s", response.Code, response.Body.String())
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", newToken, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("deactivation should invalidate existing session, got %d", response.Code)
	}
	if response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers/"+officer.ID+"/update", ketuaUtamaToken, `{"full_name":"New Manager","role":"manager","active":true}`); response.Code != http.StatusOK {
		t.Fatalf("reactivate officer: %d: %s", response.Code, response.Body.String())
	}
	managerSession := fixture.login(t, "new-manager@coop.test", "permanent123")
	if response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers/"+officer.ID+"/update", ketuaUtamaToken, `{"full_name":"New Manager","role":"ketua_i","active":true}`); response.Code != http.StatusOK {
		t.Fatalf("change Officer Role: %d: %s", response.Code, response.Body.String())
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", managerSession, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("role change should invalidate existing session, got %d", response.Code)
	}
	response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers/ketua-utama-user-id/update", ketuaUtamaToken, `{"full_name":"Ketua Utama","role":"ketua_utama","active":false}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("last active Ketua Utama must be preserved, got %d: %s", response.Code, response.Body.String())
	}
}

func TestApprovalNotificationsMoveToNextRoleAndFinishWithMember(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-003","full_name":"Notified Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	memberToken := fixture.login(t, "member@coop.test", "password")
	requestID := fixture.createLoanRequest(t, memberToken, 500_000, 5)
	var managerID string
	if err := fixture.db.QueryRow(`SELECT id FROM users WHERE email='admin@coop.test'`).Scan(&managerID); err != nil {
		t.Fatalf("read manager id: %v", err)
	}

	assertActiveNotificationCount(t, fixture, "ketua-i-user-id", 0)
	managerBody := `{"approved_amount":500000,"duration_months":5,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `"}`
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", managerToken, managerBody); response.Code != http.StatusOK {
		t.Fatalf("manager approval: %d: %s", response.Code, response.Body.String())
	}
	assertActiveNotificationCount(t, fixture, "ketua-i-user-id", 1)
	assertActiveNotificationCount(t, fixture, managerID, 0)

	stages := []string{
		fixture.login(t, "ketua-i@coop.test", "password"),
		fixture.login(t, "ketua-ii@coop.test", "password"),
		fixture.login(t, "ketua-utama@coop.test", "password"),
	}
	for _, token := range stages {
		if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", token, `{}`); response.Code != http.StatusOK {
			t.Fatalf("advance approval: %d: %s", response.Code, response.Body.String())
		}
	}
	assertActiveNotificationCount(t, fixture, "member-user-id", 1)
	var events int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notification_events WHERE request_type='loan' AND request_id=$1`, requestID).Scan(&events); err != nil {
		t.Fatalf("count notification events: %v", err)
	}
	if events != 5 {
		t.Fatalf("expected four stage events and one outcome event, got %d", events)
	}
}

func TestExistingMemberKeepsOneLoginAcrossMemberAndOfficerAreas(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	ketuaUtamaToken := fixture.login(t, "ketua-utama@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"DUAL-001","full_name":"Dual Role Member","join_date":"2026-07-14","status":"active","email":"dual-role@coop.test","password":"member-password"}`)

	var userID, originalHash string
	if err := fixture.db.QueryRow(`SELECT id,password_hash FROM users WHERE member_id=$1`, member.ID).Scan(&userID, &originalHash); err != nil {
		t.Fatalf("read Member login: %v", err)
	}
	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", ketuaUtamaToken, `{"member_id":"`+member.ID+`","role":"manager"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("appoint existing Member: %d: %s", response.Code, response.Body.String())
	}
	var officer struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &officer); err != nil || officer.ID == "" {
		t.Fatalf("decode appointment: %v body=%s", err, response.Body.String())
	}
	var currentHash string
	var userCount int
	if err := fixture.db.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&currentHash); err != nil {
		t.Fatalf("read retained login: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM users WHERE member_id=$1 AND historical_identity=FALSE`, member.ID).Scan(&userCount); err != nil {
		t.Fatalf("count current Member logins: %v", err)
	}
	if currentHash != originalHash || userCount != 1 {
		t.Fatalf("appointment replaced Member credentials: hash changed=%v users=%d", currentHash != originalHash, userCount)
	}

	dualToken := fixture.login(t, "dual-role@coop.test", "member-password")
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/member/dashboard", dualToken, ""); response.Code != http.StatusOK {
		t.Fatalf("Officer should retain Member Area: %d: %s", response.Code, response.Body.String())
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", dualToken, ""); response.Code != http.StatusOK {
		t.Fatalf("appointed Member should gain Admin Area: %d: %s", response.Code, response.Body.String())
	}

	requestID := fixture.createLoanRequest(t, dualToken, 250_000, 5)
	body := `{"approved_amount":250000,"duration_months":5,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `"}`
	if response = hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", dualToken, body); response.Code != http.StatusOK {
		t.Fatalf("self approval at assigned stage should be allowed: %d: %s", response.Code, response.Body.String())
	}
	var snapshotMemberID, snapshotMemberNo, snapshotRole string
	if err := fixture.db.QueryRow(`SELECT officer_member_id,officer_member_no,officer_role FROM loan_request_approvals WHERE request_id=$1 AND stage='manager'`, requestID).Scan(&snapshotMemberID, &snapshotMemberNo, &snapshotRole); err != nil {
		t.Fatalf("read immutable approval snapshot: %v", err)
	}
	if snapshotMemberID != member.ID || snapshotMemberNo != "DUAL-001" || snapshotRole != "manager" {
		t.Fatalf("unexpected approval snapshot: member=%q number=%q role=%q", snapshotMemberID, snapshotMemberNo, snapshotRole)
	}

	if _, err := fixture.db.Exec(`UPDATE members SET status='inactive',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, member.ID); err != nil {
		t.Fatalf("deactivate Member: %v", err)
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/member/dashboard", dualToken, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("membership change should invalidate existing session, got %d", response.Code)
	}
	if _, err := fixture.db.Exec(`UPDATE members SET status='active',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, member.ID); err != nil {
		t.Fatalf("reactivate Member: %v", err)
	}
	var appointmentActive bool
	if err := fixture.db.QueryRow(`SELECT active FROM officer_appointments WHERE id=$1`, officer.ID).Scan(&appointmentActive); err != nil {
		t.Fatalf("read suspended appointment: %v", err)
	}
	if appointmentActive {
		t.Fatal("Member reactivation must not automatically restore Officer authority")
	}
	memberOnlyToken := fixture.login(t, "dual-role@coop.test", "member-password")
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/member/dashboard", memberOnlyToken, ""); response.Code != http.StatusOK {
		t.Fatalf("reactivated Member should regain Member Area: %d: %s", response.Code, response.Body.String())
	}
	if response = hierarchyRequest(fixture, http.MethodGet, "/api/admin/dashboard", memberOnlyToken, ""); response.Code != http.StatusForbidden {
		t.Fatalf("suspended appointment should not regain Admin Area, got %d", response.Code)
	}
}

func TestDatabaseProtectsLastActiveKetuaUtamaMember(t *testing.T) {
	fixture := newTestFixture(t)
	var memberID string
	if err := fixture.db.QueryRow(`SELECT member_id FROM officer_appointments WHERE role='ketua_utama' AND active=TRUE`).Scan(&memberID); err != nil {
		t.Fatalf("read Ketua Utama Member: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE members SET status='inactive' WHERE id=$1`, memberID); err == nil {
		t.Fatal("database must reject deactivation of the last active Ketua Utama Member")
	}
}

func hierarchyRequest(fixture testFixture, method, path, token, body string) *httptest.ResponseRecorder {
	var requestBody *bytes.Buffer
	if body == "" {
		requestBody = bytes.NewBuffer(nil)
	} else {
		requestBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	fixture.server.ServeHTTP(recorder, req)
	return recorder
}

func assertActiveNotificationCount(t *testing.T, fixture testFixture, userID string, expected int) {
	t.Helper()
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND resolved_at IS NULL`, userID).Scan(&count); err != nil {
		t.Fatalf("count notifications for %s: %v", userID, err)
	}
	if count != expected {
		t.Fatalf("expected %s to have %s active notifications, got %d", userID, strconv.Itoa(expected), count)
	}
}
