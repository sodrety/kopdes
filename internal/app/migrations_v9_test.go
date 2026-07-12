package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestLoanScheduleMigrationBackfillsAndReopensPaidLoan(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:8] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}

	approvedAt := time.Date(2026, time.January, 31, 18, 0, 0, 0, time.UTC)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, email, password_hash, role) VALUES ('admin', 'admin@example.test', 'hash', 'admin')`, nil},
		{`INSERT INTO members (id, member_no, full_name, join_date) VALUES ('member', 'M-1', 'Member', '2025-01-01')`, nil},
		{`INSERT INTO members (id, member_no, full_name, join_date) VALUES ('member-active', 'M-2', 'Active Member', '2025-01-01')`, nil},
		{`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, status) VALUES ('request', 'member', 1000000, 2, 'approved')`, nil},
		{`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, status) VALUES ('request-cancelled', 'member', 500000, 2, 'approved')`, nil},
		{`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, status) VALUES ('request-paid', 'member', 100000, 1, 'approved')`, nil},
		{`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, status) VALUES ('request-active', 'member-active', 200000, 2, 'approved')`, nil},
		{`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at) VALUES ('loan', 'request', 'member', 1000000, 2, 500000, 0, 'paid', 'admin', $1)`, []any{approvedAt}},
		{`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at) VALUES ('loan-cancelled', 'request-cancelled', 'member', 500000, 2, 250000, 123, 'cancelled', 'admin', $1)`, []any{approvedAt}},
		{`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at) VALUES ('loan-paid', 'request-paid', 'member', 100000, 1, 100000, 0, 'paid', 'admin', $1)`, []any{approvedAt}},
		{`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at) VALUES ('loan-active', 'request-active', 'member-active', 200000, 2, 100000, 200000, 'active', 'admin', $1)`, []any{approvedAt}},
		{`INSERT INTO loan_repayments (id, loan_id, member_id, amount, record_date, recorded_by) VALUES ('repayment', 'loan', 'member', 1000000, '2026-02-01', 'admin')`, nil},
		{`INSERT INTO loan_repayments (id, loan_id, member_id, amount, record_date, recorded_by) VALUES ('repayment-paid', 'loan-paid', 'member', 101000, '2026-02-01', 'admin')`, nil},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyMigration(db, migrations[8]); err != nil {
		t.Fatal(err)
	}

	var startDate, status, nextDueDate, finalDueDate string
	var interest, obligation, balance int64
	if err := db.QueryRow(`SELECT start_date, total_interest, total_obligation, remaining_balance, status, next_due_date, final_due_date FROM loans WHERE id = 'loan'`).
		Scan(&startDate, &interest, &obligation, &balance, &status, &nextDueDate, &finalDueDate); err != nil {
		t.Fatal(err)
	}
	if startDate != "2026-02-01" || interest != 20_000 || obligation != 1_020_000 || balance != 20_000 || status != "adjustment_due" || nextDueDate != "2026-04-01" || finalDueDate != "2026-04-01" {
		t.Fatalf("unexpected migrated loan: start=%s interest=%d obligation=%d balance=%d status=%s next=%s final=%s", startDate, interest, obligation, balance, status, nextDueDate, finalDueDate)
	}

	rows, err := db.Query(`SELECT installment_no, scheduled_amount, paid_amount FROM loan_installments WHERE loan_id = 'loan' ORDER BY installment_no`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantPaid := []int64{510_000, 490_000}
	index := 0
	for rows.Next() {
		var number int
		var scheduled, paid int64
		if err := rows.Scan(&number, &scheduled, &paid); err != nil {
			t.Fatal(err)
		}
		if number != index+1 || scheduled != 510_000 || paid != wantPaid[index] {
			t.Fatalf("unexpected installment %d: scheduled=%d paid=%d", number, scheduled, paid)
		}
		index++
	}
	if index != 2 {
		t.Fatalf("got %d installments, want 2", index)
	}
	var cancelledStatus string
	var cancelledRate int
	var cancelledObligation, cancelledBalance, cancelledInstallments int64
	if err := db.QueryRow(`SELECT status, interest_rate_bps, total_obligation, remaining_balance, (SELECT COUNT(*) FROM loan_installments WHERE loan_id = loans.id) FROM loans WHERE id = 'loan-cancelled'`).Scan(&cancelledStatus, &cancelledRate, &cancelledObligation, &cancelledBalance, &cancelledInstallments); err != nil {
		t.Fatal(err)
	}
	if cancelledStatus != "cancelled" || cancelledRate != 0 || cancelledObligation != 0 || cancelledBalance != 0 || cancelledInstallments != 0 {
		t.Fatalf("cancelled loan was not excluded: status=%s rate=%d obligation=%d balance=%d installments=%d", cancelledStatus, cancelledRate, cancelledObligation, cancelledBalance, cancelledInstallments)
	}
	var activeStatus string
	if err := db.QueryRow(`SELECT status FROM loans WHERE id = 'loan-active'`).Scan(&activeStatus); err != nil || activeStatus != "active" {
		t.Fatalf("active loan status=%q err=%v", activeStatus, err)
	}
	var fullyPaidStatus string
	var fullyPaidBalance int64
	if err := db.QueryRow(`SELECT status, remaining_balance FROM loans WHERE id = 'loan-paid'`).Scan(&fullyPaidStatus, &fullyPaidBalance); err != nil || fullyPaidStatus != "paid" || fullyPaidBalance != 0 {
		t.Fatalf("fully paid historical loan status=%q balance=%d err=%v", fullyPaidStatus, fullyPaidBalance, err)
	}
	var repaymentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM loan_repayments WHERE loan_id = 'loan'`).Scan(&repaymentCount); err != nil || repaymentCount != 1 {
		t.Fatalf("repayments changed: count=%d err=%v", repaymentCount, err)
	}
}

