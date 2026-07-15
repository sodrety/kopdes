package app

import (
	"fmt"
	"testing"
)

func TestRegularLoanAdminFeeSQLiteMigrationRejectsInvalidSnapshots(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate through v16: %v", err)
	}

	loanUpdates := []string{
		`admin_fee_policy=NULL`,
		`admin_fee_policy='unknown'`,
		`total_admin_fee=NULL`,
		`total_obligation=NULL`,
		`monthly_admin_fee=1`,
	}
	for _, assignment := range loanUpdates {
		if _, err := db.Exec(`UPDATE loans SET ` + assignment + ` WHERE id='loan-history'`); err == nil {
			t.Fatalf("invalid Loan snapshot update succeeded: %s", assignment)
		}
	}

	requestUpdates := []string{
		`proposed_total_admin_fee=1`,
		`proposed_admin_fee_policy='unknown',proposed_total_admin_fee=1,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=NULL,proposed_total_admin_fee=1,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=1,proposed_total_admin_fee=NULL,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=1,proposed_total_admin_fee=1,proposed_total_obligation=NULL`,
		`proposed_admin_fee_policy='legacy_flat_monthly',proposed_monthly_admin_fee=1,proposed_total_admin_fee=1,proposed_total_obligation=2`,
	}
	for _, assignment := range requestUpdates {
		if _, err := db.Exec(`UPDATE loan_requests SET ` + assignment + ` WHERE id='request-approved'`); err == nil {
			t.Fatalf("invalid proposed snapshot update succeeded: %s", assignment)
		}
	}

	invalidInsertFields := []string{
		`NULL,1,NULL,NULL`,
		`'unknown',NULL,1,2`,
		`'regular_tiered_monthly_v1',NULL,1,2`,
		`'regular_tiered_monthly_v1',1,NULL,2`,
		`'regular_tiered_monthly_v1',1,1,NULL`,
		`'legacy_flat_monthly',1,1,2`,
	}
	for index, fields := range invalidInsertFields {
		query := fmt.Sprintf(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation) VALUES ('invalid-v16-%d','member-history',100,1,'invalid','rejected','regular',NULL,%s)`, index, fields)
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("invalid proposed snapshot insert succeeded: %s", fields)
		}
	}

	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage) VALUES ('snapshot-request','member-history',800000,8,'valid','pending','regular','manager')`); err != nil {
		t.Fatalf("seed pending Manager request: %v", err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET proposed_approved_amount=800000,proposed_duration_months=8,proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=8000,proposed_total_admin_fee=64000,proposed_total_obligation=864000 WHERE id='snapshot-request'`); err != nil {
		t.Fatalf("valid complete snapshot rejected: %v", err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET proposed_total_admin_fee=64001 WHERE id='snapshot-request'`); err == nil {
		t.Fatal("snapshotted proposed terms were mutable")
	}
}

