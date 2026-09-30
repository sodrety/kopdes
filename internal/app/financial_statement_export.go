package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const financialStatementSheet = "Bentuk Laporan"

type financialStatementLine struct {
	Code          string
	Name          string
	NormalBalance string
	Amount        int64
}

type financialStatementExportRow struct {
	Kind      string
	Code      string
	Name      string
	Side      string
	Indicator string
	Equation  string
	Amount    int64
}

type financialStatementExportStyles struct {
	title    int
	section  int
	detail   int
	subtotal int
	amount   int
	period   int
}

func financialStatementDateFromQuery(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().In(jakartaLocation), nil
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("invalid financial statement date")
	}
	return date, nil
}

func (s *Server) bentukLaporanWorkbook(asOf time.Time) ([]byte, error) {
	if err := s.ensureFinancialJournalsBackfilled(); err != nil {
		return nil, err
	}
	dateTo := asOf.Format("2006-01-02")
	allRows, _, err := s.financialTrialBalanceForAdmin("", dateTo)
	if err != nil {
		return nil, err
	}
	yearStart := time.Date(asOf.Year(), time.January, 1, 0, 0, 0, 0, asOf.Location()).Format("2006-01-02")
	yearRows, _, err := s.financialTrialBalanceForAdmin(yearStart, dateTo)
	if err != nil {
		return nil, err
	}
	return buildBentukLaporanWorkbook(allRows, yearRows, asOf)
}

