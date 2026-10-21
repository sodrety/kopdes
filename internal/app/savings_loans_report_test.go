package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSavingsLoansReportFiltersAndAccess(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	member := f.createMember(t, token, `{"member_no":"REPORT-001","full_name":"Report Filter Member","join_date":"2026-01-01","status":"active"}`)
	for _, record := range []struct{ id, date, ref string }{{"report-1", "2026-06-01", "REPORT-FIRST"}, {"report-2", "2026-06-30", "REPORT-LAST"}, {"report-3", "2026-07-01", "REPORT-OUTSIDE"}} {
		_, err := f.db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ($1,$2,'deposit','wajib',123456,$3,$4,'','admin-user-id')`, record.id, member.ID, record.date, record.ref)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query           string
		status          int
		present, absent []string
	}{
		{"?search=filter&date_from=2026-06-01&date_to=2026-06-30", 200, []string{"REPORT-FIRST", "REPORT-LAST", "123.456"}, []string{"REPORT-OUTSIDE"}},
		{"?search=REPORT-001&date_from=2026-06-30&date_to=2026-06-30", 200, []string{"REPORT-LAST"}, []string{"REPORT-FIRST", "REPORT-OUTSIDE"}},
		{"?search=unmatched", 200, []string{"No records match"}, []string{"REPORT-FIRST"}},
		{"?search=%25", 200, []string{"No records match"}, []string{"REPORT-FIRST"}},
		{"?date_from=2026-02-30", 400, nil, nil},
		{"?date_from=2026-07-01&date_to=2026-06-01", 400, nil, nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/reports/savings-loans"+tc.query, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			f.server.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			for _, value := range tc.present {
				if !strings.Contains(rec.Body.String(), value) {
					t.Errorf("missing %q: %s", value, rec.Body.String())
				}
			}
			for _, value := range tc.absent {
				if strings.Contains(rec.Body.String(), value) {
					t.Errorf("unexpected %q", value)
				}
			}
		})
	}
	memberToken := f.login(t, "member@coop.test", "password")
	req := httptest.NewRequest(http.MethodGet, "/admin/reports/savings-loans", nil)
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member access status %d", rec.Code)
	}
}
