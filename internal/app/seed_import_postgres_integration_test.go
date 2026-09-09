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
