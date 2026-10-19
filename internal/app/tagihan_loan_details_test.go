package app

import (
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestLoanDetailInputsRequireUniqueIdentityAndSeparateSuspension(t *testing.T) {
	month, _ := parseTagihanStatementMonth("2026-10")
	cases := []struct {
		name, action, period     string
		actual                   any
		duplicate, legacy, valid bool
	}{
		{name: "partial payment", action: "payment", period: "2026-10", actual: 50000, valid: true},
		{name: "zero payment", action: "Pembayaran", period: "2026-10", actual: 0, valid: true},
		{name: "blank action", period: "2026-10", actual: 0, valid: true},
		{name: "suspension", action: "suspend", period: "2026-10", actual: 0, valid: true},
		{name: "suspension with repayment", action: "suspend", period: "2026-10", actual: 50000},
		{name: "payment without action", period: "2026-10", actual: 50000},
		{name: "negative", action: "payment", period: "2026-10", actual: -1},
		{name: "fractional", action: "payment", period: "2026-10", actual: "1.5"},
		{name: "missing actual", action: "payment", period: "2026-10", actual: ""},
		{name: "wrong month", action: "payment", period: "2026-09", actual: 0},
		{name: "duplicate ID", action: "payment", period: "2026-10", actual: 0, duplicate: true},
		{name: "ambiguous legacy sheet", action: "payment", period: "2026-10", actual: 0, legacy: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := excelize.NewFile()
			defer w.Close()
			_, _ = w.NewSheet(loanDetailSheet)
			header := []any{"Loan ID", "Member ID"}
			_ = w.SetSheetRow(loanDetailSheet, "A1", &header)
			row := []any{"loan-1", "member-1", "001", "Member", "Reguler", tc.period, 0, 100000, 100000, 10000, tc.action, "Approved", tc.actual}
			_ = w.SetSheetRow(loanDetailSheet, "A2", &row)
			if tc.duplicate {
				_ = w.SetSheetRow(loanDetailSheet, "A3", &row)
			}
			if tc.legacy {
				_, _ = w.NewSheet(suspensionSheet)
			}
			details, actions, present, err := readLoanDetailInputs(w, month, "2026-10-25", "id")
			if !present {
				t.Fatal("detail sheet not recognized")
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.valid {
				if len(details) != 1 {
					t.Fatal(details)
				}
				if tc.action == "suspend" && len(actions) != 1 {
					t.Fatal("missing per-loan suspension")
				}
			}
		})
	}
}
