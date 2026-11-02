package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type savingCorrectionInput struct {
	Amount      int64  `json:"amount" form:"amount"`
	RecordDate  string `json:"record_date" form:"record_date"`
	ReferenceNo string `json:"reference_no" form:"reference_no"`
	Note        string `json:"note" form:"note"`
	Reason      string `json:"reason" form:"reason"`
}

type savingRemovalInput struct {
	Reason string `json:"reason" form:"reason"`
}

type savingCorrectionJournal struct {
	ID            string
	TransactionID string
	ReferenceNo   string
	Source        string
	Amount        int64
	Status        string
	BatchID       string
	Description   string
	Category      string
	LoanType      string
}

type savingCorrectionJournalLine struct {
	Side            string
	COACode         string
	Amount          int64
	Component       string
	MappingOverride bool
}

var (
	errInvalidSavingCorrection      = errors.New("invalid saving correction")
	errInvalidSavingRemoval         = errors.New("invalid saving removal")
	errSavingCorrectionNotFound     = errors.New("saving correction record not found")
	errSavingCorrectionDepositOnly  = errors.New("only deposit records can be corrected")
	errSavingRemovalWithdrawalLink  = errors.New("saving record is linked to a withdrawal request")
	errSavingJournalAlreadyReversed = errors.New("saving journal was already reversed")
	errInvalidSavingJournal         = errors.New("invalid saving journal")
)

func (s *Server) updateSavingBySuperAdmin(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}
	if !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_saving_only"))
		return
	}

	var req savingCorrectionInput
	if err := bindRequestWithRupiahAmount(c, &req, "amount"); errors.Is(err, errInvalidRupiahAmount) {
		invalidRupiahAmountResponse(c)
		return
	} else if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_saving_correction"))
		return
	}
	if err := s.correctSavingByID(user, c.Param("id"), req, lang); err != nil {
		s.respondSavingCorrectionError(c, err)
		return
	}
	if isHTMXRequest(c) {
		if trigger, err := json.Marshal(gin.H{"kopdes:toast": gin.H{"type": "success", "message": translate(lang, "toast_saving_updated"), "persist": true}}); err == nil {
			c.Header("HX-Trigger", string(trigger))
		}
	}
	respondOKOrHXRedirect(c, "/admin/savings", gin.H{"status": "updated"})
}

func (s *Server) removeSavingBySuperAdmin(c *gin.Context) {
	lang := languageFromRequest(c)
	user, ok := currentUser(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", translate(lang, "error_authentication_required"))
		return
	}
	if !isSuperAdmin(user) {
		respondError(c, http.StatusForbidden, "FORBIDDEN", translate(lang, "error_super_admin_saving_only"))
		return
	}

	var req savingRemovalInput
	if err := c.ShouldBind(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_saving_removal"))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if !validSavingCorrectionReason(reason) {
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_saving_removal"))
		return
	}

	if err := s.removeSavingByID(user, c.Param("id"), reason, lang); err != nil {
		s.respondSavingRemovalError(c, err)
		return
	}
	if isHTMXRequest(c) {
		if trigger, err := json.Marshal(gin.H{"kopdes:toast": gin.H{"type": "success", "message": translate(lang, "toast_saving_removed"), "persist": true}}); err == nil {
			c.Header("HX-Trigger", string(trigger))
		}
	}
	respondOKOrHXRedirect(c, "/admin/savings", gin.H{"status": "removed"})
}