func buildBentukLaporanWorkbook(allRows, yearRows []FinancialTrialBalanceRow, asOf time.Time) ([]byte, error) {
	allIncome, allExpense, err := financialStatementIncomeAndExpense(allRows)
	if err != nil {
		return nil, err
	}
	yearIncome, yearExpense, err := financialStatementIncomeAndExpense(yearRows)
	if err != nil {
		return nil, err
	}
	allNet, err := checkedReportSub(allIncome, allExpense)
	if err != nil {
		return nil, err
	}
	yearNet, err := checkedReportSub(yearIncome, yearExpense)
	if err != nil {
		return nil, err
	}
	priorNet, err := checkedReportSub(allNet, yearNet)
	if err != nil {
		return nil, err
	}

	var currentAssets, fixedAssets, liabilities, equity []financialStatementLine
	var operatingIncome, otherIncome, costOfSales, operatingExpenses, otherExpenses []financialStatementLine
	seenPriorSurplus, seenCurrentSurplus := false, false
	for _, row := range allRows {
		if row.AccountType != "asset" && row.AccountType != "liability" && row.AccountType != "equity" {
			continue
		}
		amount, err := statementAccountAmount(row)
		if err != nil {
			return nil, err
		}
		line := financialStatementLine{Code: row.Code, Name: row.Name, NormalBalance: row.NormalBalance, Amount: amount}
		switch row.AccountType {
		case "asset":
			if isNonCurrentAssetCode(row.Code) {
				fixedAssets = append(fixedAssets, line)
			} else {
				currentAssets = append(currentAssets, line)
			}
		case "liability":
			liabilities = append(liabilities, line)
		case "equity":
			switch row.Code {
			case "32000":
				line.Amount, err = checkedReportAdd(line.Amount, priorNet)
				seenPriorSurplus = true
			case "32001":
				line.Amount, err = checkedReportAdd(line.Amount, yearNet)
				seenCurrentSurplus = true
			}
			if err != nil {
				return nil, err
			}
			equity = append(equity, line)
		}
	}
	if !seenPriorSurplus && priorNet != 0 {
		equity = append(equity, financialStatementLine{Name: "SHU TAHUN LALU (BELUM DITUTUP)", NormalBalance: "C", Amount: priorNet})
	}
	if !seenCurrentSurplus && yearNet != 0 {
		equity = append(equity, financialStatementLine{Name: "SHU TAHUN BERJALAN (BELUM DITUTUP)", NormalBalance: "C", Amount: yearNet})
	}

	for _, row := range yearRows {
		if row.AccountType != "revenue" && row.AccountType != "expense" {
			continue
		}
		amount, err := statementAccountAmount(row)
		if err != nil {
			return nil, err
		}
		line := financialStatementLine{Code: row.Code, Name: row.Name, NormalBalance: row.NormalBalance, Amount: amount}
		if row.AccountType == "revenue" {
			if strings.HasPrefix(row.Code, "7") {
				otherIncome = append(otherIncome, line)
			} else {
				operatingIncome = append(operatingIncome, line)
			}
		} else if strings.HasPrefix(row.Code, "5") {
			costOfSales = append(costOfSales, line)
		} else if strings.HasPrefix(row.Code, "8") {
			otherExpenses = append(otherExpenses, line)
		} else {
			operatingExpenses = append(operatingExpenses, line)
		}
	}
	sortFinancialStatementLines(currentAssets)
	sortFinancialStatementLines(fixedAssets)
	sortFinancialStatementLines(liabilities)
	sortFinancialStatementLines(equity)
	sortFinancialStatementLines(operatingIncome)
	sortFinancialStatementLines(otherIncome)
	sortFinancialStatementLines(costOfSales)
	sortFinancialStatementLines(operatingExpenses)
	sortFinancialStatementLines(otherExpenses)

	currentAssetsTotal, err := sumFinancialStatementLines(currentAssets)
	if err != nil {
		return nil, err
	}
	fixedAssetsTotal, err := sumFinancialStatementLines(fixedAssets)
	if err != nil {
		return nil, err
	}
	assetsTotal, err := checkedReportAdd(currentAssetsTotal, fixedAssetsTotal)
	if err != nil {
		return nil, err
	}
	liabilitiesTotal, err := sumFinancialStatementLines(liabilities)
	if err != nil {
		return nil, err
	}
	equityTotal, err := sumFinancialStatementLines(equity)
	if err != nil {
		return nil, err
	}
	operatingIncomeTotal, err := sumFinancialStatementLines(operatingIncome)
	if err != nil {
		return nil, err
	}
	otherIncomeTotal, err := sumFinancialStatementLines(otherIncome)
	if err != nil {
		return nil, err
	}
	costOfSalesTotal, err := sumFinancialStatementLines(costOfSales)
	if err != nil {
		return nil, err
	}
	operatingExpensesTotal, err := sumFinancialStatementLines(operatingExpenses)
	if err != nil {
		return nil, err
	}
	otherExpensesTotal, err := sumFinancialStatementLines(otherExpenses)
	if err != nil {
		return nil, err
	}
	liabilitiesAndEquity, err := checkedReportAdd(liabilitiesTotal, equityTotal)
	if err != nil {
		return nil, err
	}
	grossProfit, err := checkedReportSub(operatingIncomeTotal, costOfSalesTotal)
	if err != nil {
		return nil, err
	}
	operatingProfit, err := checkedReportSub(grossProfit, operatingExpensesTotal)
	if err != nil {
		return nil, err
	}
	netProfit, err := checkedReportAdd(operatingProfit, otherIncomeTotal)
	if err != nil {
		return nil, err
	}
	netProfit, err = checkedReportSub(netProfit, otherExpensesTotal)
	if err != nil {
		return nil, err
	}

	leftRows := buildBentukLaporanNeracaRows(currentAssets, currentAssetsTotal, fixedAssets, fixedAssetsTotal, assetsTotal, liabilities, liabilitiesTotal, equity, equityTotal, liabilitiesAndEquity)
	rightRows := buildBentukLaporanProfitLossRows(operatingIncome, operatingIncomeTotal, costOfSales, costOfSalesTotal, grossProfit, operatingExpenses, operatingExpensesTotal, operatingProfit, otherIncome, otherIncomeTotal, otherExpenses, otherExpensesTotal, netProfit)
	return writeBentukLaporanSheet(leftRows, rightRows, asOf)
}

