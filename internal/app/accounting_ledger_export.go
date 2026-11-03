package app

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

func financialLedgerWorkbook(lang string, entries []FinancialLedgerLine, opening, ending int64, account COAAccount, dateFrom, dateTo string) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	sheet := book.GetSheetName(0)
	sheetName := translate(lang, "general_ledger")
	if err := book.SetSheetName(sheet, sheetName); err != nil {
		return nil, err
	}
	sheet = sheetName

	for row, values := range [][]any{
		{translate(lang, "account_code"), account.Code, translate(lang, "account_name"), account.Name},
		{translate(lang, "date_from"), dateFrom, translate(lang, "date_to"), dateTo},
		{translate(lang, "opening_balance"), opening, translate(lang, "ending_balance"), ending},
	} {
		if err := book.SetSheetRow(sheet, fmt.Sprintf("A%d", row+1), &values); err != nil {
			return nil, err
		}
	}

	keys := []string{"date", "reference", "description", "debit", "credit", "balance"}
	headers := make([]any, len(keys))
	for i, key := range keys {
		headers[i] = translate(lang, key)
	}
	if err := book.SetSheetRow(sheet, "A5", &headers); err != nil {
		return nil, err
	}

	for index, entry := range entries {
		values := []any{entry.Date, entry.ReferenceNo, entry.Description, entry.Debit, entry.Credit, entry.Balance}
		if err := book.SetSheetRow(sheet, fmt.Sprintf("A%d", index+6), &values); err != nil {
			return nil, err
		}
	}

	labelStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	for _, cellRange := range [][2]string{{"A1", "A3"}, {"C1", "C3"}} {
		if err := book.SetCellStyle(sheet, cellRange[0], cellRange[1], labelStyle); err != nil {
			return nil, err
		}
	}
	headerStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"234C91"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "A5", "F5", headerStyle); err != nil {
		return nil, err
	}
	if err := book.SetRowHeight(sheet, 5, 32); err != nil {
		return nil, err
	}
	for column, width := range map[string]float64{"A": 16, "B": 24, "C": 42, "D": 18, "E": 18, "F": 18} {
		if err := book.SetColWidth(sheet, column, column, width); err != nil {
			return nil, err
		}
	}
	if err := book.AutoFilter(sheet, fmt.Sprintf("A5:F%d", len(entries)+5), nil); err != nil {
		return nil, err
	}
	if err := book.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 5, TopLeftCell: "A6", ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
