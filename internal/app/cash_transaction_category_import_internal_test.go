package app

import (
	"bytes"
	"testing"
)

func TestParseEmbeddedCashTransactionCategoryTemplate(t *testing.T) {
	rows, err := parseCashTransactionCategoryTemplate(bytes.NewReader(cashTransactionCategoryTemplateXLSX))
	if err != nil {
		t.Fatalf("parse embedded category template: %v", err)
	}
	if len(rows) != 139 {
		t.Fatalf("expected 139 template rows, got %d", len(rows))
	}
}
