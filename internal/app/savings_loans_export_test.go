package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestSavingsLoansExcelExportUsesFiltersAndNumericBalances(t *testing.T) {
	f := newTestFixture(t)
	token := f.login(t, "admin@coop.test", "password")
	for i := 0; i < 13; i++ {
		name := fmt.Sprintf("Excel Member %02d", i)
		if i == 0 {
			name = "=1+1"
		}
		member := f.createMember(t, token, fmt.Sprintf(`{"member_no":"XLSX-%02d","full_name":%q,"join_date":"2026-01-01","status":"active"}`, i, name))
		date := "2026-06-15"
		if i == 12 {
			date = "2026-07-01"
		}
		if _, err := f.db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,recorded_by) VALUES ($1,$2,'deposit','wajib',3000000000,$3,'admin-user-id')`, fmt.Sprintf("export-%d", i), member.ID, date); err != nil {
			t.Fatal(err)
		}
	}
	for _, lang := range []string{"en", "id"} {
		rec := savingsLoansReportResponse(f, token, "?export=xlsx&search=XLSX-&date_from=2026-06-01&date_to=2026-06-30", lang)
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
			t.Fatalf("content type %q", got)
		}
		if !strings.Contains(rec.Header().Get("Content-Disposition"), "laporan-simpan-pinjam.xlsx") {
			t.Fatal("missing download filename")
		}
		book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		sheet := book.GetSheetName(0)
		rows, err := book.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 18 {
			t.Fatalf("expected 12 members plus headers and total, got %d rows", len(rows))
		}
		for cell, want := range map[string]string{"B3": "XLSX-", "E3": "2026-06-01", "H3": "2026-06-30", "A6": "XLSX-00", "B6": "=1+1", "D6": "3000000000", "H6": "3000000000", "D18": "36000000000", "H18": "36000000000", "I18": "0", "J18": "0"} {
			got, err := book.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
			if err != nil || got != want {
				t.Errorf("%s=%q want %q (%v)", cell, got, want, err)
			}
		}
		kind, err := book.GetCellType(sheet, "D6")
		if err != nil || (kind != excelize.CellTypeNumber && kind != excelize.CellTypeUnset) {
			t.Fatalf("balance must be numeric, type=%v error=%v", kind, err)
		}
		if formula, _ := book.GetCellFormula(sheet, "B6"); formula != "" {
			t.Fatalf("member name became a formula: %q", formula)
		}
		header, _ := book.GetCellValue(sheet, "I5")
		expected := "Running loans including fees"
		if lang == "id" {
			expected = "Total pinjaman berjalan + biaya"
		}
		if header != expected {
			t.Fatalf("%s heading=%q", lang, header)
		}
		_ = book.Close()
	}
	empty := savingsLoansReportResponse(f, token, "?export=xlsx&search=absent", "")
	book, err := excelize.OpenReader(bytes.NewReader(empty.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	if value, _ := book.GetCellValue(book.GetSheetName(0), "H6", excelize.Options{RawCellValue: true}); value != "0" {
		t.Fatalf("empty export total=%q", value)
	}
	if rec := savingsLoansReportResponse(f, token, "?export=xlsx&date_from=invalid", ""); rec.Code != 400 {
		t.Fatalf("invalid export date status=%d", rec.Code)
	}
	memberToken := f.login(t, "member@coop.test", "password")
	if rec := savingsLoansReportResponse(f, memberToken, "?export=xlsx", ""); rec.Code != 403 {
		t.Fatalf("member export status=%d", rec.Code)
	}
}