func (s *Server) removeSavingByID(actor User, savingID, reason, language string) error {
	reason = strings.TrimSpace(reason)
	if !isSuperAdmin(actor) || !validSavingCorrectionReason(reason) {
		return errInvalidSavingRemoval
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var before SavingRecord
	err = tx.QueryRow(`SELECT id,member_id,type,category,source,COALESCE(coa_code,''),amount,record_date,reference_no,note,recorded_by,CAST(created_at AS TEXT)
		FROM saving_records WHERE id=$1`+rowLockClause(s.db), savingID).
		Scan(&before.ID, &before.MemberID, &before.Type, &before.Category, &before.Source, &before.COACode, &before.Amount, &before.RecordDate, &before.ReferenceNo, &before.Note, &before.RecordedBy, &before.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errSavingCorrectionNotFound
	}
	if err != nil {
		return err
	}
	if before.Type != "deposit" {
		return errSavingCorrectionDepositOnly
	}
	var linkedToWithdrawal bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM withdrawal_requests WHERE saving_record_id=$1)`, before.ID).Scan(&linkedToWithdrawal); err != nil {
		return err
	}
	if linkedToWithdrawal {
		return errSavingRemovalWithdrawalLink
	}

	sourceJournalID, reversalJournalID, err := s.prepareSavingJournalForRemovalTx(tx, actor, reason, before, language)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO saving_record_removals(id,member_id,type,category,source,coa_code,amount,record_date,reference_no,note,recorded_by,created_at,removed_by,removal_reason,source_journal_id,reversal_journal_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, before.ID, before.MemberID, before.Type, before.Category, before.Source, nullIfEmpty(before.COACode), before.Amount, before.RecordDate, before.ReferenceNo, before.Note, before.RecordedBy, before.CreatedAt, actor.ID, reason, sourceJournalID, reversalJournalID); err != nil {
		return err
	}
	if err := recordFinancialTransactionAuditTx(tx, before.ID, "savings", actor.ID, "removed", "false", "true", ""); err != nil {
		return err
	}
	if err := recordFinancialTransactionAuditTx(tx, before.ID, "savings", actor.ID, "removal_reason", "", reason, ""); err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM saving_records WHERE id=$1 AND type='deposit'`, before.ID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return errSavingCorrectionNotFound
	}
	return tx.Commit()
}

func (s *Server) prepareSavingJournalForRemovalTx(tx *sql.Tx, actor User, reason string, saving SavingRecord, language string) (string, string, error) {
	var journal savingCorrectionJournal
	loadSourceJournal := func() error {
		return tx.QueryRow(`SELECT id,transaction_id,reference_no,source,amount,status,batch_id,description,category,loan_type
			FROM financial_journal_entries
			WHERE transaction_id=$1 AND transaction_type='savings' AND reversal_of IS NULL
			ORDER BY created_at DESC,id DESC LIMIT 1`+rowLockClause(s.db), saving.ID).
			Scan(&journal.ID, &journal.TransactionID, &journal.ReferenceNo, &journal.Source, &journal.Amount, &journal.Status, &journal.BatchID, &journal.Description, &journal.Category, &journal.LoanType)
	}
	err := loadSourceJournal()
	if errors.Is(err, sql.ErrNoRows) {
		var anyJournal bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='savings')`, saving.ID).Scan(&anyJournal); err != nil {
			return "", "", err
		}
		if anyJournal {
			return "", "", errInvalidSavingJournal
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: saving.ReferenceNo, TransactionID: saving.ID, TransactionType: "savings",
			TransactionDate: saving.RecordDate, Source: saving.Source, Amount: saving.Amount, Category: saving.Category,
			Components: []accountingJournalComponent{
				{Component: "cash_bank", Side: accountingDirectionDebit, Amount: saving.Amount},
				{Component: "savings_liability", Side: accountingDirectionCredit, Amount: saving.Amount},
			},
			COAOverrides: map[string]string{"savings_liability": saving.COACode},
			Description:  translate(language, "simpanan_"+saving.Category), RecordedBy: actor.ID,
		}); err != nil {
			return "", "", err
		}
		if err := loadSourceJournal(); err != nil {
			return "", "", err
		}
	} else if err != nil {
		return "", "", err
	}

	var reversalJournalID string
	err = tx.QueryRow(`SELECT id FROM financial_journal_entries WHERE reversal_of=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, journal.ID).Scan(&reversalJournalID)
	if err == nil {
		return journal.ID, reversalJournalID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}

	switch journal.Status {
	case "pending_mapping":
		if _, err := tx.Exec(`DELETE FROM financial_journal_lines WHERE journal_id=$1`, journal.ID); err != nil {
			return "", "", err
		}
		if _, err := tx.Exec(`DELETE FROM financial_journal_entries WHERE id=$1 AND status='pending_mapping'`, journal.ID); err != nil {
			return "", "", err
		}
		if err := recordFinancialTransactionAuditTx(tx, saving.ID, "savings", actor.ID, "unposted_journal_removed", journal.ID, "", ""); err != nil {
			return "", "", err
		}
		return "", "", nil
	case "posted":
		lines, err := savingCorrectionJournalLinesTx(tx, journal.ID)
		if err != nil {
			return "", "", err
		}
		reversalJournalID, err := insertSavingJournalReversalTx(tx, journal, lines, actor.ID, reason, language)
		if err != nil {
			return "", "", err
		}
		if err := recordFinancialTransactionAuditTx(tx, saving.ID, "savings", actor.ID, "reversed_by", "", reversalJournalID, ""); err != nil {
			return "", "", err
		}
		return journal.ID, reversalJournalID, nil
	default:
		return "", "", errJournalNotPosted
	}
}

