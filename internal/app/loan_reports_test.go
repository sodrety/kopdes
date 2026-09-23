package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApprovedLoanReportPDFsAlignToFormFields(t *testing.T) {
	fixture := newTestFixture(t)
	adminToken := fixture.login(t, "admin@coop.test", "password")
	fixture.createMember(t, adminToken, `{"member_no":"M-LOAN-REPORT","full_name":"Loan Report Layout Member","join_date":"2026-01-01","status":"active","email":"loan-report-layout@coop.test","password":"member-password"}`)
	memberToken := fixture.login(t, "loan-report-layout@coop.test", "member-password")
	requestID := fixture.createLoanRequest(t, memberToken, 500_000, 3)
	loan := fixture.approveLoanRequest(t, adminToken, requestID, 500_000, 3)

	reports := []struct {
		path, filename    string
		fieldPositions    []string
		minimumFieldMasks int
	}{
		{
			path:     "/api/admin/loans/" + loan.ID + "/reports/application.pdf",
			filename: "form-pengajuan-pinjaman-" + loan.ID + ".pdf",
			fieldPositions: []string{
				"1 0 0 1 132.28 686.08 Tm (Loan Report Layout Member) Tj ET",
				"1 0 0 1 77.57 543.32 Tm (Purpose: Test loan) Tj ET",
			},
			minimumFieldMasks: 11,
		},
		{
			path:     "/api/admin/loans/" + loan.ID + "/reports/acceptance.pdf",
			filename: "surat-akseptasi-pinjaman-" + loan.ID + ".pdf",
			fieldPositions: []string{
				"1 0 0 1 220.07 603.45 Tm (Loan Report Layout Member \\(M-LOAN-REPORT\\)) Tj ET",
				"1 0 0 1 138.30 519.27 Tm (500.000) Tj ET",
				"1 0 0 1 305.46 519.27 Tm (lima ratus ribu rupiah\\)) Tj ET",
				"1 0 0 1 39.69 76.97 Tm (Purpose: Test loan) Tj ET",
			},
			minimumFieldMasks: 12,
		},
	}

	for _, report := range reports {
		t.Run(report.filename, func(t *testing.T) {
			response := loanReportRequest(fixture, report.path, adminToken)
			if response.Code != http.StatusOK {
				t.Fatalf("export loan report: status=%d body=%s", response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "application/pdf") {
				t.Fatalf("content type=%q; want application/pdf", contentType)
			}
			if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, `filename="`+report.filename+`"`) {
				t.Fatalf("content disposition=%q; want filename %q", disposition, report.filename)
			}

			pdf := response.Body.String()
			if !strings.HasPrefix(pdf, "%PDF-1.4") {
				t.Fatalf("response is not a PDF: prefix=%q", pdf[:min(len(pdf), 16)])
			}
			for _, fieldPosition := range report.fieldPositions {
				if !strings.Contains(pdf, fieldPosition) {
					t.Fatalf("form field is not placed on the matching row; expected PDF content %q", fieldPosition)
				}
			}
			if maskCount := strings.Count(pdf, "1 1 1 rg "); maskCount < report.minimumFieldMasks {
				t.Fatalf("PDF has %d white field masks; want at least %d to clear the printed placeholders", maskCount, report.minimumFieldMasks)
			}
			if strings.Index(pdf, "1 1 1 rg ") > strings.Index(pdf, "BT /F1") {
				t.Fatal("white field masks must be painted before generated text")
			}
		})
	}

	seedUser(t, fixture.db, "loan-report-treasurer-id", "loan-report-treasurer@coop.test", "password", "bendahara")
	treasurerToken := fixture.login(t, "loan-report-treasurer@coop.test", "password")
	for _, report := range reports {
		response := loanReportRequest(fixture, report.path, treasurerToken)
		if response.Code != http.StatusOK {
			t.Fatalf("bendahara cannot export %s: status=%d body=%s", report.filename, response.Code, response.Body.String())
		}
	}
}

func loanReportRequest(fixture testFixture, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	fixture.server.ServeHTTP(response, request)
	return response
}
