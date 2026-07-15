package app

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseRupiahAmount(t *testing.T) {
	t.Parallel()

	valid := map[string]int64{
		"1":                            1,
		"1250000":                      1250000,
		"9223372036854775807":          9223372036854775807,
		"Rp 9.223.372.036.854.775.807": 9223372036854775807,
		"Rp 1.250.000":                 1250000,
		"rp 1,250,000":                 1250000,
		"  Rp 25.000.000  ":            25000000,
	}
	for input, expected := range valid {
		input, expected := input, expected
		t.Run("valid_"+url.QueryEscape(input), func(t *testing.T) {
			t.Parallel()
			actual, err := parseRupiahAmount(input)
			if err != nil || actual != expected {
				t.Fatalf("parseRupiahAmount(%q) = %d, %v; want %d", input, actual, err, expected)
			}
		})
	}

	for _, input := range []string{"", "Rp", "-1", "1.50", "1,50", "1.000,00", "1,000.00", "12.34.567", "1 000", "abc", "9223372036854775808", "Rp 9.223.372.036.854.775.808", "999999999999999999999999"} {
		input := input
		t.Run("invalid_"+url.QueryEscape(input), func(t *testing.T) {
			t.Parallel()
			if amount, err := parseRupiahAmount(input); err == nil {
				t.Fatalf("parseRupiahAmount(%q) = %d; want error", input, amount)
			}
		})
	}
}

func TestBindRequestWithRupiahAmountNormalizesBrowserForm(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	form := url.Values{"amount": {"Rp 1.250.000"}, "note": {"exact amount"}}
	request := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	var destination struct {
		Amount int64  `form:"amount"`
		Note   string `form:"note"`
	}
	if err := bindRequestWithRupiahAmount(context, &destination, "amount"); err != nil {
		t.Fatalf("bind normalized form: %v", err)
	}
	if destination.Amount != 1250000 || destination.Note != "exact amount" {
		t.Fatalf("unexpected normalized form: %+v", destination)
	}
}
