package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestConcurrentLoanApprovalsCreateOneDecisionPerStageAndOneLoan(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-CONCURRENT","full_name":"Concurrent Loan Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	requestID := fixture.createLoanRequest(t, fixture.login(t, "member@coop.test", "password"), 1_000_000, 10)
	managerBody := `{"approved_amount":900000,"duration_months":9,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `"}`

	runConcurrent := func(token, body string) []int {
		t.Helper()
		codes := make([]int, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		for index := range codes {
			go func(index int) {
				defer wait.Done()
				codes[index] = hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", token, body).Code
			}(index)
		}
		wait.Wait()
		return codes
	}

	managerCodes := runConcurrent(managerToken, managerBody)
	assertExactlyOneOK := func(stage string, codes []int) {
		t.Helper()
		ok := 0
		for _, code := range codes {
			if code == http.StatusOK {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("%s concurrent statuses=%v, want exactly one success", stage, codes)
		}
	}
	assertExactlyOneOK("manager", managerCodes)
	assertCount := func(query string, expected int) {
		t.Helper()
		var count int
		if err := fixture.db.QueryRow(query).Scan(&count); err != nil || count != expected {
			t.Fatalf("count query=%q got=%d err=%v want=%d", query, count, err, expected)
		}
	}
	assertCount(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='`+requestID+`' AND stage='manager'`, 1)
	assertCount(`SELECT COUNT(*) FROM loans WHERE loan_request_id='`+requestID+`'`, 0)

	for _, credentials := range []struct{ email, password string }{{"ketua-i@coop.test", "password"}, {"ketua-ii@coop.test", "password"}} {
		token := fixture.login(t, credentials.email, credentials.password)
		if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", token, `{}`); response.Code != http.StatusOK {
			t.Fatalf("advance to final stage: %d %s", response.Code, response.Body.String())
		}
	}
	finalCodes := runConcurrent(fixture.login(t, "ketua-utama@coop.test", "password"), `{}`)
	assertExactlyOneOK("ketua_utama", finalCodes)
	assertCount(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='`+requestID+`' AND stage='ketua_utama'`, 1)
	assertCount(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='`+requestID+`'`, 4)
	assertCount(`SELECT COUNT(*) FROM loans WHERE loan_request_id='`+requestID+`'`, 1)
	assertCount(`SELECT COUNT(*) FROM loan_requests WHERE id='`+requestID+`' AND status='approved' AND current_approval_stage IS NULL`, 1)
}

func TestManagerMayDecreaseLoanTermsAndLaterStagesCannotReplaceSnapshot(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-LOCK","full_name":"Locked Terms Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	requestID := fixture.createLoanRequest(t, fixture.login(t, "member@coop.test", "password"), 1_000_000, 10)
	startDate := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
	managerBody := `{"approved_amount":800000,"duration_months":8,"start_date":"` + startDate + `"}`
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", managerToken, managerBody); response.Code != http.StatusOK {
		t.Fatalf("manager decrease: %d %s", response.Code, response.Body.String())
	}

	ketuaIToken := fixture.login(t, "ketua-i@coop.test", "password")
	tamperingPayload := `{"approved_amount":950000,"duration_months":20,"start_date":"2099-01-01","note":"review only"}`
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", ketuaIToken, tamperingPayload); response.Code != http.StatusOK {
		t.Fatalf("later-stage review payload: %d %s", response.Code, response.Body.String())
	}
	for _, credentials := range []struct{ email, password string }{{"ketua-ii@coop.test", "password"}, {"ketua-utama@coop.test", "password"}} {
		if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", fixture.login(t, credentials.email, credentials.password), `{}`); response.Code != http.StatusOK {
			t.Fatalf("complete approval: %d %s", response.Code, response.Body.String())
		}
	}

	var amount, monthlyFee, totalFee, obligation int64
	var duration int
	var storedStart string
	if err := fixture.db.QueryRow(`SELECT approved_amount,duration_months,start_date,monthly_admin_fee,total_admin_fee,total_obligation FROM loans WHERE loan_request_id=$1`, requestID).Scan(&amount, &duration, &storedStart, &monthlyFee, &totalFee, &obligation); err != nil {
		t.Fatalf("read locked final terms: %v", err)
	}
	if amount != 800_000 || duration != 8 || storedStart != startDate || monthlyFee != 8_000 || totalFee != 64_000 || obligation != 864_000 {
		t.Fatalf("later stage replaced manager snapshot: amount=%d duration=%d start=%s monthly=%d total=%d obligation=%d", amount, duration, storedStart, monthlyFee, totalFee, obligation)
	}
}

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

	managerBody := `{"approved_amount":900000,"duration_months":9,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `","note":"Terms verified"}`
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

	var decisions int
	var approvedAmount, monthlyAdminFee, totalAdminFee int64
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id=$1`, requestID).Scan(&decisions); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT approved_amount,monthly_admin_fee,total_admin_fee FROM loans WHERE loan_request_id=$1`, requestID).Scan(&approvedAmount, &monthlyAdminFee, &totalAdminFee); err != nil {
		t.Fatalf("read final loan: %v", err)
	}
	if decisions != 4 || approvedAmount != 900_000 || monthlyAdminFee != 9_000 || totalAdminFee != 81_000 {
		t.Fatalf("unexpected final loan decisions=%d amount=%d monthly fee=%d total fee=%d", decisions, approvedAmount, monthlyAdminFee, totalAdminFee)
	}
}

func TestFinalLoanApprovalCopiesTheRequestedLoanType(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-TYPE","full_name":"Typed Loan Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	requestID := fixture.createLoanRequest(t, fixture.login(t, "member@coop.test", "password"), 1_000_000, 10)
	fixture.approveLoanRequest(t, managerToken, requestID, 900_000, 9)
	var requestType, loanType string
	if err := fixture.db.QueryRow(`SELECT lr.loan_type,l.loan_type FROM loan_requests lr JOIN loans l ON l.loan_request_id=lr.id WHERE lr.id=$1`, requestID).Scan(&requestType, &loanType); err != nil {
		t.Fatalf("read finalized typed Loan: %v", err)
	}
	if requestType != "regular" || loanType != requestType {
		t.Fatalf("final Loan Type=%q, want request snapshot %q", loanType, requestType)
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
	if _, err := fixture.db.Exec(`UPDATE users SET must_change_password=TRUE WHERE member_id=$1`, member.ID); err != nil {
		t.Fatalf("mark officer member password temporary: %v", err)
	}
	memberEmail := "member@coop.test"
	response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/officers", ketuaUtamaToken, `{"member_id":"`+member.ID+`","role":"manager"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create officer: %d: %s", response.Code, response.Body.String())
	}
	var officer struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &officer); err != nil || officer.ID == "" {
		t.Fatalf("decode officer: %v body=%s", err, response.Body.String())
	}
	newToken := fixture.login(t, memberEmail, "password")
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
	managerSession := fixture.login(t, memberEmail, "permanent123")
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

func TestManagerChangedLoanTermsNotifyMember(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, managerToken, `{"member_no":"HIER-TERMS","full_name":"Terms Changed Member","join_date":"2026-07-01","status":"active"}`)
	if _, err := fixture.db.Exec(`UPDATE users SET member_id=$1 WHERE id='member-user-id'`, member.ID); err != nil {
		t.Fatalf("link member user: %v", err)
	}
	memberToken := fixture.login(t, "member@coop.test", "password")
	requestID := fixture.createLoanRequest(t, memberToken, 500_000, 5)

	managerBody := `{"approved_amount":600000,"duration_months":5,"start_date":"` + time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02") + `"}`
	if response := hierarchyRequest(fixture, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", managerToken, managerBody); response.Code != http.StatusOK {
		t.Fatalf("manager approval: %d: %s", response.Code, response.Body.String())
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notifications n JOIN notification_events e ON e.id=n.event_id WHERE n.user_id='member-user-id' AND n.audience='member' AND n.resolved_at IS NULL AND e.event_type='loan_terms_changed'`).Scan(&count); err != nil {
		t.Fatalf("count terms changed notifications: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one active terms-changed member notification, got %d", count)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/notifications?audience=member", nil)
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("notifications status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Notifications []struct {
			Title string `json:"title"`
			Link  string `json:"link"`
		} `json:"notifications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	if len(payload.Notifications) != 1 || payload.Notifications[0].Title != "Loan terms changed" || payload.Notifications[0].Link != "/member/loan-requests" {
		t.Fatalf("unexpected member notification: %+v", payload.Notifications)
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
