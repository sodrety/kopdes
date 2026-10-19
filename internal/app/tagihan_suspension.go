package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

const suspensionSheet = "Penangguhan Cicilan"
const legacySuspensionSheet = "LoanSuspensions"

var errSuspensionConflict = errors.New("suspension conflicts with loan or statement state")

var loanSuspensionMigrationStatements = []string{
	`CREATE TABLE loan_suspensions (
		id TEXT PRIMARY KEY, loan_id TEXT NOT NULL REFERENCES loans(id),
		member_id TEXT NOT NULL REFERENCES members(id), statement_month TEXT NOT NULL,
		fee_amount BIGINT NOT NULL CHECK (fee_amount>0), record_date TEXT NOT NULL,
		reason TEXT NOT NULL, recorded_by TEXT NOT NULL REFERENCES users(id),
		before_schedule TEXT NOT NULL, before_final_due_date TEXT NOT NULL,
		after_final_due_date TEXT NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(loan_id,statement_month)
	)`,
	`ALTER TABLE loan_suspensions ENABLE ROW LEVEL SECURITY`,
}

type suspensionAction struct {
	ExcelRow   int    `json:"-" form:"-"`
	LoanID     string `json:"loan_id" form:"loan_id"`
	MemberID   string `json:"member_id" form:"member_id"`
	Month      string `json:"statement_month" form:"statement_month"`
	Fee        int64  `json:"fee_amount" form:"fee_amount"`
	RecordDate string `json:"record_date" form:"record_date"`
	Reason     string `json:"reason" form:"reason"`
}

type suspensionLoan struct {
	ID, MemberID, MemberNo, FullName, FirstDue, FinalDue, SuspendedMonth string
	Fee                                                                  int64
	InstallmentAmount                                                    int64
	Duration                                                             int
}

type suspensionHistoryRow struct {
	MemberNo, FullName, LoanID, Reason, RecordDate, BeforeFinal, AfterFinal string
	Fee                                                                     int64
}

