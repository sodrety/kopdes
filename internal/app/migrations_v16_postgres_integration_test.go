//go:build postgres_integration

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegularLoanAdminFeePostgresMigrationBackfillsLegacyTotal(t *testing.T) {
	db := postgresV14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	applyPostgresMigration15(t, db)

	var legacyTotal int64
	if err := db.QueryRow(`SELECT total_interest FROM loans WHERE id='loan-history'`).Scan(&legacyTotal); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("apply PostgreSQL migration 16: %v", err)
	}

	var policy string
	var monthlyAdminFee sql.NullInt64
	var totalAdminFee int64
	if err := db.QueryRow(`SELECT admin_fee_policy,monthly_admin_fee,total_admin_fee FROM loans WHERE id='loan-history'`).Scan(&policy, &monthlyAdminFee, &totalAdminFee); err != nil {
		t.Fatal(err)
	}
	if policy != "legacy_flat_monthly" || monthlyAdminFee.Valid || totalAdminFee != legacyTotal {
		t.Fatalf("legacy Admin Fee backfill policy=%q monthly=%v total=%d want total=%d", policy, monthlyAdminFee, totalAdminFee, legacyTotal)
	}
	for table, columns := range map[string][]string{
		"saving_records":          {"amount"},
		"withdrawal_requests":     {"amount"},
		"withdrawal_reservations": {"amount"},
		"loan_requests":           {"requested_amount", "proposed_approved_amount", "proposed_monthly_admin_fee", "proposed_total_admin_fee", "proposed_total_obligation"},
		"loans":                   {"approved_amount", "monthly_installment", "remaining_balance", "total_interest", "total_obligation", "monthly_admin_fee", "total_admin_fee"},
		"loan_installments":       {"scheduled_amount", "paid_amount"},
		"loan_repayments":         {"amount"},
	} {
		for _, column := range columns {
			var dataType string
			if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, table, column).Scan(&dataType); err != nil {
				t.Fatal(err)
			}
			if dataType != "bigint" {
				t.Fatalf("%s.%s type=%q want bigint", table, column, dataType)
			}
		}
	}
	for _, column := range []string{"admin_fee_policy", "monthly_admin_fee", "total_admin_fee"} {
		var defaultValue string
		if err := db.QueryRow(`SELECT COALESCE(column_default,'') FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='loans' AND column_name=$1`, column).Scan(&defaultValue); err != nil {
			t.Fatal(err)
		}
		if defaultValue != "" {
			t.Fatalf("loans.%s retained default %q", column, defaultValue)
		}
	}
	insertPostgresPendingSnapshottedLoanRequest(t, db, "request-overflow", 200000000, 24, 2875000, 69000000, 269000000)
	if _, err := db.Exec(`UPDATE loan_requests SET proposed_total_admin_fee=69000001 WHERE id='request-overflow'`); err == nil {
		t.Fatal("PostgreSQL allowed direct mutation of a proposed fee snapshot")
	}
	deletePostgresLoanRequestFixture(t, db, "request-overflow")
	insertPostgresOriginLoanRequest(t, db, "request-incoherent", 1000000, 12, "Tamper")
	insertPostgresLoanRequestApproval(t, db, "request-incoherent", "manager")
	if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_i',proposed_approved_amount=1000000,proposed_duration_months=12,proposed_start_date='2026-01-01',proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=10000,proposed_total_admin_fee=120001,proposed_total_obligation=1120001 WHERE id='request-incoherent'`); err == nil {
		t.Fatal("PostgreSQL allowed an arithmetically incoherent proposed fee snapshot")
	}
	deletePostgresLoanRequestFixture(t, db, "request-incoherent")
	for _, status := range []string{"rejected", "cancelled"} {
		requestID := "request-wrong-tier-" + status
		insertPostgresOriginLoanRequest(t, db, requestID, 30000000, 24, "Wrong policy")
		insertPostgresLoanRequestApproval(t, db, requestID, "manager")
		if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_i',proposed_approved_amount=30000000,proposed_duration_months=24,proposed_start_date='2026-01-01',proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=300000,proposed_total_admin_fee=7200000,proposed_total_obligation=37200000 WHERE id=$1`, requestID); err == nil {
			t.Fatalf("PostgreSQL accepted wrong Regular v1 formula for %s snapshot", status)
		}
		deletePostgresLoanRequestFixture(t, db, requestID)
	}
	for _, assignment := range []string{
		`admin_fee_policy=NULL`,
		`admin_fee_policy='unknown'`,
		`total_admin_fee=NULL`,
		`total_obligation=NULL`,
		`monthly_admin_fee=1`,
	} {
		if _, err := db.Exec(`UPDATE loans SET ` + assignment + ` WHERE id='loan-history'`); err == nil {
			t.Fatalf("PostgreSQL accepted invalid Loan snapshot update: %s", assignment)
		}
	}
	for _, assignment := range []string{
		`proposed_total_admin_fee=1`,
		`proposed_admin_fee_policy='unknown',proposed_total_admin_fee=1,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=NULL,proposed_total_admin_fee=1,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=1,proposed_total_admin_fee=NULL,proposed_total_obligation=2`,
		`proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=1,proposed_total_admin_fee=1,proposed_total_obligation=NULL`,
		`proposed_admin_fee_policy='legacy_flat_monthly',proposed_monthly_admin_fee=1,proposed_total_admin_fee=1,proposed_total_obligation=2`,
	} {
		if _, err := db.Exec(`UPDATE loan_requests SET ` + assignment + ` WHERE id='request-approved'`); err == nil {
			t.Fatalf("PostgreSQL accepted invalid proposed snapshot update: %s", assignment)
		}
	}

	const maxInt64 = int64(9223372036854775807)
	var savingTotal int64
	if err := db.QueryRow(`SELECT saving_records_total FROM monetary_aggregate_totals WHERE singleton=TRUE`).Scan(&savingTotal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ('pg-aggregate-fill','member-affected','deposit','sukarela',$1,'2026-07-15','','','user-officer')`, maxInt64-savingTotal-1); err != nil {
		t.Fatalf("fill PostgreSQL saving aggregate: %v", err)
	}
	db.SetMaxOpenConns(2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"pg-aggregate-race-a", "pg-aggregate-race-b"} {
		go func(id string) {
			<-start
			_, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ($1,'member-affected','deposit','sukarela',1,'2026-07-15','','','user-officer')`, id)
			results <- err
		}(id)
	}
	close(start)
	successes := 0
	capacityFailures := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if strings.Contains(err.Error(), "monetary aggregate capacity exceeded") {
			capacityFailures++
		} else {
			t.Fatalf("unexpected concurrent aggregate error: %v", err)
		}
	}
	if successes != 1 || capacityFailures != 1 {
		t.Fatalf("concurrent boundary writes: successes=%d capacity_failures=%d", successes, capacityFailures)
	}
	var persistedSum int64
	if err := db.QueryRow(`SELECT SUM(amount) FROM saving_records`).Scan(&persistedSum); err != nil || persistedSum != maxInt64 {
		t.Fatalf("PostgreSQL persisted sum=%d err=%v", persistedSum, err)
	}
	server := &Server{db: db}
	if summary, err := server.adminDashboardSummary(); err != nil || summary.TotalSavings <= 0 {
		t.Fatalf("PostgreSQL dashboard failed at boundary: summary=%+v err=%v", summary, err)
	}

	var requestTotal int64
	if err := db.QueryRow(`SELECT withdrawal_requests_total FROM monetary_aggregate_totals WHERE singleton=TRUE`).Scan(&requestTotal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO withdrawal_requests (id,member_id,amount,note,status,current_approval_stage) VALUES ('pg-aggregate-request','member-affected',$1,'boundary','pending','manager')`, maxInt64-requestTotal); err != nil {
		t.Fatalf("fill PostgreSQL withdrawal request aggregate: %v", err)
	}
	if _, err := db.Exec(`UPDATE withdrawal_requests SET amount=amount+1 WHERE id='pg-aggregate-request'`); err == nil {
		t.Fatal("PostgreSQL allowed withdrawal request aggregate beyond MaxInt64")
	}
	var reservationTotal int64
	if err := db.QueryRow(`SELECT withdrawal_reservations_total FROM monetary_aggregate_totals WHERE singleton=TRUE`).Scan(&reservationTotal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO withdrawal_reservations (id,request_id,member_id,amount,status) VALUES ('pg-aggregate-reservation','pg-aggregate-request','member-affected',$1,'active')`, maxInt64-reservationTotal); err != nil {
		t.Fatalf("fill PostgreSQL withdrawal reservation aggregate: %v", err)
	}
	if _, err := db.Exec(`UPDATE withdrawal_reservations SET amount=amount+1 WHERE id='pg-aggregate-reservation'`); err == nil {
		t.Fatal("PostgreSQL allowed withdrawal reservation aggregate beyond MaxInt64")
	}
	if amount, err := server.pendingWithdrawalAmount(); err != nil || amount < 0 {
		t.Fatalf("PostgreSQL pending withdrawal report failed at boundary: amount=%d err=%v", amount, err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 1)
}

