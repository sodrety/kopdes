package seeddata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const ManifestVersion = 1

type Manifest struct {
	Version         int            `json:"version"`
	SnapshotID      string         `json:"snapshot_id"`
	Sources         []Source       `json:"sources"`
	Members         []Member       `json:"members"`
	Savings         []SavingRow    `json:"savings"`
	Loans           []Loan         `json:"loans"`
	LoanEvidence    []LoanEvidence `json:"loan_evidence"`
	ExcludedSources []Source       `json:"excluded_sources"`
}

type Source struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type SourceRef struct {
	Source string `json:"source"`
	Sheet  string `json:"sheet"`
	Row    int    `json:"row"`
}

func (r SourceRef) Key() string {
	return fmt.Sprintf("%s:%s:%d", r.Source, r.Sheet, r.Row)
}

type Member struct {
	Source       SourceRef     `json:"source"`
	OldNPP       string        `json:"old_npp"`
	CurrentNPP   string        `json:"current_npp"`
	FullName     string        `json:"full_name"`
	SourceStatus string        `json:"source_status"`
	MemberType   string        `json:"member_type,omitempty"`
	JoinDate     string        `json:"join_date"`
	Area         string        `json:"area"`
	Balances     MemberAmounts `json:"balances"`
}

type MemberAmounts struct {
	Pokok    string `json:"pokok"`
	Wajib    string `json:"wajib"`
	Sukarela string `json:"sukarela"`
	PKPRI    string `json:"pkpri"`
	Khusus   string `json:"khusus"`
}

type SavingRow struct {
	Source       SourceRef `json:"source"`
	MemberName   string    `json:"member_name"`
	SourceNPP    string    `json:"source_npp"`
	RecordDate   string    `json:"record_date"`
	SourceMarker string    `json:"source_marker"`
	Month        string    `json:"month"`
	Description  string    `json:"description"`
	Category     string    `json:"category"`
	Pokok        string    `json:"pokok"`
	Khusus       string    `json:"khusus"`
	SHU          string    `json:"shu"`
	Wajib        string    `json:"wajib"`
	Sukarela     string    `json:"sukarela"`
}

type Loan struct {
	Source             SourceRef `json:"source"`
	LoanType           string    `json:"loan_type"`
	MemberName         string    `json:"member_name"`
	SourceNPP          string    `json:"source_npp"`
	SourceHint         string    `json:"source_hint"`
	Principal          string    `json:"principal"`
	AdminFee           string    `json:"admin_fee"`
	TotalObligation    string    `json:"total_obligation"`
	SourceRemaining    string    `json:"source_remaining"`
	MonthlyInstallment string    `json:"monthly_installment"`
	DurationMonths     int       `json:"duration_months"`
	StartDate          string    `json:"start_date"`
	EndDate            string    `json:"end_date"`
	SourceStatus       string    `json:"source_status"`
	Purpose            string    `json:"purpose"`
}

type LoanEvidence struct {
	Source      SourceRef `json:"source"`
	MemberName  string    `json:"member_name"`
	SourceNPP   string    `json:"source_npp"`
	LoanHint    string    `json:"loan_hint"`
	Method      string    `json:"method"`
	Amount      string    `json:"amount"`
	RecordDate  string    `json:"record_date"`
	Description string    `json:"description"`
}

func (m Manifest) CanonicalBytes() ([]byte, error) {
	return json.Marshal(m)
}

func (m Manifest) Hash() (string, error) {
	b, err := m.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func SnapshotID(sources []Source) string {
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		parts = append(parts, strings.TrimSpace(source.Name)+":"+strings.TrimSpace(source.SHA256))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "seed-" + hex.EncodeToString(sum[:])[:16]
}
