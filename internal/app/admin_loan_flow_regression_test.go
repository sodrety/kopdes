package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAdminLoanApprovalAndDisbursementUseKetuaUtamaThreshold(t *testing.T) {
	tests := []struct {
		name          string
		amount        int64
		finalStage    string
		approvalCount int
	}{
		{name: "twenty million", amount: 20_000_000, approvalCount: 3},
		{name: "just below threshold", amount: 24_999_999, approvalCount: 3},
		{name: "at threshold", amount: 25_000_000, finalStage: "ketua_utama", approvalCount: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTestFixture(t)
			managerToken := f.login(t, "admin@coop.test", "password")
			superToken := establishSuperAdmin(t, f)
			_, adminToken := provisionAdminOfficer(t, f, superToken, managerToken)
			member := f.createMember(t, managerToken, `{"member_no":"LOAN-THRESHOLD","full_name":"Approval Threshold Borrower","join_date":"2026-01-01","status":"active"}`)

			createBody := fmt.Sprintf(`{"member_no":"LOAN-THRESHOLD","loan_type":"regular","requested_amount":%d,"duration_months":24,"purpose":"Approval threshold regression","acknowledge_warnings":true,"idempotency_key":"approval-threshold-%d"}`, tt.amount, tt.amount)
			created := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/on-behalf", adminToken, createBody)
			if created.Code != http.StatusCreated {
				t.Fatalf("create admin-origin loan: %d %s", created.Code, created.Body.String())
			}
			var requestID string
			if err := f.db.QueryRow(`SELECT id FROM loan_requests WHERE member_id=$1`, member.ID).Scan(&requestID); err != nil {
				t.Fatalf("read created loan request: %v", err)
			}

			startDate := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
			managerApproval := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", managerToken, fmt.Sprintf(`{"approved_amount":%d,"duration_months":24,"start_date":"%s"}`, tt.amount, startDate))
			if managerApproval.Code != http.StatusOK {
				t.Fatalf("manager approval: %d %s", managerApproval.Code, managerApproval.Body.String())
			}
			for _, approver := range []string{"ketua-ii@coop.test", "ketua-i@coop.test"} {
				response := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", f.login(t, approver, "password"), `{}`)
				if response.Code != http.StatusOK {
					t.Fatalf("%s approval: %d %s", approver, response.Code, response.Body.String())
				}
			}

			var status, finalStage string
			if err := f.db.QueryRow(`SELECT status,COALESCE(current_approval_stage,'') FROM loan_requests WHERE id=$1`, requestID).Scan(&status, &finalStage); err != nil {
				t.Fatalf("read request after Ketua I approval: %v", err)
			}
			if tt.finalStage == "" {
				if status != "approved" || finalStage != "" {
					t.Fatalf("amount below threshold should be final at Ketua I, got status=%q stage=%q", status, finalStage)
				}
			} else if status != "pending" || finalStage != tt.finalStage {
				t.Fatalf("amount at threshold should continue to %s, got status=%q stage=%q", tt.finalStage, status, finalStage)
			}

			if tt.finalStage != "" {
				response := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", f.login(t, "ketua-utama@coop.test", "password"), `{}`)
				if response.Code != http.StatusOK {
					t.Fatalf("Ketua Utama approval: %d %s", response.Code, response.Body.String())
				}
			}

			var approvalCount int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id=$1 AND decision='approved'`, requestID).Scan(&approvalCount); err != nil {
				t.Fatalf("count loan approvals: %v", err)
			}
			if approvalCount != tt.approvalCount {
				t.Fatalf("expected %d approvals, got %d", tt.approvalCount, approvalCount)
			}
			f.disburseLoanRequest(t, requestID, startDate)
		})
	}
}

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
	body := `{"member_no":"` + memberNo + `","loan_type":"regular","requested_amount":55000000,"duration_months":24,"purpose":"Browser regression above savings limit","acknowledge_warnings":true,"idempotency_key":"admin-browser-regression"}`
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
	adminApprovalAction := `action="/api/admin/loan-requests/` + requestID + `/approve"`
	adminApprovalStart := strings.Index(page.Body.String(), adminApprovalAction)
	if adminApprovalStart < 0 {
		t.Fatalf("missing manager approval form for admin-origin request: %s", page.Body.String())
	}
	adminApprovalEnd := strings.Index(page.Body.String()[adminApprovalStart:], "</form>")
	if adminApprovalEnd < 0 {
		t.Fatalf("unterminated manager approval form for admin-origin request")
	}
	adminApprovalForm := page.Body.String()[adminApprovalStart : adminApprovalStart+adminApprovalEnd]
	if strings.Contains(adminApprovalForm, `data-rupiah-max=`) {
		t.Error("admin-origin manager approval form still applies the member savings limit")
	}
	if strings.Contains(adminApprovalForm, `approved-amount-hint-`) {
		t.Error("admin-origin manager approval form still displays the member savings limit hint")
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
		{"admin@coop.test", `{"approved_amount":55000000,"duration_months":24,"start_date":"` + date + `"}`},
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

func TestLoanApprovalRechecksSavingsLimitAtFinalStage(t *testing.T) {
	f := newTestFixture(t)
	managerToken := f.login(t, "admin@coop.test", "password")
	member := f.createMember(t, managerToken, `{"member_no":"LIMIT-RECHECK-001","full_name":"Limit Recheck Borrower","join_date":"2026-01-01","status":"active","member_type":"customer","email":"limit-recheck@coop.test","password":"member-password"}`)
	memberToken := f.login(t, "limit-recheck@coop.test", "member-password")
	f.ensureLoanCapacityForMember(t, managerToken, member.ID, 26_000_000)
	requestID := f.createLoanRequest(t, memberToken, 26_000_000, 12)
	managerPage := adminLoanJSONRequest(f, http.MethodGet, "/admin/loan-requests", managerToken, "")
	if managerPage.Code != http.StatusOK {
		t.Fatalf("manager loan page: %d", managerPage.Code)
	}
	approvalAction := `action="/api/admin/loan-requests/` + requestID + `/approve"`
	formStart := strings.Index(managerPage.Body.String(), approvalAction)
	if formStart < 0 {
		t.Fatalf("missing manager approval form for member-origin request: %s", managerPage.Body.String())
	}
	formEnd := strings.Index(managerPage.Body.String()[formStart:], "</form>")
	if formEnd < 0 || !strings.Contains(managerPage.Body.String()[formStart:formStart+formEnd], `data-rupiah-max=`) {
		t.Error("member-origin manager approval form must retain the savings limit")
	}
	date := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")

	managerApproval := hierarchyRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", managerToken, `{"approved_amount":26000000,"duration_months":12,"start_date":"`+date+`"}`)
	if managerApproval.Code != http.StatusOK {
		t.Fatalf("manager approval: %d %s", managerApproval.Code, managerApproval.Body.String())
	}
	var stage string
	if err := f.db.QueryRow(`SELECT current_approval_stage FROM loan_requests WHERE id=$1`, requestID).Scan(&stage); err != nil {
		t.Fatalf("read stage after manager approval: %v", err)
	}
	if stage != "ketua_ii" {
		t.Fatalf("expected request to advance to ketua_ii, got %q", stage)
	}

	// Model a savings adjustment after the manager approved but before the
	// final approver decides. The approved amount should now exceed the cap.
	result, err := f.db.Exec(`UPDATE saving_records SET amount=10000000 WHERE member_id=$1 AND type='deposit' AND category='wajib'`, member.ID)
	if err != nil {
		t.Fatalf("reduce member savings after manager approval: %v", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		t.Fatalf("expected to adjust one Simpanan Wajib deposit, changed %d rows", changed)
	}
	var limit int64
	if err := f.db.QueryRow(`SELECT loan_limit FROM member_loan_amount_limits WHERE member_id=$1`, member.ID).Scan(&limit); err != nil {
		t.Fatalf("read reduced loan limit: %v", err)
	}
	if limit >= 26_000_000 {
		t.Fatalf("test setup did not reduce the current loan limit below the proposed amount: %d", limit)
	}

	for _, approver := range []struct{ email, stage string }{
		{"ketua-ii@coop.test", "ketua_ii"},
		{"ketua-i@coop.test", "ketua_i"},
	} {
		response := hierarchyRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", f.login(t, approver.email, "password"), `{}`)
		if response.Code != http.StatusOK {
			t.Fatalf("%s approval before final stage: %d %s", approver.stage, response.Code, response.Body.String())
		}
	}

	finalApproval := hierarchyRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", f.login(t, "ketua-utama@coop.test", "password"), `{}`)
	if finalApproval.Code != http.StatusBadRequest {
		t.Fatalf("expected final approval to reject the now-over-limit request, got %d: %s", finalApproval.Code, finalApproval.Body.String())
	}
	assertError(t, finalApproval.Body.Bytes(), "VALIDATION_ERROR", "Loan amount exceeds the member-type limit based on Simpanan Wajib and Simpanan Manasuka.")

	var status, currentStage string
	if err := f.db.QueryRow(`SELECT status,current_approval_stage FROM loan_requests WHERE id=$1`, requestID).Scan(&status, &currentStage); err != nil {
		t.Fatalf("read request after rejected final approval: %v", err)
	}
	if status != "pending" || currentStage != "ketua_utama" {
		t.Fatalf("rejected final approval should leave the request pending at ketua_utama, got status=%q stage=%q", status, currentStage)
	}
	var finalDecisions, loans int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM loan_request_approvals WHERE request_id=$1 AND stage='ketua_utama'`, requestID).Scan(&finalDecisions); err != nil {
		t.Fatalf("count final-stage approval decisions: %v", err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM loans WHERE loan_request_id=$1`, requestID).Scan(&loans); err != nil {
		t.Fatalf("count loans for rejected request: %v", err)
	}
	if finalDecisions != 0 || loans != 0 {
		t.Fatalf("rejected approval must roll back its decision and create no loan: final decisions=%d loans=%d", finalDecisions, loans)
	}
}
