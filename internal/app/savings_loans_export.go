package app

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

func savingsLoansReportWorkbook(lang, search, from, to string, rows []savingsLoansReportRow, totals savingsLoansReportAmounts) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	sheet := book.GetSheetName(0)
	// Report values are a current-balance snapshot, matching the browser totals.
	var buildErr error
	apply := func(err error) {
		if buildErr == nil {
			buildErr = err
		}
	}
	apply(book.MergeCell(sheet, "A1", "J1"))
	apply(book.SetCellValue(sheet, "A1", translate(lang, "savings_loans_report")))
	apply(book.MergeCell(sheet, "A2", "J2"))
	apply(book.SetCellValue(sheet, "A2", translate(lang, "savings_loans_report_description")))
	apply(book.SetSheetRow(sheet, "A3", &[]any{translate(lang, "search"), search, "", translate(lang, "date_from"), from, "", translate(lang, "date_to"), to}))
	keys := []string{"member_no", "full_name", "simpanan_pokok", "simpanan_wajib", "simpanan_manasuka", "simpanan_shu_tertahan", "simpanan_khusus", "saving_balance", "savings_loans_running_total", "savings_loans_remaining_total"}
	headers := make([]any, len(keys))
	for i, key := range keys {
		headers[i] = translate(lang, key)
	}
	apply(book.SetSheetRow(sheet, "A5", &headers))
	amounts := func(a savingsLoansReportAmounts) []any {
		return []any{a.PokokBalance, a.WajibBalance, a.SukarelaBalance, a.SHUBalance, a.KhususBalance, a.SavingsBalance, a.RunningLoanTotal, a.RemainingLoanBalance}
	}
	for i, row := range rows {
		values := append([]any{row.MemberNo, row.FullName}, amounts(row.savingsLoansReportAmounts)...)
		apply(book.SetSheetRow(sheet, fmt.Sprintf("A%d", i+6), &values))
	}
	lastData := len(rows) + 5
	totalRow := lastData + 1
	totalValues := append([]any{translate(lang, "total"), ""}, amounts(totals)...)
	apply(book.SetSheetRow(sheet, fmt.Sprintf("A%d", totalRow), &totalValues))
	apply(book.MergeCell(sheet, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("B%d", totalRow)))
	apply(book.SetColWidth(sheet, "A", "A", 22))
	apply(book.SetColWidth(sheet, "B", "B", 34))
	apply(book.SetColWidth(sheet, "C", "J", 23))
	apply(book.SetRowHeight(sheet, 1, 28))
	apply(book.SetRowHeight(sheet, 2, 42))
	apply(book.SetRowHeight(sheet, 5, 44))
	apply(book.SetRowHeight(sheet, totalRow, 24))
	titleStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 16, Color: "234C91"}})
	apply(err)
	noteStyle, err := book.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}})
	apply(err)
	headerStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"234C91"}}, Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}})
	apply(err)
	moneyStyle, err := book.NewStyle(&excelize.Style{NumFmt: 3})
	apply(err)
	totalStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 3, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8EFFA"}}, Border: []excelize.Border{{Type: "top", Color: "234C91", Style: 1}}})
	apply(err)
	apply(book.SetCellStyle(sheet, "A1", "J1", titleStyle))
	apply(book.SetCellStyle(sheet, "A2", "J2", noteStyle))
	apply(book.SetCellStyle(sheet, "A5", "J5", headerStyle))
	apply(book.SetCellStyle(sheet, "C6", fmt.Sprintf("J%d", totalRow), moneyStyle))
	apply(book.SetCellStyle(sheet, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("J%d", totalRow), totalStyle))
	apply(book.AutoFilter(sheet, fmt.Sprintf("A5:J%d", lastData), nil))
	apply(book.SetPanes(sheet, &excelize.Panes{Freeze: true, XSplit: 2, YSplit: 5, TopLeftCell: "C6", ActivePane: "bottomRight"}))
	if buildErr != nil {
		return nil, buildErr
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
