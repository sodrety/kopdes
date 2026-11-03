package app

import (
	"encoding/base64"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRenderLoanFormPrintPagePreservesBackgroundImage(t *testing.T) {
	forms := []struct {
		name       string
		background []byte
	}{
		{
			name:       "application",
			background: mustReadLoanFormTestAsset(t, disbursementApplicationFormTemplate, "templates/loan-disbursement-application-form.jpg"),
		},
		{
			name:       "acceptance",
			background: mustReadLoanFormTestAsset(t, loanReportTemplates, "templates/loan-acceptance-form.jpg"),
		},
	}

	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			renderLoanFormPrintPage(context, "Form", "/", form.background, nil, nil)

			wantImageSource := `src="data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString(form.background) + `"`
			body := html.UnescapeString(recorder.Body.String())
			if !strings.Contains(body, wantImageSource) {
				t.Fatalf("rendered form background source is missing or rewritten; expected the embedded JPEG data URI")
			}
		})
	}
}

func TestLoanAcceptancePrintUsesFirstDueDateAndTemplateTypography(t *testing.T) {
	report := loanReportData{
		Loan:         Loan{StartDate: "2026-09-12", ApprovedAmount: 10_000_000, DurationMonths: 10, MonthlyInstallment: 1_100_000},
		FirstDueDate: "2026-10-27",
	}
	texts := loanAcceptanceFormText(report)
	if got, want := texts[6].value, "27 Oktober 2026"; got != want {
		t.Fatalf("acceptance repayment date = %q, want first due date %q", got, want)
	}

	report.FirstDueDate = ""
	texts = loanAcceptanceFormText(report)
	if got, want := texts[6].value, "20 Oktober 2026"; got != want {
		t.Fatalf("acceptance repayment date fallback = %q, want scheduled first due date %q", got, want)
	}
	report.FirstDueDate = "2026-10-27"
	texts = loanAcceptanceFormText(report)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	background := mustReadLoanFormTestAsset(t, loanReportTemplates, "templates/loan-acceptance-form.jpg")
	renderLoanFormPrintPage(context, "Acceptance", "/", background, texts, loanAcceptanceFormMasks(report), loanAcceptanceFormPrintStyles())

	body := html.UnescapeString(recorder.Body.String())
	dateEnd := strings.Index(body, "27 Oktober 2026</span>")
	if dateEnd < 0 {
		t.Fatal("rendered acceptance print page is missing the first due date")
	}
	dateStart := strings.LastIndex(body[:dateEnd], "<span")
	dateField := body[dateStart:dateEnd]
	for _, want := range []string{"loan-form-field-template-font", "loan-form-field-bold", "loan-form-field-italic", "font-size:9.50pt"} {
		if !strings.Contains(dateField, want) {
			t.Errorf("acceptance due date field is missing template style %q: %s", want, dateField)
		}
	}
}

func mustReadLoanFormTestAsset(t *testing.T, assets interface {
	ReadFile(string) ([]byte, error)
}, path string) []byte {
	t.Helper()
	content, err := assets.ReadFile(path)
	if err != nil {
		t.Fatalf("read form asset %s: %v", path, err)
	}
	return content
}
