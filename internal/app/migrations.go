package app

import (
	"context"
	"database/sql"
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
	if isSQLite && migration.Version == 9 {
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
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
		migration.Version,
		migration.Name,
	); err != nil {
		return err
	}
	return tx.Commit()
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
