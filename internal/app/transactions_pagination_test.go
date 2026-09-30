package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminTransactionsPageUsesServerSidePagination(t *testing.T) {
	fixture := newTestFixture(t)
	adminCookie := fixture.browserLogin(t, "admin@coop.test", "password")

	var adminID string
	if err := fixture.db.QueryRow(`SELECT id FROM users WHERE email=$1`, "admin@coop.test").Scan(&adminID); err != nil {
		t.Fatalf("find admin user: %v", err)
	}
	if _, err := fixture.db.Exec(`INSERT INTO coa_accounts (id,code,name,account_type,subtype,normal_balance,is_group,active) VALUES ('pagination-income','PAGINATION-INCOME','Pagination income','revenue','Laba Rugi','C',FALSE,TRUE)`); err != nil {
		t.Fatalf("seed pagination income COA: %v", err)
	}
	for i := 0; i < 55; i++ {
		transactionID := fmt.Sprintf("manual-page-%03d", i)
		journalID := "journal-" + transactionID
		transactionDate := fmt.Sprintf("2026-%02d-01", 1+i/5)
		description := fmt.Sprintf("Pagination row %03d", i)
		referenceNo := fmt.Sprintf("PAGE-%03d", i)
		amount := int64(i + 1)
		if _, err := fixture.db.Exec(`
			INSERT INTO manual_cash_transactions
				(id, transaction_date, direction, category_id, description, amount, reference_no, note, recorded_by, source, coa_code, accounting_direction)
			VALUES ($1, $2, 'cash_in', NULL, $3, $4, $5, '', $6, 'bank', 'BANK', 'debit')`,
			transactionID,
			transactionDate,
			description,
			amount,
			referenceNo,
			adminID,
		); err != nil {
			t.Fatalf("insert pagination row %d: %v", i, err)
		}
		if _, err := fixture.db.Exec(`INSERT INTO financial_journal_entries
			(id,reference_no,transaction_id,transaction_type,transaction_date,source,amount,status,description,recorded_by)
			VALUES ($1,$2,$3,'manual',$4,'bank',$5,'posted',$6,$7)`, journalID, referenceNo, transactionID, transactionDate, amount, description, adminID); err != nil {
			t.Fatalf("insert pagination journal %d: %v", i, err)
		}
		if _, err := fixture.db.Exec(`INSERT INTO financial_journal_lines (id,journal_id,side,coa_code,amount,component) VALUES
			($1,$2,'debit','BANK',$3,'manual'),($4,$2,'credit','PAGINATION-INCOME',$3,'manual')`, "line-debit-"+transactionID, journalID, amount, "line-credit-"+transactionID); err != nil {
			t.Fatalf("insert pagination journal lines %d: %v", i, err)
		}
	}

	requestPage := func(page string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/admin/transactions?page="+page, nil)
		req.AddCookie(adminCookie)
		rec := httptest.NewRecorder()
		fixture.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected transactions page %s status 200, got %d: %s", page, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	firstPage := requestPage("1")
	if got := strings.Count(firstPage, `name="ids"`); got != 50 {
		t.Fatalf("expected first page to render 50 transaction rows, got %d", got)
	}
	if !strings.Contains(firstPage, "Page 1 / 2") || !strings.Contains(firstPage, "Pagination row 054") || strings.Contains(firstPage, "Pagination row 000") {
		t.Fatalf("expected first page to contain only the newest page of transactions, got %s", firstPage)
	}
	if !strings.Contains(firstPage, `href="/admin/transactions?page=2"`) {
		t.Fatalf("expected first page to link to page 2, got %s", firstPage)
	}

	secondPage := requestPage("2")
	if got := strings.Count(secondPage, `name="ids"`); got != 5 {
		t.Fatalf("expected second page to render 5 transaction rows, got %d", got)
	}
	if !strings.Contains(secondPage, "Page 2 / 2") || !strings.Contains(secondPage, "Pagination row 000") {
		t.Fatalf("expected second page to contain the remaining transactions, got %s", secondPage)
	}
	if !strings.Contains(secondPage, `href="/admin/transactions?page=1"`) {
		t.Fatalf("expected second page to link back to page 1, got %s", secondPage)
	}
}
