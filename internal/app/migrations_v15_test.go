package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLegacyLoanMigrationClassifiesHistoryPreservesBalancesAndRemovesPendingWork(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)

	var beforeBalance, beforeInterest, beforeObligation, beforeScheduled, beforePaid, beforeRepayment int
	if err := db.QueryRow(`SELECT remaining_balance,total_interest,total_obligation FROM loans WHERE id='loan-history'`).Scan(&beforeBalance, &beforeInterest, &beforeObligation); err != nil {
		t.Fatalf("read legacy Loan totals before migration: %v", err)
	}
	if err := db.QueryRow(`SELECT scheduled_amount,paid_amount FROM loan_installments WHERE id='installment-history'`).Scan(&beforeScheduled, &beforePaid); err != nil {
		t.Fatalf("read legacy installment before migration: %v", err)
	}
	if err := db.QueryRow(`SELECT amount FROM loan_repayments WHERE id='repayment-history'`).Scan(&beforeRepayment); err != nil {
		t.Fatalf("read legacy repayment before migration: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate to v15: %v", err)
	}

	for _, requestID := range []string{"request-approved", "request-rejected", "request-cancelled"} {
		var loanType string
		var legacyTerms bool
		if err := db.QueryRow(`SELECT loan_type,legacy_terms FROM loan_requests WHERE id=$1`, requestID).Scan(&loanType, &legacyTerms); err != nil {
			t.Fatalf("read historical Loan Request %s: %v", requestID, err)
		}
		if loanType != "regular" || !legacyTerms {
			t.Fatalf("historical Loan Request %s classified as %q legacy=%v", requestID, loanType, legacyTerms)
		}
	}

	var loanType string
	var legacyTerms bool
	var balance, interest, obligation, scheduled, paid, repayment int
	if err := db.QueryRow(`SELECT loan_type,legacy_terms,remaining_balance,total_interest,total_obligation FROM loans WHERE id='loan-history'`).Scan(&loanType, &legacyTerms, &balance, &interest, &obligation); err != nil {
		t.Fatalf("read migrated Loan: %v", err)
	}
	if err := db.QueryRow(`SELECT scheduled_amount,paid_amount FROM loan_installments WHERE id='installment-history'`).Scan(&scheduled, &paid); err != nil {
		t.Fatalf("read migrated installment: %v", err)
	}
	if err := db.QueryRow(`SELECT amount FROM loan_repayments WHERE id='repayment-history'`).Scan(&repayment); err != nil {
		t.Fatalf("read migrated repayment: %v", err)
	}
	if loanType != "regular" || !legacyTerms || balance != beforeBalance || interest != beforeInterest || obligation != beforeObligation || scheduled != beforeScheduled || paid != beforePaid || repayment != beforeRepayment {
		t.Fatalf("historical obligation changed: type=%q legacy=%v balance=%d/%d interest=%d/%d obligation=%d/%d installment=%d/%d paid=%d/%d repayment=%d/%d", loanType, legacyTerms, balance, beforeBalance, interest, beforeInterest, obligation, beforeObligation, scheduled, beforeScheduled, paid, beforePaid, repayment, beforeRepayment)
	}

	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-pending'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='request-pending'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-pending-active'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-pending-resolved'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-unrelated-active'`, 1)

	server := &Server{db: db}
	notifications, err := server.notificationsForUser("user-affected", "member", "en")
	if err != nil {
		t.Fatalf("load migration notification: %v", err)
	}
	if len(notifications) != 1 || notifications[0].EventType != "loan_request_removed_for_type_selection" || notifications[0].Title != "Loan request must be resubmitted" || !strings.Contains(notifications[0].Body, "select a Loan Type") || notifications[0].Link != "/member/loan-requests" {
		t.Fatalf("unexpected migration notification: %+v", notifications)
	}
	if got := translate("id", "notification_loan_request_removed_body"); !strings.Contains(got, "pilih Jenis Pinjaman") {
		t.Fatalf("missing Bahasa migration copy: %q", got)
	}
}

func TestLegacyLoanMigrationIsSafeThroughMigrationReruns(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,loan_type,current_approval_stage) VALUES ('request-new','member-affected',500000,6,'pending','secondary_goods','manager')`); err != nil {
		t.Fatalf("insert post-migration request: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	var loanType string
	var legacyTerms bool
	if err := db.QueryRow(`SELECT loan_type,legacy_terms FROM loan_requests WHERE id='request-new'`).Scan(&loanType, &legacyTerms); err != nil {
		t.Fatalf("post-migration request was changed on rerun: %v", err)
	}
	if loanType != "secondary_goods" || legacyTerms {
		t.Fatalf("new request changed type=%q legacy=%v", loanType, legacyTerms)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=15`, 1)
}

func TestLegacyLoanMigrationRequiresExplicitLoanTypeForFutureRows(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate to v15: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,current_approval_stage) VALUES ('request-omitted','member-affected',500000,6,'pending','manager')`); err == nil {
		t.Fatal("future Loan Request must not silently default to Regular")
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,loan_type,current_approval_stage) VALUES ('request-invalid','member-affected',500000,6,'pending','invalid','manager')`); err == nil {
		t.Fatal("future Loan Request must reject an invalid Loan Type")
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,loan_type,current_approval_stage) VALUES ('request-secondary','member-affected',500000,6,'pending','secondary_goods','manager')`); err != nil {
		t.Fatalf("insert explicitly typed Loan Request: %v", err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET loan_type=NULL WHERE id='request-secondary'`); err == nil {
		t.Fatal("future Loan Request must reject clearing its Loan Type")
	}
	if _, err := db.Exec(`INSERT INTO loans (id,loan_request_id,member_id,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date) VALUES ('loan-omitted','request-secondary','member-affected',500000,6,90000,540000,'active','user-officer','2026-01-15',100,40000,540000,'2026-02-15','2026-07-15')`); err == nil {
		t.Fatal("future Loan must not silently default to Regular")
	}
	if _, err := db.Exec(`INSERT INTO loans (id,loan_request_id,member_id,loan_type,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date) VALUES ('loan-invalid','request-secondary','member-affected','invalid',500000,6,90000,540000,'active','user-officer','2026-01-15',100,40000,540000,'2026-02-15','2026-07-15')`); err == nil {
		t.Fatal("future Loan must reject an invalid Loan Type")
	}
}

func TestLegacyLoanMigrationDropsRuntimeTemporaryTables(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate to v15: %v", err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM sqlite_temp_master WHERE name LIKE 'migration_15_%'`, 0)
}

func TestLegacyLoanMigrationRollsBackCleanupWhenNotificationCreationFails(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if _, err := db.Exec(`INSERT INTO notification_events (id,event_type) VALUES ('migration-15-loan-request-removed-event-member-affected','collision')`); err != nil {
		t.Fatalf("seed migration collision: %v", err)
	}

	err := Migrate(db)
	if err == nil {
		t.Fatal("expected migration to fail on duplicate notification event")
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-pending'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='request-pending'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-pending-active'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=15`, 0)
}

func TestLegacyLoanMigrationAbortsBeforeCleanupWithoutCanonicalMemberUser(t *testing.T) {
	db := v14TestDatabase(t)
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('member-without-user','M-015-NOUSER','No User','2026-01-01','active')`); err != nil {
		t.Fatalf("seed Member without User: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,current_approval_stage) VALUES ('request-without-user','member-without-user',500000,5,'pending','manager')`); err != nil {
		t.Fatalf("seed pending request without User: %v", err)
	}

	if err := Migrate(db); err == nil {
		t.Fatal("expected migration to abort when a pending request Member has no canonical current User")
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-without-user'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=15`, 0)

	if _, err := db.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,historical_identity) VALUES ('user-current','current@test.local','hash','member','member-without-user','No User',TRUE,FALSE)`); err != nil {
		t.Fatalf("seed canonical current User: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("retry migration after restoring canonical current User: %v", err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-without-user'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE user_id='user-current' AND title_key='notification_loan_request_removed_title'`, 1)
}

func TestLegacyLoanPostgresAndSupabaseMigrationMirrorsMatch(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	postgresPath := filepath.Join(repositoryRoot, "migrations", "015_classify_legacy_loans_and_remove_pending_requests.sql")
	supabasePath := filepath.Join(repositoryRoot, "supabase", "migrations", "20260715053400_classify_legacy_loans_and_remove_pending_requests.sql")
	postgresMigration, err := os.ReadFile(postgresPath)
	if err != nil {
		t.Fatalf("read PostgreSQL migration: %v", err)
	}
	supabaseMigration, err := os.ReadFile(supabasePath)
	if err != nil {
		t.Fatalf("read Supabase migration mirror: %v", err)
	}
	if string(postgresMigration) != string(supabaseMigration) {
		t.Fatalf("Supabase migration %s must exactly mirror PostgreSQL migration %s", supabasePath, postgresPath)
	}
	for _, required := range []string{"migration_15_notification_preflight", "missing_member_count = 0", "users.historical_identity = FALSE"} {
		if !strings.Contains(string(postgresMigration), required) {
			t.Fatalf("PostgreSQL migration is missing canonical-User preflight fragment %q", required)
		}
	}
}

func v14TestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	for _, migration := range migrations[:14] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	return db
}

