package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRespondErrorResolvesMessageExactlyOnceForHTMLAndJSON(t *testing.T) {
	tests := []struct {
		name        string
		language    string
		message     string
		htmx        bool
		wantMessage string
	}{
		{name: "English key HTMX", language: "en", message: "error_invalid_rupiah_amount", htmx: true, wantMessage: "Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807"},
		{name: "Bahasa key HTMX", language: "id", message: "error_invalid_rupiah_amount", htmx: true, wantMessage: "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807"},
		{name: "English key JSON", language: "en", message: "error_invalid_rupiah_amount", wantMessage: "Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807"},
		{name: "Bahasa key JSON", language: "id", message: "error_invalid_rupiah_amount", wantMessage: "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807"},
		{name: "Pre-resolved Bahasa remains unchanged", language: "id", message: "Persetujuan pinjaman tidak valid", htmx: true, wantMessage: "Persetujuan pinjaman tidak valid"},
		{name: "Legacy canonical JSON remains compatible", language: "id", message: "Invalid loan approval", wantMessage: "Invalid loan approval"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodPost, "/api/test", nil)
			context.Request.AddCookie(&http.Cookie{Name: languageCookie, Value: test.language})
			if test.htmx {
				context.Request.Header.Set("HX-Request", "true")
			}

			respondError(context, http.StatusBadRequest, "VALIDATION_ERROR", test.message)

			if test.htmx {
				if body := response.Body.String(); !strings.Contains(body, test.wantMessage) || strings.Contains(body, "error_invalid_rupiah_amount") {
					t.Fatalf("unexpected localized HTMX error: %s", body)
				}
				return
			}
			var body errorBody
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode JSON error: %v", err)
			}
			if body.Error.Message != test.wantMessage {
				t.Fatalf("message=%q, want %q", body.Error.Message, test.wantMessage)
			}
		})
	}
}