func financialStatementIncomeAndExpense(rows []FinancialTrialBalanceRow) (int64, int64, error) {
	var income, expense int64
	for _, row := range rows {
		if row.AccountType != "revenue" && row.AccountType != "expense" {
			continue
		}
		amount, err := statementAccountAmount(row)
		if err != nil {
			return 0, 0, err
		}
		if row.AccountType == "revenue" {
			income, err = checkedReportAdd(income, amount)
		} else {
			expense, err = checkedReportAdd(expense, amount)
		}
		if err != nil {
			return 0, 0, err
		}
	}
	return income, expense, nil
}

func statementAccountAmount(row FinancialTrialBalanceRow) (int64, error) {
	switch row.AccountType {
	case "asset", "expense":
		return checkedReportSub(row.Debit, row.Credit)
	case "liability", "equity", "revenue":
		return checkedReportSub(row.Credit, row.Debit)
	default:
		return 0, fmt.Errorf("unsupported chart-of-accounts type %q", row.AccountType)
	}
}

func isNonCurrentAssetCode(code string) bool {
	if len(code) < 2 {
		return false
	}
	prefix := code[:2]
	return prefix >= "16" && prefix <= "19"
}

func sumFinancialStatementLines(lines []financialStatementLine) (int64, error) {
	var total int64
	for _, line := range lines {
		value, err := checkedReportAdd(total, line.Amount)
		if err != nil {
			return 0, err
		}
		total = value
	}
	return total, nil
}

func buildBentukLaporanNeracaRows(currentAssets []financialStatementLine, currentAssetsTotal int64, fixedAssets []financialStatementLine, fixedAssetsTotal int64, assetsTotal int64, liabilities []financialStatementLine, liabilitiesTotal int64, equity []financialStatementLine, equityTotal int64, liabilitiesAndEquity int64) []financialStatementExportRow {
	rows := []financialStatementExportRow{{Kind: "section", Name: "AKTIVA"}, {Kind: "blank"}}
	rows = appendFinancialStatementDetails(rows, currentAssets)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "AKTIVA LANCAR", Indicator: "XXX", Equation: "A", Amount: currentAssetsTotal}, financialStatementExportRow{Kind: "blank"})
	rows = appendFinancialStatementDetails(rows, fixedAssets)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "AKTIVA TIDAK LANCAR", Indicator: "XXX", Equation: "B", Amount: fixedAssetsTotal})
	rows = append(rows, financialStatementExportRow{Kind: "total", Name: "TOTAL AKTIVA", Indicator: "XXX", Equation: "C = A + B", Amount: assetsTotal}, financialStatementExportRow{Kind: "blank"}, financialStatementExportRow{Kind: "section", Name: "PASIVA"})
	rows = appendFinancialStatementDetails(rows, liabilities)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL PASIVA", Indicator: "XXX", Equation: "D", Amount: liabilitiesTotal}, financialStatementExportRow{Kind: "blank"}, financialStatementExportRow{Kind: "section", Name: "MODAL"})
	rows = appendFinancialStatementDetails(rows, equity)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL MODAL", Indicator: "XXX", Equation: "E", Amount: equityTotal})
	rows = append(rows, financialStatementExportRow{Kind: "total", Name: "TOTAL PASIVA DAN MODAL", Indicator: "XXX", Equation: "F = D + E", Amount: liabilitiesAndEquity})
	return rows
}

