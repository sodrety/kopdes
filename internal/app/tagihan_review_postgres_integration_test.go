//go:build postgres_integration

package app

import (
	"bytes"
	"testing"
)

func TestPostgresTagihanReviewedPreviewAndConfirmation(t *testing.T) {
	db := postgresV14TestDatabase(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAdminUser(db, "review-pg@coop.test", "password"); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := db.QueryRow(`SELECT id FROM users WHERE email='review-pg@coop.test'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO members(id,member_no,full_name,join_date,status) VALUES('review-pg-member','REVIEW-PG','Reviewed PG','2026-01-01','active')`,
		`INSERT INTO member_tagihan_configs(member_id,simpanan_wajib,simpanan_manasuka) VALUES('review-pg-member',10000,0) ON CONFLICT(member_id) DO UPDATE SET simpanan_wajib=10000`,
		`INSERT INTO coa_accounts(id,code,name,account_type,subtype,normal_balance,is_group,active) VALUES('review-pg-bank','REVIEW-BANK','Bank','asset','bank','D',FALSE,TRUE),('review-pg-liability','REVIEW-LIABILITY','Savings','liability','','C',FALSE,TRUE)`,
		`INSERT INTO accounting_mappings(id,mapping_key,transaction_type,component,coa_code,active) VALUES('review-pg-bank','review-pg-bank','savings','cash_bank','REVIEW-BANK',TRUE),('review-pg-liability','review-pg-liability','savings','savings_liability','REVIEW-LIABILITY',TRUE)`,
	}
	for _, q := range statements {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	month, _ := parseTagihanStatementMonth("2026-10")
	s := &Server{db: db, cfg: Config{JWTSecret: "review-pg-secret"}}
	w, err := buildTagihanWorkbook(month, []TagihanRow{{MemberID: "review-pg-member", FullName: "Reviewed PG", SimpananWajib: 10000, Total: 10000, Status: "paid"}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err = s.addLoanDetailSheet(w, month, "en"); err != nil {
		t.Fatal(err)
	}
	_ = w.SetCellValue("Tagihan", "N2", 5000)
	var b bytes.Buffer
	if err = w.Write(&b); err != nil {
		t.Fatal(err)
	}
	plan, err := s.reviewTagihan(month, "2026-10-25", b.Bytes(), actor, "en", nil)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM saving_records WHERE member_id='review-pg-member'`).Scan(&count)
	if count != 0 || plan.Committed || plan.ActualTotal != 5000 {
		t.Fatal("preview persisted or repriced", plan)
	}
	claim := tagihanPreviewClaim{State: plan.fingerprint}
	result, err := s.reviewTagihan(month, "2026-10-25", b.Bytes(), actor, "en", &claim)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed {
		t.Fatal("not committed")
	}
	var amount, journal int64
	_ = db.QueryRow(`SELECT amount FROM saving_records WHERE member_id='review-pg-member'`).Scan(&amount)
	_ = db.QueryRow(`SELECT SUM(x.amount) FROM financial_journal_lines x JOIN financial_journal_entries j ON j.id=x.journal_id WHERE j.batch_id='TAGIHAN-2026-10' AND x.side='debit'`).Scan(&journal)
	if amount != 5000 || journal != 5000 {
		t.Fatal("wrong actual/journal", amount, journal)
	}
}
