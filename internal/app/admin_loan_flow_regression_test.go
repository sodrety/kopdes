package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAdminLoanPageSupportsOfficerStagesAndMemberSelection(t *testing.T) {
	f := newTestFixture(t)
	manager := f.login(t, "admin@coop.test", "password")
	super := establishSuperAdmin(t, f)
	_, admin := provisionAdminOfficer(t, f, super, manager)
	member := f.createMember(t, manager, `{"member_no":"ADMIN-DROPDOWN-001","full_name":"Dropdown Borrower","join_date":"2026-01-01","status":"active","bank_name":"Test","bank_account":"123456","email":"dropdown@coop.test","password":"member-password"}`)
	f.ensureLoanCapacityForMember(t, manager, member.ID, 30_000_000)
	var memberNo string
	if err := f.db.QueryRow(`SELECT member_no FROM members WHERE id=$1`, member.ID).Scan(&memberNo); err != nil {
		t.Fatal(err)
	}
	body := `{"member_no":"` + memberNo + `","loan_type":"regular","requested_amount":30000000,"duration_months":24,"purpose":"Browser regression","acknowledge_warnings":true,"idempotency_key":"admin-browser-regression"}`
	created := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/on-behalf", admin, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var requestID string
	if err := f.db.QueryRow(`SELECT id FROM loan_requests WHERE member_id=$1`, member.ID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	var creatorMember, creatorID string
	if err := f.db.QueryRow(`SELECT u.member_id,u.id FROM users u JOIN loan_requests lr ON lr.created_by=u.id WHERE lr.id=$1`, requestID).Scan(&creatorMember, &creatorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE users SET full_name='' WHERE id=$1`, creatorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE members SET full_name='Creator Member Name' WHERE id=$1`, creatorMember); err != nil {
		t.Fatal(err)
	}
	page := adminLoanJSONRequest(f, http.MethodGet, "/admin/loan-requests", admin, "")
	if page.Code != http.StatusOK {
		t.Fatalf("page: %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), `<select name="member_no"`) || !strings.Contains(page.Body.String(), memberNo+`">Dropdown Borrower`) {
		t.Error("expected member dropdown showing borrower name and NPP")
	}
	list := adminLoanJSONRequest(f, http.MethodGet, "/api/admin/loan-requests", admin, "")
	var payload struct {
		Requests []struct {
			CreatedByName string `json:"created_by_name"`
		} `json:"loan_requests"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].CreatedByName != "Creator Member Name" {
		t.Errorf("creator fallback: %s", list.Body.String())
	}
	date := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
	stages := []struct{ email, body string }{
		{"admin@coop.test", `{"approved_amount":30000000,"duration_months":24,"start_date":"` + date + `"}`},
		{"ketua-ii@coop.test", `{}`}, {"ketua-i@coop.test", `{}`}, {"ketua-utama@coop.test", `{}`},
	}
	action := `action="/api/admin/loan-requests/` + requestID + `/approve"`
	for _, stage := range stages {
		token := f.login(t, stage.email, "password")
		page := adminLoanJSONRequest(f, http.MethodGet, "/admin/loan-requests", token, "")
		if !strings.Contains(page.Body.String(), action) {
			t.Errorf("missing approval form for %s", stage.email)
		}
		if stage.email != "admin@coop.test" && strings.Contains(page.Body.String(), `name="approved_amount"`) {
			t.Errorf("later officer can edit locked terms: %s", stage.email)
		}
		approved := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", token, stage.body)
		if approved.Code != http.StatusOK {
			t.Fatalf("approve %s: %d %s", stage.email, approved.Code, approved.Body.String())
		}
	}
	page = adminLoanJSONRequest(f, http.MethodGet, "/admin/loan-requests?status=approved", manager, "")
	if strings.Contains(page.Body.String(), action) {
		t.Error("approved request still has approval form")
	}
	f.disburseLoanRequest(t, requestID, date)
}