func buildBentukLaporanProfitLossRows(operatingIncome []financialStatementLine, operatingIncomeTotal int64, costOfSales []financialStatementLine, costOfSalesTotal int64, grossProfit int64, operatingExpenses []financialStatementLine, operatingExpensesTotal int64, operatingProfit int64, otherIncome []financialStatementLine, otherIncomeTotal int64, otherExpenses []financialStatementLine, otherExpensesTotal int64, netProfit int64) []financialStatementExportRow {
	rows := []financialStatementExportRow{{Kind: "blank"}, {Kind: "blank"}}
	rows = appendFinancialStatementDetails(rows, operatingIncome)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL PENDAPATAN", Indicator: "XXX", Equation: "A", Amount: operatingIncomeTotal}, financialStatementExportRow{Kind: "blank"})
	rows = appendFinancialStatementDetails(rows, costOfSales)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL BEBAN POKOK", Indicator: "XXX", Equation: "B", Amount: costOfSalesTotal}, financialStatementExportRow{Kind: "subtotal", Name: "LABA (RUGI) KOTOR", Indicator: "XXX", Equation: "C = A - B", Amount: grossProfit}, financialStatementExportRow{Kind: "blank"})
	rows = appendFinancialStatementDetails(rows, operatingExpenses)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL BEBAN ADMINISTRASI DAN UMUM", Indicator: "XXX", Equation: "D", Amount: operatingExpensesTotal}, financialStatementExportRow{Kind: "subtotal", Name: "LABA (RUGI) OPERASIONAL", Indicator: "XXX", Equation: "E = C - D", Amount: operatingProfit})
	rows = appendFinancialStatementDetails(rows, otherIncome)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL PENDAPATAN LAIN LAIN", Indicator: "XXX", Equation: "F", Amount: otherIncomeTotal})
	rows = appendFinancialStatementDetails(rows, otherExpenses)
	rows = append(rows, financialStatementExportRow{Kind: "subtotal", Name: "TOTAL BEBAN LAIN LAIN", Indicator: "XXX", Equation: "G", Amount: otherExpensesTotal}, financialStatementExportRow{Kind: "total", Name: "SISA HASIL USAHA", Indicator: "XXX", Equation: "E + F - G", Amount: netProfit})
	return rows
}

func appendFinancialStatementDetails(rows []financialStatementExportRow, lines []financialStatementLine) []financialStatementExportRow {
	for _, line := range lines {
		side := "DR"
		if line.NormalBalance == "C" {
			side = "CR"
		}
		rows = append(rows, financialStatementExportRow{Kind: "detail", Code: line.Code, Name: line.Name, Side: side, Amount: line.Amount})
	}
	return rows
}

func writeBentukLaporanSheet(leftRows, rightRows []financialStatementExportRow, asOf time.Time) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	if err := book.SetSheetName("Sheet1", financialStatementSheet); err != nil {
		return nil, err
	}
	styles, err := newFinancialStatementExportStyles(book)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		cell  string
		value string
		style int
	}{
		{cell: "A1", value: "KKSUK DHARMAJAYA", style: styles.title},
		{cell: "G1", value: "KKSUK DHARMAJAYA", style: styles.title},
		{cell: "A2", value: "NERACA", style: styles.section},
		{cell: "G2", value: "LABA RUGI", style: styles.section},
		{cell: "A3", value: "Per " + asOf.Format("02-01-2006"), style: styles.period},
		{cell: "G3", value: "YTD 01-01-" + fmt.Sprint(asOf.Year()) + " s.d. " + asOf.Format("02-01-2006"), style: styles.period},
	} {
		if err := book.SetCellValue(financialStatementSheet, item.cell, item.value); err != nil {
			return nil, err
		}
		if err := book.SetCellStyle(financialStatementSheet, item.cell, item.cell, item.style); err != nil {
			return nil, err
		}
	}
	if err := setBentukLaporanColumnWidths(book); err != nil {
		return nil, err
	}
	if err := writeFinancialStatementRows(book, leftRows, 1, styles); err != nil {
		return nil, err
	}
	if err := writeFinancialStatementRows(book, rightRows, 7, styles); err != nil {
		return nil, err
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func newFinancialStatementExportStyles(book *excelize.File) (financialStatementExportStyles, error) {
	title, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14, Color: "1F2937"}})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	section, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "1F2937"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DCE6F1"}}})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	detail, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Size: 10, Color: "1F2937"}})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	subtotal, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "1F2937"}, Border: []excelize.Border{{Type: "top", Color: "64748B", Style: 1}}})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	amount, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Size: 10, Color: "1F2937"}, Alignment: &excelize.Alignment{Horizontal: "right"}, NumFmt: 3})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	period, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Italic: true, Size: 9, Color: "475569"}})
	if err != nil {
		return financialStatementExportStyles{}, err
	}
	return financialStatementExportStyles{title: title, section: section, detail: detail, subtotal: subtotal, amount: amount, period: period}, nil
}

