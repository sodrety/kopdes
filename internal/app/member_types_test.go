package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sodrety/kopdes/internal/app"
	_ "modernc.org/sqlite"
)

func TestMemberTypeMigrationBackfillsExistingMembers(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE members (
		id TEXT PRIMARY KEY,
		member_no TEXT NOT NULL UNIQUE,
		full_name TEXT NOT NULL,
		phone TEXT NOT NULL DEFAULT '',
		address TEXT NOT NULL DEFAULT '',
		join_date TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create legacy members table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status) VALUES ('legacy-member','LEGACY-1','Legacy Member','2026-01-01','inactive')`); err != nil {
		t.Fatalf("insert legacy member: %v", err)
	}
	for version := 1; version <= 16; version++ {
		if _, err := db.Exec(`INSERT INTO schema_migrations (version,name) VALUES ($1,$2)`, version, fmt.Sprintf("legacy_%d", version)); err != nil {
			t.Fatalf("mark migration %d applied: %v", version, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version,name) VALUES (18,'legacy_18')`); err != nil {
		t.Fatalf("mark unrelated Loan Amount Limit migration applied: %v", err)
	}

	if err := app.Migrate(db); err != nil {
		t.Fatalf("apply Member Type migration: %v", err)
	}
	var memberType string
	if err := db.QueryRow(`SELECT member_type FROM members WHERE id='legacy-member'`).Scan(&memberType); err != nil {
		t.Fatalf("read backfilled Member Type: %v", err)
	}
	if memberType != "employee" {
		t.Fatalf("expected existing Member to migrate to employee, got %q", memberType)
	}
	if _, err := db.Exec(`UPDATE members SET member_type='unknown' WHERE id='legacy-member'`); err == nil {
		t.Fatal("expected database to reject an unknown Member Type")
	}
}

