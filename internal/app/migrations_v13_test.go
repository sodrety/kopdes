package app

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMemberBackedOfficerMigrationKeepsCanonicalMemberIdentity(t *testing.T) {
	db := v12TestDatabase(t)
	for _, statement := range []string{
		`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('member-manager','M-013','Canonical Manager','2026-07-14','active')`,
		`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active) VALUES ('legacy-manager','legacy-manager@coop.test','legacy-hash','manager',NULL,'Legacy Manager',TRUE)`,
		`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active) VALUES ('member-login','member-manager@coop.test','member-hash','member','member-manager','Canonical Manager',TRUE)`,
		`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,status,current_approval_stage) VALUES ('request-13','member-manager',100000,6,'pending','manager')`,
		`INSERT INTO loan_request_approvals (id,request_id,stage,decision,officer_id,officer_name,officer_role) VALUES ('approval-13','request-13','manager','approved','legacy-manager','Legacy Manager','manager')`,
		`INSERT INTO officer_audit_events (id,actor_id,actor_name,target_id,target_name,action,new_role,new_active) VALUES ('audit-13','legacy-manager','Legacy Manager','legacy-manager','Legacy Manager','created','manager',TRUE)`,
		`INSERT INTO notification_events (id,event_type) VALUES ('event-13','legacy')`,
		`INSERT INTO notifications (id,event_id,user_id,title_key,body_key,link) VALUES ('notice-13','event-13','legacy-manager','title','body','/admin/dashboard')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed v12 database: %v", err)
		}
	}
	if err := PrepareLegacyOfficerMappings(db, map[string]string{"legacy-manager@coop.test": "member-manager"}); err != nil {
		t.Fatalf("prepare mapping: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate to v13: %v", err)
	}

	var appointmentMemberID, appointmentRole string
	var appointmentActive bool
	if err := db.QueryRow(`SELECT member_id,role,active FROM officer_appointments`).Scan(&appointmentMemberID, &appointmentRole, &appointmentActive); err != nil {
		t.Fatalf("read appointment: %v", err)
	}
	if appointmentMemberID != "member-manager" || appointmentRole != "manager" || !appointmentActive {
		t.Fatalf("unexpected appointment member=%q role=%q active=%v", appointmentMemberID, appointmentRole, appointmentActive)
	}

	var legacyActive, historical bool
	var legacyRole, legacyPassword string
	var legacyMemberID sql.NullString
	if err := db.QueryRow(`SELECT role,member_id,active,historical_identity,password_hash FROM users WHERE id='legacy-manager'`).Scan(&legacyRole, &legacyMemberID, &legacyActive, &historical, &legacyPassword); err != nil {
		t.Fatalf("read historical identity: %v", err)
	}
	if legacyRole != "member" || legacyMemberID.Valid || legacyActive || !historical || legacyPassword != "!historical!" {
		t.Fatalf("legacy identity still usable: role=%q member=%v active=%v historical=%v password=%q", legacyRole, legacyMemberID, legacyActive, historical, legacyPassword)
	}

	var canonicalHash, noticeUser, noticeAudience, snapshotMemberID, snapshotMemberNo string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id='member-login'`).Scan(&canonicalHash); err != nil {
		t.Fatalf("read canonical login: %v", err)
	}
	if err := db.QueryRow(`SELECT user_id,audience FROM notifications WHERE id='notice-13'`).Scan(&noticeUser, &noticeAudience); err != nil {
		t.Fatalf("read migrated notice: %v", err)
	}
	if err := db.QueryRow(`SELECT officer_member_id,officer_member_no FROM loan_request_approvals WHERE id='approval-13'`).Scan(&snapshotMemberID, &snapshotMemberNo); err != nil {
		t.Fatalf("read approval snapshot: %v", err)
	}
	if canonicalHash != "member-hash" || noticeUser != "member-login" || noticeAudience != "officer" || snapshotMemberID != "member-manager" || snapshotMemberNo != "M-013" {
		t.Fatalf("migration did not preserve canonical state: hash=%q notice=%q/%q snapshot=%q/%q", canonicalHash, noticeUser, noticeAudience, snapshotMemberID, snapshotMemberNo)
	}
}

func TestMemberBackedOfficerMigrationRequiresExplicitLegacyMapping(t *testing.T) {
	db := v12TestDatabase(t)
	if _, err := db.Exec(`INSERT INTO users (id,email,password_hash,role,full_name,active) VALUES ('legacy-manager','legacy-manager@coop.test','hash','manager','Legacy Manager',TRUE)`); err != nil {
		t.Fatalf("seed legacy Officer: %v", err)
	}

	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "requires an explicit Member mapping") {
		t.Fatalf("expected explicit mapping failure, got %v", err)
	}
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=13`).Scan(&applied); err != nil {
		t.Fatalf("check migration version: %v", err)
	}
	if applied != 0 {
		t.Fatal("failed migration 13 must not be recorded")
	}
}

func TestEmptyLegacyOfficerMappingAllowsFreshDatabaseBootstrap(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := PrepareLegacyOfficerMappings(db, nil); err != nil {
		t.Fatalf("empty mapping should be a no-op: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate fresh database: %v", err)
	}
}

func v12TestDatabase(t *testing.T) *sql.DB {
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
	for _, migration := range migrations[:12] {
		if err := applyMigration(db, migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
	return db
}
