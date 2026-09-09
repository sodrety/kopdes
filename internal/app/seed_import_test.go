package app

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sodrety/kopdes/internal/seeddata"
	_ "modernc.org/sqlite"
)

func TestParseSeedMoneyPreservesWholeAndFractionalValues(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		want       int64
		fractional bool
	}{
		{name: "indonesian grouped", raw: "Rp15,000,000", want: 15000000},
		{name: "parenthesized", raw: "(440,000)", want: -440000},
		{name: "decimal comma", raw: "12,5", fractional: true},
		{name: "decimal point", raw: "12.5", fractional: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, fractional, err := parseSeedMoney(test.raw)
			if err != nil {
				t.Fatalf("parseSeedMoney(%q): %v", test.raw, err)
			}
			if got != test.want || fractional != test.fractional {
				t.Fatalf("parseSeedMoney(%q) = (%d, %v), want (%d, %v)", test.raw, got, fractional, test.want, test.fractional)
			}
		})
	}
}

func TestParseSeedMoneyRoundedUsesWholeRupiahRounding(t *testing.T) {
	tests := []struct {
		raw  string
		want int64
	}{
		{raw: "10,278,196.8397077", want: 10278197},
		{raw: "-16,267,444.3805523", want: -16267444},
		{raw: "2,149,999.99999995", want: 2150000},
		{raw: "12.5", want: 13},
	}
	for _, test := range tests {
		got, fractional, err := parseSeedMoneyRounded(test.raw)
		if err != nil {
			t.Fatalf("parseSeedMoneyRounded(%q): %v", test.raw, err)
		}
		if got != test.want || !fractional {
			t.Fatalf("parseSeedMoneyRounded(%q) = (%d, %v), want (%d, true)", test.raw, got, fractional, test.want)
		}
	}
}

func TestMapMemberSourceRules(t *testing.T) {
	tests := []struct {
		sourceStatus string
		status       string
		memberType   string
	}{
		{sourceStatus: "Aktif", status: "active", memberType: "employee"},
		{sourceStatus: "Baru", status: "active", memberType: "employee"},
		{sourceStatus: "Purna Bakti 2025", status: "inactive", memberType: "employee"},
		{sourceStatus: "PHL Distribusi", status: "active", memberType: "daily_worker"},
		{sourceStatus: "Nasabah", status: "active", memberType: "self_employed"},
	}
	for _, test := range tests {
		status, memberType := mapMemberSource(test.sourceStatus)
		if status != test.status || memberType != test.memberType {
			t.Fatalf("mapMemberSource(%q) = (%q, %q), want (%q, %q)", test.sourceStatus, status, memberType, test.status, test.memberType)
		}
	}
}

func TestValidateSeedManifestUsesTemplateMemberTypeAndAllowsMissingBalanceCheckpoints(t *testing.T) {
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KKSUK-000001",
			FullName:     "Template Member",
			SourceStatus: "Aktif",
			MemberType:   "daily_worker",
			JoinDate:     "2026-09-07",
		}},
	}
	prepared, issues := validateSeedManifest(manifest)
	if len(issues) != 0 {
		t.Fatalf("template member with blank balance checkpoints produced issues: %+v", issues)
	}
	if len(prepared.Members) != 1 || prepared.Members[0].MemberType != "daily_worker" {
		t.Fatalf("template member type was not preserved: %+v", prepared.Members)
	}
}

