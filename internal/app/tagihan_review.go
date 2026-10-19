package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

var errTagihanReview = errors.New("invalid reviewed collection")
var errTagihanPreviewChanged = errors.New("collection preview changed")
var reviewKeys = []string{"review_wajib", "review_manasuka", "review_regular", "review_secondary", "review_purchase", "review_voucher"}
var reviewTypes = []string{"wajib", "sukarela", "regular", "secondary_goods", "goods_purchase_paylater", "voucher"}

type tagihanReviewComponent struct {
	LoanID          string `json:"loan_id,omitempty"`
	Deferred        int64  `json:"deferred"`
	MemberID        string `json:"member_id"`
	Name            string `json:"name"`
	Component       string `json:"component"`
	Label           string `json:"label"`
	Expected        int64  `json:"expected"`
	Actual          int64  `json:"actual"`
	Difference      int64  `json:"difference"`
	Remaining       int64  `json:"remaining"`
	AlreadyRecorded int64  `json:"already_recorded"`
}
type tagihanReviewResult struct {
	TagihanImportResult
	Components    []tagihanReviewComponent `json:"components"`
	PreviewToken  string                   `json:"preview_token,omitempty"`
	ExpectedTotal int64                    `json:"expected_total"`
	ActualTotal   int64                    `json:"actual_total"`
	Difference    int64                    `json:"difference"`
	fingerprint   string
}
type tagihanReviewRow struct {
	memberID, status string
	values           [6]int64
	explicit         bool
	excelRow         int
}
type tagihanReviewLoan struct {
	ID, Type, Status, Final string
	Balance, Due, Recorded  int64
	Schedule                [][4]string
}
type tagihanReviewWork struct {
	memberID string
	savings  tagihanSavingAmounts
	dues     []tagihanLoanDue
}
type tagihanPreviewClaim struct {
	Actor, Month, Date, File, State string
	Expires                         int64
}

func reviewDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (s *Server) signTagihanPreview(claim tagihanPreviewClaim) string {
	data, _ := json.Marshal(claim)
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	_, _ = mac.Write([]byte("tagihan-preview:" + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Server) readTagihanPreview(token string) (tagihanPreviewClaim, error) {
	var claim tagihanPreviewClaim
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claim, errTagihanPreviewChanged
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claim, errTagihanPreviewChanged
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	_, _ = mac.Write([]byte("tagihan-preview:" + parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return claim, errTagihanPreviewChanged
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claim, errTagihanPreviewChanged
	}
	if json.Unmarshal(data, &claim) != nil || claim.Expires <= time.Now().Unix() {
		return claim, errTagihanPreviewChanged
	}
	return claim, nil
}

func readTagihanReviewRows(w *excelize.File) ([]tagihanReviewRow, error) {
	sheet := "Tagihan"
	rows, err := w.GetRows(sheet)
	if err != nil || len(rows) < 1 || len(rows) > 5001 {
		return nil, errTagihanReview
	}
	headers := tagihanHeaderIndexes(rows[0])
	memberCol, ok := headers["member id"]
	if !ok {
		return nil, errTagihanReview
	}
	statusCol, ok := headers["status"]
	if !ok {
		return nil, errTagihanReview
	}
	var cols [6]int
	detailIndex, _ := w.GetSheetIndex(loanDetailSheet)
	limit := 6
	if detailIndex >= 0 {
		limit = 2
	}
	present := 0
	for i, key := range reviewKeys[:limit] {
		cols[i] = -1
		for _, lang := range []string{"en", "id"} {
			if col, ok := headers[strings.ToLower(translate(lang, key))]; ok {
				cols[i] = col
				break
			}
		}
		if cols[i] >= 0 {
			present++
		}
	}
	if present != 0 && present != limit {
		return nil, errTagihanReview
	}
	seen := map[string]bool{}
	result := []tagihanReviewRow{}
	for index, row := range rows[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		r := tagihanReviewRow{memberID: tagihanCell(row, memberCol), explicit: present == limit, excelRow: index + 2}
		r.status, ok = parseTagihanStatus(tagihanCell(row, statusCol))
		if !ok || r.memberID == "" || seen[r.memberID] {
			return nil, fmt.Errorf("%w: %d", errTagihanReview, index+2)
		}
		seen[r.memberID] = true
		if r.explicit && r.status == "paid" {
			for i, col := range cols[:limit] {
				value := tagihanCell(row, col)
				cell, _ := excelize.CoordinatesToCellName(col+1, index+2)
				formula, _ := w.GetCellFormula(sheet, cell)
				if formula != "" {
					value, err = w.CalcCellValue(sheet, cell)
					if err != nil {
						return nil, errTagihanReview
					}
				}
				amount, e := strconv.ParseInt(value, 10, 64)
				if e != nil || amount < 0 || amount > 1_000_000_000_000 {
					return nil, fmt.Errorf("%w: %d", errTagihanReview, index+2)
				}
				r.values[i] = amount
			}
		}
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].memberID < result[j].memberID })
	return result, nil
}

// loadReviewLoans locks authoritative loan/schedule state. Stable snapshots bind
// confirmation to the data actually previewed, including external repayments.
func (s *Server) loadReviewLoans(tx *sql.Tx, member string, month tagihanStatementMonth, reference string) ([]tagihanReviewLoan, error) {
	rows, err := tx.Query(`SELECT id,loan_type,status,remaining_balance,final_due_date FROM loans WHERE member_id=$1 ORDER BY id`+rowLockClause(s.db), member)
	if err != nil {
		return nil, err
	}
	var loans []tagihanReviewLoan
	for rows.Next() {
		var l tagihanReviewLoan
		if err = rows.Scan(&l.ID, &l.Type, &l.Status, &l.Balance, &l.Final); err != nil {
			rows.Close()
			return nil, err
		}
		loans = append(loans, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range loans {
		l := &loans[i]
		ir, e := tx.Query(`SELECT installment_no,due_date,scheduled_amount,paid_amount FROM loan_installments WHERE loan_id=$1 ORDER BY installment_no`+rowLockClause(s.db), l.ID)
		if e != nil {
			return nil, e
		}
		for ir.Next() {
			var n int
			var date string
			var due, paid int64
			if e = ir.Scan(&n, &date, &due, &paid); e != nil {
				ir.Close()
				return nil, e
			}
			l.Schedule = append(l.Schedule, [4]string{strconv.Itoa(n), date, strconv.FormatInt(due, 10), strconv.FormatInt(paid, 10)})
			if date <= month.CutoffDate && (l.Status == "active" || l.Status == "adjustment_due") {
				l.Due += due - paid
			}
		}
		e = ir.Err()
		ir.Close()
		if e != nil {
			return nil, e
		}
		if e = tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM loan_repayments WHERE loan_id=$1 AND reference_no=$2`, l.ID, reference).Scan(&l.Recorded); e != nil {
			return nil, e
		}
	}
	// Oldest due loan first, with a stable ID tie-breaker.
	sort.SliceStable(loans, func(i, j int) bool {
		first := func(l tagihanReviewLoan) string {
			for _, r := range l.Schedule {
				if r[2] != r[3] {
					return r[1]
				}
			}
			return "9999-12-31"
		}
		a, b := first(loans[i]), first(loans[j])
		if a == b {
			return loans[i].ID < loans[j].ID
		}
		return a < b
	})
	return loans, nil
}

func (s *Server) reviewTagihan(month tagihanStatementMonth, date string, data []byte, actor, lang string, confirm *tagihanPreviewClaim) (tagihanReviewResult, error) {
	result := tagihanReviewResult{TagihanImportResult: TagihanImportResult{StatementMonth: month.Value, RecordDate: date}, Components: []tagihanReviewComponent{}}
	w, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer w.Close()
	input, err := readTagihanReviewRows(w)
	if err != nil {
		return result, err
	}
	details, actions, detailed, err := readLoanDetailInputs(w, month, date, lang)
	if err == nil && !detailed {
		actions, err = readSuspensionActions(w, month, date, lang)
	}
	if err != nil {
		return result, err
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].MemberID == actions[j].MemberID {
			return actions[i].LoanID < actions[j].LoanID
		}
		return actions[i].MemberID < actions[j].MemberID
	})
	selected := map[string]bool{}
	seenMembers := map[string]bool{}
	for _, r := range input {
		seenMembers[r.memberID] = true
	}
	for _, a := range actions {
		if !seenMembers[a.MemberID] {
			return result, errTagihanReview
		}
		selected[a.LoanID] = true
	}
	s.financialMu.Lock()
	defer s.financialMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	snapshot := []any{month.Value, date, actor, actions, details}
	usedDetails := map[string]bool{}
	work := []tagihanReviewWork{}
	ref := tagihanReference(month)
	for _, r := range input {
		var name, status, memberType string
		if err = tx.QueryRow(`SELECT full_name,status,member_type FROM members WHERE id=$1`+rowLockClause(s.db), r.memberID).Scan(&name, &status, &memberType); err != nil {
			return result, errTagihanReview
		}
		if status != "active" || memberType == memberTypeDailyWorker || memberType == memberTypeCustomer {
			return result, errTagihanReview
		}
		pending, e := hasPendingMemberDeactivation(tx, r.memberID)
		if e != nil {
			return result, e
		}
		var expected, recorded [6]int64
		e = tx.QueryRow(`SELECT simpanan_wajib,simpanan_manasuka FROM member_tagihan_configs WHERE member_id=$1`+rowLockClause(s.db), r.memberID).Scan(&expected[0], &expected[1])
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return result, e
		}
		for i := 0; i < 2; i++ {
			if e = tx.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM saving_records WHERE member_id=$1 AND category=$2 AND type='deposit' AND reference_no=$3`, r.memberID, reviewTypes[i], ref).Scan(&recorded[i]); e != nil {
				return result, e
			}
		}
		loans, e := s.loadReviewLoans(tx, r.memberID, month, ref)
		if e != nil {
			return result, e
		}
		for _, l := range loans {
			if selected[l.ID] {
				continue
			}
			for k := 2; k < 6; k++ {
				if l.Type == reviewTypes[k] {
					expected[k] += l.Due + l.Recorded
					recorded[k] += l.Recorded
				}
			}
		}
		snapshot = append(snapshot, []any{r.memberID, status, pending, expected, recorded, loans})
		actual := r.values
		if !r.explicit && r.status == "paid" {
			actual = expected
		}
		if r.status == "unpaid" {
			actual = [6]int64{}
		}
		job := tagihanReviewWork{memberID: r.memberID}
		limit := 6
		if detailed {
			limit = 2
		}
		for k := 0; k < limit; k++ {
			if r.status == "paid" && recorded[k] > 0 && recorded[k] != actual[k] {
				return result, errTagihanReview
			}
			if k >= 2 && actual[k] > expected[k] {
				return result, errTagihanReview
			}
			if pending && k < 2 && actual[k] > recorded[k] {
				return result, errTagihanReview
			}
			component := tagihanReviewComponent{MemberID: r.memberID, Name: name, Component: reviewTypes[k], Label: translate(lang, reviewKeys[k]), Expected: expected[k], Actual: actual[k], Difference: actual[k] - expected[k], Remaining: max(int64(0), expected[k]-actual[k]), AlreadyRecorded: recorded[k]}
			result.Components = append(result.Components, component)
			result.ExpectedTotal += expected[k]
			result.ActualTotal += actual[k]
			if r.status == "unpaid" || recorded[k] > 0 {
				continue
			}
			if k == 0 {
				job.savings.Wajib = actual[k]
			} else if k == 1 {
				job.savings.Manasuka = actual[k]
			} else {
				count := 0
				for _, loan := range loans {
					if loan.Type == reviewTypes[k] && !selected[loan.ID] && loan.Due+loan.Recorded > 0 {
						count++
					}
				}
				if count > 1 && actual[k] > 0 {
					return result, errTagihanReview
				}
				left := actual[k]
				for _, l := range loans {
					if l.Type != reviewTypes[k] || selected[l.ID] || l.Due <= 0 || left == 0 {
						continue
					}
					amount := min(left, l.Due)
					job.dues = append(job.dues, tagihanLoanDue{LoanID: l.ID, Amount: amount})
					left -= amount
				}
				if left != 0 {
					return result, errTagihanReview
				}
			}
		}
		if detailed {
			for _, l := range loans {
				d, found := details[l.ID]
				if found {
					if d.MemberID != r.memberID {
						return result, errTagihanReview
					}
					usedDetails[l.ID] = true
				}
				if !found && l.Due == 0 {
					continue
				}
				if err = appendLoanDetailPreview(&result, &job, l, d, r.memberID, name, lang); err != nil {
					return result, err
				}
			}
		}
		result.Rows = append(result.Rows, TagihanImportRowResult{ExcelRow: r.excelRow, MemberID: r.memberID, FullName: name, Status: r.status, Result: "skipped"})
		work = append(work, job)
	}
	if detailed && len(usedDetails) != len(details) {
		return result, errTagihanReview
	}
	// Include suspension history so a new receipt/extension invalidates the preview.
	for _, a := range actions {
		var n int
		var fee int64
		var reason, recordDate string
		e := tx.QueryRow(`SELECT fee_amount,reason,record_date FROM loan_suspensions WHERE loan_id=$1 AND statement_month=$2`, a.LoanID, a.Month).Scan(&fee, &reason, &recordDate)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return result, e
		}
		if e == nil {
			n = 1
		}
		snapshot = append(snapshot, []any{a.LoanID, n, fee, reason, recordDate})
		result.Components = append(result.Components, tagihanReviewComponent{MemberID: a.MemberID, LoanID: a.LoanID, Name: a.LoanID, Component: "loan_suspension_fee", Label: translate(lang, "suspension_fee"), Expected: a.Fee, Actual: a.Fee, AlreadyRecorded: fee})
		result.ExpectedTotal += a.Fee
		result.ActualTotal += a.Fee
	}
	state, _ := json.Marshal(snapshot)
	result.fingerprint = reviewDigest(state)
	result.Difference = result.ActualTotal - result.ExpectedTotal
	if confirm != nil && confirm.State != result.fingerprint {
		return result, errTagihanPreviewChanged
	}
	var priorPending int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM financial_journal_entries WHERE batch_id=$1 AND status<>'posted'`, ref).Scan(&priorPending); err != nil {
		return result, err
	}
	// Validate all accounting and schedule operations in a rollback-only transaction
	// for preview; confirmation executes exactly the same operations atomically.
	for _, a := range actions {
		created, e := s.applyLoanSuspensionTx(tx, a, actor, lang)
		if e != nil {
			return result, e
		}
		if created {
			result.SuspensionsCreated++
		} else {
			result.SuspensionsSkipped++
		}
	}
	for i, job := range work {
		sc, rc, notes, e := s.recordPaidTagihanRowTx(tx, job.memberID, month, date, transactionSourceBank, actor, lang, job.savings, job.dues)
		if e != nil {
			return result, e
		}
		result.Rows[i].SavingsCreated = sc
		result.Rows[i].Repayments = rc
		result.Rows[i].Messages = notes
		if sc+rc > 0 {
			result.Rows[i].Result = "imported"
			result.Summary.Imported++
		} else {
			result.Summary.Skipped++
		}
	}
	var pendingJournals int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM financial_journal_entries WHERE batch_id=$1 AND status<>'posted'`, ref).Scan(&pendingJournals); err != nil {
		return result, err
	}
	if pendingJournals > priorPending {
		return result, errTagihanReview
	}
	if confirm == nil {
		result.Message = translate(lang, "review_preview_help")
		return result, nil
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.Committed = true
	result.Message = translate(lang, "tagihan_import_committed")
	return result, nil
}