func setBentukLaporanColumnWidths(book *excelize.File) error {
	widths := []struct {
		column string
		width  float64
	}{
		{column: "A", width: 10}, {column: "B", width: 40}, {column: "C", width: 8}, {column: "D", width: 12}, {column: "E", width: 18}, {column: "F", width: 3},
		{column: "G", width: 10}, {column: "H", width: 44}, {column: "I", width: 8}, {column: "J", width: 12}, {column: "K", width: 18},
	}
	for _, item := range widths {
		if err := book.SetColWidth(financialStatementSheet, item.column, item.column, item.width); err != nil {
			return err
		}
	}
	return nil
}

func writeFinancialStatementRows(book *excelize.File, rows []financialStatementExportRow, firstColumn int, styles financialStatementExportStyles) error {
	statementName := "Neraca"
	if firstColumn == 7 {
		statementName = "Laba Rugi"
	}
	for index, row := range rows {
		rowNumber := index + 4
		nameColumn := firstColumn + 1
		styleStart, styleEnd := firstColumn, firstColumn+4
		nameCell, _ := excelize.CoordinatesToCellName(nameColumn, rowNumber)
		style := styles.detail
		switch row.Kind {
		case "section":
			if err := book.SetCellValue(financialStatementSheet, nameCell, row.Name); err != nil {
				return err
			}
			style = styles.section
		case "detail":
			for offset, value := range []any{row.Code, row.Name, row.Side, statementName, row.Amount} {
				cell, _ := excelize.CoordinatesToCellName(firstColumn+offset, rowNumber)
				if err := book.SetCellValue(financialStatementSheet, cell, value); err != nil {
					return err
				}
			}
			amountCell, _ := excelize.CoordinatesToCellName(firstColumn+4, rowNumber)
			if err := book.SetCellStyle(financialStatementSheet, amountCell, amountCell, styles.amount); err != nil {
				return err
			}
		case "subtotal", "total":
			for offset, value := range []any{"", row.Name, row.Indicator, row.Equation, row.Amount} {
				cell, _ := excelize.CoordinatesToCellName(firstColumn+offset, rowNumber)
				if err := book.SetCellValue(financialStatementSheet, cell, value); err != nil {
					return err
				}
			}
			style = styles.subtotal
			amountCell, _ := excelize.CoordinatesToCellName(firstColumn+4, rowNumber)
			if err := book.SetCellStyle(financialStatementSheet, amountCell, amountCell, styles.amount); err != nil {
				return err
			}
		}
		if row.Kind == "blank" {
			continue
		}
		startCell, _ := excelize.CoordinatesToCellName(styleStart, rowNumber)
		endCell, _ := excelize.CoordinatesToCellName(styleEnd, rowNumber)
		if err := book.SetCellStyle(financialStatementSheet, startCell, endCell, style); err != nil {
			return err
		}
		if row.Kind == "detail" || row.Kind == "subtotal" || row.Kind == "total" {
			amountCell, _ := excelize.CoordinatesToCellName(firstColumn+4, rowNumber)
			if err := book.SetCellStyle(financialStatementSheet, amountCell, amountCell, styles.amount); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortFinancialStatementLines(lines []financialStatementLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].Code == "" {
			return false
		}
		if lines[j].Code == "" {
			return true
		}
		return lines[i].Code < lines[j].Code
	})
}
