package app

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const loanDetailSheet = "Rincian Pinjaman"

type tagihanLoanDetailInput struct {
	LoanID, MemberID, Action, Reason string
	Actual                           int64
	ExcelRow                         int
}

func reviewedExcelAmount(w *excelize.File, sheet, cell string) (int64, error) {
	value, err := w.GetCellValue(sheet, cell)
	if err != nil {
		return 0, err
	}
	formula, err := w.GetCellFormula(sheet, cell)
	if err != nil {
		return 0, err
	}
	if formula != "" {
		value, err = w.CalcCellValue(sheet, cell)
		if err != nil {
			return 0, err
		}
	}
	amount, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || amount < 0 || amount > 1_000_000_000_000 {
		return 0, errTagihanReview
	}
	return amount, nil
}

func readLoanDetailInputs(w *excelize.File, month tagihanStatementMonth, date, lang string) (map[string]tagihanLoanDetailInput, []suspensionAction, bool, error) {
	index, err := w.GetSheetIndex(loanDetailSheet)
	if err != nil {
		return nil, nil, false, err
	}
	if index < 0 {
		return nil, nil, false, nil
	}
	for _, old := range []string{suspensionSheet, legacySuspensionSheet} {
		i, e := w.GetSheetIndex(old)
		if e != nil || i >= 0 {
			return nil, nil, true, errTagihanReview
		}
	}
	rows, err := w.GetRows(loanDetailSheet)
	if err != nil || len(rows) == 0 || len(rows) > 10001 {
		return nil, nil, true, errTagihanReview
	}
	inputs := map[string]tagihanLoanDetailInput{}
	var actions []suspensionAction
	for i, row := range rows[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		d := tagihanLoanDetailInput{LoanID: tagihanCell(row, 0), MemberID: tagihanCell(row, 1), Reason: tagihanCell(row, 11), ExcelRow: i + 2}
		if d.LoanID == "" || d.MemberID == "" || tagihanCell(row, 5) != month.Value {
			return nil, nil, true, errTagihanReview
		}
		if _, ok := inputs[d.LoanID]; ok {
			return nil, nil, true, errTagihanReview
		}
		action := strings.ToLower(tagihanCell(row, 10))
		switch action {
		case "":
			d.Action = ""
		case "payment", strings.ToLower(translate("en", "detail_payment")), strings.ToLower(translate("id", "detail_payment")):
			d.Action = "payment"
		case "suspend", strings.ToLower(translate("en", "suspension_action")), strings.ToLower(translate("id", "suspension_action")):
			d.Action = "suspend"
		default:
			return nil, nil, true, errTagihanReview
		}
		d.Actual, err = reviewedExcelAmount(w, loanDetailSheet, fmt.Sprintf("M%d", i+2))
		if err != nil {
			return nil, nil, true, err
		}
		if d.Action != "payment" && d.Actual != 0 {
			return nil, nil, true, errTagihanReview
		}
		if d.Action == "suspend" {
			fee, e := reviewedExcelAmount(w, loanDetailSheet, fmt.Sprintf("J%d", i+2))
			if e != nil {
				return nil, nil, true, e
			}
			actions = append(actions, suspensionAction{LoanID: d.LoanID, MemberID: d.MemberID, Month: month.Value, Fee: fee, RecordDate: date, Reason: d.Reason, ExcelRow: i + 2})
		}
		inputs[d.LoanID] = d
	}
	return inputs, actions, true, nil
}