func (s *Server) handleTagihanReview(c *gin.Context, month tagihanStatementMonth, date, actor string) {
	lang := languageFromRequest(c)
	c.Header("Cache-Control", "no-store")
	var data []byte
	var err error
	// A bounded encoded workbook carries the exact reviewed file through browser
	// confirmation. It is untrusted until its HMAC-bound digest is checked.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	if encoded := c.PostForm("workbook"); encoded != "" {
		data, err = base64.StdEncoding.DecodeString(encoded)
	} else {
		var f io.ReadCloser
		f, _, err = c.Request.FormFile("file")
		if err == nil {
			defer f.Close()
			data, err = io.ReadAll(io.LimitReader(f, (10<<20)+1))
		}
	}
	if err != nil || len(data) == 0 || len(data) > 10<<20 {
		respondError(c, 400, "VALIDATION_ERROR", translate(lang, "error_invalid_tagihan_file"))
		return
	}
	var claim *tagihanPreviewClaim
	if token := c.PostForm("preview_token"); token != "" {
		parsed, e := s.readTagihanPreview(token)
		if e != nil || parsed.Actor != actor || parsed.Month != month.Value || parsed.Date != date || parsed.File != reviewDigest(data) {
			respondError(c, 409, "PREVIEW_CHANGED", translate(lang, "review_changed"))
			return
		}
		claim = &parsed
	}
	result, err := s.reviewTagihan(month, date, data, actor, lang, claim)
	if err != nil {
		key := "review_invalid"
		code := 422
		if errors.Is(err, errTagihanPreviewChanged) {
			key = "review_changed"
			code = 409
		}
		respondError(c, code, "VALIDATION_ERROR", translate(lang, key))
		return
	}
	if claim == nil {
		result.PreviewToken = s.signTagihanPreview(tagihanPreviewClaim{Actor: actor, Month: month.Value, Date: date, File: reviewDigest(data), State: result.fingerprint, Expires: time.Now().Add(30 * time.Minute).Unix()})
	}
	if strings.Contains(c.GetHeader("Accept"), "text/html") {
		c.Header("Content-Type", "text/html; charset=utf-8")
		renderPage(c, "admin-tagihan-review", pageData(c, translate(lang, "review_title"), "tagihan", translate(lang, "review_title"), result.Message, gin.H{"Review": result, "Workbook": base64.StdEncoding.EncodeToString(data)}))
		return
	}
	c.JSON(200, result)
}

