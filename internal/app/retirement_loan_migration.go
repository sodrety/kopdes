package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// addRetirementLoanSupport extends Voucher's manual-fee path with the
// Retirement product and persists an optional first due date on admin intake.
func addRetirementLoanSupport(tx *sql.Tx, isSQLite bool) error {
	if !isSQLite {
		for _, statement := range postgresRetirementLoanStatements {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		return nil
	}
	for _, table := range []string{"loan_requests", "loans", "loan_request_batch_rows"} {
		exists, err := sqliteTableExists(tx, table)
		if err != nil || !exists {
			return err
		}
	}
	type object struct{ kind, name, table, definition string }
	rows, err := tx.Query(`SELECT type,name,tbl_name,sql FROM sqlite_master WHERE sql IS NOT NULL AND (type IN ('trigger','view') OR (type='index' AND tbl_name IN ('loan_requests','loans'))) ORDER BY type,name`)
	if err != nil {
		return err
	}
	var objects []object
	for rows.Next() {
		var item object
		if err := rows.Scan(&item.kind, &item.name, &item.table, &item.definition); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	quote := func(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
	for _, item := range objects {
		if _, err := tx.Exec(`DROP ` + item.kind + ` ` + quote(item.name)); err != nil {
			return err
		}
	}
	for _, table := range []string{"loan_requests", "loans"} {
		var definition string
		if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=$1`, table).Scan(&definition); err != nil {
			return err
		}
		definition = extendSQLiteRetirementDefinition(definition)
		for _, statement := range []string{
			`CREATE TEMP TABLE ` + quote(table+"_retirement_backup") + ` AS SELECT * FROM ` + quote(table),
			`DROP TABLE ` + quote(table),
			definition,
			`INSERT INTO ` + quote(table) + ` SELECT * FROM ` + quote(table+"_retirement_backup"),
			`DROP TABLE ` + quote(table+"_retirement_backup"),
		} {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("rebuild %s for Retirement: %w", table, err)
			}
		}
	}
	if _, err := tx.Exec(`ALTER TABLE loan_requests ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE loan_request_batch_rows ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')`); err != nil {
		return err
	}
	obsoleteVoucherTriggers := map[string]bool{
		"loan_requests_voucher_insert":   true,
		"loan_requests_voucher_update":   true,
		"loan_requests_voucher_snapshot": true,
		"loans_voucher_insert":           true,
		"loans_voucher_update":           true,
	}
	for _, item := range objects {
		if obsoleteVoucherTriggers[item.name] {
			continue
		}
		definition := item.definition
		if item.table == "loan_requests" || item.table == "loans" {
			definition = extendSQLiteRetirementDefinition(definition)
		}
		if _, err := tx.Exec(definition); err != nil {
			return fmt.Errorf("restore %s: %w", item.name, err)
		}
	}
	requestCheck := `(NEW.loan_type='voucher' AND (NEW.creation_source<>'admin' OR NEW.created_by IS NULL OR NEW.batch_id IS NULL OR NEW.legacy_terms<>0 OR NEW.duration_months<>1 OR NEW.requested_admin_fee IS NULL OR typeof(NEW.requested_admin_fee)<>'integer' OR NEW.requested_admin_fee<0 OR NEW.requested_amount>9223372036854775807-NEW.requested_admin_fee)) OR (NEW.loan_type='retirement' AND (NEW.creation_source<>'admin' OR NEW.created_by IS NULL OR NEW.legacy_terms<>0 OR NEW.duration_months<>1 OR NEW.requested_admin_fee IS NULL OR typeof(NEW.requested_admin_fee)<>'integer' OR NEW.requested_admin_fee<0 OR NEW.requested_amount>9223372036854775807-NEW.requested_admin_fee)) OR (NEW.loan_type NOT IN ('voucher','retirement') AND NEW.requested_admin_fee IS NOT NULL)`
	snapshotCheck := `(NEW.loan_type='voucher' AND NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.proposed_admin_fee_policy<>'voucher_nominal_one_time_v1' OR NEW.proposed_duration_months<>1 OR NEW.proposed_monthly_admin_fee IS NOT NULL OR NEW.proposed_total_admin_fee IS NULL OR NEW.proposed_total_admin_fee<>NEW.requested_admin_fee OR NEW.proposed_approved_amount IS NULL OR NEW.proposed_approved_amount<=0 OR NEW.proposed_approved_amount>9223372036854775807-NEW.requested_admin_fee OR NEW.proposed_total_obligation IS NULL OR NEW.proposed_total_obligation-NEW.proposed_approved_amount<>NEW.requested_admin_fee)) OR (NEW.loan_type='retirement' AND NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.proposed_admin_fee_policy<>'retirement_manual_one_time_v1' OR NEW.proposed_duration_months<>1 OR NEW.proposed_monthly_admin_fee IS NOT NULL OR NEW.proposed_total_admin_fee IS NULL OR NEW.proposed_total_admin_fee<>NEW.requested_admin_fee OR NEW.proposed_approved_amount IS NULL OR NEW.proposed_approved_amount<=0 OR NEW.proposed_approved_amount>9223372036854775807-NEW.requested_admin_fee OR NEW.proposed_total_obligation IS NULL OR NEW.proposed_total_obligation-NEW.proposed_approved_amount<>NEW.requested_admin_fee))`
	loanCheck := `(NEW.loan_type='voucher' AND (NEW.legacy_terms<>0 OR NEW.admin_fee_policy<>'voucher_nominal_one_time_v1' OR NEW.duration_months<>1 OR NEW.monthly_admin_fee IS NOT NULL OR NEW.total_admin_fee IS NULL OR typeof(NEW.total_admin_fee)<>'integer' OR NEW.total_admin_fee<0 OR NEW.total_obligation IS NULL OR NEW.total_obligation-NEW.approved_amount<>NEW.total_admin_fee)) OR (NEW.loan_type='retirement' AND (NEW.legacy_terms<>0 OR NEW.admin_fee_policy<>'retirement_manual_one_time_v1' OR NEW.duration_months<>1 OR NEW.monthly_admin_fee IS NOT NULL OR NEW.total_admin_fee IS NULL OR typeof(NEW.total_admin_fee)<>'integer' OR NEW.total_admin_fee<0 OR NEW.total_obligation IS NULL OR NEW.total_obligation-NEW.approved_amount<>NEW.total_admin_fee))`
	for _, statement := range []string{
		`CREATE TRIGGER loan_requests_manual_fee_insert BEFORE INSERT ON loan_requests WHEN ` + requestCheck + ` BEGIN SELECT RAISE(ABORT,'invalid manual-fee loan intake'); END`,
		`CREATE TRIGGER loan_requests_manual_fee_update BEFORE UPDATE ON loan_requests WHEN ` + requestCheck + ` OR (OLD.loan_type IN ('voucher','retirement') AND (NEW.requested_admin_fee IS NOT OLD.requested_admin_fee OR NEW.creation_source<>OLD.creation_source OR NEW.created_by IS NOT OLD.created_by OR NEW.batch_id IS NOT OLD.batch_id)) BEGIN SELECT RAISE(ABORT,'invalid manual-fee loan intake'); END`,
		`CREATE TRIGGER loan_requests_manual_fee_snapshot BEFORE UPDATE ON loan_requests WHEN ` + snapshotCheck + ` BEGIN SELECT RAISE(ABORT,'invalid manual-fee loan snapshot'); END`,
		`CREATE TRIGGER loan_requests_first_due_date_immutable BEFORE UPDATE OF first_due_date ON loan_requests WHEN NEW.first_due_date IS NOT OLD.first_due_date BEGIN SELECT RAISE(ABORT,'first due date is immutable'); END`,
		`CREATE TRIGGER loans_manual_fee_insert BEFORE INSERT ON loans WHEN ` + loanCheck + ` BEGIN SELECT RAISE(ABORT,'invalid manual-fee loan terms'); END`,
		`CREATE TRIGGER loans_manual_fee_update BEFORE UPDATE ON loans WHEN ` + loanCheck + ` BEGIN SELECT RAISE(ABORT,'invalid manual-fee loan terms'); END`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func extendSQLiteRetirementDefinition(definition string) string {
	definition = extendSQLiteVoucherDefinition(definition)
	definition = strings.ReplaceAll(definition, `'regular','secondary_goods','goods_purchase_paylater','voucher'`, `'regular','secondary_goods','goods_purchase_paylater','voucher','retirement'`)
	definition = strings.ReplaceAll(definition, `'voucher_nominal_one_time_v1','legacy_flat_monthly'`, `'voucher_nominal_one_time_v1','retirement_manual_one_time_v1','legacy_flat_monthly'`)
	identity := regexp.MustCompile(`\(NEW\.loan_type='goods_purchase_paylater' AND (?:NEW\.legacy_terms=0 AND )?NEW\.(proposed_)?admin_fee_policy='goods_purchase_paylater_one_time_v1'\)`)
	return identity.ReplaceAllStringFunc(definition, func(match string) string {
		retirement := strings.ReplaceAll(match, "goods_purchase_paylater_one_time_v1", retirementAdminFeePolicy)
		retirement = strings.ReplaceAll(retirement, "goods_purchase_paylater", "retirement")
		return match + " OR " + retirement
	})
}

var postgresRetirementLoanStatements = []string{
	`ALTER TABLE loan_requests ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$')`,
	`ALTER TABLE loan_request_batch_rows ADD COLUMN first_due_date TEXT NOT NULL DEFAULT '' CHECK (first_due_date='' OR first_due_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$')`,
	`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_loan_type_check`,
	`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_loan_type_check CHECK (loan_type IN ('regular','secondary_goods','goods_purchase_paylater','voucher','retirement'))`,
	`ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_loan_type_check`,
	`ALTER TABLE loans ADD CONSTRAINT loans_loan_type_check CHECK (loan_type IN ('regular','secondary_goods','goods_purchase_paylater','voucher','retirement'))`,
	`ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_check`,
	`ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_check CHECK (admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','voucher_nominal_one_time_v1','retirement_manual_one_time_v1','legacy_flat_monthly'))`,
	`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_policy_check`,
	`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_check CHECK (proposed_admin_fee_policy IS NULL OR proposed_admin_fee_policy IN ('regular_tiered_monthly_v1','secondary_goods_one_time_v1','goods_purchase_paylater_one_time_v1','voucher_nominal_one_time_v1','retirement_manual_one_time_v1','legacy_flat_monthly'))`,
	`ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_fields_check`,
	`ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_fields_check CHECK (
    (admin_fee_policy='regular_tiered_monthly_v1' AND approved_amount>0 AND duration_months BETWEEN 1 AND 24 AND monthly_admin_fee IS NOT NULL
      AND monthly_admin_fee=CASE WHEN approved_amount<=25000000 THEN (approved_amount+50)/100 ELSE 250000+((approved_amount-25000000)/200)*3+((((approved_amount-25000000)%200)*3+100)/200) END
      AND total_admin_fee IS NOT NULL AND total_admin_fee%NULLIF(duration_months,0)=0 AND total_admin_fee/NULLIF(duration_months,0)=monthly_admin_fee
      AND total_obligation IS NOT NULL AND total_obligation>=approved_amount AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='secondary_goods_one_time_v1' AND approved_amount>0 AND duration_months BETWEEN 1 AND 12 AND monthly_admin_fee IS NULL
      AND total_admin_fee=(approved_amount*20+50)/100 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='goods_purchase_paylater_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL
      AND total_admin_fee=(approved_amount*5+50)/100 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (admin_fee_policy='legacy_flat_monthly' AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_obligation IS NOT NULL)
    OR (loan_type='voucher' AND legacy_terms=FALSE AND admin_fee_policy='voucher_nominal_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_admin_fee>=0 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee)
    OR (loan_type='retirement' AND legacy_terms=FALSE AND admin_fee_policy='retirement_manual_one_time_v1' AND approved_amount>0 AND duration_months=1 AND monthly_admin_fee IS NULL AND total_admin_fee IS NOT NULL AND total_admin_fee>=0 AND total_obligation IS NOT NULL AND total_obligation-approved_amount=total_admin_fee))`,
	`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_fields_check`,
	`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_fields_check CHECK (
    (proposed_admin_fee_policy IS NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NULL AND proposed_total_obligation IS NULL)
    OR (proposed_admin_fee_policy='regular_tiered_monthly_v1' AND proposed_approved_amount>0 AND proposed_duration_months BETWEEN 1 AND 24 AND proposed_monthly_admin_fee IS NOT NULL
      AND proposed_monthly_admin_fee=CASE WHEN proposed_approved_amount<=25000000 THEN (proposed_approved_amount+50)/100 ELSE 250000+((proposed_approved_amount-25000000)/200)*3+((((proposed_approved_amount-25000000)%200)*3+100)/200) END
      AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee%NULLIF(proposed_duration_months,0)=0 AND proposed_total_admin_fee/NULLIF(proposed_duration_months,0)=proposed_monthly_admin_fee
      AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation>=proposed_approved_amount AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='secondary_goods_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months BETWEEN 1 AND 12 AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee=(proposed_approved_amount*20+50)/100 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL
      AND proposed_total_admin_fee=(proposed_approved_amount*5+50)/100 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (proposed_admin_fee_policy='legacy_flat_monthly' AND proposed_approved_amount IS NOT NULL AND proposed_duration_months IS NOT NULL AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_obligation IS NOT NULL)
    OR (loan_type='voucher' AND legacy_terms=FALSE AND proposed_admin_fee_policy='voucher_nominal_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee>=0 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee)
    OR (loan_type='retirement' AND legacy_terms=FALSE AND proposed_admin_fee_policy='retirement_manual_one_time_v1' AND proposed_approved_amount>0 AND proposed_duration_months=1 AND proposed_monthly_admin_fee IS NULL AND proposed_total_admin_fee IS NOT NULL AND proposed_total_admin_fee>=0 AND proposed_total_obligation IS NOT NULL AND proposed_total_obligation-proposed_approved_amount=proposed_total_admin_fee))`,
	`ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_admin_fee_policy_identity_check`,
	`ALTER TABLE loans ADD CONSTRAINT loans_admin_fee_policy_identity_check CHECK (
    (loan_type='regular' AND legacy_terms=FALSE AND admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type IN ('regular','secondary_goods','goods_purchase_paylater') AND legacy_terms=TRUE AND admin_fee_policy='legacy_flat_monthly') OR
    (loan_type='voucher' AND legacy_terms=FALSE AND admin_fee_policy='voucher_nominal_one_time_v1') OR
    (loan_type='retirement' AND legacy_terms=FALSE AND admin_fee_policy='retirement_manual_one_time_v1'))`,
	`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_admin_fee_policy_identity_check`,
	`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_admin_fee_policy_identity_check CHECK (
    proposed_admin_fee_policy IS NULL OR
    (loan_type='regular' AND legacy_terms=FALSE AND proposed_admin_fee_policy='regular_tiered_monthly_v1') OR
    (loan_type='secondary_goods' AND legacy_terms=FALSE AND proposed_admin_fee_policy='secondary_goods_one_time_v1') OR
    (loan_type='goods_purchase_paylater' AND legacy_terms=FALSE AND proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
    (loan_type IN ('regular','secondary_goods','goods_purchase_paylater') AND legacy_terms=TRUE AND proposed_admin_fee_policy='legacy_flat_monthly') OR
    (loan_type='voucher' AND legacy_terms=FALSE AND proposed_admin_fee_policy='voucher_nominal_one_time_v1') OR
    (loan_type='retirement' AND legacy_terms=FALSE AND proposed_admin_fee_policy='retirement_manual_one_time_v1'))`,
	`CREATE OR REPLACE FUNCTION validate_loan_admin_fee_policy_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NOT ((NEW.legacy_terms=TRUE AND NEW.admin_fee_policy='legacy_flat_monthly') OR (NEW.legacy_terms=FALSE AND (
        (NEW.loan_type='regular' AND NEW.admin_fee_policy='regular_tiered_monthly_v1') OR
        (NEW.loan_type='secondary_goods' AND NEW.admin_fee_policy='secondary_goods_one_time_v1') OR
        (NEW.loan_type='goods_purchase_paylater' AND NEW.admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
        (NEW.loan_type='voucher' AND NEW.admin_fee_policy='voucher_nominal_one_time_v1') OR
        (NEW.loan_type='retirement' AND NEW.admin_fee_policy='retirement_manual_one_time_v1')))) THEN
        RAISE EXCEPTION 'loan admin fee policy does not match loan terms';
    END IF;
    RETURN NEW;
END $$`,
	`CREATE OR REPLACE FUNCTION validate_proposed_loan_admin_fee_policy_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.proposed_admin_fee_policy IS NOT NULL AND NOT ((NEW.legacy_terms=TRUE AND NEW.proposed_admin_fee_policy='legacy_flat_monthly') OR (NEW.legacy_terms=FALSE AND (
        (NEW.loan_type='regular' AND NEW.proposed_admin_fee_policy='regular_tiered_monthly_v1') OR
        (NEW.loan_type='secondary_goods' AND NEW.proposed_admin_fee_policy='secondary_goods_one_time_v1') OR
        (NEW.loan_type='goods_purchase_paylater' AND NEW.proposed_admin_fee_policy='goods_purchase_paylater_one_time_v1') OR
        (NEW.loan_type='voucher' AND NEW.proposed_admin_fee_policy='voucher_nominal_one_time_v1') OR
        (NEW.loan_type='retirement' AND NEW.proposed_admin_fee_policy='retirement_manual_one_time_v1')))) THEN
        RAISE EXCEPTION 'proposed loan admin fee policy does not match loan terms';
    END IF;
    RETURN NEW;
END $$`,
	`ALTER TABLE loan_requests DROP CONSTRAINT IF EXISTS loan_requests_voucher_intake_check`,
	`ALTER TABLE loan_requests ADD CONSTRAINT loan_requests_manual_fee_intake_check CHECK (
    (loan_type='voucher' AND creation_source='admin' AND created_by IS NOT NULL AND batch_id IS NOT NULL AND legacy_terms=FALSE AND duration_months=1 AND requested_admin_fee IS NOT NULL AND requested_admin_fee>=0 AND requested_amount<=9223372036854775807-requested_admin_fee AND (proposed_admin_fee_policy IS NULL OR (proposed_admin_fee_policy='voucher_nominal_one_time_v1' AND proposed_total_admin_fee=requested_admin_fee))) OR
    (loan_type='retirement' AND creation_source='admin' AND created_by IS NOT NULL AND legacy_terms=FALSE AND duration_months=1 AND requested_admin_fee IS NOT NULL AND requested_admin_fee>=0 AND requested_amount<=9223372036854775807-requested_admin_fee AND (proposed_admin_fee_policy IS NULL OR (proposed_admin_fee_policy='retirement_manual_one_time_v1' AND proposed_total_admin_fee=requested_admin_fee))) OR
    (loan_type NOT IN ('voucher','retirement') AND requested_admin_fee IS NULL))`,
	`CREATE OR REPLACE FUNCTION protect_voucher_intake() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
    IF NEW.first_due_date IS DISTINCT FROM OLD.first_due_date THEN
        RAISE EXCEPTION 'First due date is immutable';
    END IF;
    IF OLD.loan_type IN ('voucher','retirement') AND (NEW.requested_admin_fee IS DISTINCT FROM OLD.requested_admin_fee OR NEW.creation_source IS DISTINCT FROM OLD.creation_source OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.batch_id IS DISTINCT FROM OLD.batch_id) THEN
        RAISE EXCEPTION 'Manual-fee loan intake and provenance are immutable';
    END IF;
    RETURN NEW;
END $$`,
}