func (s *Server) correctSavingByID(actor User, savingID string, req savingCorrectionInput, language string) error {
	req.RecordDate = strings.TrimSpace(req.RecordDate)
	req.ReferenceNo = strings.TrimSpace(req.ReferenceNo)
	req.Note = strings.TrimSpace(req.Note)
	req.Reason = strings.TrimSpace(req.Reason)
	if !isSuperAdmin(actor) || req.Amount <= 0 || !validRepaymentCorrectionDate(req.RecordDate) || !validSavingCorrectionReason(req.Reason) {
		return errInvalidSavingCorrection
	}

	s.financialMu.Lock()
	defer s.financialMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var before SavingRecord
	err = tx.QueryRow(`SELECT id,member_id,type,category,source,COALESCE(coa_code,''),amount,record_date,reference_no,note,recorded_by,CAST(created_at AS TEXT)
		FROM saving_records WHERE id=$1`+rowLockClause(s.db), savingID).
		Scan(&before.ID, &before.MemberID, &before.Type, &before.Category, &before.Source, &before.COACode, &before.Amount, &before.RecordDate, &before.ReferenceNo, &before.Note, &before.RecordedBy, &before.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errSavingCorrectionNotFound
	}
	if err != nil {
		return err
	}
	if before.Type != "deposit" {
		return errSavingCorrectionDepositOnly
	}

	after := before
	after.Amount = req.Amount
	after.RecordDate = req.RecordDate
	after.ReferenceNo = req.ReferenceNo
	after.Note = req.Note
	if before.Amount == after.Amount && before.RecordDate == after.RecordDate && before.ReferenceNo == after.ReferenceNo && before.Note == after.Note {
		return errInvalidSavingCorrection
	}
	if _, err := tx.Exec(`UPDATE saving_records SET amount=$1,record_date=$2,reference_no=$3,note=$4 WHERE id=$5`, after.Amount, after.RecordDate, after.ReferenceNo, after.Note, before.ID); err != nil {
		return err
	}

	for _, change := range []struct {
		field string
		old   string
		new   string
	}{
		{"amount", strconv.FormatInt(before.Amount, 10), strconv.FormatInt(after.Amount, 10)},
		{"record_date", before.RecordDate, after.RecordDate},
		{"reference_no", before.ReferenceNo, after.ReferenceNo},
		{"note", before.Note, after.Note},
	} {
		if change.old != change.new {
			if err := recordFinancialTransactionAuditTx(tx, before.ID, "savings", actor.ID, change.field, change.old, change.new, ""); err != nil {
				return err
			}
		}
	}
	if err := recordFinancialTransactionAuditTx(tx, before.ID, "savings", actor.ID, "correction_reason", "", req.Reason, ""); err != nil {
		return err
	}

	financialFieldsChanged := before.Amount != after.Amount || before.RecordDate != after.RecordDate || before.ReferenceNo != after.ReferenceNo
	if financialFieldsChanged {
		if err := s.syncSavingCorrectionJournalTx(tx, actor.ID, req.Reason, after, language); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) syncSavingCorrectionJournalTx(tx *sql.Tx, actorID, reason string, saving SavingRecord, language string) error {
	var journal savingCorrectionJournal
	err := tx.QueryRow(`SELECT e.id,e.transaction_id,e.reference_no,e.source,e.amount,e.status,e.batch_id,e.description,e.category,e.loan_type
		FROM financial_journal_entries e
		WHERE e.transaction_id=$1 AND e.transaction_type='savings' AND e.reversal_of IS NULL
		  AND NOT EXISTS(SELECT 1 FROM financial_journal_entries r WHERE r.reversal_of=e.id)
		ORDER BY e.created_at DESC,e.id DESC LIMIT 1`+rowLockClause(s.db), saving.ID).
		Scan(&journal.ID, &journal.TransactionID, &journal.ReferenceNo, &journal.Source, &journal.Amount, &journal.Status, &journal.BatchID, &journal.Description, &journal.Category, &journal.LoanType)
	if errors.Is(err, sql.ErrNoRows) {
		var anyJournal bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='savings')`, saving.ID).Scan(&anyJournal); err != nil {
			return err
		}
		if anyJournal {
			return errSavingJournalAlreadyReversed
		}
		if err := s.createFinancialJournalTx(tx, accountingJournalInput{
			ReferenceNo: saving.ReferenceNo, TransactionID: saving.ID, TransactionType: "savings",
			TransactionDate: saving.RecordDate, Source: saving.Source, Amount: saving.Amount, Category: saving.Category,
			Components: []accountingJournalComponent{
				{Component: "cash_bank", Side: accountingDirectionDebit, Amount: saving.Amount},
				{Component: "savings_liability", Side: accountingDirectionCredit, Amount: saving.Amount},
			},
			COAOverrides: map[string]string{"savings_liability": saving.COACode},
			Description:  translate(language, "simpanan_"+saving.Category), RecordedBy: actorID,
		}); err != nil {
			return err
		}
		var journalID string
		if err := tx.QueryRow(`SELECT id FROM financial_journal_entries WHERE transaction_id=$1 AND transaction_type='savings' ORDER BY created_at DESC,id DESC LIMIT 1`, saving.ID).Scan(&journalID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE financial_journal_entries SET correction_reason=$1 WHERE id=$2`, reason, journalID)
		return err
	}
	if err != nil {
		return err
	}

	if journal.Status == "pending_mapping" {
		if _, err := tx.Exec(`UPDATE financial_journal_entries SET reference_no=$1,transaction_date=$2,amount=$3,correction_reason=$4 WHERE id=$5 AND status='pending_mapping'`, saving.ReferenceNo, saving.RecordDate, saving.Amount, reason, journal.ID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE financial_journal_lines SET amount=$1 WHERE journal_id=$2`, saving.Amount, journal.ID)
		return err
	}
	if journal.Status != "posted" {
		return errJournalNotPosted
	}

	lines, err := savingCorrectionJournalLinesTx(tx, journal.ID)
	if err != nil {
		return err
	}
	reversalID, err := insertSavingJournalReversalTx(tx, journal, lines, actorID, reason, language)
	if err != nil {
		return err
	}
	if err := recordFinancialTransactionAuditTx(tx, saving.ID, "savings", actorID, "reversed_by", "", reversalID, ""); err != nil {
		return err
	}

	newJournalID := newID()
	description := journal.Description
	if description == "" {
		description = translate(language, "simpanan_"+saving.Category)
	}
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type,correction_reason)
		VALUES($1,$2,$3,'savings',$4,$5,$6,'posted',$7,$8,$9,$10,$11,$12)`, newJournalID, saving.ReferenceNo, saving.ID, saving.RecordDate, saving.Source, saving.Amount, journal.BatchID, description, actorID, saving.Category, journal.LoanType, reason); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines(id,journal_id,side,coa_code,amount,component,mapping_override) VALUES($1,$2,$3,$4,$5,$6,$7)`, newID(), newJournalID, line.Side, line.COACode, saving.Amount, line.Component, line.MappingOverride); err != nil {
			return err
		}
	}
	return nil
}

func savingCorrectionJournalLinesTx(tx *sql.Tx, journalID string) ([]savingCorrectionJournalLine, error) {
	rows, err := tx.Query(`SELECT side,coa_code,amount,component,mapping_override FROM financial_journal_lines WHERE journal_id=$1 ORDER BY created_at,id`, journalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := make([]savingCorrectionJournalLine, 0)
	for rows.Next() {
		var line savingCorrectionJournalLine
		if err := rows.Scan(&line.Side, &line.COACode, &line.Amount, &line.Component, &line.MappingOverride); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func insertSavingJournalReversalTx(tx *sql.Tx, entry savingCorrectionJournal, lines []savingCorrectionJournalLine, actorID, reason, language string) (string, error) {
	if len(lines) == 0 || entry.Amount <= 0 {
		return "", errInvalidSavingJournal
	}
	var debits, credits int64
	for _, line := range lines {
		if line.Amount <= 0 || line.COACode == "" {
			return "", errInvalidSavingJournal
		}
		switch line.Side {
		case accountingDirectionDebit:
			if line.Amount > math.MaxInt64-debits {
				return "", errInvalidSavingJournal
			}
			debits += line.Amount
		case accountingDirectionCredit:
			if line.Amount > math.MaxInt64-credits {
				return "", errInvalidSavingJournal
			}
			credits += line.Amount
		default:
			return "", errInvalidSavingJournal
		}
	}
	if debits == 0 || debits != credits || debits != entry.Amount {
		return "", errInvalidSavingJournal
	}
	reference := "REV-" + entry.ReferenceNo
	if entry.ReferenceNo == "" {
		reference = "REV-" + entry.ID
	}
	description := translate(language, "journal_reversal_prefix") + ": " + entry.Description
	reversalID := newID()
	reversalDate := time.Now().In(jakartaLocation).Format("2006-01-02")
	if _, err := tx.Exec(`INSERT INTO financial_journal_entries(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,batch_id,description,recorded_by,category,loan_type,reversal_of,correction_reason)
		VALUES($1,$2,$3,'savings',$4,$5,$6,'posted',$7,$8,$9,$10,$11,$12,$13)`, reversalID, reference, entry.TransactionID, reversalDate, entry.Source, debits, entry.BatchID, description, actorID, entry.Category, entry.LoanType, entry.ID, reason); err != nil {
		return "", err
	}
	for _, line := range lines {
		side := accountingDirectionCredit
		if line.Side == accountingDirectionCredit {
			side = accountingDirectionDebit
		}
		if _, err := tx.Exec(`INSERT INTO financial_journal_lines(id,journal_id,side,coa_code,amount,component,mapping_override) VALUES($1,$2,$3,$4,$5,$6,TRUE)`, newID(), reversalID, side, line.COACode, line.Amount, line.Component); err != nil {
			return "", err
		}
	}
	return reversalID, nil
}

func validSavingCorrectionReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return len(reason) > 0 && len(reason) <= 500
}

func (s *Server) respondSavingCorrectionError(c *gin.Context, err error) {
	lang := languageFromRequest(c)
	switch {
	case errors.Is(err, errInvalidSavingCorrection):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_saving_correction"))
	case errors.Is(err, errSavingCorrectionNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_saving_correction_not_found"))
	case errors.Is(err, errSavingCorrectionDepositOnly):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_saving_correction_deposit_only"))
	case errors.Is(err, errSavingJournalAlreadyReversed), errors.Is(err, errInvalidSavingJournal), errors.Is(err, errJournalNotPosted):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_saving_correction_journal"))
	case isMonetaryAggregateCapacityError(err):
		respondError(c, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", translate(lang, "error_monetary_aggregate_capacity"))
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
	}
}

func (s *Server) respondSavingRemovalError(c *gin.Context, err error) {
	lang := languageFromRequest(c)
	switch {
	case errors.Is(err, errInvalidSavingRemoval):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", translate(lang, "error_invalid_saving_removal"))
	case errors.Is(err, errSavingCorrectionNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", translate(lang, "error_saving_correction_not_found"))
	case errors.Is(err, errSavingCorrectionDepositOnly):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_saving_removal_not_deposit"))
	case errors.Is(err, errSavingRemovalWithdrawalLink):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_saving_removal_withdrawal_linked"))
	case errors.Is(err, errInvalidSavingJournal), errors.Is(err, errJournalNotPosted):
		respondError(c, http.StatusConflict, "BUSINESS_RULE_VIOLATION", translate(lang, "error_saving_correction_journal"))
	case isMonetaryAggregateCapacityError(err):
		respondError(c, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", translate(lang, "error_monetary_aggregate_capacity"))
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", translate(lang, "error.Internal server error"))
	}
}