func TestValidateSeedManifestStoresAllTemplateSavingCategoriesAndSkipsUnmatchedRows(t *testing.T) {
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KKSUK-000001",
			FullName:     "Template Member",
			SourceStatus: "Aktif",
			JoinDate:     "2026-09-07",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 5},
			MemberName: "Template Member",
			RecordDate: "2025-12-31",
			Pokok:      "1.5",
			Wajib:      "10,278,196.8397077",
			Sukarela:   "2,149,999.99999995",
			SHU:        "12.5",
			Khusus:     "-40",
		}, {
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 6},
			MemberName: "Unknown Member",
			RecordDate: "2025-12-31",
			Wajib:      "100,000",
		}},
	}
	prepared, issues := validateSeedManifest(manifest)
	if len(prepared.Savings) != 5 {
		t.Fatalf("prepared savings = %d, want 5 categories from the matched row", len(prepared.Savings))
	}
	if len(issues) != 5 {
		t.Fatalf("issues = %d, want four rounding warnings and one unmatched warning: %+v", len(issues), issues)
	}
	for _, issue := range issues {
		if issue.Blocker {
			t.Fatalf("unexpected blocker: %+v", issue)
		}
	}
	categoryAmounts := map[string]int64{}
	for _, saving := range prepared.Savings {
		categoryAmounts[saving.Category] = saving.Amount
	}
	if categoryAmounts["pokok"] != 2 || categoryAmounts["wajib"] != 10278197 || categoryAmounts["sukarela"] != 2150000 || categoryAmounts["shu"] != 13 || categoryAmounts["khusus"] != 40 {
		t.Fatalf("unexpected rounded category amounts: %+v", categoryAmounts)
	}
}

