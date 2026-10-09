package app

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// addVoucherLoans preserves all existing guards while extending the product
// and policy identities. SQLite needs table rebuilds to extend column CHECKs.
func addVoucherLoans(tx *sql.Tx, isSQLite bool) error {
	if !isSQLite {
		for _, statement := range postgresVoucherLoanStatements {
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
	err = rows.Err()
	rows.Close()
	if err != nil {
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
		definition = extendSQLiteVoucherDefinition(definition)
		for _, statement := range []string{
			`CREATE TEMP TABLE ` + quote(table+"_voucher_backup") + ` AS SELECT * FROM ` + quote(table),
			`DROP TABLE ` + quote(table),
			definition,
			`INSERT INTO ` + quote(table) + ` SELECT * FROM ` + quote(table+"_voucher_backup"),
			`DROP TABLE ` + quote(table+"_voucher_backup"),
		} {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("rebuild %s for Voucher: %w", table, err)
			}
		}
	}
	if _, err := tx.Exec(`ALTER TABLE loan_requests ADD COLUMN requested_admin_fee BIGINT NULL`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE loan_request_batch_rows ADD COLUMN admin_fee BIGINT NULL`); err != nil {
		return err
	}
	for _, item := range objects {
		definition := item.definition
		if item.table == "loan_requests" || item.table == "loans" {
			definition = extendSQLiteVoucherDefinition(definition)
		}
		if _, err := tx.Exec(definition); err != nil {
			return fmt.Errorf("restore %s: %w", item.name, err)
		}
	}
	requestCheck := `(NEW.loan_type='voucher' AND (NEW.creation_source<>'admin' OR NEW.created_by IS NULL OR NEW.batch_id IS NULL OR NEW.legacy_terms<>0 OR NEW.duration_months<>1 OR NEW.requested_admin_fee IS NULL OR typeof(NEW.requested_admin_fee)<>'integer' OR NEW.requested_admin_fee<0 OR NEW.requested_amount>9223372036854775807-NEW.requested_admin_fee)) OR (NEW.loan_type<>'voucher' AND NEW.requested_admin_fee IS NOT NULL)`
	snapshotCheck := `NEW.loan_type='voucher' AND NEW.proposed_admin_fee_policy IS NOT NULL AND (NEW.proposed_admin_fee_policy<>'voucher_nominal_one_time_v1' OR NEW.proposed_duration_months<>1 OR NEW.proposed_monthly_admin_fee IS NOT NULL OR NEW.proposed_total_admin_fee IS NULL OR NEW.proposed_total_admin_fee<>NEW.requested_admin_fee OR NEW.proposed_approved_amount IS NULL OR NEW.proposed_approved_amount<=0 OR NEW.proposed_approved_amount>9223372036854775807-NEW.requested_admin_fee OR NEW.proposed_total_obligation IS NULL OR NEW.proposed_total_obligation-NEW.proposed_approved_amount<>NEW.requested_admin_fee)`
	loanCheck := `NEW.loan_type='voucher' AND (NEW.legacy_terms<>0 OR NEW.admin_fee_policy<>'voucher_nominal_one_time_v1' OR NEW.duration_months<>1 OR NEW.monthly_admin_fee IS NOT NULL OR NEW.total_admin_fee IS NULL OR typeof(NEW.total_admin_fee)<>'integer' OR NEW.total_admin_fee<0 OR NEW.total_obligation IS NULL OR NEW.total_obligation-NEW.approved_amount<>NEW.total_admin_fee)`
	for _, statement := range []string{
		`CREATE TRIGGER loan_requests_voucher_insert BEFORE INSERT ON loan_requests WHEN ` + requestCheck + ` BEGIN SELECT RAISE(ABORT,'invalid Voucher intake'); END`,
		`CREATE TRIGGER loan_requests_voucher_update BEFORE UPDATE ON loan_requests WHEN ` + requestCheck + ` OR (OLD.loan_type='voucher' AND (NEW.requested_admin_fee IS NOT OLD.requested_admin_fee OR NEW.creation_source<>OLD.creation_source OR NEW.created_by IS NOT OLD.created_by OR NEW.batch_id IS NOT OLD.batch_id)) BEGIN SELECT RAISE(ABORT,'invalid Voucher intake'); END`,
		`CREATE TRIGGER loan_requests_voucher_snapshot BEFORE UPDATE ON loan_requests WHEN ` + snapshotCheck + ` BEGIN SELECT RAISE(ABORT,'invalid Voucher snapshot'); END`,
		`CREATE TRIGGER loans_voucher_insert BEFORE INSERT ON loans WHEN ` + loanCheck + ` BEGIN SELECT RAISE(ABORT,'invalid Voucher terms'); END`,
		`CREATE TRIGGER loans_voucher_update BEFORE UPDATE ON loans WHEN ` + loanCheck + ` BEGIN SELECT RAISE(ABORT,'invalid Voucher terms'); END`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func extendSQLiteVoucherDefinition(definition string) string {
	definition = strings.ReplaceAll(definition, `'regular','secondary_goods','goods_purchase_paylater'`, `'regular','secondary_goods','goods_purchase_paylater','voucher'`)
	definition = strings.ReplaceAll(definition, `'goods_purchase_paylater_one_time_v1','legacy_flat_monthly'`, `'goods_purchase_paylater_one_time_v1','voucher_nominal_one_time_v1','legacy_flat_monthly'`)
	// Identity guards appear in both flat and nested legacy/nonlegacy forms.
	identity := regexp.MustCompile(`\(NEW\.loan_type='goods_purchase_paylater' AND (?:NEW\.legacy_terms=0 AND )?NEW\.(proposed_)?admin_fee_policy='goods_purchase_paylater_one_time_v1'\)`)
	return identity.ReplaceAllStringFunc(definition, func(match string) string {
		voucher := strings.ReplaceAll(match, "goods_purchase_paylater_one_time_v1", "voucher_nominal_one_time_v1")
		voucher = strings.ReplaceAll(voucher, "goods_purchase_paylater", "voucher")
		return match + " OR " + voucher
	})
}
