package app

import "database/sql"

func addAdminLoanRequestIntakeAudit(tx *sql.Tx, isSQLite bool) error {
	if isSQLite {
		loanRequestsExist, err := sqliteTableExists(tx, "loan_requests")
		if err != nil {
			return err
		}
		usersExist, err := sqliteTableExists(tx, "users")
		if err != nil {
			return err
		}
		if !loanRequestsExist || !usersExist {
			return nil
		}
	}
	statements := []string{
		`CREATE TABLE loan_request_batches (
			id TEXT PRIMARY KEY,
			created_by TEXT NOT NULL REFERENCES users(id),
			file_hash TEXT NOT NULL,
			template_version TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('preview','validation_failed','committed')),
			source_file_name TEXT NOT NULL DEFAULT '',
			row_count INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
			created_count INTEGER NOT NULL DEFAULT 0 CHECK (created_count >= 0),
			error_count INTEGER NOT NULL DEFAULT 0 CHECK (error_count >= 0),
			warning_count INTEGER NOT NULL DEFAULT 0 CHECK (warning_count >= 0),
			batch_warnings TEXT NOT NULL DEFAULT '',
			warnings_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
			preview_expires_at TIMESTAMP NULL,
			confirmed_by TEXT NULL REFERENCES users(id),
			confirmed_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX idx_loan_request_batches_created_by ON loan_request_batches(created_by,created_at)`,
		`CREATE INDEX idx_loan_request_batches_file_hash ON loan_request_batches(file_hash,status)`,
		`CREATE TABLE loan_request_batch_rows (
			id TEXT PRIMARY KEY,
			batch_id TEXT NOT NULL REFERENCES loan_request_batches(id),
			excel_row INTEGER NOT NULL CHECK (excel_row > 0),
			member_no TEXT NOT NULL DEFAULT '',
			full_name TEXT NOT NULL DEFAULT '',
			loan_type TEXT NOT NULL DEFAULT '',
			requested_amount BIGINT NULL,
			duration_months INTEGER NULL,
			purpose TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('valid','warning','error','created')),
			errors TEXT NOT NULL DEFAULT '',
			warnings TEXT NOT NULL DEFAULT '',
			request_id TEXT NULL REFERENCES loan_requests(id),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(batch_id,excel_row)
		)`,
		`CREATE INDEX idx_loan_request_batch_rows_batch ON loan_request_batch_rows(batch_id,excel_row)`,
		`ALTER TABLE loan_requests ADD COLUMN created_by TEXT NULL REFERENCES users(id)`,
		`ALTER TABLE loan_requests ADD COLUMN creation_source TEXT NOT NULL DEFAULT 'member' CHECK (creation_source IN ('member','admin'))`,
		`ALTER TABLE loan_requests ADD COLUMN batch_id TEXT NULL REFERENCES loan_request_batches(id)`,
		`ALTER TABLE loan_requests ADD COLUMN creation_idempotency_key TEXT NULL`,
		`ALTER TABLE loan_requests ADD COLUMN cancellation_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE loan_requests ADD COLUMN cancelled_by TEXT NULL REFERENCES users(id)`,
		`ALTER TABLE loan_requests ADD COLUMN cancelled_at TIMESTAMP NULL`,
		`CREATE INDEX idx_loan_requests_creation_source ON loan_requests(creation_source,created_at)`,
		`CREATE INDEX idx_loan_requests_batch ON loan_requests(batch_id,created_at)`,
		`CREATE UNIQUE INDEX idx_loan_requests_admin_idempotency ON loan_requests(created_by,creation_idempotency_key) WHERE creation_idempotency_key IS NOT NULL`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if !isSQLite {
		if _, err := tx.Exec(`ALTER TABLE loan_request_batches ENABLE ROW LEVEL SECURITY`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE loan_request_batch_rows ENABLE ROW LEVEL SECURITY`); err != nil {
			return err
		}
	}
	return nil
}