func TestRegularLoanAdminFeeSQLiteMigrationRollsBackAndReruns(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := applyMigration(db, migrations[14]); err != nil {
		t.Fatalf("apply migration 15: %v", err)
	}
	if _, err := db.Exec(`CREATE TRIGGER loans_admin_fee_terms_insert BEFORE INSERT ON loans BEGIN SELECT 1; END`); err != nil {
		t.Fatalf("create deliberate v16 trigger collision: %v", err)
	}
	if err := applyMigration(db, migrations[15]); err == nil {
		t.Fatal("migration 16 should fail on deliberate trigger collision")
	}

	var columnCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('loans') WHERE name='admin_fee_policy'`).Scan(&columnCount); err != nil || columnCount != 0 {
		t.Fatalf("failed v16 left schema changes: count=%d err=%v", columnCount, err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 0)

	if _, err := db.Exec(`DROP TRIGGER loans_admin_fee_terms_insert`); err != nil {
		t.Fatalf("drop deliberate trigger collision: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("rerun migration 16 after rollback: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("tracked migration rerun: %v", err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 1)
}

func TestRegularLoanAdminFeeSQLiteRejectsWrongTierFormulaEvenWhenTotalsAreCoherent(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate through v16: %v", err)
	}

	// Rp30m must use Rp325k/month. These terms are internally coherent but
	// encode the wrong policy, and cancelled snapshots do not bypass policy.
	for _, status := range []string{"rejected", "cancelled"} {
		_, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,proposed_approved_amount,proposed_duration_months,proposed_start_date,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation) VALUES ($1,'member-history',30000000,24,'wrong tier',$2,'regular',NULL,30000000,24,'2026-01-15','regular_tiered_monthly_v1',300000,7200000,37200000)`, "wrong-tier-"+status, status)
		if err == nil {
			t.Fatalf("SQLite accepted wrong Regular v1 formula for %s snapshot", status)
		}
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage) VALUES ('correct-tier','member-history',30000000,24,'correct tier','pending','regular','manager')`); err != nil {
		t.Fatalf("SQLite rejected clean Regular request: %v", err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET proposed_approved_amount=30000000,proposed_duration_months=24,proposed_start_date='2026-01-15',proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=325000,proposed_total_admin_fee=7800000,proposed_total_obligation=37800000 WHERE id='correct-tier'`); err != nil {
		t.Fatalf("SQLite rejected exact Regular v1 formula: %v", err)
	}
	for _, approval := range []struct {
		id, stage string
	}{
		{"correct-tier-manager", "manager"},
		{"correct-tier-ketua-i", "ketua_i"},
		{"correct-tier-ketua-ii", "ketua_ii"},
		{"correct-tier-ketua-utama", "ketua_utama"},
	} {
		if _, err := db.Exec(`INSERT INTO loan_request_approvals (id,request_id,stage,decision,officer_id,officer_name,officer_role,officer_member_id,officer_member_no) VALUES ($1,'correct-tier',$2,'approved','user-officer','Officer Member',$2,'member-officer','M-015-O')`, approval.id, approval.stage); err != nil {
			t.Fatalf("seed %s approval: %v", approval.stage, err)
		}
		var err error
		switch approval.stage {
		case "manager":
			_, err = db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_i' WHERE id='correct-tier'`)
		case "ketua_i":
			_, err = db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_ii' WHERE id='correct-tier'`)
		case "ketua_ii":
			_, err = db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_utama' WHERE id='correct-tier'`)
		case "ketua_utama":
			_, err = db.Exec(`UPDATE loan_requests SET status='approved',current_approval_stage=NULL WHERE id='correct-tier'`)
		}
		if err != nil {
			t.Fatalf("advance after %s approval: %v", approval.stage, err)
		}
	}
	loanSQL := `INSERT INTO loans (id,loan_request_id,member_id,loan_type,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date,admin_fee_policy,monthly_admin_fee,total_admin_fee) VALUES ($1,'correct-tier','member-history','regular',30000000,24,$2,$3,'cancelled','user-officer','2026-01-15',100,$4,$5,'2026-02-15','2028-01-15','regular_tiered_monthly_v1',$6,$7)`
	if _, err := db.Exec(loanSQL, "wrong-tier-loan", 1550000, 37200000, 7200000, 37200000, 300000, 7200000); err == nil {
		t.Fatal("SQLite accepted wrong Regular v1 formula for current Loan snapshot")
	}
	if _, err := db.Exec(loanSQL, "correct-tier-loan", 1575000, 37800000, 7800000, 37800000, 325000, 7800000); err != nil {
		t.Fatalf("SQLite rejected exact current Loan snapshot: %v", err)
	}
}