func TestLoanScheduleMigrationPreservesLegacyTenorAboveCurrentLimit(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:8] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	approvedAt := time.Date(2026, time.January, 31, 18, 0, 0, 0, time.UTC)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, email, password_hash, role) VALUES ('admin', 'admin@example.test', 'hash', 'admin')`, nil},
		{`INSERT INTO members (id, member_no, full_name, join_date) VALUES ('member', 'M-121', 'Legacy Member', '2025-01-01')`, nil},
		{`INSERT INTO loan_requests (id, member_id, requested_amount, duration_months, status) VALUES ('request', 'member', 121000, 121, 'approved')`, nil},
		{`INSERT INTO loans (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at) VALUES ('loan', 'request', 'member', 121000, 121, 1000, 121000, 'active', 'admin', $1)`, []any{approvedAt}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, migration := range migrations[8:] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}

	var duration, count int
	var interest, obligation, scheduledTotal int64
	var firstDue, finalDue string
	err = db.QueryRow(`SELECT duration_months, total_interest, total_obligation, final_due_date,
		(SELECT COUNT(*) FROM loan_installments WHERE loan_id='loan'),
		(SELECT SUM(scheduled_amount) FROM loan_installments WHERE loan_id='loan'),
		(SELECT due_date FROM loan_installments WHERE loan_id='loan' ORDER BY installment_no LIMIT 1)
		FROM loans WHERE id='loan'`).Scan(&duration, &interest, &obligation, &finalDue, &count, &scheduledTotal, &firstDue)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 121 || count != 121 || interest != 146410 || obligation != 267410 || scheduledTotal != obligation || firstDue != "2026-03-01" || finalDue != "2036-03-01" {
		t.Fatalf("legacy schedule duration=%d count=%d interest=%d obligation=%d sum=%d first=%s final=%s", duration, count, interest, obligation, scheduledTotal, firstDue, finalDue)
	}
}

func TestLoanScheduleMigrationRestoresSQLiteForeignKeysOnPooledFileDatabase(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "migration.db") + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	connections := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	for index := 0; index < 4; index++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		var enabled int
		if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
			t.Fatalf("connection %d foreign_keys=%d err=%v", index, enabled, err)
		}
	}
}

func TestLoanScheduleMigrationPreservesSQLiteForeignKeysOff(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	var enabled int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 0 {
		t.Fatalf("foreign_keys=%d err=%v, want prior OFF state", enabled, err)
	}
}

func TestLoanScheduleMigrationFailureRollsBackAndRestoresSQLiteForeignKeys(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:8] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	failing := migration{Version: 9, Name: "forced_failure", Statements: []string{
		`CREATE TABLE migration_rollback_marker (id INTEGER)`,
		`THIS IS NOT VALID SQL`,
	}}
	if err := applyMigration(db, failing); err == nil {
		t.Fatal("expected migration failure")
	}
	var enabled int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign_keys=%d err=%v, want restored ON state", enabled, err)
	}
	var markerCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration_rollback_marker'`).Scan(&markerCount); err != nil || markerCount != 0 {
		t.Fatalf("rollback marker count=%d err=%v", markerCount, err)
	}
	var migrationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 9`).Scan(&migrationCount); err != nil || migrationCount != 0 {
		t.Fatalf("migration record count=%d err=%v", migrationCount, err)
	}
}

func TestPendingRequestMigrationReconcilesDuplicatesDeterministically(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:9] {
		if err = applyMigration(db, migration); err != nil {
			t.Fatalf("migration %d: %v", migration.Version, err)
		}
	}
	if _, err = db.Exec(`INSERT INTO members(id,member_no,full_name,join_date) VALUES('m','M-DUP','Duplicate','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,status,created_at) VALUES('older','m',100,2,'pending','2026-01-01 00:00:00')`,
		`INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,status,created_at) VALUES('newer','m',100,2,'pending','2026-01-02 00:00:00')`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = applyMigration(db, migrations[9]); err != nil {
		t.Fatal(err)
	}
	var older, newer string
	if err = db.QueryRow(`SELECT status FROM loan_requests WHERE id='older'`).Scan(&older); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT status FROM loan_requests WHERE id='newer'`).Scan(&newer); err != nil {
		t.Fatal(err)
	}
	if older != "pending" || newer != "rejected" {
		t.Fatalf("statuses older=%s newer=%s", older, newer)
	}
}

func TestMaximumTenorMigrationEnforcesFutureWritesAndPreservesLegacy(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:10] {
		if err = applyMigration(db, migration); err != nil {
			t.Fatalf("migration %d: %v", migration.Version, err)
		}
	}
	if _, err = db.Exec(`INSERT INTO members(id,member_no,full_name,join_date) VALUES('m','M-LONG','Legacy','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,status) VALUES('legacy','m',100,121,'rejected')`); err != nil {
		t.Fatal(err)
	}
	if err = applyMigration(db, migrations[10]); err != nil {
		t.Fatal(err)
	}
	var duration int
	if err = db.QueryRow(`SELECT duration_months FROM loan_requests WHERE id='legacy'`).Scan(&duration); err != nil || duration != 121 {
		t.Fatalf("legacy duration=%d err=%v", duration, err)
	}
	if _, err = db.Exec(`INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,status) VALUES('future','m',100,121,'rejected')`); err == nil {
		t.Fatal("expected future over-limit write to fail")
	}
}
