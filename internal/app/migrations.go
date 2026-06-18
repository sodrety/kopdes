package app

import (
	"database/sql"
	"fmt"
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
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, statement := range migration.Statements {
		if _, err := tx.Exec(statement); err != nil {
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