func addTagihanRealizationColumns(w *excelize.File, lang string) error {
	if index, _ := w.GetSheetIndex(loanDetailSheet); index >= 0 {
		return addDetailedTagihanSummary(w, lang)
	}
	rows, err := w.GetRows("Tagihan")
	if err != nil {
		return err
	}
	_ = w.SetCellValue("Tagihan", "M1", translate(lang, "suspension_collection_total"))
	for i, key := range reviewKeys {
		cell, _ := excelize.CoordinatesToCellName(14+i, 1)
		if err = w.SetCellValue("Tagihan", cell, translate(lang, key)); err != nil {
			return err
		}
	}
	_ = w.SetCellValue("Tagihan", "T1", translate(lang, "review_actual_total"))
	_ = w.SetCellValue("Tagihan", "U1", translate(lang, "review_difference"))
	_ = w.SetCellValue("Tagihan", "W1", translate(lang, "review_excel_help"))
	_ = w.SetColWidth("Tagihan", "N", "U", 25)
	_ = w.SetColWidth("Tagihan", "W", "W", 90)
	suspensionIndex, _ := w.GetSheetIndex(suspensionSheet)
	suspensionRows, _ := w.GetRows(suspensionSheet)
	end := max(2, len(suspensionRows))
	for i := 1; i < len(rows); i++ {
		r := i + 1
		for j, source := range []string{"E", "F", "G", "H", "I", "L"} {
			cell, _ := excelize.CoordinatesToCellName(14+j, r)
			formula := fmt.Sprintf("%s%d", source, r)
			if j == 2 && suspensionIndex >= 0 {
				for _, action := range []string{"suspend", translate("en", "suspension_action"), translate("id", "suspension_action")} {
					formula += fmt.Sprintf(`-SUMIFS('%s'!$K$2:$K$%d,'%s'!$B$2:$B$%d,A%d,'%s'!$G$2:$G$%d,"%s")`, suspensionSheet, end, suspensionSheet, end, r, suspensionSheet, end, action)
				}
			}
			if err = w.SetCellFormula("Tagihan", cell, formula); err != nil {
				return err
			}
		}
		if suspensionIndex < 0 {
			_ = w.SetCellFormula("Tagihan", fmt.Sprintf("M%d", r), fmt.Sprintf("J%d", r))
		}
		total := fmt.Sprintf(`IF(OR(K%d="paid",K%d="lunas"),SUM(N%d:S%d),0)`, r, r, r, r)
		if suspensionIndex >= 0 {
			for _, action := range []string{"suspend", translate("en", "suspension_action"), translate("id", "suspension_action")} {
				total += fmt.Sprintf(`+SUMIFS('%s'!$F$2:$F$%d,'%s'!$B$2:$B$%d,A%d,'%s'!$G$2:$G$%d,"%s")`, suspensionSheet, end, suspensionSheet, end, r, suspensionSheet, end, action)
			}
		}
		_ = w.SetCellFormula("Tagihan", fmt.Sprintf("T%d", r), total)
		_ = w.SetCellFormula("Tagihan", fmt.Sprintf("U%d", r), fmt.Sprintf("T%d-M%d", r, r))
	}
	return nil
}