func TestStandalonePostgresMigration16RollsBackAndReruns(t *testing.T) {
	db := postgresV14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	applyPostgresMigration15(t, db)
	if _, err := db.Exec(`ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_check CHECK (approved_amount > 0)`); err != nil {
		t.Fatalf("create deliberate v16 constraint collision: %v", err)
	}
	if _, err := db.Exec(postgresMigration16SQL(t)); err == nil {
		t.Fatal("standalone migration 16 should fail on deliberate constraint collision")
	}
	// A multi-statement file can leave the session in an aborted explicit
	// transaction after an error. Roll it back before inspecting the schema.
	_, _ = db.Exec(`ROLLBACK`)
	assertRowCount(t, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='loans' AND column_name='admin_fee_policy'`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 0)
	if _, err := db.Exec(`ALTER TABLE loans DROP CONSTRAINT loans_admin_fee_policy_check`); err != nil {
		t.Fatalf("drop deliberate v16 constraint collision: %v", err)
	}
	if _, err := db.Exec(postgresMigration16SQL(t)); err != nil {
		t.Fatalf("rerun standalone migration 16 after rollback: %v", err)
	}
	assertPostgresV16PolicyAndAggregateConstraints(t, db, "standalone")
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 1)
}

func TestSupabaseMigration16TracksVersionBeforeApplicationMigrate(t *testing.T) {
	db := postgresV14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	applyPostgresMigration15(t, db)
	contents, err := os.ReadFile(filepath.Join("..", "..", "supabase", "migrations", "20260715084236_add_regular_loan_admin_fee_terms.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// Supabase deploys in public; the integration harness isolates each test in
	// its own schema, so redirect qualified objects while preserving semantics.
	var qualifiedSchema string
	if err := db.QueryRow(`SELECT quote_ident(current_schema())`).Scan(&qualifiedSchema); err != nil {
		t.Fatalf("read current PostgreSQL test schema: %v", err)
	}
	sqlText := strings.ReplaceAll(string(contents), "public.", qualifiedSchema+".")
	sqlText = strings.ReplaceAll(sqlText, "private.", "")
	sqlText = strings.ReplaceAll(sqlText, "create schema if not exists private;", "")
	sqlText = strings.ReplaceAll(sqlText, "revoke all on schema private from anon, authenticated;", "")
	sqlText = strings.ReplaceAll(sqlText, ", 'private');", ", '');")
	sqlText = strings.ReplaceAll(sqlText, "revoke all on table monetary_aggregate_totals from anon, authenticated;", "")
	sqlText = strings.ReplaceAll(sqlText, "revoke all on function maintain_monetary_aggregate_total() from public, anon, authenticated;", "revoke all on function maintain_monetary_aggregate_total() from public;")
	if _, err := db.Exec(sqlText); err != nil {
		t.Fatalf("apply Supabase migration 16: %v", err)
	}
	assertPostgresV16PolicyAndAggregateConstraints(t, db, "supabase")
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 1)
	if err := Migrate(db); err != nil {
		t.Fatalf("application Migrate duplicated Supabase migration 16: %v", err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=16`, 1)
}

