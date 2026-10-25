//go:build postgres_integration

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminLoanBatchCommitPostgresAllowsAdminLimitOverrideWithoutBankDetails(t *testing.T) {
	db := postgresV14TestDatabase(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("apply current PostgreSQL migrations: %v", err)
	}
	if err := EnsureAdminUser(db, "loan-batch-admin@test.local", "password"); err != nil {
		t.Fatalf("create manager actor: %v", err)
	}
	var actorID string
	if err := db.QueryRow(`SELECT id FROM users WHERE email='loan-batch-admin@test.local'`).Scan(&actorID); err != nil {
		t.Fatalf("load manager actor: %v", err)
	}

	memberID := newID()
	memberNo := "BULK-PG-OVERRIDE"
	if _, err := db.Exec(`INSERT INTO members (id,member_no,full_name,join_date,status,member_type,bank_name,bank_account) VALUES ($1,$2,'Postgres Batch Borrower','2026-01-01','active','employee','','')`, memberID, memberNo); err != nil {
		t.Fatalf("create borrower without bank details: %v", err)
	}
	batchID := newID()
	if _, err := db.Exec(`INSERT INTO loan_request_batches (id,created_by,file_hash,template_version,status,source_file_name,row_count,preview_expires_at) VALUES ($1,$2,$3,'2','preview','batch.xlsx',1,CURRENT_TIMESTAMP + INTERVAL '15 minutes')`, batchID, actorID, newID()); err != nil {
		t.Fatalf("create preview batch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO loan_request_batch_rows (id,batch_id,excel_row,member_no,full_name,loan_type,requested_amount,duration_months,purpose,status) VALUES ($1,$2,5,$3,'Postgres Batch Borrower','regular',70000000,24,'Admin limit override','valid')`, newID(), batchID, memberNo); err != nil {
		t.Fatalf("create preview row: %v", err)
	}

	server := NewServer(Config{JWTSecret: "0123456789abcdef0123456789abcdef"}, db)
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"loan-batch-admin@test.local","password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	server.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("manager login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var loginPayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode manager login: %v", err)
	}
	if loginPayload.Token == "" {
		t.Fatal("expected manager login token")
	}
	commit := httptest.NewRequest(http.MethodPost, "/api/admin/loan-requests/bulk/"+batchID+"/commit", strings.NewReader("acknowledge_warnings=on"))
	commit.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	commit.Header.Set("Authorization", "Bearer "+loginPayload.Token)
	commit.Header.Set("HX-Request", "true")
	commitResponse := httptest.NewRecorder()
	server.ServeHTTP(commitResponse, commit)
	if commitResponse.Code != http.StatusNoContent {
		t.Fatalf("high-value PostgreSQL batch commit status=%d body=%s", commitResponse.Code, commitResponse.Body.String())
	}
	var requestedAmount int64
	var creationSource string
	if err := db.QueryRow(`SELECT requested_amount,creation_source FROM loan_requests WHERE batch_id=$1`, batchID).Scan(&requestedAmount, &creationSource); err != nil {
		t.Fatalf("load committed PostgreSQL loan request: %v", err)
	}
	if requestedAmount != 70000000 || creationSource != "admin" {
		t.Fatalf("unexpected PostgreSQL loan request: amount=%d source=%q", requestedAmount, creationSource)
	}
}
