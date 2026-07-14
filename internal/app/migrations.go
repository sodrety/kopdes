package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type migration struct {
	Version    int
	Name       string
	Statements []string
}

var migrations = []migration{
	{
		Version: 1,
		Name:    "create_users",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS users (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
				member_id TEXT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
	},
	{
		Version: 2,
		Name:    "create_members",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS members (
				id TEXT PRIMARY KEY,
				member_no TEXT NOT NULL UNIQUE,
				full_name TEXT NOT NULL,
				phone TEXT NOT NULL DEFAULT '',
				address TEXT NOT NULL DEFAULT '',
				join_date TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'suspended')),
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
	},
	{
		Version: 3,
		Name:    "create_saving_records",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS saving_records (
				id TEXT PRIMARY KEY,
				member_id TEXT NOT NULL,
				type TEXT NOT NULL CHECK (type IN ('deposit', 'withdrawal')),
				amount INTEGER NOT NULL CHECK (amount > 0),
				record_date TEXT NOT NULL,
				reference_no TEXT NOT NULL DEFAULT '',
				note TEXT NOT NULL DEFAULT '',
				recorded_by TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (member_id) REFERENCES members(id),
				FOREIGN KEY (recorded_by) REFERENCES users(id)
			)`,
		},
	},
	{
		Version: 4,
		Name:    "create_loan_requests",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS loan_requests (
				id TEXT PRIMARY KEY,
				member_id TEXT NOT NULL,
				requested_amount INTEGER NOT NULL CHECK (requested_amount > 0),
				duration_months INTEGER NOT NULL CHECK (duration_months > 0),
				purpose TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
				reviewed_by TEXT NULL,
				reviewed_at TIMESTAMP NULL,
				rejection_reason TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (member_id) REFERENCES members(id),
				FOREIGN KEY (reviewed_by) REFERENCES users(id)
			)`,
		},
	},
	{
		Version: 5,
		Name:    "create_loans",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS loans (
				id TEXT PRIMARY KEY,
				loan_request_id TEXT NOT NULL UNIQUE,
				member_id TEXT NOT NULL,
				approved_amount INTEGER NOT NULL CHECK (approved_amount > 0),
				duration_months INTEGER NOT NULL CHECK (duration_months > 0),
				monthly_installment INTEGER NOT NULL CHECK (monthly_installment > 0),
				remaining_balance INTEGER NOT NULL CHECK (remaining_balance >= 0),
				status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paid', 'cancelled')),
				approved_by TEXT NOT NULL,
				approved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (loan_request_id) REFERENCES loan_requests(id),
				FOREIGN KEY (member_id) REFERENCES members(id),
				FOREIGN KEY (approved_by) REFERENCES users(id)
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_loans_one_active_per_member ON loans(member_id) WHERE status = 'active'`,
		},
	},
	{
		Version: 6,
		Name:    "create_loan_repayments",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS loan_repayments (
				id TEXT PRIMARY KEY,
				loan_id TEXT NOT NULL,
				member_id TEXT NOT NULL,
				amount INTEGER NOT NULL CHECK (amount > 0),
				record_date TEXT NOT NULL,
				reference_no TEXT NOT NULL DEFAULT '',
				note TEXT NOT NULL DEFAULT '',
				recorded_by TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (loan_id) REFERENCES loans(id),
				FOREIGN KEY (member_id) REFERENCES members(id),
				FOREIGN KEY (recorded_by) REFERENCES users(id)
			)`,
		},
	},
	{
		Version: 7,
		Name:    "add_saving_record_categories",
		Statements: []string{
			`ALTER TABLE saving_records
			 ADD COLUMN category TEXT NOT NULL DEFAULT 'sukarela' CHECK (category IN ('pokok', 'wajib', 'sukarela'))`,
		},
	},
	{
		Version: 8,
		Name:    "create_withdrawal_requests",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS withdrawal_requests (
				id TEXT PRIMARY KEY,
				member_id TEXT NOT NULL,
				amount INTEGER NOT NULL CHECK (amount > 0),
				note TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
				reviewed_by TEXT NULL,
				reviewed_at TIMESTAMP NULL,
				rejection_reason TEXT NOT NULL DEFAULT '',
				saving_record_id TEXT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (member_id) REFERENCES members(id),
				FOREIGN KEY (reviewed_by) REFERENCES users(id),
				FOREIGN KEY (saving_record_id) REFERENCES saving_records(id)
			)`,
		},
	},
	{
		Version: 9,
		Name:    "add_loan_schedules_and_deadlines",
	},
	{
		Version: 10,
		Name:    "enforce_one_pending_loan_request_per_member",
		Statements: []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_loan_requests_one_pending_per_member ON loan_requests(member_id) WHERE status = 'pending'`,
		},
	},
	{
		Version: 11,
		Name:    "enforce_maximum_loan_tenor",
	},
	{
		Version: 12,
		Name:    "add_officer_hierarchy_and_approvals",
	},
	{
		Version: 13,
		Name:    "add_member_backed_officer_appointments",
	},
	{
		Version: 14,
		Name:    "lock_officer_trigger_function_search_path",
	},
}

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, migration := range migrations {
		applied, err := migrationApplied(db, migration.Version)
		if err != nil {
			return fmt.Errorf("check migration %03d %s: %w", migration.Version, migration.Name, err)
		}
		if applied {
			continue
		}
		if err := applyMigration(db, migration); err != nil {
			return fmt.Errorf("apply migration %03d %s: %w", migration.Version, migration.Name, err)
		}
	}
	return nil
}