func TestPostgresMigrateSerializesConcurrentApplicationInstances(t *testing.T) {
	db := postgresV14TestDatabase(t)
	seedLegacyLoanMigrationData(t, db)
	db.SetMaxOpenConns(4)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- Migrate(db)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent application Migrate: %v", err)
		}
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version IN (15,16)`, 2)
	assertRowCount(t, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='loans' AND column_name='admin_fee_policy'`, 1)
}

func assertPostgresV16PolicyAndAggregateConstraints(t *testing.T, db *sql.DB, prefix string) {
	t.Helper()
	insertPostgresOriginLoanRequest(t, db, prefix+"-wrong-tier", 30000000, 24, "Wrong policy")
	insertPostgresLoanRequestApproval(t, db, prefix+"-wrong-tier", "manager")
	if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_i',proposed_approved_amount=30000000,proposed_duration_months=24,proposed_start_date='2026-01-01',proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=300000,proposed_total_admin_fee=7200000,proposed_total_obligation=37200000 WHERE id=$1`, prefix+"-wrong-tier"); err == nil {
		t.Fatalf("%s migration accepted wrong Regular v1 formula", prefix)
	}
	deletePostgresLoanRequestFixture(t, db, prefix+"-wrong-tier")
	requestID := prefix + "-correct-tier"
	insertPostgresApprovedLoanRequest(t, db, requestID, 30000000, 24, 325000, 7800000, 37800000)
	loanSQL := `INSERT INTO loans (id,loan_request_id,member_id,loan_type,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date,admin_fee_policy,monthly_admin_fee,total_admin_fee) VALUES ($1,$2,'member-affected','regular',30000000,24,$3,$4,'cancelled','user-officer','2026-01-01',100,$5,$6,'2026-02-01','2027-12-01','regular_tiered_monthly_v1',$7,$8)`
	if _, err := db.Exec(loanSQL, prefix+"-wrong-tier-loan", requestID, 1550000, 37200000, 7200000, 37200000, 300000, 7200000); err == nil {
		t.Fatalf("%s migration accepted wrong current Loan formula", prefix)
	}
	if _, err := db.Exec(loanSQL, prefix+"-correct-tier-loan", requestID, 1575000, 37800000, 7800000, 37800000, 325000, 7800000); err != nil {
		t.Fatalf("%s migration rejected exact current Loan snapshot: %v", prefix, err)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM monetary_aggregate_totals`, 1)
	var total int64
	if err := db.QueryRow(`SELECT saving_records_total FROM monetary_aggregate_totals WHERE singleton=TRUE`).Scan(&total); err != nil {
		t.Fatalf("%s aggregate ledger: %v", prefix, err)
	}
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ($1,'member-affected','deposit','sukarela',$2,'2026-07-15','','','user-officer')`, prefix+"-aggregate-max", int64(9223372036854775807)-total); err != nil {
		t.Fatalf("%s fill aggregate ledger: %v", prefix, err)
	}
	if _, err := db.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by) VALUES ($1,'member-affected','deposit','sukarela',1,'2026-07-15','','','user-officer')`, prefix+"-aggregate-over"); err == nil {
		t.Fatalf("%s migration allowed aggregate beyond MaxInt64", prefix)
	}
}

func insertPostgresOriginLoanRequest(t *testing.T, db *sql.DB, requestID string, amount int64, duration int, purpose string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage) VALUES ($1,'member-affected',$2,$3,$4,'pending','regular','manager')`, requestID, amount, duration, purpose); err != nil {
		t.Fatalf("insert origin Loan Request %s: %v", requestID, err)
	}
}