func TestSeedImportStoresExtendedCategoriesSkipsUnmatchedAndCreatesCredentials(t *testing.T) {
	db := v14TestDatabase(t)
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Sources: []seeddata.Source{{Name: "template.xlsx", SHA256: "template-hash"}},
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KKSUK-000001",
			FullName:     "Active Template Member",
			SourceStatus: "Aktif",
			JoinDate:     "2026-09-07",
		}, {
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 6},
			CurrentNPP:   "KKSUK-000002",
			FullName:     "Inactive Template Member",
			SourceStatus: "Purna Bakti 2026",
			JoinDate:     "2026-09-07",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 5},
			MemberName: "Active Template Member",
			RecordDate: "2025-12-31",
			Pokok:      "40.5",
			Wajib:      "10.5",
			Sukarela:   "20",
			SHU:        "30.5",
			Khusus:     "-40.5",
		}, {
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 6},
			MemberName: "Unknown Template Member",
			RecordDate: "2025-12-31",
			Wajib:      "100,000",
		}},
	}
	manifest.SnapshotID = seeddata.SnapshotID(manifest.Sources)
	credentialsPath := filepath.Join(t.TempDir(), "credentials.csv")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	report, err := RunSeedImport(db, manifest, SeedImportOptions{CredentialsPath: credentialsPath, ReportPath: reportPath})
	if err != nil {
		t.Fatalf("RunSeedImport: %v\nreport=%+v", err, report)
	}
	if report.Status != "succeeded" || report.Counts["members"] != 2 || report.Counts["savings"] != 5 || report.Counts["accounts"] != 1 || report.Counts["blockers"] != 0 || report.Counts["warnings"] != 5 {
		t.Fatalf("unexpected extended seed report: %+v", report)
	}
	rows, err := db.Query(`SELECT category,COUNT(*) FROM saving_records GROUP BY category`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	categoryCounts := map[string]int{}
	for rows.Next() {
		var category string
		var count int
		if err := rows.Scan(&category, &count); err != nil {
			t.Fatal(err)
		}
		categoryCounts[category] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(categoryCounts) != 5 || categoryCounts["pokok"] != 1 || categoryCounts["wajib"] != 1 || categoryCounts["sukarela"] != 1 || categoryCounts["shu"] != 1 || categoryCounts["khusus"] != 1 {
		t.Fatalf("unexpected imported saving categories: %+v", categoryCounts)
	}
	credentialFile, err := os.Open(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	credentialRows, err := csv.NewReader(credentialFile).ReadAll()
	_ = credentialFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(credentialRows) != 2 || credentialRows[1][0] != "kksuk-000001" || credentialRows[1][2] != "active-template-member@koperasidj.id" {
		t.Fatalf("unexpected credential rows: %v", credentialRows)
	}
}

func TestSeedImportAppendPreservesExistingBootstrapMember(t *testing.T) {
	db := v14TestDatabase(t)
	if err := MigrateTo(db, 18); err != nil {
		t.Fatalf("prepare existing production schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('bootstrap-member','KETUA-UMUM','Ketua Utama','2025-01-01','active')`); err != nil {
		t.Fatalf("insert bootstrap member: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ('bootstrap-user','ketua-umum@koperasidj.id','existing-hash','member','bootstrap-member','Ketua Utama',TRUE,FALSE,FALSE)`); err != nil {
		t.Fatalf("insert bootstrap user: %v", err)
	}

	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Sources: []seeddata.Source{{Name: "template.xlsx", SHA256: "append-template-hash"}},
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 5},
			CurrentNPP:   "KETUA-UMUM",
			FullName:     "Ketua Utama",
			SourceStatus: "Aktif",
			JoinDate:     "2025-01-01",
		}, {
			Source:       seeddata.SourceRef{Source: "template.xlsx", Sheet: "01_Anggota", Row: 6},
			CurrentNPP:   "KKSUK-000001",
			FullName:     "Appended Member",
			SourceStatus: "Aktif",
			JoinDate:     "2026-09-07",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 5},
			MemberName: "Ketua Utama",
			RecordDate: "2025-12-31",
			Wajib:      "60",
		}, {
			Source:     seeddata.SourceRef{Source: "template.xlsx", Sheet: "03_Simpanan", Row: 6},
			MemberName: "Appended Member",
			RecordDate: "2025-12-31",
			Pokok:      "10",
			Wajib:      "20",
			Sukarela:   "30",
			SHU:        "40",
			Khusus:     "50",
		}},
	}
	manifest.SnapshotID = seeddata.SnapshotID(manifest.Sources)
	credentialsPath := filepath.Join(t.TempDir(), "credentials.csv")

	report, err := RunSeedImport(db, manifest, SeedImportOptions{Append: true, CredentialsPath: credentialsPath})
	if err != nil {
		t.Fatalf("RunSeedImport append: %v\nreport=%+v", err, report)
	}
	if report.Status != "succeeded" || report.Counts["members"] != 2 || report.Counts["savings"] != 6 || report.Counts["accounts"] != 1 {
		t.Fatalf("unexpected append report: %+v", report)
	}
	assertRowCount(t, db, `SELECT COUNT(*) FROM members`, 2)
	assertRowCount(t, db, `SELECT COUNT(*) FROM saving_records`, 6)
	var bootstrapName, bootstrapEmail string
	if err := db.QueryRow(`SELECT members.full_name,users.email FROM members INNER JOIN users ON users.member_id=members.id WHERE members.id='bootstrap-member'`).Scan(&bootstrapName, &bootstrapEmail); err != nil {
		t.Fatalf("read bootstrap identity: %v", err)
	}
	if bootstrapName != "Ketua Utama" || bootstrapEmail != "ketua-umum@koperasidj.id" {
		t.Fatalf("bootstrap identity changed: name=%q email=%q", bootstrapName, bootstrapEmail)
	}
	var maxVersion int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if maxVersion != 19 {
		t.Fatalf("schema max version = %d, want 19", maxVersion)
	}
	credentialFile, err := os.Open(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	credentialRows, err := csv.NewReader(credentialFile).ReadAll()
	_ = credentialFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(credentialRows) != 2 || credentialRows[1][0] != "kksuk-000001" || credentialRows[1][2] != "appended-member@koperasidj.id" {
		t.Fatalf("unexpected append credential rows: %v", credentialRows)
	}

	repeated, err := RunSeedImport(db, manifest, SeedImportOptions{Append: true, CredentialsPath: credentialsPath})
	if err != nil {
		t.Fatalf("repeat RunSeedImport append: %v", err)
	}
	if repeated.Status != "already_succeeded" {
		t.Fatalf("repeat append status = %q, want already_succeeded", repeated.Status)
	}
}

