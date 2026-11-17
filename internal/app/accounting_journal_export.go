package app

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

func financialJournalsWorkbook(lang string, entries []FinancialJournalEntryView, dateFrom, dateTo string) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	sheet := book.GetSheetName(0)
	if err := book.SetSheetName(sheet, "Jurnal Umum"); err != nil {
		return nil, err
	}
	sheet = "Jurnal Umum"

	period := []any{translate(lang, "date_from"), dateFrom, translate(lang, "date_to"), dateTo}
	if err := book.SetSheetRow(sheet, "A1", &period); err != nil {
		return nil, err
	}
	periodLabelStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "A1", "A1", periodLabelStyle); err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "C1", "C1", periodLabelStyle); err != nil {
		return nil, err
	}

	keys := []string{"date", "reference", "transaction_type", "description", "member_number", "full_name", "status", "recorded_by", "account_code", "account_name", "debit", "credit", "correction_reason", "reversal_of"}
	headers := make([]any, len(keys))
	for i, key := range keys {
		headers[i] = translate(lang, key)
	}
	if err := book.SetSheetRow(sheet, "A3", &headers); err != nil {
		return nil, err
	}

	rowNumber := 4
	for _, entry := range entries {
		base := []any{
			entry.Date,
			entry.ReferenceNo,
			translate(lang, "transaction_type_"+entry.TransactionType),
			entry.Description,
			entry.MemberNo,
			entry.MemberName,
			translate(lang, "journal_status_"+entry.Status),
			entry.RecordedBy,
		}
		if len(entry.Lines) == 0 {
			values := append(append([]any{}, base...), "", "", int64(0), int64(0), entry.CorrectionReason, entry.ReversalOf)
			if err := book.SetSheetRow(sheet, fmt.Sprintf("A%d", rowNumber), &values); err != nil {
				return nil, err
			}
			rowNumber++
			continue
		}
		for _, line := range entry.Lines {
			var debit, credit int64
			if line.Side == "debit" {
				debit = line.Amount
			} else if line.Side == "credit" {
				credit = line.Amount
			}
			values := append(append([]any{}, base...), line.COACode, line.COAName, debit, credit, entry.CorrectionReason, entry.ReversalOf)
			if err := book.SetSheetRow(sheet, fmt.Sprintf("A%d", rowNumber), &values); err != nil {
				return nil, err
			}
			rowNumber++
		}
	}

	headerStyle, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"234C91"}, Pattern: 1}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	if err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "A3", "N3", headerStyle); err != nil {
		return nil, err
	}
	if err := book.SetRowHeight(sheet, 3, 32); err != nil {
		return nil, err
	}
	if err := book.SetColWidth(sheet, "A", "A", 14); err != nil {
		return nil, err
	}
	for column, width := range map[string]float64{"B": 24, "C": 22, "D": 42, "E": 20, "F": 30, "G": 20, "H": 25, "I": 16, "J": 32, "K": 18, "L": 18, "M": 32, "N": 24} {
		if err := book.SetColWidth(sheet, column, column, width); err != nil {
			return nil, err
		}
	}
	if rowNumber > 4 {
		if err := book.AutoFilter(sheet, fmt.Sprintf("A3:N%d", rowNumber-1), nil); err != nil {
			return nil, err
		}
	}
	if err := book.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 3, TopLeftCell: "A4", ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