func TestMemberTypeDefaultsUpdatesAndUsesCurrentBahasaLabelInReports(t *testing.T) {
	fixture := newTestFixture(t)
	managerToken := fixture.login(t, "admin@coop.test", "password")

	defaultMember := fixture.createMember(t, managerToken, `{"member_no":"TYPE-DEFAULT","full_name":"Default Type","join_date":"2026-07-15","status":"suspended"}`)
	defaultDetailReq := httptest.NewRequest(http.MethodGet, "/api/admin/members/"+defaultMember.ID, nil)
	defaultDetailReq.Header.Set("Authorization", "Bearer "+managerToken)
	defaultDetailRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(defaultDetailRec, defaultDetailReq)
	if defaultDetailRec.Code != http.StatusOK {
		t.Fatalf("get default Member Type: %d %s", defaultDetailRec.Code, defaultDetailRec.Body.String())
	}
	var defaultDetail struct {
		MemberType      string `json:"member_type"`
		MemberTypeLabel string `json:"member_type_label"`
	}
	if err := json.Unmarshal(defaultDetailRec.Body.Bytes(), &defaultDetail); err != nil {
		t.Fatalf("decode default Member Type: %v", err)
	}
	if defaultDetail.MemberType != "employee" || defaultDetail.MemberTypeLabel != "Karyawan" {
		t.Fatalf("unexpected default Member Type mapping: %+v", defaultDetail)
	}
	suspendedUpdateReq := httptest.NewRequest(http.MethodPost, "/api/admin/members/"+defaultMember.ID+"/type", strings.NewReader(`{"member_type":"daily_worker"}`))
	suspendedUpdateReq.Header.Set("Content-Type", "application/json")
	suspendedUpdateReq.Header.Set("Authorization", "Bearer "+managerToken)
	suspendedUpdateRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(suspendedUpdateRec, suspendedUpdateReq)
	if suspendedUpdateRec.Code != http.StatusOK || !strings.Contains(suspendedUpdateRec.Body.String(), `"member_type_label":"PHL"`) {
		t.Fatalf("expected suspended Member Type to be changeable: %d %s", suspendedUpdateRec.Code, suspendedUpdateRec.Body.String())
	}

	member := fixture.createMember(t, managerToken, `{"member_no":"TYPE-PHL","full_name":"Report Type","join_date":"2026-07-15","status":"active","member_type":"daily_worker","email":"member-type@coop.test","password":"member-password"}`)
	savingReq := httptest.NewRequest(http.MethodPost, "/api/admin/savings", bytes.NewBufferString(`{"member_id":"`+member.ID+`","type":"deposit","category":"wajib","amount":150000,"record_date":"2026-07-15"}`))
	savingReq.Header.Set("Content-Type", "application/json")
	savingReq.Header.Set("Authorization", "Bearer "+managerToken)
	savingRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(savingRec, savingReq)
	if savingRec.Code != http.StatusCreated {
		t.Fatalf("record saving for Member Type report: %d %s", savingRec.Code, savingRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPost, "/api/admin/members/"+member.ID+"/type", strings.NewReader(`{"member_type":"self_employed"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Authorization", "Bearer "+managerToken)
	updateRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update Member Type: %d %s", updateRec.Code, updateRec.Body.String())
	}
	var updated struct {
		MemberType      string `json:"member_type"`
		MemberTypeLabel string `json:"member_type_label"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated Member Type: %v", err)
	}
	if updated.MemberType != "self_employed" || updated.MemberTypeLabel != "Mandiri" {
		t.Fatalf("unexpected updated Member Type mapping: %+v", updated)
	}

	memberToken := fixture.login(t, "member-type@coop.test", "member-password")
	profileReq := httptest.NewRequest(http.MethodGet, "/api/member/profile", nil)
	profileReq.Header.Set("Authorization", "Bearer "+memberToken)
	profileRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("get member profile with Member Type: %d %s", profileRec.Code, profileRec.Body.String())
	}
	if body := profileRec.Body.String(); !strings.Contains(body, `"member_type":"self_employed"`) || !strings.Contains(body, `"member_type_label":"Mandiri"`) {
		t.Fatalf("expected member profile to expose the consistent mapping, got %s", body)
	}

	reportsReq := httptest.NewRequest(http.MethodGet, "/api/admin/reports", nil)
	reportsReq.Header.Set("Authorization", "Bearer "+managerToken)
	reportsRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(reportsRec, reportsReq)
	if reportsRec.Code != http.StatusOK {
		t.Fatalf("get operational reports: %d %s", reportsRec.Code, reportsRec.Body.String())
	}
	var reports struct {
		SavingsByMember []struct {
			MemberNo        string `json:"member_no"`
			MemberType      string `json:"member_type"`
			MemberTypeLabel string `json:"member_type_label"`
		} `json:"SavingsByMember"`
	}
	if err := json.Unmarshal(reportsRec.Body.Bytes(), &reports); err != nil {
		t.Fatalf("decode operational reports: %v", err)
	}
	if len(reports.SavingsByMember) != 1 || reports.SavingsByMember[0].MemberNo != "TYPE-PHL" || reports.SavingsByMember[0].MemberType != "self_employed" || reports.SavingsByMember[0].MemberTypeLabel != "Mandiri" {
		t.Fatalf("expected report to use current Member Type, got %+v", reports.SavingsByMember)
	}

	csvReq := httptest.NewRequest(http.MethodGet, "/api/admin/exports/savings.csv", nil)
	csvReq.Header.Set("Authorization", "Bearer "+managerToken)
	csvRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(csvRec, csvReq)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("export savings CSV: %d %s", csvRec.Code, csvRec.Body.String())
	}
	if body := csvRec.Body.String(); !strings.Contains(body, "Member type") || !strings.Contains(body, "Mandiri") || strings.Contains(body, "self_employed") {
		t.Fatalf("expected CSV to expose the Bahasa Member Type label only, got %s", body)
	}
}