func TestSeedEmailAllocatorUsesNameAndMemberNoForDuplicates(t *testing.T) {
	db := v14TestDatabase(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	allocator, err := newSeedEmailAllocator(tx, []preparedMember{{Source: seeddata.Member{FullName: "Same Name"}, MemberNo: "A-001"}, {Source: seeddata.Member{FullName: "Same Name"}, MemberNo: "B-002"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := allocator.next("Same Name", "A-001"); got != "same-name-a-001@koperasidj.id" {
		t.Fatalf("first duplicate email = %q", got)
	}
	if got := allocator.next("Same Name", "B-002"); got != "same-name-b-002@koperasidj.id" {
		t.Fatalf("second duplicate email = %q", got)
	}
	if got := allocator.next("Dewi Kartini", "C-003"); got != "dewi-kartini@koperasidj.id" {
		t.Fatalf("unique name email = %q", got)
	}
}

func TestRunSeedEmailRenameUpdatesDatabaseAndPreservesPasswords(t *testing.T) {
	db := v14TestDatabase(t)
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('rename-member-a','A-001','Same Name','2026-01-01','active'),('rename-member-b','B-002','Same Name','2026-01-01','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ('rename-user-a','a@koperasidj.id','hash-a','member','rename-member-a','Same Name',TRUE,TRUE,FALSE),('rename-user-b','b@koperasidj.id','hash-b','member','rename-member-b','Same Name',TRUE,TRUE,FALSE)`); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(t.TempDir(), "credentials.csv")
	outputPath := filepath.Join(t.TempDir(), "renamed-credentials.csv")
	input := "member_no,full_name,email,temporary_password\nA-001,Same Name,a@koperasidj.id,password-a\nB-002,Same Name,b@koperasidj.id,password-b\n"
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := RunSeedEmailRename(db, SeedEmailRenameOptions{CredentialsPath: inputPath, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("RunSeedEmailRename: %v", err)
	}
	if report.Status != "succeeded" || report.Updated != 2 {
		t.Fatalf("unexpected rename report: %+v", report)
	}
	var emailA, emailB, hashA, hashB string
	if err := db.QueryRow(`SELECT email,password_hash FROM users WHERE id='rename-user-a'`).Scan(&emailA, &hashA); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT email,password_hash FROM users WHERE id='rename-user-b'`).Scan(&emailB, &hashB); err != nil {
		t.Fatal(err)
	}
	if emailA != "same-name-a-001@koperasidj.id" || emailB != "same-name-b-002@koperasidj.id" || hashA != "hash-a" || hashB != "hash-b" {
		t.Fatalf("unexpected renamed users: %q/%q %q/%q", emailA, emailB, hashA, hashB)
	}
	outputFile, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(outputFile).ReadAll()
	_ = outputFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][2] != "same-name-a-001@koperasidj.id" || rows[1][3] != "password-a" || rows[2][2] != "same-name-b-002@koperasidj.id" || rows[2][3] != "password-b" {
		t.Fatalf("unexpected renamed credentials: %v", rows)
	}
}

func TestSeedImportStagesHistoricalDataAndIsIdempotent(t *testing.T) {
	db := v14TestDatabase(t)
	manifest := seeddata.Manifest{
		Version: seeddata.ManifestVersion,
		Sources: []seeddata.Source{{Name: "fixture.xlsx", SHA256: "fixture-hash"}},
		Members: []seeddata.Member{{
			Source:       seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Master Anggota", Row: 3},
			OldNPP:       "00123",
			CurrentNPP:   "00123",
			FullName:     "Fixture Member",
			SourceStatus: "Aktif",
			JoinDate:     "2025-01-15",
			Balances:     seeddata.MemberAmounts{Pokok: "20,000", Wajib: "100,000", Sukarela: "150,000"},
		}, {
			Source:       seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Master Anggota", Row: 4},
			OldNPP:       "00124",
			CurrentNPP:   "00124",
			FullName:     "Secondary Fixture Member",
			SourceStatus: "Baru",
			JoinDate:     "2025-02-01",
		}},
		Savings: []seeddata.SavingRow{{
			Source:     seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Detail Simpanan", Row: 4},
			MemberName: "Fixture Member",
			RecordDate: "2026-01-23",
			Wajib:      "100,000",
			Sukarela:   "150,000",
		}},
		Loans: []seeddata.Loan{{
			Source:             seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Data Base Pinjaman", Row: 5},
			LoanType:           "regular",
			MemberName:         "Fixture Member",
			Principal:          "1,000,000",
			AdminFee:           "100,000",
			MonthlyInstallment: "100,000",
			DurationMonths:     11,
			StartDate:          "2026-01-01",
		}, {
			Source:             seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Pinjaman Barang Sekunder", Row: 5},
			LoanType:           "secondary_goods",
			MemberName:         "Secondary Fixture Member",
			SourceHint:         "1",
			Principal:          "500,000",
			AdminFee:           "50,000",
			TotalObligation:    "550,000",
			MonthlyInstallment: "183,333",
			DurationMonths:     3,
			StartDate:          "2025-01-01",
			SourceStatus:       "Lunas",
		}},
		LoanEvidence: []seeddata.LoanEvidence{{
			Source:     seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Pinjaman Barang Sekunder", Row: 5},
			MemberName: "Secondary Fixture Member",
			LoanHint:   "1",
			Method:     "3. Cicilan",
			Amount:     "183,333",
			RecordDate: "2025-02-01",
		}, {
			Source:     seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Pinjaman Barang Sekunder", Row: 5},
			MemberName: "Secondary Fixture Member",
			LoanHint:   "1",
			Method:     "3. Cicilan",
			Amount:     "183,333",
			RecordDate: "2025-03-01",
		}, {
			Source:     seeddata.SourceRef{Source: "fixture.xlsx", Sheet: "Pinjaman Barang Sekunder", Row: 5},
			MemberName: "Secondary Fixture Member",
			LoanHint:   "1",
			Method:     "3. Cicilan",
			Amount:     "183,334",
			RecordDate: "2025-04-01",
		}},
	}
	manifest.SnapshotID = seeddata.SnapshotID(manifest.Sources)
	credentialsPath := filepath.Join(t.TempDir(), "credentials.csv")
	reportPath := filepath.Join(t.TempDir(), "report.json")

	report, err := RunSeedImport(db, manifest, SeedImportOptions{CredentialsPath: credentialsPath, ReportPath: reportPath})
	if err != nil {
		t.Fatalf("RunSeedImport: %v\nreport=%+v", err, report)
	}
	if report.Status != "succeeded" || report.Counts["members"] != 2 || report.Counts["savings"] != 3 || report.Counts["loans"] != 2 || report.Counts["installments"] != 14 || report.Counts["accounts"] != 2 {
		t.Fatalf("unexpected seed report: %+v", report)
	}
	var maxVersion int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion != 19 {
		t.Fatalf("schema max version = %d, want 19", maxVersion)
	}
	var legacyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM loans WHERE legacy_terms=TRUE AND admin_fee_policy='legacy_flat_monthly'`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 2 {
		t.Fatalf("legacy imported loan count = %d, want 2", legacyCount)
	}
	var secondaryCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM loans WHERE loan_type='secondary_goods' AND legacy_terms=TRUE`).Scan(&secondaryCount); err != nil {
		t.Fatal(err)
	}
	if secondaryCount != 1 {
		t.Fatalf("secondary imported loan count = %d, want 1", secondaryCount)
	}
	var sourceRecords int
	if err := db.QueryRow(`SELECT COUNT(*) FROM seed_import_records`).Scan(&sourceRecords); err != nil {
		t.Fatal(err)
	}
	if sourceRecords < 3 {
		t.Fatalf("seed audit records = %d, want source and operational records", sourceRecords)
	}
	info, err := os.Stat(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %o, want 600", info.Mode().Perm())
	}
	file, err := os.Open(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(file).ReadAll()
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][2] != "fixture-member@koperasidj.id" || strings.TrimSpace(rows[1][3]) == "" || rows[2][2] != "secondary-fixture-member@koperasidj.id" || strings.TrimSpace(rows[2][3]) == "" {
		t.Fatalf("unexpected credentials output: %v", rows)
	}

	retry, err := RunSeedImport(db, manifest, SeedImportOptions{CredentialsPath: filepath.Join(t.TempDir(), "retry.csv")})
	if err != nil || retry.Status != "already_succeeded" {
		t.Fatalf("idempotent rerun = %+v, err=%v", retry, err)
	}
	var userCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE member_id IS NOT NULL`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 2 {
		t.Fatalf("member account count after rerun = %d, want 2", userCount)
	}
}
