package app_test

import (
	"database/sql"
	"testing"

	"github.com/sodrety/kopdes/internal/app"
	_ "modernc.org/sqlite"
)

func TestSeedWorkbookMemberTypeSyncMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if err := app.MigrateTo(db, 22); err != nil {
		t.Fatalf("migrate base schema: %v", err)
	}
	for _, member := range []struct {
		memberNo   string
		memberType string
	}{
		{memberNo: "kksuk-000001", memberType: "daily_worker"},
		{memberNo: "kksuk-000050", memberType: "employee"},
		{memberNo: "kksuk-000098", memberType: "employee"},
		{memberNo: "kksuk-000174", memberType: "employee"},
		{memberNo: "NOT-IN-WORKBOOK", memberType: "employee"},
	} {
		_, err := db.Exec(`INSERT INTO members (id, member_no, full_name, join_date, status, member_type) VALUES (?, ?, ?, '2026-01-01', 'active', ?)`,
			member.memberNo, member.memberNo, member.memberNo, member.memberType)
		if err != nil {
			t.Fatalf("insert %s: %v", member.memberNo, err)
		}
	}

	if err := app.MigrateTo(db, 25); err != nil {
		t.Fatalf("migrate member types: %v", err)
	}

	expected := map[string]string{
		"kksuk-000001":    "employee",
		"kksuk-000050":    "contract_worker",
		"kksuk-000098":    "daily_worker",
		"kksuk-000174":    "customer",
		"NOT-IN-WORKBOOK": "employee",
	}
	for memberNo, want := range expected {
		var got string
		if err := db.QueryRow(`SELECT member_type FROM members WHERE member_no = ?`, memberNo).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", memberNo, err)
		}
		if got != want {
			t.Errorf("member %s type = %q, want %q", memberNo, got, want)
		}
	}
}
