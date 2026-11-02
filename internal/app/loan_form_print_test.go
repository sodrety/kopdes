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