func deletePostgresLoanRequestFixture(t *testing.T, db *sql.DB, requestID string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM loan_request_approvals WHERE request_id=$1`, requestID); err != nil {
		t.Fatalf("delete approvals for %s: %v", requestID, err)
	}
	if _, err := db.Exec(`DELETE FROM loan_requests WHERE id=$1`, requestID); err != nil {
		t.Fatalf("delete Loan Request %s: %v", requestID, err)
	}
}

func insertPostgresLoanRequestApproval(t *testing.T, db *sql.DB, requestID, stage string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO loan_request_approvals (id,request_id,stage,decision,officer_id,officer_name,officer_role) VALUES ($1,$2,$3,'approved','user-officer','Officer Member',$3)`, "approval-"+requestID+"-"+stage, requestID, stage); err != nil {
		t.Fatalf("insert %s approval for %s: %v", stage, requestID, err)
	}
}

func insertPostgresPendingSnapshottedLoanRequest(t *testing.T, db *sql.DB, requestID string, amount int64, duration int, monthlyFee, totalFee, obligation int64) {
	t.Helper()
	insertPostgresOriginLoanRequest(t, db, requestID, amount, duration, "Correct policy")
	insertPostgresLoanRequestApproval(t, db, requestID, "manager")
	if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_i',proposed_approved_amount=$2,proposed_duration_months=$3,proposed_start_date='2026-01-01',proposed_admin_fee_policy='regular_tiered_monthly_v1',proposed_monthly_admin_fee=$4,proposed_total_admin_fee=$5,proposed_total_obligation=$6 WHERE id=$1`, requestID, amount, duration, monthlyFee, totalFee, obligation); err != nil {
		t.Fatalf("snapshot Manager proposal for %s: %v", requestID, err)
	}
}