func seedLegacyLoanMigrationData(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('member-affected','M-015-A','Affected Member','2026-01-01','active'),('member-history','M-015-H','History Member','2025-01-01','active'),('member-officer','M-015-O','Officer Member','2025-01-01','active')`,
		`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,historical_identity) VALUES ('user-affected','affected@test.local','hash','member','member-affected','Affected Member',TRUE,FALSE),('user-history','history@test.local','hash','member','member-history','History Member',TRUE,FALSE),('user-officer','officer@test.local','hash','member','member-officer','Officer Member',TRUE,FALSE)`,
		`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,current_approval_stage) VALUES ('request-pending','member-affected',900000,9,'Pending','pending','ketua_i'),('request-approved','member-history',800000,8,'Approved','approved',NULL),('request-rejected','member-history',700000,7,'Rejected','rejected',NULL),('request-cancelled','member-history',600000,6,'Cancelled','cancelled',NULL)`,
		`INSERT INTO loan_request_approvals (id,request_id,stage,decision,officer_id,officer_name,officer_role,officer_member_id,officer_member_no) VALUES ('approval-pending','request-pending','manager','approved','user-officer','Officer Member','manager','member-officer','M-015-O')`,
		`INSERT INTO loans (id,loan_request_id,member_id,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date) VALUES ('loan-history','request-approved','member-history',800000,8,108000,648000,'active','user-officer','2026-01-15',100,64000,864000,'2026-04-15','2026-09-15')`,
		`INSERT INTO loan_installments (id,loan_id,installment_no,due_date,scheduled_amount,paid_amount) VALUES ('installment-history','loan-history',1,'2026-02-15',108000,108000)`,
		`INSERT INTO loan_repayments (id,loan_id,member_id,amount,record_date,recorded_by) VALUES ('repayment-history','loan-history','member-history',216000,'2026-03-01','user-officer')`,
		`INSERT INTO notification_events (id,event_type,request_type,request_id) VALUES ('event-pending','loan_approval_required','loan','request-pending'),('event-unrelated','loan_approval_required','loan','request-approved')`,
		`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link,is_read,resolved_at,audience) VALUES ('notice-pending-active','event-pending','user-officer','notification_approval_title','notification_approval_body','/admin/loan-requests',FALSE,NULL,'officer'),('notice-pending-resolved','event-pending','user-officer','notification_approval_title','notification_approval_body','/admin/loan-requests',TRUE,CURRENT_TIMESTAMP,'officer'),('notice-unrelated-active','event-unrelated','user-officer','notification_approval_title','notification_approval_body','/admin/loan-requests',FALSE,NULL,'officer')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed v14 data: %v", err)
		}
	}
}

func assertRowCount(t *testing.T, db *sql.DB, query string, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows with %q: %v", query, err)
	}
	if count != expected {
		t.Fatalf("count rows with %q = %d, want %d", query, count, expected)
	}
}
