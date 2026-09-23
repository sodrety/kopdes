package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterAdminLoans(t *testing.T) {
	loans := []AdminLoan{
		{MemberNo: "KKSUK-000010", FullName: "Dedy Kurniawan", LoanType: "secondary_goods", Status: "active"},
		{MemberNo: "KKSUK-000011", FullName: "Dewi Kartini", LoanType: "goods_purchase_paylater", Status: "paid"},
		{MemberNo: "KKSUK-000012", FullName: "Darmawan", LoanType: "regular", Status: "paid"},
	}

	tests := []struct {
		name     string
		loanType string
		status   string
		search   string
		want     []string
	}{
		{name: "all loans include settled loans", want: []string{"KKSUK-000010", "KKSUK-000011", "KKSUK-000012"}},
		{name: "filter by loan type", loanType: "secondary_goods", want: []string{"KKSUK-000010"}},
		{name: "filter by settled status", status: "paid", want: []string{"KKSUK-000011", "KKSUK-000012"}},
		{name: "case insensitive member search", search: "dEdY", want: []string{"KKSUK-000010"}},
		{name: "combine filters", loanType: "goods_purchase_paylater", status: "paid", search: "kksuk-000011", want: []string{"KKSUK-000011"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := filterAdminLoans(loans, test.loanType, test.status, test.search)
			if len(got) != len(test.want) {
				t.Fatalf("got %d loans, want %d: %+v", len(got), len(test.want), got)
			}
			for i, loan := range got {
				if loan.MemberNo != test.want[i] {
					t.Errorf("loan %d member number = %q, want %q", i, loan.MemberNo, test.want[i])
				}
			}
		})
	}
}

func TestLoanTypeLabelPreservesLegacyLoanType(t *testing.T) {
	tests := []struct {
		lang     string
		typeName string
		want     string
	}{
		{lang: "en", typeName: "secondary_goods", want: "Secondary Goods — Legacy Terms"},
		{lang: "en", typeName: "goods_purchase_paylater", want: "Goods Purchase/Paylater — Legacy Terms"},
		{lang: "id", typeName: "secondary_goods", want: "Barang Sekunder — Ketentuan Lama"},
		{lang: "id", typeName: "goods_purchase_paylater", want: "Pembelian Barang/Paylater — Ketentuan Lama"},
	}

	for _, test := range tests {
		t.Run(test.lang+"/"+test.typeName, func(t *testing.T) {
			var output bytes.Buffer
			data := map[string]any{"Lang": test.lang, "LoanType": test.typeName, "LegacyTerms": true}
			if err := pageTemplates.ExecuteTemplate(&output, "loan-type-label", data); err != nil {
				t.Fatalf("render loan type label: %v", err)
			}
			if got := strings.TrimSpace(output.String()); got != test.want {
				t.Errorf("label = %q, want %q", got, test.want)
			}
		})
	}
}
