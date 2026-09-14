//go:build postgres_integration

package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sodrety/kopdes/internal/seeddata"
)

func TestSeedImportPostgresStoresSavingTimestamp(t *testing.T) {
	db := postgresV14TestDatabase(t)
	if err := MigrateTo(db, 18); err != nil {
		t.Fatalf("prepare PostgreSQL production schema: %v", err)
	}
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Sources: []seeddata.Source{{Name: "template.xlsx", SHA256: "postgres-template-hash"}},
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KKSUK-TEST-001",
			FullName:     "PostgreSQL Template Member",
			SourceStatus: "Aktif",
			JoinDate:     "2026-09-07",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 5},
			MemberName: "PostgreSQL Template Member",
			RecordDate: "2025-12-31",
			Wajib:      "100,000",
		}},
	}
	manifest.SnapshotID = seeddata.SnapshotID(manifest.Sources)
	report, err := RunSeedImport(db, manifest, SeedImportOptions{
		Append:          true,
		CredentialsPath: filepath.Join(t.TempDir(), "credentials.csv"),
		ReportPath:      filepath.Join(t.TempDir(), "report.json"),
	})
	if err != nil {
		t.Fatalf("RunSeedImport append: %v\nreport=%+v", err, report)
	}
	if report.Status != "succeeded" || report.Counts["savings"] != 1 {
		t.Fatalf("unexpected PostgreSQL append report: %+v", report)
	}
	var createdAt time.Time
	if err := db.QueryRow(`SELECT created_at FROM saving_records LIMIT 1`).Scan(&createdAt); err != nil {
		t.Fatalf("read PostgreSQL saving timestamp: %v", err)
	}
	if createdAt.IsZero() {
		t.Fatal("PostgreSQL saving timestamp is zero")
	}
}

func TestSeedImportPostgresHistoricalLoansPreservesLegacyTerms(t *testing.T) {
	db := postgresV14TestDatabase(t)
	if err := MigrateTo(db, 18); err != nil {
		t.Fatalf("prepare PostgreSQL production schema: %v", err)
	}
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Sources: []seeddata.Source{{Name: "historical-loans.xlsx", SHA256: "historical-loans-template-hash"}},
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "historical-loans.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KKSUK-HISTORY-001",
			FullName:     "Historical Loan Member",
			SourceStatus: "Aktif",
			JoinDate:     "2026-01-01",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "historical-loans.xlsx", Sheet: "03_Simpanan", Row: 5},
			MemberName: "Historical Loan Member",
			RecordDate: "2025-12-31",
			Wajib:      "1",
		}},
		Loans: []seeddata.Loan{{
			Source:             seeddata.SourceRef{Source: "historical-loans.xlsx", Sheet: "04_Pinjaman", Row: 5},
			LoanType:           "regular",
			MemberName:         "Historical Loan Member",
			SourceHint:         "1",
			Principal:          "100",
			AdminFee:           "20",
			TotalObligation:    "120",
			MonthlyInstallment: "120",
			DurationMonths:     1,
			StartDate:          "2026-01-01",
			SourceStatus:       "Lunas",
		}},
		LoanEvidence: []seeddata.LoanEvidence{{
			Source:     seeddata.SourceRef{Source: "historical-loans.xlsx", Sheet: "05_Angsuran", Row: 5},
			MemberName: "Historical Loan Member",
			LoanHint:   "1",
			Method:     "3. Cicilan",
			Amount:     "120",
			RecordDate: "2026-01-31",
		}},
	}
	manifest.SnapshotID = seeddata.SnapshotID(manifest.Sources)
	report, err := RunSeedImport(db, manifest, SeedImportOptions{
		Append:          true,
		HistoricalLoans: true,
		CredentialsPath: filepath.Join(t.TempDir(), "credentials.csv"),
		ReportPath:      filepath.Join(t.TempDir(), "report.json"),
	})
	if err != nil {
		t.Fatalf("RunSeedImport historical loans: %v\nreport=%+v", err, report)
	}
	if report.Status != "succeeded" || report.Counts["loans"] != 1 || report.Counts["repayments"] != 1 || report.Counts["savings"] != 0 {
		t.Fatalf("unexpected historical loan report: %+v", report)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM saving_records`, 0)
	assertRowCount(t, db, `SELECT COUNT(*) FROM seed_import_records WHERE entity='saving_source'`, 1)
	var loanType string
	var legacyTerms bool
	var adminFeePolicy string
	var totalAdminFee int64
	if err := db.QueryRow(`SELECT loan_type,legacy_terms,admin_fee_policy,total_admin_fee FROM loans LIMIT 1`).Scan(&loanType, &legacyTerms, &adminFeePolicy, &totalAdminFee); err != nil {
		t.Fatal(err)
	}
	if loanType != "regular" || !legacyTerms || adminFeePolicy != "legacy_flat_monthly" || totalAdminFee != 20 {
		t.Fatalf("unexpected historical loan terms: type=%q legacy=%v policy=%q admin=%d", loanType, legacyTerms, adminFeePolicy, totalAdminFee)
	}
	var disabledTriggers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pg_trigger WHERE tgrelid IN ('loan_requests'::regclass,'loans'::regclass) AND NOT tgisinternal AND tgenabled <> 'O'`).Scan(&disabledTriggers); err != nil {
		t.Fatal(err)
	}
	if disabledTriggers != 0 {
		t.Fatalf("historical loan triggers left disabled: %d", disabledTriggers)
	}
	var approvedTotal, obligationTotal, remainingTotal, repaymentsTotal, scheduledTotal, paidTotal int64
	if err := db.QueryRow(`SELECT loans_approved_total,loans_obligation_total,loans_remaining_total,loan_repayments_total,loan_installments_scheduled_total,loan_installments_paid_total FROM monetary_aggregate_totals`).Scan(&approvedTotal, &obligationTotal, &remainingTotal, &repaymentsTotal, &scheduledTotal, &paidTotal); err != nil {
		t.Fatal(err)
	}
	if approvedTotal != 100 || obligationTotal != 120 || remainingTotal != 0 || repaymentsTotal != 120 || scheduledTotal != 120 || paidTotal != 120 {
		t.Fatalf("monetary aggregate totals = %d/%d/%d/%d/%d/%d", approvedTotal, obligationTotal, remainingTotal, repaymentsTotal, scheduledTotal, paidTotal)
	}
}