func migrationApplied(db *sql.DB, version int) (bool, error) {
	var storedVersion int
	err := db.QueryRow(`SELECT version FROM schema_migrations WHERE version = $1`, version).Scan(&storedVersion)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func applyMigration(db *sql.DB, migration migration) error {
	isSQLite := strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite")
	if isSQLite && (migration.Version == 9 || migration.Version == 12) {
		conn, err := db.Conn(context.Background())
		if err != nil {
			return err
		}
		defer conn.Close()
		var priorForeignKeys int
		if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&priorForeignKeys); err != nil {
			return fmt.Errorf("read sqlite foreign keys: %w", err)
		}
		if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		migrationErr := applyMigrationOnTx(func() (*sql.Tx, error) {
			return conn.BeginTx(context.Background(), nil)
		}, migration, true)
		restoreSetting := "OFF"
		if priorForeignKeys != 0 {
			restoreSetting = "ON"
		}
		_, restoreErr := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = `+restoreSetting)
		if migrationErr != nil {
			if restoreErr != nil {
				return fmt.Errorf("%v; restore sqlite foreign keys: %w", migrationErr, restoreErr)
			}
			return migrationErr
		}
		if restoreErr != nil {
			return fmt.Errorf("restore sqlite foreign keys: %w", restoreErr)
		}
		return nil
	}
	return applyMigrationOnTx(db.Begin, migration, isSQLite)
}

func applyMigrationOnTx(begin func() (*sql.Tx, error), migration migration, isSQLite bool) error {
	tx, err := begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if migration.Version == 10 {
		// Preserve the oldest pending request per member. Later duplicates are
		// deterministically rejected before the unique index is installed.
		if _, err := tx.Exec(`UPDATE loan_requests SET status = 'rejected', updated_at = CURRENT_TIMESTAMP
			WHERE id IN (SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY created_at, id) AS sequence FROM loan_requests WHERE status = 'pending') ranked WHERE sequence > 1)`); err != nil {
			return err
		}
	}
	for _, statement := range migration.Statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if migration.Version == 9 {
		if err := applyLoanScheduleMigration(tx, isSQLite); err != nil {
			return err
		}
	}
	if migration.Version == 11 {
		if err := enforceMaximumLoanTenor(tx, isSQLite); err != nil {
			return err
		}
	}
	if migration.Version == 12 {
		if err := addOfficerHierarchyAndApprovals(tx, isSQLite); err != nil {
			return err
		}
	}
	if migration.Version == 13 {
		if err := addMemberBackedOfficerAppointments(tx, isSQLite); err != nil {
			return err
		}
	}
	if migration.Version == 14 && !isSQLite {
		if err := lockOfficerTriggerFunctionSearchPath(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
		migration.Version,
		migration.Name,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// PrepareLegacyOfficerMappings stores the explicit one-time mapping required
// before migration 13 can convert standalone Officer users into appointments.
// Keys may be legacy user IDs or email addresses; values are existing Member IDs.
func PrepareLegacyOfficerMappings(db *sql.DB, mappings map[string]string) error {
	if len(mappings) == 0 {
		return nil
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS legacy_officer_member_mappings (
		legacy_user_id TEXT PRIMARY KEY,
		member_id TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (legacy_user_id) REFERENCES users(id),
		FOREIGN KEY (member_id) REFERENCES members(id)
	)`); err != nil {
		return fmt.Errorf("create legacy Officer mappings: %w", err)
	}
	isSQLite := strings.Contains(strings.ToLower(fmt.Sprintf("%T", db.Driver())), "sqlite")
	if !isSQLite {
		if _, err := db.Exec(`ALTER TABLE legacy_officer_member_mappings ENABLE ROW LEVEL SECURITY`); err != nil {
			return fmt.Errorf("secure legacy Officer mappings: %w", err)
		}
	}
	for key, memberID := range mappings {
		key = strings.TrimSpace(key)
		memberID = strings.TrimSpace(memberID)
		if key == "" || memberID == "" {
			return fmt.Errorf("legacy Officer mappings must use non-empty user/email and Member IDs")
		}
		var userID string
		if err := db.QueryRow(`SELECT id FROM users WHERE id=$1 OR LOWER(email)=LOWER($1)`, key).Scan(&userID); err != nil {
			return fmt.Errorf("resolve legacy Officer %q: %w", key, err)
		}
		if _, err := db.Exec(`INSERT INTO legacy_officer_member_mappings (legacy_user_id,member_id) VALUES ($1,$2)
			ON CONFLICT(legacy_user_id) DO UPDATE SET member_id=excluded.member_id`, userID, memberID); err != nil {
			return fmt.Errorf("store legacy Officer mapping for %q: %w", key, err)
		}
	}
	return nil
}