func (s *Server) suspensionHistory(month string) ([]suspensionHistoryRow, error) {
	rows, err := s.db.Query(`SELECT m.member_no,m.full_name,x.loan_id,x.reason,x.record_date,x.before_final_due_date,x.after_final_due_date,x.fee_amount FROM loan_suspensions x JOIN members m ON m.id=x.member_id WHERE x.statement_month=$1 ORDER BY x.created_at,x.id`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []suspensionHistoryRow{}
	for rows.Next() {
		var x suspensionHistoryRow
		if err = rows.Scan(&x.MemberNo, &x.FullName, &x.LoanID, &x.Reason, &x.RecordDate, &x.BeforeFinal, &x.AfterFinal, &x.Fee); err != nil {
			return nil, err
		}
		result = append(result, x)
	}
	return result, rows.Err()
}

func suspensionFee(monthly sql.NullInt64, total int64, duration int) int64 {
	if monthly.Valid {
		return monthly.Int64
	}
	if duration <= 0 || total <= 0 {
		return 0
	}
	// Round a legacy monthly fee to whole Rupiah, without repricing the loan.
	fee := total / int64(duration)
	if total%int64(duration) >= (int64(duration)+1)/2 {
		fee++
	}
	return fee
}

func nextSuspensionDate(value string) (string, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", err
	}
	next := time.Date(date.Year(), date.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	day := date.Day()
	if last := next.AddDate(0, 1, -1).Day(); day > last {
		day = last
	}
	return next.AddDate(0, 0, day-1).Format("2006-01-02"), nil
}

func (s *Server) suspensionLoans(month tagihanStatementMonth) ([]suspensionLoan, error) {
	rows, err := s.db.Query(`SELECT l.id,l.member_id,m.member_no,m.full_name,l.duration_months,l.monthly_admin_fee,l.total_admin_fee,l.final_due_date,
		COALESCE((SELECT MIN(i.due_date) FROM loan_installments i WHERE i.loan_id=l.id AND i.due_date >= $1 AND i.due_date <= $2 AND i.paid_amount=0),''),
		COALESCE((SELECT statement_month FROM loan_suspensions x WHERE x.loan_id=l.id AND x.statement_month=$3),''),
		COALESCE((SELECT SUM(i.scheduled_amount) FROM loan_installments i WHERE i.loan_id=l.id AND i.due_date >= $1 AND i.due_date <= $2),0)
		FROM loans l JOIN members m ON m.id=l.member_id WHERE m.status='active' AND m.member_type NOT IN ('daily_worker','customer') AND l.status='active' AND l.remaining_balance>0 AND l.loan_type='regular' ORDER BY m.member_no,l.created_at`, month.Value+"-01", month.CutoffDate, month.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []suspensionLoan{}
	for rows.Next() {
		var l suspensionLoan
		var monthly sql.NullInt64
		var total int64
		if err = rows.Scan(&l.ID, &l.MemberID, &l.MemberNo, &l.FullName, &l.Duration, &monthly, &total, &l.FinalDue, &l.FirstDue, &l.SuspendedMonth, &l.InstallmentAmount); err != nil {
			return nil, err
		}
		l.Fee = suspensionFee(monthly, total, l.Duration)
		if l.FirstDue != "" && l.Fee > 0 && l.SuspendedMonth == "" {
			result = append(result, l)
		}
	}
	return result, rows.Err()
}

// applyLoanSuspensionTx is shared by the app and XLSX import. The unique loan/
// month key and the loan row lock make retries safe across app processes.
func (s *Server) applyLoanSuspensionTx(tx *sql.Tx, a suspensionAction, actor, language string) (bool, error) {
	month, err := parseTagihanStatementMonth(a.Month)
	if err != nil || len(a.Reason) > 2000 || a.Fee <= 0 {
		return false, errSuspensionConflict
	}
	if _, err = time.Parse("2006-01-02", a.RecordDate); err != nil || a.RecordDate < a.Month+"-01" {
		return false, errSuspensionConflict
	}
	var member, status, loanType, final string
	var duration int
	var monthly sql.NullInt64
	var total, balance int64
	var memberStatus string
	if err = tx.QueryRow(`SELECT status FROM members WHERE id=$1`+rowLockClause(s.db), a.MemberID).Scan(&memberStatus); err != nil {
		return false, err
	}
	err = tx.QueryRow(`SELECT member_id,status,loan_type,duration_months,monthly_admin_fee,total_admin_fee,remaining_balance,final_due_date FROM loans WHERE id=$1`+rowLockClause(s.db), a.LoanID).Scan(&member, &status, &loanType, &duration, &monthly, &total, &balance, &final)
	if err != nil {
		return false, err
	}
	if member != a.MemberID {
		return false, errSuspensionConflict
	}
	var existingFee int64
	var existingDate, existingReason string
	err = tx.QueryRow(`SELECT fee_amount,record_date,reason FROM loan_suspensions WHERE loan_id=$1 AND statement_month=$2`, a.LoanID, a.Month).Scan(&existingFee, &existingDate, &existingReason)
	if err == nil {
		if existingFee != a.Fee || existingDate != a.RecordDate || existingReason != strings.TrimSpace(a.Reason) {
			return false, errSuspensionConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if status != "active" || balance <= 0 || loanType != "regular" || suspensionFee(monthly, total, duration) != a.Fee {
		return false, errSuspensionConflict
	}
	if memberStatus != "active" {
		return false, errSuspensionConflict
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM loan_repayment_events WHERE loan_id=$1 AND event_type='suspension' AND record_date >= $2 AND record_date <= $3`, a.LoanID, a.Month+"-01", month.CutoffDate).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, errSuspensionConflict
	}
	rows, err := tx.Query(`SELECT id,installment_no,due_date,scheduled_amount,paid_amount FROM loan_installments WHERE loan_id=$1 ORDER BY installment_no`, a.LoanID)
	if err != nil {
		return false, err
	}
	type installment struct {
		ID              string
		Number          int
		Due             string
		Scheduled, Paid int64
	}
	items := []installment{}
	target := -1
	for rows.Next() {
		var i installment
		if err = rows.Scan(&i.ID, &i.Number, &i.Due, &i.Scheduled, &i.Paid); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, i)
		if i.Due >= a.Month+"-01" && i.Due <= month.CutoffDate {
			if target >= 0 || i.Paid > 0 {
				rows.Close()
				return false, errSuspensionConflict
			}
			target = len(items) - 1
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if target < 0 {
		return false, errSuspensionConflict
	}
	for _, i := range items[:target] {
		if i.Paid < i.Scheduled {
			return false, errSuspensionConflict
		}
	}
	// Do not move subsequent installments that already have financial activity.
	for _, i := range items[target:] {
		if i.Paid > 0 {
			return false, errSuspensionConflict
		}
	}
	before, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	for _, i := range items[target:] {
		due, e := nextSuspensionDate(i.Due)
		if e != nil {
			return false, e
		}
		if _, err = tx.Exec(`UPDATE loan_installments SET due_date=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, due, i.ID); err != nil {
			return false, err
		}
	}
	after, err := nextSuspensionDate(final)
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(`UPDATE loans SET final_due_date=$1,next_due_date=COALESCE((SELECT MIN(due_date) FROM loan_installments WHERE loan_id=$2 AND paid_amount<scheduled_amount),''),updated_at=CURRENT_TIMESTAMP WHERE id=$2`, after, a.LoanID); err != nil {
		return false, err
	}
	id := newID()
	if _, err = tx.Exec(`INSERT INTO loan_suspensions(id,loan_id,member_id,statement_month,fee_amount,record_date,reason,recorded_by,before_schedule,before_final_due_date,after_final_due_date) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, a.LoanID, member, a.Month, a.Fee, a.RecordDate, strings.TrimSpace(a.Reason), actor, string(before), final, after); err != nil {
		return false, err
	}
	// Require a posted journal, never a silently pending mapping. Reuse the
	// cooperative's repayment bank mapping; income must use the agreed 40201.
	bank, err := s.accountingMappingCOACodeTx(tx, "repayment", "cash_bank", "", "regular", a.RecordDate)
	if err != nil {
		return false, err
	}
	if bank == "" || bank == "40201" {
		return false, errSuspensionConflict
	}
	bankAccount, err := s.coaAccountForPostingTx(tx, bank)
	if err != nil {
		return false, err
	}
	if bankAccount.AccountType != "asset" || !(strings.EqualFold(bankAccount.Subtype, "bank") || strings.EqualFold(bankAccount.Subtype, "cash") || bankAccount.SystemKey == "BANK" || bankAccount.SystemKey == "CASH") {
		return false, errSuspensionConflict
	}
	income, err := s.coaAccountForPostingTx(tx, "40201")
	if err != nil {
		return false, err
	}
	if income.AccountType != "revenue" {
		return false, errSuspensionConflict
	}
	err = s.createFinancialJournalTx(tx, accountingJournalInput{TransactionID: id, TransactionType: "loan_suspension_fee", TransactionDate: a.RecordDate, ReferenceNo: "SUSPENSION-" + a.Month + "-" + a.LoanID, Source: transactionSourceBank, Amount: a.Fee, LoanType: loanType, Description: translate(language, "suspension_fee"), RecordedBy: actor, Lines: []accountingJournalLineInput{{Component: "cash_bank", Side: accountingDirectionDebit, COACode: bank, Amount: a.Fee}, {Component: "fee_income", Side: accountingDirectionCredit, COACode: "40201", Amount: a.Fee}}})
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) recordTagihanSuspension(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		return
	}
	var a suspensionAction
	if err := c.ShouldBind(&a); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "suspension_invalid"))
		return
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	defer tx.Rollback()
	_, err = s.applyLoanSuspensionTx(tx, a, user.ID, lang)
	if err != nil {
		respondError(c, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", translate(lang, "suspension_invalid"))
		return
	}
	if err = tx.Commit(); err != nil {
		respondError(c, 500, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
		return
	}
	if !strings.Contains(c.GetHeader("Content-Type"), "application/json") {
		c.Redirect(http.StatusSeeOther, "/admin/tagihan?month="+a.Month)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": translate(lang, "suspension_recorded")})
}

func (s *Server) addSuspensionSheet(w *excelize.File, month tagihanStatementMonth, language string) error {
	return s.addLoanDetailSheet(w, month, language)
}

func readSuspensionActions(w *excelize.File, month tagihanStatementMonth, recordDate, language string) ([]suspensionAction, error) {
	sheet := suspensionSheet
	index, err := w.GetSheetIndex(sheet)
	legacyIndex, legacyErr := w.GetSheetIndex(legacySuspensionSheet)
	if legacyErr != nil {
		return nil, legacyErr
	}
	if index >= 0 && legacyIndex >= 0 {
		return nil, errSuspensionConflict
	}
	if index < 0 && legacyIndex >= 0 {
		sheet = legacySuspensionSheet
		index = legacyIndex
	}
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, nil
	}
	rows, err := w.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errSuspensionConflict
	}
	result := []suspensionAction{}
	seen := map[string]bool{}
	for index, row := range rows[1:] {
		status := strings.ToLower(tagihanCell(row, 6))
		if status == "" {
			continue
		}
		if status != "suspend" && status != "ditangguhkan" && status != "suspended" && status != strings.ToLower(translate("en", "suspension_action")) && status != strings.ToLower(translate("id", "suspension_action")) {
			return nil, errSuspensionConflict
		}
		fee, err := strconv.ParseInt(tagihanCell(row, 5), 10, 64)
		if err != nil {
			return nil, errSuspensionConflict
		}
		a := suspensionAction{LoanID: tagihanCell(row, 0), MemberID: tagihanCell(row, 1), Month: tagihanCell(row, 4), Fee: fee, Reason: tagihanCell(row, 7), RecordDate: recordDate}
		a.ExcelRow = index + 2
		if a.Month != month.Value || a.LoanID == "" || a.MemberID == "" || seen[a.LoanID] {
			return nil, errSuspensionConflict
		}
		seen[a.LoanID] = true
		result = append(result, a)
	}
	return result, nil
}