func insertPostgresApprovedLoanRequest(t *testing.T, db *sql.DB, requestID string, amount int64, duration int, monthlyFee, totalFee, obligation int64) {
	t.Helper()
	insertPostgresPendingSnapshottedLoanRequest(t, db, requestID, amount, duration, monthlyFee, totalFee, obligation)
	insertPostgresLoanRequestApproval(t, db, requestID, "ketua_i")
	if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_ii' WHERE id=$1`, requestID); err != nil {
		t.Fatalf("advance %s to Ketua II: %v", requestID, err)
	}
	insertPostgresLoanRequestApproval(t, db, requestID, "ketua_ii")
	if _, err := db.Exec(`UPDATE loan_requests SET current_approval_stage='ketua_utama' WHERE id=$1`, requestID); err != nil {
		t.Fatalf("advance %s to Ketua Utama: %v", requestID, err)
	}
	insertPostgresLoanRequestApproval(t, db, requestID, "ketua_utama")
	if _, err := db.Exec(`UPDATE loan_requests SET status='approved',current_approval_stage=NULL WHERE id=$1`, requestID); err != nil {
		t.Fatalf("approve %s: %v", requestID, err)
	}
}

func postgresMigration16SQL(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "016_add_regular_loan_admin_fee_terms.sql"))
	if err != nil {
		t.Fatalf("read PostgreSQL migration 16: %v", err)
	}
	return string(contents)
}