func (s *Server) addLoanDetailSheet(w *excelize.File, month tagihanStatementMonth, lang string) error {
	if _, err := w.NewSheet(loanDetailSheet); err != nil {
		return err
	}
	keys := []string{"suspension_loan_id", "member_id", "member_no", "full_name", "loan_type", "statement_month", "detail_arrears", "detail_current", "detail_bill", "suspension_fee", "detail_action", "suspension_reason", "detail_actual", "detail_fee_received", "review_remaining", "review_difference", "suspension_first_due", "suspension_final_due"}
	header := make([]any, len(keys))
	for i, k := range keys {
		header[i] = translate(lang, k)
	}
	if err := w.SetSheetRow(loanDetailSheet, "A1", &header); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT l.id,l.member_id,m.member_no,m.full_name,l.loan_type,l.monthly_admin_fee,l.total_admin_fee,l.duration_months,l.final_due_date,
 COALESCE(SUM(CASE WHEN i.due_date<$1 THEN i.scheduled_amount-i.paid_amount ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN i.due_date>=$1 THEN i.scheduled_amount-i.paid_amount ELSE 0 END),0),MIN(i.due_date)
 FROM loans l JOIN members m ON m.id=l.member_id JOIN loan_installments i ON i.loan_id=l.id
 WHERE m.status='active' AND m.member_type NOT IN ('daily_worker','customer') AND l.status IN ('active','adjustment_due') AND l.remaining_balance>0 AND i.paid_amount<i.scheduled_amount AND i.due_date<=$2
 GROUP BY l.id,l.member_id,m.member_no,m.full_name,l.loan_type,l.monthly_admin_fee,l.total_admin_fee,l.duration_months,l.final_due_date ORDER BY m.member_no,l.id`, month.Value+"-01", month.CutoffDate)
	if err != nil {
		return err
	}
	defer rows.Close()
	r := 2
	for rows.Next() {
		var id, member, npp, name, kind, final, first string
		var monthly sql.NullInt64
		var admin, arrears, current int64
		var duration int
		if err = rows.Scan(&id, &member, &npp, &name, &kind, &monthly, &admin, &duration, &final, &arrears, &current, &first); err != nil {
			return err
		}
		fee := int64(0)
		if kind == "regular" {
			fee = suspensionFee(monthly, admin, duration)
		}
		values := []any{id, member, npp, name, translate(lang, "loan_type_"+kind), month.Value, arrears, current, arrears + current, fee, "", "", nil, nil, nil, nil, first, final}
		cell, _ := excelize.CoordinatesToCellName(1, r)
		if err = w.SetSheetRow(loanDetailSheet, cell, &values); err != nil {
			return err
		}
		// Stable internal identifiers drive summary formulas; display labels remain translated.
		_ = w.SetCellValue(loanDetailSheet, fmt.Sprintf("U%d", r), kind)
		isPayment := fmt.Sprintf(`OR(K%d="payment",K%d="%s",K%d="%s")`, r, r, translate("en", "detail_payment"), r, translate("id", "detail_payment"))
		isSuspension := fmt.Sprintf(`OR(K%d="suspend",K%d="%s",K%d="%s")`, r, r, translate("en", "suspension_action"), r, translate("id", "suspension_action"))
		_ = w.SetCellFormula(loanDetailSheet, fmt.Sprintf("M%d", r), fmt.Sprintf("IF(%s,I%d,0)", isPayment, r))
		_ = w.SetCellFormula(loanDetailSheet, fmt.Sprintf("N%d", r), fmt.Sprintf("IF(%s,J%d,0)", isSuspension, r))
		_ = w.SetCellFormula(loanDetailSheet, fmt.Sprintf("O%d", r), fmt.Sprintf("IF(%s,0,MAX(0,I%d-M%d))", isSuspension, r, r))
		_ = w.SetCellFormula(loanDetailSheet, fmt.Sprintf("P%d", r), fmt.Sprintf("M%d-I%d", r, r))
		r++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_ = w.SetColVisible(loanDetailSheet, "U", false)
	_ = w.SetColWidth(loanDetailSheet, "A", "B", 33)
	_ = w.SetColWidth(loanDetailSheet, "C", "R", 24)
	_ = w.SetColWidth(loanDetailSheet, "L", "L", 45)
	_ = w.SetCellValue(loanDetailSheet, "W1", translate(lang, "detail_help"))
	_ = w.SetColWidth(loanDetailSheet, "W", "W", 90)
	if r > 2 {
		v := excelize.NewDataValidation(true)
		v.Sqref = fmt.Sprintf("K2:K%d", r-1)
		if err = v.SetDropList([]string{translate(lang, "detail_payment"), translate(lang, "suspension_action")}); err != nil {
			return err
		}
		if err = w.AddDataValidation(loanDetailSheet, v); err != nil {
			return err
		}
	}
	if err = addTagihanRealizationColumns(w, lang); err != nil {
		return err
	}
	mode := "auto"
	recalc := true
	return w.SetCalcProps(&excelize.CalcPropsOptions{CalcMode: &mode, FullCalcOnLoad: &recalc})
}

func appendLoanDetailPreview(result *tagihanReviewResult, job *tagihanReviewWork, l tagihanReviewLoan, d tagihanLoanDetailInput, member, name, lang string) error {
	expected := l.Due + l.Recorded
	actual := d.Actual
	if actual > expected {
		return errTagihanReview
	}
	if d.Action == "payment" && l.Recorded > 0 && l.Recorded != actual {
		return errTagihanReview
	}
	remaining := max(int64(0), expected-actual)
	deferred := int64(0)
	if d.Action == "suspend" {
		remaining = 0
		deferred = expected
	}
	c := tagihanReviewComponent{MemberID: member, LoanID: l.ID, Name: name, Component: l.Type, Label: translate(lang, "review_"+map[string]string{"regular": "regular", "secondary_goods": "secondary", "goods_purchase_paylater": "purchase", "voucher": "voucher"}[l.Type]), Expected: expected, Actual: actual, Difference: actual - expected, Remaining: remaining, AlreadyRecorded: l.Recorded, Deferred: deferred}
	result.Components = append(result.Components, c)
	result.ExpectedTotal += expected
	result.ActualTotal += actual
	if d.Action == "payment" && actual > 0 && l.Recorded == 0 {
		job.dues = append(job.dues, tagihanLoanDue{LoanID: l.ID, Amount: actual})
	}
	return nil
}

func addDetailedTagihanSummary(w *excelize.File, lang string) error {
	rows, err := w.GetRows("Tagihan")
	if err != nil {
		return err
	}
	loans, err := w.GetRows(loanDetailSheet)
	if err != nil {
		return err
	}
	end := max(2, len(loans))
	// Remove earlier realization inputs and formulas before laying out loan-only summaries.
	for i := range rows {
		for col := 14; col <= 21; col++ {
			cell, _ := excelize.CoordinatesToCellName(col, i+1)
			_ = w.SetCellFormula("Tagihan", cell, "")
			_ = w.SetCellValue("Tagihan", cell, "")
		}
	}
	for i, key := range reviewKeys[2:] {
		cell, _ := excelize.CoordinatesToCellName(14+i, 1)
		_ = w.SetCellValue("Tagihan", cell, translate(lang, key))
	}
	_ = w.SetCellValue("Tagihan", "K1", translate(lang, "savings_auto_status"))
	_ = w.SetCellValue("Tagihan", "M1", translate(lang, "suspension_collection_total"))
	_ = w.SetCellValue("Tagihan", "R1", translate(lang, "review_actual_total"))
	_ = w.SetCellValue("Tagihan", "S1", translate(lang, "review_difference"))
	_ = w.SetCellValue("Tagihan", "W1", translate(lang, "detail_summary_help"))
	for i := 1; i < len(rows); i++ {
		r := i + 1
		_ = w.SetCellValue("Tagihan", fmt.Sprintf("K%d", r), "paid")
		for j, kind := range reviewTypes[2:] {
			cell, _ := excelize.CoordinatesToCellName(14+j, r)
			formula := fmt.Sprintf(`SUMIFS('%s'!$M$2:$M$%d,'%s'!$B$2:$B$%d,A%d,'%s'!$U$2:$U$%d,"%s")`, loanDetailSheet, end, loanDetailSheet, end, r, loanDetailSheet, end, kind)
			if err = w.SetCellFormula("Tagihan", cell, formula); err != nil {
				return err
			}
		}
		fees := fmt.Sprintf(`SUMIFS('%s'!$N$2:$N$%d,'%s'!$B$2:$B$%d,A%d)`, loanDetailSheet, end, loanDetailSheet, end, r)
		_ = w.SetCellFormula("Tagihan", fmt.Sprintf("R%d", r), fmt.Sprintf("SUM(E%d:F%d)+SUM(N%d:Q%d)+%s", r, r, r, r, fees))
		_ = w.SetCellFormula("Tagihan", fmt.Sprintf("S%d", r), fmt.Sprintf("R%d-J%d", r, r))
		_ = w.SetCellFormula("Tagihan", fmt.Sprintf("M%d", r), fmt.Sprintf("J%d", r))
	}
	_ = w.SetColWidth("Tagihan", "N", "S", 25)
	return nil
}