func TestSQLiteMonetaryAggregateInvariantKeepsSumsRepresentable(t *testing.T) {
	const maxInt64 = int64(9223372036854775807)
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate through v16: %v", err)
	}

	var savingTotal int64
	if err := db.QueryRow(`SELECT saving_records_total FROM monetary_aggregate_totals WHERE singleton=1`).Scan(&savingTotal); err != nil {
		t.Fatal(err)
	}
	fill := maxInt64 - savingTotal
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ('aggregate-max','member-history','deposit','sukarela',$1,'2026-07-15','','','user-officer')`, fill); err != nil {
		t.Fatalf("fill saving aggregate to MaxInt64: %v", err)
	}
	var sum int64
	if err := db.QueryRow(`SELECT SUM(amount) FROM saving_records`).Scan(&sum); err != nil || sum != maxInt64 {
		t.Fatalf("safe saving sum=%d err=%v", sum, err)
	}
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ('aggregate-over','member-history','deposit','sukarela',1,'2026-07-15','','','user-officer')`); err == nil {
		t.Fatal("SQLite allowed saving aggregate beyond MaxInt64")
	}
	if _, err := db.Exec(`UPDATE saving_records SET amount=amount WHERE id='aggregate-max'`); err != nil {
		t.Fatalf("same-value aggregate update failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE saving_records SET amount=amount-1 WHERE id='aggregate-max'`); err != nil {
		t.Fatalf("aggregate decrease failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE saving_records SET amount=amount+1 WHERE id='aggregate-max'`); err != nil {
		t.Fatalf("aggregate update back to MaxInt64 failed: %v", err)
	}
	if _, err := db.Exec(`UPDATE saving_records SET amount=amount+1 WHERE id='aggregate-max'`); err == nil {
		t.Fatal("SQLite allowed aggregate UPDATE beyond MaxInt64")
	}
	server := &Server{db: db}
	if summary, err := server.adminDashboardSummary(); err != nil || summary.TotalSavings <= 0 {
		t.Fatalf("dashboard failed at persisted boundary: summary=%+v err=%v", summary, err)
	}
	if _, err := db.Exec(`DELETE FROM saving_records WHERE id='aggregate-max'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ('aggregate-reused','member-history','deposit','sukarela',1,'2026-07-15','','','user-officer')`); err != nil {
		t.Fatalf("released aggregate capacity was not reusable: %v", err)
	}

	for _, aggregate := range []struct {
		column string
		table  string
		insert string
	}{
		{column: "withdrawal_requests_total", table: "withdrawal_requests", insert: `INSERT INTO withdrawal_requests (id,member_id,amount,note,status,current_approval_stage) VALUES ('aggregate-request','member-history',$1,'boundary','pending','manager')`},
		{column: "withdrawal_reservations_total", table: "withdrawal_reservations", insert: `INSERT INTO withdrawal_reservations (id,request_id,member_id,amount,status) VALUES ('aggregate-reservation','aggregate-request','member-history',$1,'active')`},
	} {
		var total int64
		if err := db.QueryRow(`SELECT ` + aggregate.column + ` FROM monetary_aggregate_totals WHERE singleton=1`).Scan(&total); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(aggregate.insert, maxInt64-total); err != nil {
			t.Fatalf("fill %s aggregate: %v", aggregate.table, err)
		}
		if _, err := db.Exec(`UPDATE `+aggregate.table+` SET amount=amount+1 WHERE id=CASE WHEN $1='withdrawal_requests' THEN 'aggregate-request' ELSE 'aggregate-reservation' END`, aggregate.table); err == nil {
			t.Fatalf("SQLite allowed %s aggregate beyond MaxInt64", aggregate.table)
		}
	}
	if amount, err := server.pendingWithdrawalAmount(); err != nil || amount < 0 {
		t.Fatalf("pending withdrawal report failed at persisted boundary: amount=%d err=%v", amount, err)
	}
}

func TestSQLiteLoanPolicyIdentityRequiresManagerSnapshotAndLocksTerms(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE loans SET status='cancelled' WHERE id='loan-history'`); err != nil {
		t.Fatalf("normal legacy cancellation rejected: %v", err)
	}
	if _, err := db.Exec(`UPDATE loans SET legacy_terms=0 WHERE id='loan-history'`); err == nil {
		t.Fatal("legacy Loan identity remained mutable after snapshot")
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage) VALUES ('manager-snapshot','member-history',1000000,12,'manager snapshot','pending','regular','manager')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET proposed_approved_amount=1000000,proposed_duration_months=12,proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=10000,proposed_total_admin_fee=120000,proposed_total_obligation=1120000 WHERE id='manager-snapshot'`); err != nil {
		t.Fatalf("Manager-stage snapshot rejected: %v", err)
	}
	if _, err := db.Exec(`UPDATE loan_requests SET legacy_terms=1 WHERE id='manager-snapshot'`); err == nil {
		t.Fatal("request loan identity remained mutable after snapshot")
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,proposed_approved_amount,proposed_duration_months,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation) VALUES ('bypass-snapshot','member-history',1000000,12,'bypass','approved','regular',NULL,1000000,12,'regular_tiered_monthly_v1',10000,120000,1120000)`); err == nil {
		t.Fatal("direct approved snapshot bypassed Manager-stage assignment")
	}
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,proposed_approved_amount,proposed_duration_months,proposed_admin_fee_policy,proposed_monthly_admin_fee,proposed_total_admin_fee,proposed_total_obligation) VALUES ('wrong-policy','member-history',1000000,12,'wrong policy','pending','regular','manager',1000000,12,'legacy_flat_monthly',NULL,120000,1120000)`); err == nil {
		t.Fatal("nonlegacy Regular request accepted legacy policy")
	}
}

func TestSQLiteMigrateSerializesConcurrentCallers(t *testing.T) {
	db := v14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; results <- Migrate(db) }()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent SQLite Migrate: %v", err)
		}
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version IN (15,16)`, 2)
}