func TestPostgresRegularLoanApplicationEndToEndWithBIGINTObligation(t *testing.T) {
	db := postgresV14TestDatabase(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate PostgreSQL application database: %v", err)
	}
	if err := EnsureAdminUser(db, "manager-pg@coop.test", "password"); err != nil {
		t.Fatalf("seed PostgreSQL Manager: %v", err)
	}

	seedIdentity := func(id, number, email, role string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ($1,$2,$3,$4,'active')`, id, number, number, "2026-07-15"); err != nil {
			t.Fatalf("seed %s Member: %v", role, err)
		}
		if _, err := CreateMemberUser(db, email, "password", id); err != nil {
			t.Fatalf("seed %s User: %v", role, err)
		}
		// These fixture accounts are established test identities. Production-created
		// member accounts intentionally require a password change on first login.
		if _, err := db.Exec(`UPDATE users SET must_change_password=FALSE WHERE member_id=$1`, id); err != nil {
			t.Fatalf("establish %s User password: %v", role, err)
		}
		if role != "" {
			if _, err := db.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,$3,TRUE)`, "appointment-"+id, id, role); err != nil {
				t.Fatalf("seed %s appointment: %v", role, err)
			}
		}
	}
	seedIdentity("pg-ketua-i", "PG-KI", "ketua-i-pg@coop.test", "ketua_i")
	seedIdentity("pg-ketua-ii", "PG-KII", "ketua-ii-pg@coop.test", "ketua_ii")
	seedIdentity("pg-ketua-utama", "PG-KU", "ketua-utama-pg@coop.test", "ketua_utama")
	seedIdentity("pg-borrower", "PG-BORROWER", "borrower-pg@coop.test", "")

	cfg := Config{JWTSecret: "0123456789abcdef0123456789abcdef"}
	handler := NewServer(cfg, db)
	requestOn := func(target http.Handler, method, path, token, contentType, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		target.ServeHTTP(recorder, req)
		return recorder
	}
	request := func(method, path, token, contentType, body string) *httptest.ResponseRecorder {
		return requestOn(handler, method, path, token, contentType, body)
	}
	login := func(email string) string {
		t.Helper()
		recorder := request(http.MethodPost, "/api/auth/login", "", "application/json", `{"email":"`+email+`","password":"password"}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", email, recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Token == "" {
			t.Fatalf("decode login %s: %v %s", email, err, recorder.Body.String())
		}
		return payload.Token
	}

	memberToken := login("borrower-pg@coop.test")
	grouped := url.Values{
		"loan_type": {"regular"}, "requested_amount": {"Rp 200.000.000"},
		"duration_months": {"24"}, "purpose": {"PostgreSQL BIGINT end-to-end"},
	}
	submitted := request(http.MethodPost, "/api/member/loan-requests", memberToken, "application/x-www-form-urlencoded", grouped.Encode())
	if submitted.Code != http.StatusSeeOther {
		t.Fatalf("submit grouped PostgreSQL Loan Request: %d %s", submitted.Code, submitted.Body.String())
	}
	var requestID string
	var requestedAmount int64
	if err := db.QueryRow(`SELECT id,requested_amount FROM loan_requests WHERE member_id='pg-borrower' AND status='pending'`).Scan(&requestID, &requestedAmount); err != nil || requestedAmount != 200_000_000 {
		t.Fatalf("grouped PostgreSQL amount=%d err=%v", requestedAmount, err)
	}

	startDate := time.Now().In(jakartaLocation).Format("2006-01-02")
	managerPayload := fmt.Sprintf(`{"approved_amount":200000000,"duration_months":24,"start_date":%q}`, startDate)
	approveConcurrently := func(email, payload string) {
		t.Helper()
		token := login(email)
		handlers := []http.Handler{handler, NewServer(cfg, db)}
		results := make(chan int, 2)
		for _, target := range handlers {
			go func(target http.Handler) {
				results <- requestOn(target, http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", token, "application/json", payload).Code
			}(target)
		}
		codes := []int{<-results, <-results}
		successes := 0
		for _, code := range codes {
			if code == http.StatusOK {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent %s approvals codes=%v want one success", email, codes)
		}
	}
	approveConcurrently("manager-pg@coop.test", managerPayload)
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='`+requestID+`' AND stage='manager'`, 1)
	for index, stage := range []struct{ email, payload string }{{"ketua-i-pg@coop.test", `{}`}, {"ketua-ii-pg@coop.test", `{}`}} {
		approved := request(http.MethodPost, "/api/admin/loan-requests/"+requestID+"/approve", login(stage.email), "application/json", stage.payload)
		if approved.Code != http.StatusOK {
			t.Fatalf("PostgreSQL approval stage %d: %d %s", index, approved.Code, approved.Body.String())
		}
	}
	approveConcurrently("ketua-utama-pg@coop.test", `{}`)
	assertRowCount(t, db, `SELECT COUNT(*) FROM loan_request_approvals WHERE request_id='`+requestID+`' AND stage='ketua_utama'`, 1)
	assertRowCount(t, db, `SELECT COUNT(*) FROM loans WHERE loan_request_id='`+requestID+`'`, 1)

	var loanID, policy string
	var approvedAmount, monthlyFee, totalFee, obligation, balance int64
	if err := db.QueryRow(`SELECT id,approved_amount,admin_fee_policy,monthly_admin_fee,total_admin_fee,total_obligation,remaining_balance FROM loans WHERE loan_request_id=$1`, requestID).Scan(&loanID, &approvedAmount, &policy, &monthlyFee, &totalFee, &obligation, &balance); err != nil {
		t.Fatalf("read final PostgreSQL Loan: %v", err)
	}
	if approvedAmount != 200_000_000 || policy != regularLoanAdminFeePolicy || monthlyFee != 2_875_000 || totalFee != 69_000_000 || obligation != 269_000_000 || balance != obligation {
		t.Fatalf("unexpected PostgreSQL BIGINT terms amount=%d policy=%s monthly=%d total=%d obligation=%d balance=%d", approvedAmount, policy, monthlyFee, totalFee, obligation, balance)
	}
	var installmentCount int
	var installmentTotal int64
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(scheduled_amount),0) FROM loan_installments WHERE loan_id=$1`, loanID).Scan(&installmentCount, &installmentTotal); err != nil || installmentCount != 24 || installmentTotal != obligation {
		t.Fatalf("PostgreSQL installments count=%d total=%d err=%v", installmentCount, installmentTotal, err)
	}

	repayment := url.Values{"amount": {"Rp 100.000.000"}, "record_date": {startDate}}
	repaid := request(http.MethodPost, "/api/admin/loans/"+loanID+"/repayments", login("manager-pg@coop.test"), "application/x-www-form-urlencoded", repayment.Encode())
	if repaid.Code != http.StatusSeeOther {
		t.Fatalf("record grouped PostgreSQL repayment: %d %s", repaid.Code, repaid.Body.String())
	}
	var repaymentAmount, remaining int64
	if err := db.QueryRow(`SELECT amount FROM loan_repayments WHERE loan_id=$1`, loanID).Scan(&repaymentAmount); err != nil {
		t.Fatalf("read PostgreSQL repayment: %v", err)
	}
	if err := db.QueryRow(`SELECT remaining_balance FROM loans WHERE id=$1`, loanID).Scan(&remaining); err != nil {
		t.Fatalf("read PostgreSQL balance: %v", err)
	}
	if repaymentAmount != 100_000_000 || remaining != 169_000_000 {
		t.Fatalf("PostgreSQL repayment=%d remaining=%d", repaymentAmount, remaining)
	}

	deposit := url.Values{"member_id": {"pg-borrower"}, "type": {"deposit"}, "category": {"sukarela"}, "amount": {"Rp 4.000.000.000"}, "record_date": {startDate}}
	deposited := request(http.MethodPost, "/api/admin/savings", login("manager-pg@coop.test"), "application/x-www-form-urlencoded", deposit.Encode())
	if deposited.Code != http.StatusSeeOther {
		t.Fatalf("record PostgreSQL BIGINT saving: %d %s", deposited.Code, deposited.Body.String())
	}
	withdrawal := url.Values{"amount": {"Rp 3.000.000.000"}, "note": {"PostgreSQL BIGINT withdrawal"}}
	withdrawn := request(http.MethodPost, "/api/member/withdrawal-requests", memberToken, "application/x-www-form-urlencoded", withdrawal.Encode())
	if withdrawn.Code != http.StatusSeeOther {
		t.Fatalf("submit PostgreSQL BIGINT withdrawal: %d %s", withdrawn.Code, withdrawn.Body.String())
	}
	var savingAmount, withdrawalAmount, reservationAmount int64
	if err := db.QueryRow(`SELECT amount FROM saving_records WHERE member_id='pg-borrower' AND type='deposit'`).Scan(&savingAmount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT wr.amount,r.amount FROM withdrawal_requests wr JOIN withdrawal_reservations r ON r.request_id=wr.id WHERE wr.member_id='pg-borrower'`).Scan(&withdrawalAmount, &reservationAmount); err != nil {
		t.Fatal(err)
	}
	if savingAmount != 4_000_000_000 || withdrawalAmount != 3_000_000_000 || reservationAmount != withdrawalAmount {
		t.Fatalf("PostgreSQL BIGINT saving=%d withdrawal=%d reservation=%d", savingAmount, withdrawalAmount, reservationAmount)
	}
}
