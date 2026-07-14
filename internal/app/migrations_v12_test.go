package app

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOfficerHierarchyMigrationMapsLegacyDataAndInitializesPendingWork(t *testing.T) {
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
	for _, migration := range migrations[:11] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}

	statements := []string{
		`INSERT INTO users (id,email,password_hash,role) VALUES ('legacy-admin','admin@example.test','hash','admin')`,
		`INSERT INTO members (id,member_no,full_name,join_date) VALUES ('member','M-12','Legacy Member','2025-01-01')`,
		`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status) VALUES ('loan-pending','member',100000,10,'pending')`,
		`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,reviewed_by) VALUES ('loan-approved','member',200000,10,'approved','legacy-admin')`,
		`INSERT INTO withdrawal_requests (id,member_id,amount,status) VALUES ('withdrawal-pending','member',50000,'pending')`,
		`INSERT INTO withdrawal_requests (id,member_id,amount,status,reviewed_by) VALUES ('withdrawal-rejected','member',25000,'rejected','legacy-admin')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyMigration(db, migrations[11]); err != nil {
		t.Fatalf("apply migration 12: %v", err)
	}

	var role, fullName string
	var active, mustChange bool
	if err := db.QueryRow(`SELECT role,full_name,active,must_change_password FROM users WHERE id='legacy-admin'`).Scan(&role, &fullName, &active, &mustChange); err != nil {
		t.Fatal(err)
	}
	if role != "manager" || fullName != "admin@example.test" || !active || mustChange {
		t.Fatalf("legacy admin migrated to role=%s name=%s active=%v must_change=%v", role, fullName, active, mustChange)
	}
	for _, item := range []struct {
		table, id, wantStage string
	}{
		{"loan_requests", "loan-pending", "manager"},
		{"loan_requests", "loan-approved", ""},
		{"withdrawal_requests", "withdrawal-pending", "manager"},
		{"withdrawal_requests", "withdrawal-rejected", ""},
	} {
		var stage string
		query := `SELECT COALESCE(current_approval_stage,'') FROM ` + item.table + ` WHERE id=$1`
		if err := db.QueryRow(query, item.id).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		if stage != item.wantStage {
			t.Fatalf("%s %s stage=%q want %q", item.table, item.id, stage, item.wantStage)
		}
	}
	var reservationAmount int
	var reservationStatus string
	if err := db.QueryRow(`SELECT amount,status FROM withdrawal_reservations WHERE request_id='withdrawal-pending'`).Scan(&reservationAmount, &reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationAmount != 50_000 || reservationStatus != "active" {
		t.Fatalf("unexpected migrated reservation amount=%d status=%s", reservationAmount, reservationStatus)
	}
	for _, table := range []string{"loan_request_approvals", "withdrawal_request_approvals", "officer_audit_events", "notification_events", "notifications"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=$1`, table).Scan(&name); err != nil {
			t.Fatalf("expected table %s: %v", table, err)
		}
	}
}
