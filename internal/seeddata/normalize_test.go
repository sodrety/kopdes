package seeddata

import (
	"os"
	"testing"
)

func TestTemplateMemberTypeFollowsWorkbookCategories(t *testing.T) {
	tests := map[string]string{
		"Pegawai": "employee",
		"PKWT":    "contract_worker",
		"PHL":     "daily_worker",
		"Nasabah": "customer",
	}
	for source, want := range tests {
		if got := templateMemberType(source); got != want {
			t.Fatalf("templateMemberType(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestNormalizeIsDeterministicForSeedWorkbooks(t *testing.T) {
	primary := "../../docs/seed-data/01. Data Base Simpanan dan Pinjaman Koperasi Dharma Jaya tahun buku 2026 Rev2.xlsx"
	secondary := "../../docs/seed-data/Rekap Pinjaman Sekunder KKSUK 2026.xlsx"
	requireSeedWorkbookFixtures(t, primary, secondary)
	first, err := Normalize(primary, secondary)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Normalize(primary, secondary)
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := second.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("normalization is not deterministic: %s != %s", firstHash, secondHash)
	}
	if len(first.Members) != 286 || len(first.Savings) != 1740 || len(first.Loans) != 380 || len(first.LoanEvidence) != 1969 {
		t.Fatalf("unexpected normalized counts: members=%d savings=%d loans=%d evidence=%d", len(first.Members), len(first.Savings), len(first.Loans), len(first.LoanEvidence))
	}
	fallbackJoinDates := 0
	for _, member := range first.Members {
		if member.JoinDate == "" {
			t.Fatalf("member %q has no normalized join date", member.FullName)
		}
		if member.JoinDate == fallbackJoinDate {
			fallbackJoinDates++
		}
	}
	if fallbackJoinDates != 225 {
		t.Fatalf("unexpected fallback join-date count: %d", fallbackJoinDates)
	}
}

func TestNormalizeSimpananTemplateWorkbook(t *testing.T) {
	primary := "../../docs/seed-data/seed-source-template-simpanan.xlsx"
	requireSeedWorkbookFixtures(t, primary)
	manifest, err := Normalize(primary, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Members) != 209 || len(manifest.Savings) != 1740 {
		t.Fatalf("unexpected template counts: members=%d savings=%d", len(manifest.Members), len(manifest.Savings))
	}
	if len(manifest.Loans) != 0 || len(manifest.LoanEvidence) != 0 {
		t.Fatalf("template normalization should be limited to anggota and simpanan: loans=%d evidence=%d", len(manifest.Loans), len(manifest.LoanEvidence))
	}
	if manifest.Members[0].Source.Sheet != "01_Anggota" || manifest.Members[0].MemberType != "employee" || manifest.Members[0].CurrentNPP == "" {
		t.Fatalf("unexpected first template member: %+v", manifest.Members[0])
	}
	if manifest.Savings[0].Source.Sheet != "03_Simpanan" || manifest.Savings[0].RecordDate != openingBalanceDate {
		t.Fatalf("unexpected first template saving: %+v", manifest.Savings[0])
	}
	for _, member := range manifest.Members {
		if member.JoinDate == "" {
			t.Fatalf("template member %q has no normalized join date", member.FullName)
		}
	}
}

func TestNormalizeLoanTemplateWorkbook(t *testing.T) {
	primary := "../../docs/seed-data/seed-source-template-pinjaman.xlsx"
	requireSeedWorkbookFixtures(t, primary)
	manifest, err := Normalize(primary, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Members) != 209 || len(manifest.Savings) != 1735 || len(manifest.Loans) != 77 || len(manifest.LoanEvidence) != 519 {
		t.Fatalf("unexpected loan template counts: members=%d savings=%d loans=%d evidence=%d", len(manifest.Members), len(manifest.Savings), len(manifest.Loans), len(manifest.LoanEvidence))
	}
	if manifest.Loans[0].Source.Sheet != "04_Pinjaman" || manifest.Loans[0].LoanType != "regular" || manifest.Loans[0].SourceHint != "1" || manifest.Loans[0].TotalObligation == "" {
		t.Fatalf("unexpected first template loan: %+v", manifest.Loans[0])
	}
	if manifest.LoanEvidence[0].Source.Sheet != "05_Angsuran" || manifest.LoanEvidence[0].LoanHint != "1" || manifest.LoanEvidence[0].Method != "3. Cicilan" {
		t.Fatalf("unexpected first template repayment: %+v", manifest.LoanEvidence[0])
	}
}

func requireSeedWorkbookFixtures(t *testing.T, paths ...string) {
	t.Helper()
	if os.Getenv("KOPDES_SEED_WORKBOOK_TESTS") != "1" {
		t.Skip("seed workbook regression tests are opt-in; set KOPDES_SEED_WORKBOOK_TESTS=1 when the source workbooks are available")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("seed workbook fixture %s is unavailable: %v", path, err)
		}
	}
}
