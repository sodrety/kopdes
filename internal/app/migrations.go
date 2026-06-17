package app

import "database/sql"

func Migrate(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
			member_id TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
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
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
