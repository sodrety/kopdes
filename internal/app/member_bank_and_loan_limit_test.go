package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoanRequestRequiresBankDetailsAndUsesFourTimesSavingBalance(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	member := fixture.createMember(t, adminToken, `{"member_no":"BANK-001","full_name":"Bank Member","join_date":"2026-06-18","status":"active","bank_name":"","bank_account":"","email":"bank-member@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "bank-member@coop.test", "member-password")

	request := httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", strings.NewReader(`{"requested_amount":100000,"duration_months":4,"loan_type":"regular","purpose":"Working capital"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+memberToken)
	record := httptest.NewRecorder()
	fixture.server.ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest || !strings.Contains(record.Body.String(), "Bank name and bank account number are required") {
		t.Fatalf("expected missing bank details to block loan request, got %d: %s", record.Code, record.Body.String())
	}

	update := httptest.NewRequest(http.MethodPost, "/api/admin/members/"+member.ID+"/bank-details", bytes.NewBufferString(`{"bank_name":"Test Bank","bank_account":"1234567890"}`))
	update.Header.Set("Content-Type", "application/json")
	update.Header.Set("Authorization", "Bearer "+adminToken)
	updateRecord := httptest.NewRecorder()
	fixture.server.ServeHTTP(updateRecord, update)
	if updateRecord.Code != http.StatusOK {
		t.Fatalf("save bank details: %d %s", updateRecord.Code, updateRecord.Body.String())
	}
	fixture.recordSaving(t, adminToken, member.ID, "deposit", 500000, "BANK-DEP", "Loan capacity")
	memberCookie := fixture.browserLogin(t, "bank-member@coop.test", "member-password")
	pageRequest := httptest.NewRequest(http.MethodGet, "/member/loan-requests", nil)
	pageRequest.AddCookie(memberCookie)
	pageRecord := httptest.NewRecorder()
	fixture.server.ServeHTTP(pageRecord, pageRequest)
	if pageRecord.Code != http.StatusOK || !strings.Contains(pageRecord.Body.String(), `data-rupiah-max="2000000"`) || !strings.Contains(pageRecord.Body.String(), "four times your current Saving Balance") {
		t.Fatalf("expected loan page to expose the saving-based cap, got %d: %s", pageRecord.Code, pageRecord.Body.String())
	}

	exact := fixture.createLoanRequest(t, memberToken, 2000000, 4)
	if exact == "" {
		t.Fatal("expected loan request at four times saving balance")
	}

	tooLargeMember := fixture.createMember(t, adminToken, `{"member_no":"BANK-002","full_name":"Cap Member","join_date":"2026-06-18","status":"active","email":"cap-member@coop.test","password":"member-password"}`)
	tooLargeToken := fixture.login(t, "cap-member@coop.test", "member-password")
	update = httptest.NewRequest(http.MethodPost, "/api/admin/members/"+tooLargeMember.ID+"/bank-details", bytes.NewBufferString(`{"bank_name":"Test Bank","bank_account":"1234567891"}`))
	update.Header.Set("Content-Type", "application/json")
	update.Header.Set("Authorization", "Bearer "+adminToken)
	updateRecord = httptest.NewRecorder()
	fixture.server.ServeHTTP(updateRecord, update)
	if updateRecord.Code != http.StatusOK {
		t.Fatalf("save second member bank details: %d %s", updateRecord.Code, updateRecord.Body.String())
	}
	fixture.recordSaving(t, adminToken, tooLargeMember.ID, "deposit", 500000, "BANK-DEP-2", "Loan capacity")

	request = httptest.NewRequest(http.MethodPost, "/api/member/loan-requests", strings.NewReader(`{"requested_amount":2000001,"duration_months":4,"loan_type":"regular","purpose":"Working capital"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+tooLargeToken)
	record = httptest.NewRecorder()
	fixture.server.ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest || !strings.Contains(record.Body.String(), "four times") {
		t.Fatalf("expected request above four times saving balance to fail, got %d: %s", record.Code, record.Body.String())
	}
}

func TestOfficerAssignmentCreatesMemberLoginWhenMissing(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "ketua-utama@coop.test", "password")
	memberID := "officer-without-login"
	if _, err := fixture.db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ($1,'OFFICER-NOLOGIN','New Ketua Utama','2026-06-18','active')`, memberID); err != nil {
		t.Fatalf("seed officer member: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/officers", strings.NewReader(`{"member_id":"`+memberID+`","role":"ketua_utama","email":"new-ketua@coop.test","password":"temporary-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+adminToken)
	record := httptest.NewRecorder()
	fixture.server.ServeHTTP(record, request)
	if record.Code != http.StatusCreated {
		t.Fatalf("create Officer and login: %d %s", record.Code, record.Body.String())
	}

	var userID string
	if err := fixture.db.QueryRow(`SELECT id FROM users WHERE member_id=$1 AND email='new-ketua@coop.test'`, memberID).Scan(&userID); err != nil {
		t.Fatalf("created member login not found: %v", err)
	}
	var appointmentRole string
	if err := fixture.db.QueryRow(`SELECT role FROM officer_appointments WHERE member_id=$1`, memberID).Scan(&appointmentRole); err != nil {
		t.Fatalf("created Officer appointment not found: %v", err)
	}
	if appointmentRole != "ketua_utama" {
		t.Fatalf("expected Ketua Utama appointment, got %q", appointmentRole)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"new-ketua@coop.test","password":"temporary-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecord := httptest.NewRecorder()
	fixture.server.ServeHTTP(loginRecord, loginRequest)
	if loginRecord.Code != http.StatusOK {
		t.Fatalf("created member login did not authenticate: %d %s", loginRecord.Code, loginRecord.Body.String())
	}
	var loginPayload map[string]any
	if err := json.Unmarshal(loginRecord.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("decode created login response: %v", err)
	}
	if loginPayload["token"] == nil {
		t.Fatal("expected created member login token")
	}
}