func addMemberBackedOfficerAppointments(tx *sql.Tx, isSQLite bool) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS legacy_officer_member_mappings (
			legacy_user_id TEXT PRIMARY KEY,
			member_id TEXT NOT NULL UNIQUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (legacy_user_id) REFERENCES users(id),
			FOREIGN KEY (member_id) REFERENCES members(id)
		)`,
		`ALTER TABLE users ADD COLUMN historical_identity BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE TABLE officer_appointments (
			id TEXT PRIMARY KEY,
			member_id TEXT NOT NULL UNIQUE,
			role TEXT NOT NULL CHECK (role IN ('manager','ketua_i','ketua_ii','ketua_utama')),
			active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (member_id) REFERENCES members(id)
		)`,
		`CREATE INDEX idx_officer_appointments_role_active ON officer_appointments(role,active)`,
		`ALTER TABLE loan_request_approvals ADD COLUMN officer_member_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE loan_request_approvals ADD COLUMN officer_member_no TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE withdrawal_request_approvals ADD COLUMN officer_member_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE withdrawal_request_approvals ADD COLUMN officer_member_no TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE officer_audit_events ADD COLUMN actor_member_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE officer_audit_events ADD COLUMN actor_member_no TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE officer_audit_events ADD COLUMN target_member_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE officer_audit_events ADD COLUMN target_member_no TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE officer_audit_events ADD COLUMN target_appointment_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notifications ADD COLUMN audience TEXT NOT NULL DEFAULT 'officer' CHECK (audience IN ('member','officer'))`,
		`UPDATE notifications SET audience=CASE WHEN link LIKE '/member/%' THEN 'member' ELSE 'officer' END`,
		`CREATE INDEX idx_notifications_user_audience_state ON notifications(user_id,audience,resolved_at,is_read,created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}

	rows, err := tx.Query(`SELECT id,role FROM users WHERE role <> 'member' AND historical_identity=FALSE ORDER BY id`)
	if err != nil {
		return err
	}
	type legacyOfficer struct {
		ID   string
		Role string
	}
	var legacy []legacyOfficer
	for rows.Next() {
		var officer legacyOfficer
		if err := rows.Scan(&officer.ID, &officer.Role); err != nil {
			_ = rows.Close()
			return err
		}
		legacy = append(legacy, officer)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, officer := range legacy {
		memberID := ""
		err := tx.QueryRow(`SELECT member_id FROM legacy_officer_member_mappings WHERE legacy_user_id=$1`, officer.ID).Scan(&memberID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("legacy Officer user %s requires an explicit Member mapping", officer.ID)
		}
		if err != nil {
			return err
		}
		var memberNo, memberName, memberStatus string
		if err := tx.QueryRow(`SELECT member_no,full_name,status FROM members WHERE id=$1`, memberID).Scan(&memberNo, &memberName, &memberStatus); err != nil {
			return fmt.Errorf("load mapped Member %s: %w", memberID, err)
		}
		if memberStatus != "active" {
			return fmt.Errorf("mapped Member %s must be active", memberID)
		}
		canonicalUserID := officer.ID
		var existingUserID string
		err = tx.QueryRow(`SELECT id FROM users WHERE member_id=$1 AND id<>$2 AND historical_identity=FALSE ORDER BY created_at,id LIMIT 1`, memberID, officer.ID).Scan(&existingUserID)
		if err == nil {
			canonicalUserID = existingUserID
			if _, err := tx.Exec(`UPDATE users SET role='member',member_id=NULL,full_name=$1,active=FALSE,historical_identity=TRUE,password_hash='!historical!',updated_at=CURRENT_TIMESTAMP WHERE id=$2`, memberName, officer.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE notifications SET user_id=$1 WHERE user_id=$2`, canonicalUserID, officer.ID); err != nil {
				return err
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.Exec(`UPDATE users SET role='member',member_id=$1,full_name=$2,historical_identity=FALSE,updated_at=CURRENT_TIMESTAMP WHERE id=$3`, memberID, memberName, officer.ID); err != nil {
				return err
			}
		} else {
			return err
		}
		appointmentID := "appointment-" + officer.ID
		if _, err := tx.Exec(`INSERT INTO officer_appointments (id,member_id,role,active) VALUES ($1,$2,$3,TRUE)`, appointmentID, memberID, officer.Role); err != nil {
			return err
		}
		for _, table := range []string{"loan_request_approvals", "withdrawal_request_approvals"} {
			if _, err := tx.Exec(`UPDATE `+table+` SET officer_member_id=$1,officer_member_no=$2,officer_name=CASE WHEN officer_name='' THEN $3 ELSE officer_name END WHERE officer_id=$4`, memberID, memberNo, memberName, officer.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE officer_audit_events SET actor_member_id=$1,actor_member_no=$2,actor_name=CASE WHEN actor_name='' THEN $3 ELSE actor_name END WHERE actor_id=$4`, memberID, memberNo, memberName, officer.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE officer_audit_events SET target_member_id=$1,target_member_no=$2,target_name=CASE WHEN target_name='' THEN $3 ELSE target_name END,target_appointment_id=$4 WHERE target_id=$5`, memberID, memberNo, memberName, appointmentID, officer.ID); err != nil {
			return err
		}
		_ = canonicalUserID
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX idx_users_one_current_per_member ON users(member_id) WHERE member_id IS NOT NULL AND historical_identity=FALSE`); err != nil {
		return err
	}
	if isSQLite {
		statements := []string{
			`CREATE TRIGGER protect_last_ketua_utama_member_deactivation
			 BEFORE UPDATE OF status ON members
			 WHEN OLD.status='active' AND NEW.status<>'active'
			  AND EXISTS (SELECT 1 FROM officer_appointments WHERE member_id=OLD.id AND role='ketua_utama' AND active=TRUE)
			  AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			 BEGIN SELECT RAISE(ABORT, 'at least one active Ketua Utama is required'); END`,
			`CREATE TRIGGER suspend_officer_on_member_deactivation
			 AFTER UPDATE OF status ON members
			 WHEN OLD.status='active' AND NEW.status<>'active'
			 BEGIN UPDATE officer_appointments SET active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE member_id=NEW.id AND active=TRUE; END`,
			`CREATE TRIGGER protect_last_ketua_utama_appointment
			 BEFORE UPDATE OF role,active ON officer_appointments
			 WHEN OLD.role='ketua_utama' AND OLD.active=TRUE AND (NEW.role<>'ketua_utama' OR NEW.active=FALSE)
			  AND EXISTS (SELECT 1 FROM members WHERE id=OLD.member_id AND status='active')
			  AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			 BEGIN SELECT RAISE(ABORT, 'at least one active Ketua Utama is required'); END`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
	} else {
		statements := []string{
			`CREATE FUNCTION protect_last_ketua_utama_member_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
			 BEGIN
			  IF OLD.status='active' AND NEW.status<>'active'
			   AND EXISTS (SELECT 1 FROM officer_appointments WHERE member_id=OLD.id AND role='ketua_utama' AND active=TRUE)
			   AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			  THEN RAISE EXCEPTION 'at least one active Ketua Utama is required'; END IF;
			  RETURN NEW;
			 END $$`,
			`CREATE TRIGGER protect_last_ketua_utama_member_deactivation BEFORE UPDATE OF status ON members FOR EACH ROW EXECUTE FUNCTION protect_last_ketua_utama_member_deactivation()`,
			`CREATE FUNCTION suspend_officer_on_member_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
			 BEGIN
			  IF OLD.status='active' AND NEW.status<>'active' THEN
			   UPDATE officer_appointments SET active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE member_id=NEW.id AND active=TRUE;
			  END IF;
			  RETURN NEW;
			 END $$`,
			`CREATE TRIGGER suspend_officer_on_member_deactivation AFTER UPDATE OF status ON members FOR EACH ROW EXECUTE FUNCTION suspend_officer_on_member_deactivation()`,
			`CREATE FUNCTION protect_last_ketua_utama_appointment() RETURNS trigger LANGUAGE plpgsql AS $$
			 BEGIN
			  IF OLD.role='ketua_utama' AND OLD.active=TRUE AND (NEW.role<>'ketua_utama' OR NEW.active=FALSE)
			   AND EXISTS (SELECT 1 FROM members WHERE id=OLD.member_id AND status='active')
			   AND (SELECT COUNT(*) FROM officer_appointments oa JOIN members m ON m.id=oa.member_id WHERE oa.role='ketua_utama' AND oa.active=TRUE AND m.status='active') <= 1
			  THEN RAISE EXCEPTION 'at least one active Ketua Utama is required'; END IF;
			  RETURN NEW;
			 END $$`,
			`CREATE TRIGGER protect_last_ketua_utama_appointment BEFORE UPDATE OF role,active ON officer_appointments FOR EACH ROW EXECUTE FUNCTION protect_last_ketua_utama_appointment()`,
			`REVOKE EXECUTE ON FUNCTION protect_last_ketua_utama_member_deactivation() FROM PUBLIC`,
			`REVOKE EXECUTE ON FUNCTION suspend_officer_on_member_deactivation() FROM PUBLIC`,
			`REVOKE EXECUTE ON FUNCTION protect_last_ketua_utama_appointment() FROM PUBLIC`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		for _, table := range []string{"legacy_officer_member_mappings", "officer_appointments"} {
			if _, err := tx.Exec(`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY`); err != nil {
				return err
			}
		}
	}
	return nil
}

func lockOfficerTriggerFunctionSearchPath(tx *sql.Tx) error {
	statements := []string{
		`ALTER FUNCTION protect_last_ketua_utama_member_deactivation() SET search_path = public, pg_temp`,
		`ALTER FUNCTION suspend_officer_on_member_deactivation() SET search_path = public, pg_temp`,
		`ALTER FUNCTION protect_last_ketua_utama_appointment() SET search_path = public, pg_temp`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func addOfficerHierarchyAndApprovals(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		if err := rebuildSQLiteUsersForOfficers(tx); err != nil {
			return fmt.Errorf("rebuild sqlite users: %w", err)
		}
		if err := rebuildSQLiteLoanRequestsForApprovals(tx); err != nil {
			return fmt.Errorf("rebuild sqlite loan requests: %w", err)
		}
		if err := rebuildSQLiteWithdrawalRequestsForApprovals(tx); err != nil {
			return fmt.Errorf("rebuild sqlite withdrawal requests: %w", err)
		}
	} else if err := alterPostgresForOfficerApprovals(tx); err != nil {
		return fmt.Errorf("alter postgres officer approvals: %w", err)
	}

	statements := []string{
		`CREATE TABLE loan_request_approvals (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			stage TEXT NOT NULL CHECK (stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
			officer_id TEXT NOT NULL,
			officer_name TEXT NOT NULL,
			officer_role TEXT NOT NULL CHECK (officer_role IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			note TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (request_id, stage),
			FOREIGN KEY (request_id) REFERENCES loan_requests(id),
			FOREIGN KEY (officer_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_loan_request_approvals_request ON loan_request_approvals(request_id, created_at)`,
		`CREATE TABLE withdrawal_request_approvals (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL,
			stage TEXT NOT NULL CHECK (stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
			officer_id TEXT NOT NULL,
			officer_name TEXT NOT NULL,
			officer_role TEXT NOT NULL CHECK (officer_role IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			note TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (request_id, stage),
			FOREIGN KEY (request_id) REFERENCES withdrawal_requests(id),
			FOREIGN KEY (officer_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_withdrawal_request_approvals_request ON withdrawal_request_approvals(request_id, created_at)`,
		`CREATE TABLE withdrawal_reservations (
			id TEXT PRIMARY KEY,
			request_id TEXT NOT NULL UNIQUE,
			member_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK (amount > 0),
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'consumed')),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (request_id) REFERENCES withdrawal_requests(id),
			FOREIGN KEY (member_id) REFERENCES members(id)
		)`,
		`CREATE INDEX idx_withdrawal_reservations_member_status ON withdrawal_reservations(member_id, status)`,
		`CREATE TABLE officer_audit_events (
			id TEXT PRIMARY KEY,
			actor_id TEXT NOT NULL,
			actor_name TEXT NOT NULL,
			target_id TEXT NOT NULL,
			target_name TEXT NOT NULL,
			action TEXT NOT NULL CHECK (action IN ('created', 'role_changed', 'activated', 'deactivated', 'password_reset')),
			old_role TEXT NOT NULL DEFAULT '',
			new_role TEXT NOT NULL DEFAULT '',
			old_active BOOLEAN NULL,
			new_active BOOLEAN NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (actor_id) REFERENCES users(id),
			FOREIGN KEY (target_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_officer_audit_events_target ON officer_audit_events(target_id, created_at)`,
		`CREATE TABLE notification_events (
			id TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			request_type TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE notifications (
			id TEXT PRIMARY KEY,
			event_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			title_key TEXT NOT NULL,
			body_key TEXT NOT NULL,
			link TEXT NOT NULL DEFAULT '',
			is_read BOOLEAN NOT NULL DEFAULT FALSE,
			resolved_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (event_id) REFERENCES notification_events(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_notifications_user_state ON notifications(user_id, resolved_at, is_read, created_at)`,
		`INSERT INTO withdrawal_reservations (id, request_id, member_id, amount, status)
		 SELECT 'migration-' || id, id, member_id, amount, 'active' FROM withdrawal_requests WHERE status = 'pending'`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if !isSQLite {
		for _, table := range []string{
			"loan_request_approvals",
			"withdrawal_request_approvals",
			"withdrawal_reservations",
			"officer_audit_events",
			"notification_events",
			"notifications",
		} {
			if _, err := tx.Exec(`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY`); err != nil {
				return err
			}
		}
	}
	return nil
}

func rebuildSQLiteUsersForOfficers(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE users_v12 (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('member', 'manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			member_id TEXT NULL,
			full_name TEXT NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT TRUE,
			must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO users_v12 (id, email, password_hash, role, member_id, full_name, active, must_change_password, created_at, updated_at)
		 SELECT id, email, password_hash, CASE WHEN role = 'admin' THEN 'manager' ELSE role END, member_id,
		 CASE WHEN role = 'admin' THEN email ELSE '' END, TRUE, FALSE, created_at, updated_at FROM users`,
		`DROP TABLE users`,
		`ALTER TABLE users_v12 RENAME TO users`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSQLiteLoanRequestsForApprovals(tx *sql.Tx) error {
	statements := []string{
		`DROP TRIGGER IF EXISTS loan_requests_duration_insert`,
		`DROP TRIGGER IF EXISTS loan_requests_duration_update`,
		`CREATE TABLE loan_requests_v12 (
			id TEXT PRIMARY KEY,
			member_id TEXT NOT NULL,
			requested_amount INTEGER NOT NULL CHECK (requested_amount > 0),
			duration_months INTEGER NOT NULL CHECK (duration_months > 0),
			purpose TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
			current_approval_stage TEXT NULL CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			proposed_approved_amount INTEGER NULL CHECK (proposed_approved_amount IS NULL OR proposed_approved_amount > 0),
			proposed_duration_months INTEGER NULL CHECK (proposed_duration_months IS NULL OR proposed_duration_months BETWEEN 1 AND 120),
			proposed_start_date TEXT NOT NULL DEFAULT '',
			proposed_interest_rate_bps INTEGER NULL CHECK (proposed_interest_rate_bps IS NULL OR proposed_interest_rate_bps BETWEEN 0 AND 1000),
			reviewed_by TEXT NULL,
			reviewed_at TIMESTAMP NULL,
			rejection_reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (member_id) REFERENCES members(id),
			FOREIGN KEY (reviewed_by) REFERENCES users(id)
		)`,
		`INSERT INTO loan_requests_v12 (id, member_id, requested_amount, duration_months, purpose, status, current_approval_stage, reviewed_by, reviewed_at, rejection_reason, created_at, updated_at)
		 SELECT id, member_id, requested_amount, duration_months, purpose, status, CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END, reviewed_by, reviewed_at, rejection_reason, created_at, updated_at FROM loan_requests`,
		`DROP TABLE loan_requests`,
		`ALTER TABLE loan_requests_v12 RENAME TO loan_requests`,
		`CREATE UNIQUE INDEX idx_loan_requests_one_pending_per_member ON loan_requests(member_id) WHERE status = 'pending'`,
		`CREATE TRIGGER loan_requests_duration_insert BEFORE INSERT ON loan_requests WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan request duration out of range'); END`,
		`CREATE TRIGGER loan_requests_duration_update BEFORE UPDATE OF duration_months ON loan_requests WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan request duration out of range'); END`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSQLiteWithdrawalRequestsForApprovals(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE withdrawal_requests_v12 (
			id TEXT PRIMARY KEY,
			member_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK (amount > 0),
			note TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
			current_approval_stage TEXT NULL CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama')),
			reviewed_by TEXT NULL,
			reviewed_at TIMESTAMP NULL,
			rejection_reason TEXT NOT NULL DEFAULT '',
			saving_record_id TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (member_id) REFERENCES members(id),
			FOREIGN KEY (reviewed_by) REFERENCES users(id),
			FOREIGN KEY (saving_record_id) REFERENCES saving_records(id)
		)`,
		`INSERT INTO withdrawal_requests_v12 (id, member_id, amount, note, status, current_approval_stage, reviewed_by, reviewed_at, rejection_reason, saving_record_id, created_at, updated_at)
		 SELECT id, member_id, amount, note, status, CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END, reviewed_by, reviewed_at, rejection_reason, saving_record_id, created_at, updated_at FROM withdrawal_requests`,
		`DROP TABLE withdrawal_requests`,
		`ALTER TABLE withdrawal_requests_v12 RENAME TO withdrawal_requests`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func alterPostgresForOfficerApprovals(tx *sql.Tx) error {
	statements := []string{
		`ALTER TABLE users ADD COLUMN full_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check`,
		`UPDATE users SET role = 'manager', full_name = CASE WHEN full_name = '' THEN email ELSE full_name END WHERE role = 'admin'`,
		`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('member', 'manager', 'ketua_i', 'ketua_ii', 'ketua_utama'))`,
		`ALTER TABLE loan_requests ADD COLUMN current_approval_stage TEXT NULL`,
		`ALTER TABLE loan_requests ADD COLUMN proposed_approved_amount INTEGER NULL`,
		`ALTER TABLE loan_requests ADD COLUMN proposed_duration_months INTEGER NULL`,
		`ALTER TABLE loan_requests ADD COLUMN proposed_start_date TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE loan_requests ADD COLUMN proposed_interest_rate_bps INTEGER NULL`,
		`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_status_check`,
		`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'))`,
		`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_approval_stage_check CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama'))`,
		`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_amount_check CHECK (proposed_approved_amount IS NULL OR proposed_approved_amount > 0)`,
		`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_duration_check CHECK (proposed_duration_months IS NULL OR proposed_duration_months BETWEEN 1 AND 120)`,
		`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_proposed_rate_check CHECK (proposed_interest_rate_bps IS NULL OR proposed_interest_rate_bps BETWEEN 0 AND 1000)`,
		`UPDATE loan_requests SET current_approval_stage = CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END`,
		`ALTER TABLE withdrawal_requests ADD COLUMN current_approval_stage TEXT NULL`,
		`ALTER TABLE withdrawal_requests DROP CONSTRAINT IF EXISTS withdrawal_requests_status_check`,
		`ALTER TABLE withdrawal_requests ADD CONSTRAINT withdrawal_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled'))`,
		`ALTER TABLE withdrawal_requests ADD CONSTRAINT withdrawal_requests_approval_stage_check CHECK (current_approval_stage IS NULL OR current_approval_stage IN ('manager', 'ketua_i', 'ketua_ii', 'ketua_utama'))`,
		`UPDATE withdrawal_requests SET current_approval_stage = CASE WHEN status = 'pending' THEN 'manager' ELSE NULL END`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func enforceMaximumLoanTenor(tx *sql.Tx, isSQLite bool) error {
	var statements []string
	if isSQLite {
		statements = []string{
			`CREATE TRIGGER loan_requests_duration_insert BEFORE INSERT ON loan_requests WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan request duration out of range'); END`,
			`CREATE TRIGGER loan_requests_duration_update BEFORE UPDATE OF duration_months ON loan_requests WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan request duration out of range'); END`,
			`CREATE TRIGGER loans_duration_insert BEFORE INSERT ON loans WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan duration out of range'); END`,
			`CREATE TRIGGER loans_duration_update BEFORE UPDATE OF duration_months ON loans WHEN NEW.duration_months < 1 OR NEW.duration_months > 120 BEGIN SELECT RAISE(ABORT, 'loan duration out of range'); END`,
		}
	} else {
		// NOT VALID protects legacy rows while enforcing the limit for new writes.
		statements = []string{
			`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_duration_max CHECK (duration_months BETWEEN 1 AND 120) NOT VALID`,
			`ALTER TABLE loans ADD CONSTRAINT loans_duration_max CHECK (duration_months BETWEEN 1 AND 120) NOT VALID`,
		}
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func applyLoanScheduleMigration(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		if err := rebuildSQLiteLoansForSchedules(tx); err != nil {
			return fmt.Errorf("rebuild sqlite loans: %w", err)
		}
	} else if err := alterPostgresLoansForSchedules(tx); err != nil {
		return fmt.Errorf("alter postgres loans: %w", err)
	}

	for _, statement := range []string{
		`CREATE TABLE loan_installments (
			id TEXT PRIMARY KEY,
			loan_id TEXT NOT NULL,
			installment_no INTEGER NOT NULL CHECK (installment_no > 0),
			due_date TEXT NOT NULL,
			scheduled_amount INTEGER NOT NULL CHECK (scheduled_amount > 0),
			paid_amount INTEGER NOT NULL DEFAULT 0 CHECK (paid_amount >= 0 AND paid_amount <= scheduled_amount),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (loan_id, installment_no),
			FOREIGN KEY (loan_id) REFERENCES loans(id)
		)`,
		`CREATE INDEX idx_loan_installments_due ON loan_installments(loan_id, due_date)`,
		`CREATE TABLE loan_start_date_audits (
			id TEXT PRIMARY KEY,
			loan_id TEXT NOT NULL,
			old_start_date TEXT NOT NULL,
			new_start_date TEXT NOT NULL,
			changed_by TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (loan_id) REFERENCES loans(id),
			FOREIGN KEY (changed_by) REFERENCES users(id)
		)`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return backfillLoanSchedules(tx)
}

func rebuildSQLiteLoansForSchedules(tx *sql.Tx) error {
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE loans_v9 (
			id TEXT PRIMARY KEY,
			loan_request_id TEXT NOT NULL UNIQUE,
			member_id TEXT NOT NULL,
			approved_amount INTEGER NOT NULL CHECK (approved_amount > 0),
			duration_months INTEGER NOT NULL CHECK (duration_months > 0),
			monthly_installment INTEGER NOT NULL CHECK (monthly_installment > 0),
			remaining_balance INTEGER NOT NULL CHECK (remaining_balance >= 0),
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paid', 'cancelled', 'adjustment_due')),
			approved_by TEXT NOT NULL,
			approved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			start_date TEXT NOT NULL DEFAULT '',
			interest_rate_bps INTEGER NOT NULL DEFAULT 100 CHECK (interest_rate_bps >= 0 AND interest_rate_bps <= 1000),
			total_interest INTEGER NOT NULL DEFAULT 0 CHECK (total_interest >= 0),
			total_obligation INTEGER NOT NULL DEFAULT 0 CHECK (total_obligation >= 0),
			next_due_date TEXT NOT NULL DEFAULT '',
			final_due_date TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (loan_request_id) REFERENCES loan_requests(id),
			FOREIGN KEY (member_id) REFERENCES members(id),
			FOREIGN KEY (approved_by) REFERENCES users(id)
		)`,
		`INSERT INTO loans_v9 (id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at, created_at, updated_at)
		 SELECT id, loan_request_id, member_id, approved_amount, duration_months, monthly_installment, remaining_balance, status, approved_by, approved_at, created_at, updated_at FROM loans`,
		`DROP TABLE loans`,
		`ALTER TABLE loans_v9 RENAME TO loans`,
		`CREATE UNIQUE INDEX idx_loans_one_active_per_member ON loans(member_id) WHERE status = 'active'`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func alterPostgresLoansForSchedules(tx *sql.Tx) error {
	statements := []string{
		`ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_status_check`,
		`ALTER TABLE loans ADD CONSTRAINT loans_status_check CHECK (status IN ('active', 'paid', 'cancelled', 'adjustment_due'))`,
		`ALTER TABLE loans ADD COLUMN start_date TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE loans ADD COLUMN interest_rate_bps INTEGER NOT NULL DEFAULT 100 CHECK (interest_rate_bps >= 0 AND interest_rate_bps <= 1000)`,
		`ALTER TABLE loans ADD COLUMN total_interest INTEGER NOT NULL DEFAULT 0 CHECK (total_interest >= 0)`,
		`ALTER TABLE loans ADD COLUMN total_obligation INTEGER NOT NULL DEFAULT 0 CHECK (total_obligation >= 0)`,
		`ALTER TABLE loans ADD COLUMN next_due_date TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE loans ADD COLUMN final_due_date TEXT NOT NULL DEFAULT ''`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func backfillLoanSchedules(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, approved_amount, duration_months, status, approved_at FROM loans ORDER BY id`)
	if err != nil {
		return err
	}
	type existingLoan struct {
		id, status string
		amount     int64
		duration   int
		approvedAt time.Time
	}
	var loans []existingLoan
	for rows.Next() {
		var loan existingLoan
		if err := rows.Scan(&loan.id, &loan.amount, &loan.duration, &loan.status, &loan.approvedAt); err != nil {
			_ = rows.Close()
			return err
		}
		loans = append(loans, loan)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, loan := range loans {
		if loan.status == "cancelled" {
			if _, err := tx.Exec(`UPDATE loans SET start_date = '', interest_rate_bps = 0, total_interest = 0, total_obligation = 0, remaining_balance = 0, status = 'cancelled', next_due_date = '', final_due_date = '', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, loan.id); err != nil {
				return err
			}
			continue
		}
		startDate := loan.approvedAt.In(jakartaLocation).Format("2006-01-02")
		schedule, err := calculateLegacyLoanSchedule(loan.amount, loan.duration, defaultLoanInterestRateBPS, startDate)
		if err != nil {
			return fmt.Errorf("calculate schedule for loan %s: %w", loan.id, err)
		}
		repaymentRows, err := tx.Query(`SELECT amount FROM loan_repayments WHERE loan_id = $1 ORDER BY record_date, created_at, id`, loan.id)
		if err != nil {
			return err
		}
		var repayments []int64
		for repaymentRows.Next() {
			var amount int64
			if err := repaymentRows.Scan(&amount); err != nil {
				_ = repaymentRows.Close()
				return err
			}
			repayments = append(repayments, amount)
		}
		if err := repaymentRows.Err(); err != nil {
			_ = repaymentRows.Close()
			return err
		}
		if err := repaymentRows.Close(); err != nil {
			return err
		}
		paid, err := allocateRepaymentsOldestFirst(schedule.Installments, repayments)
		if err != nil {
			return fmt.Errorf("allocate repayments for loan %s: %w", loan.id, err)
		}
		remaining := schedule.TotalObligation - paid
		status := loan.status
		if remaining == 0 {
			status = "paid"
		} else if status == "paid" {
			status = "adjustment_due"
		}
		nextDueDate := ""
		for _, installment := range schedule.Installments {
			if installment.PaidAmount < installment.ScheduledAmount {
				nextDueDate = installment.DueDate
				break
			}
		}
		finalDueDate := schedule.Installments[len(schedule.Installments)-1].DueDate
		monthlyInstallment := schedule.Installments[0].ScheduledAmount
		if _, err := tx.Exec(`UPDATE loans SET start_date = $1, interest_rate_bps = $2, total_interest = $3, total_obligation = $4, monthly_installment = $5, remaining_balance = $6, status = $7, next_due_date = $8, final_due_date = $9, updated_at = CURRENT_TIMESTAMP WHERE id = $10`,
			startDate, defaultLoanInterestRateBPS, schedule.TotalInterest, schedule.TotalObligation, monthlyInstallment, remaining, status, nextDueDate, finalDueDate, loan.id); err != nil {
			return err
		}
		for _, installment := range schedule.Installments {
			installmentID := fmt.Sprintf("%s-%03d", loan.id, installment.Number)
			if _, err := tx.Exec(`INSERT INTO loan_installments (id, loan_id, installment_no, due_date, scheduled_amount, paid_amount) VALUES ($1, $2, $3, $4, $5, $6)`,
				installmentID, loan.id, installment.Number, installment.DueDate, installment.ScheduledAmount, installment.PaidAmount); err != nil {
				return err
			}
		}
	}
	return nil
}
