package app

import (
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestSuspensionSheetBahasaAndLegacyCompatibility(t *testing.T) {
	month, _ := parseTagihanStatementMonth("2026-10")
	for _, name := range []string{suspensionSheet, legacySuspensionSheet} {
		t.Run(name, func(t *testing.T) {
			w := excelize.NewFile()
			defer w.Close()
			_, _ = w.NewSheet(name)
			header := []any{"Loan ID", "Member ID", "NPP", "Name", "Month", "Fee", "Action", "Reason"}
			_ = w.SetSheetRow(name, "A1", &header)
			row := []any{"loan-1", "member-1", "001", "Member", "2026-10", 10000, "suspend", "Approved"}
			_ = w.SetSheetRow(name, "A2", &row)
			actions, err := readSuspensionActions(w, month, "2026-10-25", "id")
			if err != nil || len(actions) != 1 || actions[0].LoanID != "loan-1" {
				t.Fatal(actions, err)
			}
			other := suspensionSheet
			if name == other {
				other = legacySuspensionSheet
			}
			_, _ = w.NewSheet(other)
			if _, err = readSuspensionActions(w, month, "2026-10-25", "id"); err == nil {
				t.Fatal("ambiguous duplicate suspension sheets accepted")
			}
		})
	}
}
