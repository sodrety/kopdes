package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoanApprovalUsesDisbursementDateAndDefaultMappings(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "manager"
		if override {
			name = "super_admin"
		}
		t.Run(name, func(t *testing.T) {
			f := newTestFixture(t)
			manager := f.login(t, "admin@coop.test", "password")
			member := f.createMember(t, manager, `{"member_no":"DEFAULT-LOAN","full_name":"Default Loan Borrower","join_date":"2026-01-01","status":"active","email":"defaults@coop.test","password":"member-password"}`)
			_ = member
			requestID := f.createLoanRequest(t, f.login(t, "defaults@coop.test", "member-password"), 1_000_000, 10)
			token := manager
			route := "approve"
			if override {
				token = establishSuperAdmin(t, f)
				route = "override-approve"
			}
			page := adminLoanJSONRequest(f, http.MethodGet, "/admin/loan-requests", token, "")
			for _, field := range []string{`name="start_date"`, `name="coa_code"`, `name="cash_coa_code"`, `name="admin_fee_coa_code"`} {
				if strings.Contains(page.Body.String(), field) {
					t.Errorf("approval page still contains %s", field)
				}
			}
			body := `{"approved_amount":1000000,"duration_months":10}`
			// Old clients cannot choose either the effective start date or posting accounts.
			if override {
				body = `{"approved_amount":1000000,"duration_months":10,"start_date":"2099-01-01","coa_code":"DO-NOT-USE","cash_coa_code":"DO-NOT-USE","admin_fee_coa_code":"DO-NOT-USE"}`
			}
			response := adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/"+route, token, body)
			if response.Code != http.StatusOK {
				t.Fatalf("approval without date: %d %s", response.Code, response.Body.String())
			}
			if !override {
				for _, email := range []string{"ketua-ii@coop.test", "ketua-i@coop.test"} {
					response = adminLoanJSONRequest(f, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", f.login(t, email, "password"), `{}`)
					if response.Code != http.StatusOK {
						t.Fatalf("later approval: %d %s", response.Code, response.Body.String())
					}
				}
			}
			var cash, receivable, fee string
			if err := f.db.QueryRow(`SELECT COALESCE(proposed_cash_coa_code,''),COALESCE(proposed_loan_coa_code,''),COALESCE(proposed_admin_fee_coa_code,'') FROM loan_requests WHERE id=$1`, requestID).Scan(&cash, &receivable, &fee); err != nil {
				t.Fatal(err)
			}
			if cash != "" || receivable != "" || fee != "" {
				t.Fatalf("unexpected overrides: %q %q %q", cash, receivable, fee)
			}
			for _, component := range []string{"cash_bank", "loan_receivable", "admin_fee_income"} {
				if _, err := f.db.Exec(`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,loan_type,coa_code,active) VALUES($1,$1,'loan',$2,'regular','BANK',TRUE)`, "default-"+component, component); err != nil {
					t.Fatal(err)
				}
			}
			date := time.Now().In(time.FixedZone("Asia/Jakarta", 7*60*60)).Format("2006-01-02")
			loan := f.disburseLoanRequest(t, requestID, date)
			var start string
			if err := f.db.QueryRow(`SELECT start_date FROM loans WHERE id=$1`, loan.ID).Scan(&start); err != nil {
				t.Fatal(err)
			}
			if start != date {
				t.Fatalf("start=%s want disbursement=%s", start, date)
			}
			var lines, overrides int
			if err := f.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN l.mapping_override THEN 1 ELSE 0 END),0) FROM financial_journal_lines l JOIN financial_journal_entries e ON e.id=l.journal_id WHERE e.transaction_id=$1 AND l.coa_code='BANK'`, loan.ID).Scan(&lines, &overrides); err != nil {
				t.Fatal(err)
			}
			if lines != 3 || overrides != 0 {
				t.Fatalf("default journal lines=%d overrides=%d", lines, overrides)
			}
		})
	}
}
