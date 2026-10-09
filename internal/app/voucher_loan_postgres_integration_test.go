//go:build postgres_integration

package app

import (
	"os"
	"strings"
	"testing"
)

func TestVoucherPostgresMigrationAndIntakeGuards(t *testing.T) {
	for _, external := range []bool{false, true} {
		name := "application"
		if external {
			name = "sql"
		}
		t.Run(name, func(t *testing.T) {
			db := postgresV14TestDatabase(t)
			if err := MigrateTo(db, 42); err != nil {
				t.Fatal(err)
			}
			if external {
				data, err := os.ReadFile("../../migrations/043_add_voucher_loans.sql")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec(string(data)); err != nil {
					t.Fatal(err)
				}
			} else if err := Migrate(db); err != nil {
				t.Fatal(err)
			}
			if err := EnsureAdminUser(db, "voucher-admin@test.local", "password"); err != nil {
				t.Fatal(err)
			}
			var actor string
			if err := db.QueryRow(`SELECT id FROM users WHERE email='voucher-admin@test.local'`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO members(id,member_no,full_name,join_date,status,member_type,bank_name,bank_account) VALUES('voucher-member','V-PG','Voucher Member','2026-01-01','active','employee','Test','123')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO saving_records(id,member_id,type,category,amount,record_date,recorded_by) VALUES('voucher-saving','voucher-member','deposit','sukarela',1000000,'2026-01-01',$1)`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO loan_request_batches(id,created_by,file_hash,template_version,status) VALUES('voucher-batch',$1,'test','2','committed')`, actor); err != nil {
				t.Fatal(err)
			}
			for index, values := range []string{
				`'member',NULL,'voucher-batch',12345`,
				`'admin',$1,NULL,12345`,
				`'admin',$1,'voucher-batch',NULL`,
				`'admin',$1,'voucher-batch',-1`,
			} {
				query := `INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,creation_source,created_by,batch_id,requested_admin_fee) VALUES('invalid-voucher','voucher-member',100000,1,'Voucher','pending','voucher','manager',` + values + `)`
				var err error
				if strings.Contains(values, "$1") {
					_, err = db.Exec(query, actor)
				} else {
					_, err = db.Exec(query)
				}
				if err == nil {
					t.Fatalf("invalid Voucher case %d accepted", index)
				}
			}
			if _, err := db.Exec(`INSERT INTO loan_requests(id,member_id,requested_amount,duration_months,purpose,status,loan_type,current_approval_stage,creation_source,created_by,batch_id,requested_admin_fee) VALUES('voucher-request','voucher-member',100000,1,'Voucher','pending','voucher','manager','admin',$1,'voucher-batch',12345)`, actor); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE loan_requests SET requested_admin_fee=0 WHERE id='voucher-request'`); err == nil {
				t.Fatal("nominal fee must be immutable")
			}
			if _, err := db.Exec(`UPDATE loan_requests SET proposed_approved_amount=120000,proposed_duration_months=1,proposed_start_date=CURRENT_DATE,proposed_admin_fee_policy='voucher_nominal_one_time_v1',proposed_total_admin_fee=12345,proposed_total_obligation=132345 WHERE id='voucher-request'`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
