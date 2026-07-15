//go:build postgres_integration

package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestLegacyLoanPostgresMigration(t *testing.T) {
	t.Run("classifies history, preserves financials, cleans pending work, notifies, and reruns safely", func(t *testing.T) {
		db := postgresV14TestDatabase(t)
		seedLegacyLoanMigrationData(t, db)

		var beforeBalance, beforeInterest, beforeObligation int
		if err := db.QueryRow(`SELECT remaining_balance,total_interest,total_obligation FROM loans WHERE id='loan-history'`).Scan(&beforeBalance, &beforeInterest, &beforeObligation); err != nil {
			t.Fatalf("read PostgreSQL legacy financials: %v", err)
		}
		applyPostgresMigration15(t, db)

		var loanType string
		var legacyTerms bool
		var balance, interest, obligation int
		if err := db.QueryRow(`SELECT loan_type,legacy_terms,remaining_balance,total_interest,total_obligation FROM loans WHERE id='loan-history'`).Scan(&loanType, &legacyTerms, &balance, &interest, &obligation); err != nil {
			t.Fatalf("read migrated PostgreSQL Loan: %v", err)
		}
		if loanType != "regular" || !legacyTerms || balance != beforeBalance || interest != beforeInterest || obligation != beforeObligation {
			t.Fatalf("PostgreSQL migration changed historical obligation: type=%q legacy=%v balance=%d/%d interest=%d/%d obligation=%d/%d", loanType, legacyTerms, balance, beforeBalance, interest, beforeInterest, obligation, beforeObligation)
		}
		assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-pending'`, 0)
		assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='request-pending'`, 0)
		assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-pending-active'`, 0)
		assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE user_id='user-affected' AND title_key='notification_loan_request_removed_title'`, 1)

		for _, table := range []string{"loan_requests", "loans"} {
			var nullable, defaultValue string
			if err := db.QueryRow(`SELECT is_nullable,COALESCE(column_default,'') FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='loan_type'`, table).Scan(&nullable, &defaultValue); err != nil {
				t.Fatalf("inspect PostgreSQL %s.loan_type: %v", table, err)
			}
			if nullable != "NO" || defaultValue != "" {
				t.Fatalf("PostgreSQL %s.loan_type nullable=%q default=%q, want required with no default", table, nullable, defaultValue)
			}
		}
		if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,current_approval_stage) VALUES ('request-omitted','member-affected',500000,6,'pending','manager')`); err == nil {
			t.Fatal("PostgreSQL future Loan Request must not silently default to Regular")
		}
		if err := Migrate(db); err != nil {
			t.Fatalf("rerun tracked PostgreSQL migrations: %v", err)
		}
		assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=15`, 1)
	})

	t.Run("rolls back all cleanup when notification creation fails", func(t *testing.T) {
		db := postgresV14TestDatabase(t)
		seedLegacyLoanMigrationData(t, db)
		if _, err := db.Exec(`INSERT INTO notification_events (id,event_type) VALUES ('migration-15-loan-request-removed-event-member-affected','collision')`); err != nil {
			t.Fatalf("seed PostgreSQL collision: %v", err)
		}
		migrationSQL := postgresMigration15SQL(t)
		if _, err := db.Exec(migrationSQL); err == nil {
			t.Fatal("expected PostgreSQL migration collision to roll back")
		}
		assertRowCount(t, db, `SELECT COUNT(*) FROM loan_requests WHERE id='request-pending'`, 1)
		assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='request-pending'`, 1)
		assertRowCount(t, db, `SELECT COUNT(*) FROM notifications WHERE id='notice-pending-active'`, 1)
		assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=15`, 0)
		assertRowCount(t, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='loan_requests' AND column_name='loan_type'`, 0)
	})
}

func postgresV14TestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("KOPDES_POSTGRES_TEST_URL")
	if databaseURL == "" {
		t.Skip("set KOPDES_POSTGRES_TEST_URL to run the PostgreSQL migration integration test")
	}

	adminConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse KOPDES_POSTGRES_TEST_URL: %v", err)
	}
	adminConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	adminDB := stdlib.OpenDB(*adminConfig)
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Fatalf("connect to KOPDES_POSTGRES_TEST_URL: %v", err)
	}

	schema := fmt.Sprintf("kopdes_m15_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
		_ = adminDB.Close()
	})

	testConfig := adminConfig.Copy()
	testConfig.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*testConfig)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create PostgreSQL migration table: %v", err)
	}
	for _, migration := range migrations[:14] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply PostgreSQL migration %d: %v", migration.Version, err)
		}
	}
	return db
}

func applyPostgresMigration15(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(postgresMigration15SQL(t)); err != nil {
		t.Fatalf("apply PostgreSQL migration 15: %v", err)
	}
}

func postgresMigration15SQL(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "015_classify_legacy_loans_and_remove_pending_requests.sql"))
	if err != nil {
		t.Fatalf("read PostgreSQL migration 15: %v", err)
	}
	return string(contents)
}
