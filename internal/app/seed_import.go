package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sodrety/kopdes/internal/seeddata"
	"golang.org/x/crypto/bcrypt"
)

const (
	seedEmailDomain       = "koperasidj.id"
	seedImporterEmail     = "historical-importer@koperasidj.id"
	seedImporterPassword  = "!historical!"
	seedOpeningBalanceDay = "2025-12-31"
)

type SeedImportOptions struct {
	DryRun          bool
	Append          bool
	CredentialsPath string
	ReportPath      string
}

type SeedImportIssue struct {
	Severity   string `json:"severity"`
	Blocker    bool   `json:"blocker"`
	Entity     string `json:"entity"`
	SourceKey  string `json:"source_key"`
	Reason     string `json:"reason"`
	RawPayload string `json:"raw_payload,omitempty"`
}

type SeedImportCredential struct {
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Password string `json:"temporary_password"`
}

type SeedImportReport struct {
	RunID           string            `json:"run_id"`
	SnapshotID      string            `json:"snapshot_id"`
	ManifestHash    string            `json:"manifest_hash"`
	Status          string            `json:"status"`
	Counts          map[string]int    `json:"counts"`
	Issues          []SeedImportIssue `json:"issues"`
	CredentialsPath string            `json:"credentials_path,omitempty"`
}

type preparedMember struct {
	Source     seeddata.Member
	ID         string
	MemberNo   string
	Status     string
	MemberType string
}

type preparedSaving struct {
	SourceKey  string
	MemberID   string
	Category   string
	Amount     int64
	Type       string
	RecordDate string
	Reference  string
	Note       string
	Raw        string
}

type preparedRepayment struct {
	SourceKey  string
	Amount     int64
	RecordDate string
	Note       string
	Raw        string
}

type preparedLoan struct {
	Source      seeddata.Loan
	ID          string
	RequestID   string
	MemberID    string
	Principal   int64
	AdminFee    int64
	Obligation  int64
	Remaining   int64
	Installment int64
	Status      string
	Repayments  []preparedRepayment
	Schedule    []preparedInstallment
}

type preparedInstallment struct {
	Number    int
	DueDate   string
	Scheduled int64
	Paid      int64
}

type preparedSeed struct {
	Members      []preparedMember
	MemberByName map[string]preparedMember
	Savings      []preparedSaving
	Loans        []preparedLoan
	LoanByID     map[string]preparedLoan
}

func RunSeedImport(db *sql.DB, manifest seeddata.Manifest, options SeedImportOptions) (SeedImportReport, error) {
	manifestHash, err := manifest.Hash()
	if err != nil {
		return SeedImportReport{}, fmt.Errorf("hash manifest: %w", err)
	}
	if err := MigrateTo(db, 14); err != nil {
		return SeedImportReport{}, fmt.Errorf("prepare schema through migration 14: %w", err)
	}
	if err := ensureSeedAuditTables(db); err != nil {
		return SeedImportReport{}, err
	}

	if existing, found, err := successfulSeedSnapshot(db, manifest.SnapshotID); err != nil {
		return SeedImportReport{}, err
	} else if found {
		if existing != manifestHash {
			return SeedImportReport{}, fmt.Errorf("snapshot %s already exists with a different manifest hash", manifest.SnapshotID)
		}
		return SeedImportReport{SnapshotID: manifest.SnapshotID, ManifestHash: manifestHash, Status: "already_succeeded", Counts: map[string]int{}}, nil
	}
	maxVersion, err := currentSchemaVersion(db)
	if err != nil {
		return SeedImportReport{}, err
	}
	if !options.Append && maxVersion > 14 {
		return SeedImportReport{}, fmt.Errorf("database is already migrated through version %d; initial seed import must run before migrations 15-18", maxVersion)
	}
	if options.Append && maxVersion < 18 {
		return SeedImportReport{}, fmt.Errorf("append seed import requires schema version 18 or later; current version is %d", maxVersion)
	}

	// Each attempt gets its own audit run so a failed validation can be
	// rerun after source corrections; snapshot identity remains content-based.
	runID := deterministicID("seed-run", fmt.Sprintf("%s:%s:%d", manifest.SnapshotID, manifestHash, time.Now().UTC().UnixNano()))
	report := SeedImportReport{RunID: runID, SnapshotID: manifest.SnapshotID, ManifestHash: manifestHash, Status: "validating", Counts: map[string]int{}}
	if err := insertSeedRun(db, report); err != nil {
		return SeedImportReport{}, err
	}
	if err := persistSeedManifestRecords(db, runID, manifest); err != nil {
		return SeedImportReport{}, err
	}

	prepared, issues := validateSeedManifest(manifest)
	report.Issues = issues
	report.Counts = seedCounts(prepared, issues)
	if options.DryRun || hasBlockers(issues) {
		if hasBlockers(issues) {
			report.Status = "validation_failed"
		} else {
			report.Status = "dry_run"
		}
		if err := persistSeedIssues(db, runID, issues); err != nil {
			return report, err
		}
		if err := updateSeedRun(db, runID, report.Status); err != nil {
			return report, err
		}
		_ = writeSeedReport(options.ReportPath, report)
		if hasBlockers(issues) {
			return report, errors.New("seed validation failed; see the quarantine report")
		}
		return report, nil
	}
	needsExtendedSavingCategories := hasExtendedSavingCategories(prepared)
	if options.Append {
		if len(prepared.Loans) != 0 {
			return report, errors.New("append seed import currently supports members and savings only")
		}
		if err := MigrateTo(db, 19); err != nil {
			return report, fmt.Errorf("prepare append schema: %w", err)
		}
		prepared, err = reconcileAppendMemberIDs(db, prepared)
		if err != nil {
			return report, fmt.Errorf("reconcile existing members for append: %w", err)
		}
	} else if needsExtendedSavingCategories {
		// The template currently imports members and savings only. Apply the
		// completed schema before staging these extra categories, because the
		// legacy loan migrations intentionally run after historical seed rows.
		if len(prepared.Loans) != 0 {
			return report, errors.New("extended saving categories require a loan-free seed manifest")
		}
		if err := MigrateTo(db, 19); err != nil {
			return report, fmt.Errorf("prepare extended saving categories: %w", err)
		}
	}

	if !options.Append {
		if err := validateEmptyOperationalDatabase(db); err != nil {
			report.Status = "validation_failed"
			report.Issues = append(report.Issues, SeedImportIssue{Severity: "error", Blocker: true, Entity: "database", Reason: err.Error()})
			_ = persistSeedIssues(db, runID, report.Issues)
			_ = updateSeedRun(db, runID, report.Status)
			_ = writeSeedReport(options.ReportPath, report)
			return report, err
		}
	}

	credentials := make([]SeedImportCredential, 0)
	if err := seedBaseSchema(db, runID, prepared, &credentials, options.Append); err != nil {
		report.Status = "failed"
		_ = updateSeedRun(db, runID, report.Status)
		_ = writeSeedReport(options.ReportPath, report)
		return report, fmt.Errorf("seed base data: %w", err)
	}
	if !options.Append && !needsExtendedSavingCategories {
		if err := MigrateTo(db, 15); err != nil {
			return report, fmt.Errorf("apply migration 15 after seed staging: %w", err)
		}
	}
	if !options.Append {
		if err := assignImportedLoanTypes(db, prepared.Loans); err != nil {
			return report, fmt.Errorf("assign imported loan types: %w", err)
		}
	}
	if !options.Append && !needsExtendedSavingCategories {
		if err := MigrateTo(db, 19); err != nil {
			return report, fmt.Errorf("finish schema migrations after seed staging: %w", err)
		}
	}
	if err := updateImportedMemberTypes(db, prepared.Members); err != nil {
		return report, fmt.Errorf("assign imported member types: %w", err)
	}

	report.Status = "succeeded"
	report.Counts = seedCounts(prepared, report.Issues)
	report.Counts["accounts"] = len(credentials)
	if err := updateSeedRun(db, runID, report.Status); err != nil {
		return report, err
	}
	if options.CredentialsPath == "" {
		options.CredentialsPath = filepath.Join("output", "seed", manifest.SnapshotID+"-credentials.csv")
	}
	if err := writeSeedCredentials(options.CredentialsPath, credentials); err != nil {
		return report, fmt.Errorf("write credentials: %w", err)
	}
	report.CredentialsPath = options.CredentialsPath
	if err := writeSeedReport(options.ReportPath, report); err != nil {
		return report, err
	}
	return report, nil
}

func reconcileAppendMemberIDs(db *sql.DB, prepared preparedSeed) (preparedSeed, error) {
	memberIDs := make(map[string]string, len(prepared.Members))
	for index := range prepared.Members {
		member := &prepared.Members[index]
		var existingID, existingName string
		err := db.QueryRow(`SELECT id,full_name FROM members WHERE LOWER(member_no)=LOWER($1)`, member.MemberNo).Scan(&existingID, &existingName)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return preparedSeed{}, err
		}
		if normalizedName(existingName) != normalizedName(member.Source.FullName) {
			return preparedSeed{}, fmt.Errorf("member number %s already belongs to %q, but the template contains %q", member.MemberNo, existingName, member.Source.FullName)
		}
		memberIDs[member.ID] = existingID
		member.ID = existingID
	}
	for key, member := range prepared.MemberByName {
		if replacement, ok := memberIDs[member.ID]; ok {
			member.ID = replacement
			prepared.MemberByName[key] = member
		}
	}
	for index := range prepared.Savings {
		if replacement, ok := memberIDs[prepared.Savings[index].MemberID]; ok {
			prepared.Savings[index].MemberID = replacement
		}
	}
	for index := range prepared.Loans {
		if replacement, ok := memberIDs[prepared.Loans[index].MemberID]; ok {
			prepared.Loans[index].MemberID = replacement
		}
	}
	return prepared, nil
}

func hasExtendedSavingCategories(prepared preparedSeed) bool {
	for _, saving := range prepared.Savings {
		if saving.Category == "shu" || saving.Category == "khusus" {
			return true
		}
	}
	return false
}

func validateSeedManifest(manifest seeddata.Manifest) (preparedSeed, []SeedImportIssue) {
	result := preparedSeed{
		MemberByName: map[string]preparedMember{},
		LoanByID:     map[string]preparedLoan{},
	}
	var issues []SeedImportIssue
	add := func(severity string, blocker bool, entity, sourceKey, reason string, raw any) {
		payload := ""
		if raw != nil {
			if encoded, err := json.Marshal(raw); err == nil {
				payload = string(encoded)
			}
		}
		issues = append(issues, SeedImportIssue{Severity: severity, Blocker: blocker, Entity: entity, SourceKey: sourceKey, Reason: reason, RawPayload: payload})
	}

	nameCounts := map[string]int{}
	nppCounts := map[string]int{}
	for _, member := range manifest.Members {
		nameKey := normalizedName(member.FullName)
		nameCounts[nameKey]++
		base := normalizeMemberNo(member.CurrentNPP)
		if base == "" {
			base = normalizeMemberNo(member.OldNPP)
		}
		if base == "" {
			base = "unassigned"
		}
		nppCounts[base]++
	}
	for _, member := range manifest.Members {
		nameKey := normalizedName(member.FullName)
		if nameCounts[nameKey] != 1 {
			add("error", true, "member", member.Source.Key(), "member name must be unique in Master Anggota", member)
			continue
		}
		base := normalizeMemberNo(member.CurrentNPP)
		if base == "" {
			base = normalizeMemberNo(member.OldNPP)
		}
		if base == "" {
			base = "unassigned"
		}
		canonical := base
		if nppCounts[base] > 1 || isPlaceholderNPP(base) {
			canonical = fmt.Sprintf("%s-r%d", base, member.Source.Row)
		}
		status, memberType := mapMemberSource(member.SourceStatus)
		switch member.MemberType {
		case "daily_worker", "employee", "self_employed":
			memberType = member.MemberType
		}
		if member.JoinDate == "" {
			add("error", true, "member", member.Source.Key(), "join date is required", member)
		}
		preparedMemberValue := preparedMember{Source: member, ID: deterministicID("member", member.Source.Key()), MemberNo: canonical, Status: status, MemberType: memberType}
		result.Members = append(result.Members, preparedMemberValue)
		result.MemberByName[nameKey] = preparedMemberValue
		for category, raw := range map[string]string{"pokok": member.Balances.Pokok, "wajib": member.Balances.Wajib, "sukarela": member.Balances.Sukarela} {
			value, fractional, err := parseSeedMoneyRounded(raw)
			if err != nil {
				add("error", true, "member", member.Source.Key(), category+" balance is invalid: "+err.Error(), member)
			} else if value < 0 {
				add("error", true, "member", member.Source.Key(), category+" balance cannot be negative", member)
			} else if fractional {
				add("warning", false, "member", member.Source.Key(), fmt.Sprintf("%s balance rounded from %q to %d", category, raw, value), member)
			}
		}
		for category, raw := range map[string]string{"pkpri": member.Balances.PKPRI, "khusus": member.Balances.Khusus} {
			value, fractional, err := parseSeedMoneyRounded(raw)
			if err != nil {
				add("error", true, "member", member.Source.Key(), category+" balance is invalid: "+err.Error(), member)
			} else if category == "pkpri" && value != 0 {
				add("error", true, "member", member.Source.Key(), category+" has no confirmed application mapping", member)
			} else if value < 0 {
				add("error", true, "member", member.Source.Key(), category+" balance cannot be negative", member)
			} else if fractional {
				add("warning", false, "member", member.Source.Key(), fmt.Sprintf("%s balance rounded from %q to %d", category, raw, value), member)
			}
		}
	}
	if len(result.MemberByName) != len(result.Members) {
		add("error", true, "member", "", "canonical member names or identifiers collide", nil)
	}
	for _, member := range result.Members {
		if _, exists := result.MemberByName[normalizedName(member.Source.FullName)]; !exists {
			continue
		}
	}
	for _, member := range result.Members {
		for category, raw := range map[string]string{"pokok": member.Source.Balances.Pokok, "khusus": member.Source.Balances.Khusus} {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			value, _, err := parseSeedMoneyRounded(raw)
			if err == nil && value > 0 {
				result.Savings = append(result.Savings, preparedSaving{SourceKey: member.Source.Source.Key() + ":" + category, MemberID: member.ID, Category: category, Amount: value, Type: "deposit", RecordDate: seedOpeningBalanceDay, Reference: "seed-" + member.Source.Source.Key() + "-" + category, Note: "Historical opening balance from Master Anggota " + strings.ToUpper(category), Raw: raw})
			}
		}
	}

	ledgerTotals := map[string]map[string]int64{}
	for _, row := range manifest.Savings {
		member, ok := result.MemberByName[normalizedName(row.MemberName)]
		if !ok {
			if alias := resolveSeedAlias(row.MemberName, result.MemberByName); alias != "" {
				member, ok = result.MemberByName[alias]
			}
		}
		if !ok {
			add("warning", false, "saving", row.Source.Key(), "member name is not present in Master Anggota; saving row skipped", row)
			continue
		}
		if ledgerTotals[member.ID] == nil {
			ledgerTotals[member.ID] = map[string]int64{}
		}
		for category, raw := range map[string]string{"pokok": row.Pokok, "wajib": row.Wajib, "sukarela": row.Sukarela, "shu": row.SHU, "khusus": row.Khusus} {
			value, fractional, err := parseSeedMoneyRounded(raw)
			if err != nil {
				add("error", true, "saving", row.Source.Key(), category+" amount is invalid: "+err.Error(), row)
				continue
			}
			if fractional && strings.TrimSpace(raw) != "" {
				add("warning", false, "saving", row.Source.Key(), fmt.Sprintf("%s amount rounded from %q to %d", category, raw, value), row)
			}
			if value == 0 {
				continue
			}
			if row.RecordDate == "" {
				add("error", true, "saving", row.Source.Key(), "record date is missing or invalid", row)
				continue
			}
			typeName := "deposit"
			amount := value
			if value < 0 {
				typeName, amount = "withdrawal", -value
			}
			ledgerTotals[member.ID][category] += value
			result.Savings = append(result.Savings, preparedSaving{SourceKey: row.Source.Key() + ":" + category, MemberID: member.ID, Category: category, Amount: amount, Type: typeName, RecordDate: row.RecordDate, Reference: "seed-" + row.Source.Key() + "-" + category, Note: savingNote(row), Raw: raw})
		}
	}
	for _, member := range result.Members {
		for category, raw := range map[string]string{"wajib": member.Source.Balances.Wajib, "sukarela": member.Source.Balances.Sukarela} {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			value, fractional, err := parseSeedMoneyRounded(raw)
			if err != nil || fractional {
				if err == nil && fractional && strings.TrimSpace(raw) != "" {
					add("warning", false, "saving_reconciliation", member.Source.Source.Key(), fmt.Sprintf("%s balance rounded from %q before reconciliation", category, raw), member)
				}
				continue
			}
			if ledgerTotals[member.ID][category] != value {
				add("error", true, "saving_reconciliation", member.Source.Source.Key(), fmt.Sprintf("%s ledger total %d does not match Master Anggota balance %d", category, ledgerTotals[member.ID][category], value), member)
			}
		}
	}

	loanByMember := map[string][]*preparedLoan{}
	for _, sourceLoan := range manifest.Loans {
		member, ok := result.MemberByName[normalizedName(sourceLoan.MemberName)]
		if !ok {
			if alias := resolveSeedAlias(sourceLoan.MemberName, result.MemberByName); alias != "" {
				member, ok = result.MemberByName[alias]
			}
		}
		if !ok {
			add("error", true, "loan", sourceLoan.Source.Key(), "member name is not present in Master Anggota", sourceLoan)
			continue
		}
		principal, principalFractional, principalErr := parseSeedMoney(sourceLoan.Principal)
		adminFee, adminFractional, adminErr := parseSeedMoney(sourceLoan.AdminFee)
		obligation, obligationFractional, obligationErr := parseSeedMoney(sourceLoan.TotalObligation)
		installment, installmentFractional, installmentErr := parseSeedMoney(sourceLoan.MonthlyInstallment)
		if obligationErr == nil && obligation == 0 && principalErr == nil && adminErr == nil && principal > 0 && adminFee >= 0 {
			// The primary workbook exposes Pokok and Admin but has no total
			// obligation column. Derive the exact integer sum; never round.
			if principal <= math.MaxInt64-adminFee {
				obligation = principal + adminFee
			}
		}
		if installmentErr != nil && strings.EqualFold(strings.TrimSpace(sourceLoan.MonthlyInstallment), "lunas") {
			// In the primary workbook this cell is reused as a status field for
			// paid loans, so the schedule amount must be derived below.
			installment, installmentErr = 0, nil
		}
		if installmentErr == nil && installment == 0 && obligation > 0 && sourceLoan.DurationMonths > 0 {
			installment = obligation / int64(sourceLoan.DurationMonths)
			if installment > 0 {
				add("warning", false, "loan", sourceLoan.Source.Key(), "monthly installment derived from total obligation and duration because the source value is empty, zero, or a status; final installment preserves any exact remainder", sourceLoan)
			}
		}
		for label, fractional := range map[string]bool{"principal": principalFractional, "admin_fee": adminFractional, "total_obligation": obligationFractional, "monthly_installment": installmentFractional} {
			if fractional {
				add("error", true, "loan", sourceLoan.Source.Key(), label+" is fractional; no rounding is allowed", sourceLoan)
			}
		}
		for label, parseErr := range map[string]error{"principal": principalErr, "admin_fee": adminErr, "total_obligation": obligationErr, "monthly_installment": installmentErr} {
			if parseErr != nil {
				add("error", true, "loan", sourceLoan.Source.Key(), label+" is invalid: "+parseErr.Error(), sourceLoan)
			}
		}
		if principal <= 0 {
			add("error", true, "loan", sourceLoan.Source.Key(), "principal must be a positive whole Rupiah amount", sourceLoan)
		}
		if adminFee < 0 {
			add("error", true, "loan", sourceLoan.Source.Key(), "admin fee cannot be negative", sourceLoan)
		}
		if obligation <= 0 {
			add("error", true, "loan", sourceLoan.Source.Key(), "total obligation must be present and positive", sourceLoan)
		}
		if sourceLoan.DurationMonths < 1 || sourceLoan.DurationMonths > 120 {
			add("error", true, "loan", sourceLoan.Source.Key(), "duration must be between 1 and 120 months", sourceLoan)
		}
		if sourceLoan.StartDate == "" {
			add("error", true, "loan", sourceLoan.Source.Key(), "start date is missing or invalid", sourceLoan)
		}
		if installment <= 0 {
			add("error", true, "loan", sourceLoan.Source.Key(), "monthly installment must be present and positive", sourceLoan)
		}
		loan := preparedLoan{Source: sourceLoan, ID: deterministicID("loan", sourceLoan.Source.Key()), RequestID: deterministicID("loan-request", sourceLoan.Source.Key()), MemberID: member.ID, Principal: principal, AdminFee: adminFee, Obligation: obligation, Installment: installment, Status: "active"}
		result.Loans = append(result.Loans, loan)
		result.LoanByID[loan.ID] = loan
		loanByMember[member.ID] = append(loanByMember[member.ID], &result.Loans[len(result.Loans)-1])
	}

	for evidenceIndex, evidence := range manifest.LoanEvidence {
		if strings.TrimSpace(evidence.Method) == "" {
			add("warning", false, "loan_evidence", evidence.Source.Key(), "blank Metod excluded as unclassified/pending source data", evidence)
			continue
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(evidence.Method)), "3.") {
			continue
		}
		member, ok := result.MemberByName[normalizedName(evidence.MemberName)]
		if !ok {
			if alias := resolveSeedAlias(evidence.MemberName, result.MemberByName); alias != "" {
				member, ok = result.MemberByName[alias]
			}
		}
		if !ok {
			add("error", true, "loan_repayment", evidence.Source.Key(), "member name is not present in Master Anggota", evidence)
			continue
		}
		candidates := loanByMember[member.ID]
		var selected *preparedLoan
		if evidence.Source.Sheet == "Detail Pinjaman" && evidence.RecordDate != "" {
			// Detail Pinjaman's No column is a detail-row number, not the
			// master-loan number. For that workbook, use the source date window
			// and require a single candidate when a member has overlapping loans.
			for _, candidate := range candidates {
				if candidate.Source.StartDate == "" || evidence.RecordDate < candidate.Source.StartDate {
					continue
				}
				if candidate.Source.EndDate != "" && evidence.RecordDate > candidate.Source.EndDate {
					continue
				}
				if selected != nil {
					selected = nil
					break
				}
				selected = candidate
			}
		} else {
			for _, candidate := range candidates {
				if evidence.LoanHint != "" && strings.EqualFold(strings.TrimSpace(evidence.LoanHint), strings.TrimSpace(candidate.Source.SourceHint)) {
					if selected != nil {
						selected = nil
						break
					}
					selected = candidate
				}
			}
		}
		if selected == nil && len(candidates) == 1 {
			selected = candidates[0]
		}
		if selected == nil {
			add("error", true, "loan_repayment", evidence.Source.Key(), "repayment cannot be matched to exactly one source loan", evidence)
			continue
		}
		amount, fractional, err := parseSeedMoney(evidence.Amount)
		if err == nil && amount < 0 {
			if amount == math.MinInt64 {
				err = errors.New("repayment amount overflows int64 when converted to positive")
			} else {
				amount = -amount
			}
		}
		if err != nil || fractional || amount <= 0 {
			add("error", true, "loan_repayment", evidence.Source.Key(), "repayment amount must be a positive whole Rupiah amount", evidence)
			continue
		}
		if evidence.RecordDate == "" {
			add("error", true, "loan_repayment", evidence.Source.Key(), "repayment date is missing or invalid", evidence)
			continue
		}
		repaymentSourceKey := fmt.Sprintf("%s:repayment-%d", evidence.Source.Key(), evidenceIndex)
		selected.Repayments = append(selected.Repayments, preparedRepayment{SourceKey: repaymentSourceKey, Amount: amount, RecordDate: evidence.RecordDate, Note: "Historical import; source Metod=" + evidence.Method, Raw: evidence.Amount})
	}
	for index := range result.Loans {
		loan := &result.Loans[index]
		var repaid int64
		for _, repayment := range loan.Repayments {
			repaid += repayment.Amount
		}
		loan.Remaining = loan.Obligation - repaid
		if loan.Remaining < 0 {
			add("error", true, "loan_reconciliation", loan.Source.Source.Key(), "repayments exceed total obligation", loan.Source)
			continue
		}
		if loan.Remaining == 0 {
			loan.Status = "paid"
		}
		if sourceRemaining := strings.TrimSpace(loan.Source.SourceRemaining); sourceRemaining != "" {
			sourceValue, fractional, err := parseSeedMoney(sourceRemaining)
			if err != nil || fractional || sourceValue != loan.Remaining {
				add("error", true, "loan_reconciliation", loan.Source.Source.Key(), fmt.Sprintf("derived remaining balance %d does not match source remaining balance %q", loan.Remaining, sourceRemaining), loan.Source)
			}
		}
		if strings.Contains(strings.ToLower(loan.Source.SourceStatus), "lunas") && loan.Status != "paid" {
			add("error", true, "loan_reconciliation", loan.Source.Source.Key(), "source status Lunas conflicts with derived outstanding balance", loan.Source)
		}
		if strings.Contains(strings.ToLower(loan.Source.SourceStatus), "cicilan") && loan.Status == "paid" {
			add("error", true, "loan_reconciliation", loan.Source.Source.Key(), "source status Cicilan conflicts with derived paid balance", loan.Source)
		}
		if loan.Source.StartDate != "" && loan.Source.DurationMonths > 0 && loan.Installment > 0 {
			firstDueDateSameMonth := loan.Source.Source.Sheet == "Data Base Pinjaman"
			loan.Schedule = buildSeedSchedule(loan.Source.StartDate, loan.Source.DurationMonths, loan.Obligation, loan.Repayments, firstDueDateSameMonth)
			if len(loan.Schedule) != loan.Source.DurationMonths {
				add("error", true, "loan_schedule", loan.Source.Source.Key(), "could not generate a complete installment schedule", loan.Source)
			}
			if loan.Source.EndDate != "" && len(loan.Schedule) > 0 {
				generatedEnd := loan.Schedule[len(loan.Schedule)-1].DueDate
				if generatedEnd != loan.Source.EndDate {
					add("error", true, "loan_schedule", loan.Source.Source.Key(), fmt.Sprintf("generated final installment date %s does not match source end date %s", generatedEnd, loan.Source.EndDate), loan.Source)
				}
			}
		}
	}
	for memberID, loans := range loanByMember {
		active := 0
		for _, loan := range loans {
			for index := range result.Loans {
				if result.Loans[index].ID == loan.ID {
					if result.Loans[index].Status == "active" && result.Loans[index].Remaining > 0 {
						active++
					}
				}
			}
		}
		if active > 1 {
			add("error", true, "loan_overlap", memberID, fmt.Sprintf("member has %d outstanding active source loans; app requires an explicit overlap resolution", active), nil)
		}
	}
	return result, issues
}

func seedBaseSchema(db *sql.DB, runID string, prepared preparedSeed, credentials *[]SeedImportCredential, appendMode bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	importerID, err := ensureHistoricalImporter(tx)
	if err != nil {
		return err
	}
	for _, member := range prepared.Members {
		if !appendMode {
			if _, err := tx.Exec(`INSERT INTO members (id,member_no,full_name,phone,address,join_date,status) VALUES ($1,$2,$3,'','',$4,$5)`, member.ID, member.MemberNo, member.Source.FullName, member.Source.JoinDate, member.Status); err != nil {
				return fmt.Errorf("insert member %s: %w", member.Source.FullName, err)
			}
		} else {
			var existingID string
			err := tx.QueryRow(`SELECT id FROM members WHERE id=$1`, member.ID).Scan(&existingID)
			if errors.Is(err, sql.ErrNoRows) {
				if _, err := tx.Exec(`INSERT INTO members (id,member_no,full_name,phone,address,join_date,status) VALUES ($1,$2,$3,'','',$4,$5)`, member.ID, member.MemberNo, member.Source.FullName, member.Source.JoinDate, member.Status); err != nil {
					return fmt.Errorf("insert member %s: %w", member.Source.FullName, err)
				}
			} else if err != nil {
				return err
			}
		}
		if member.Status != "active" {
			continue
		}
		if appendMode {
			var existingUserID string
			err := tx.QueryRow(`SELECT id FROM users WHERE member_id=$1 AND historical_identity=FALSE LIMIT 1`, member.ID).Scan(&existingUserID)
			if err == nil {
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		password, err := randomTemporaryPassword()
		if err != nil {
			return err
		}
		email := slugSeed(member.MemberNo) + "@" + seedEmailDomain
		userID := deterministicID("user", member.ID)
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'member',$4,$5,TRUE,TRUE,FALSE)`, userID, email, string(hash), member.ID, member.Source.FullName); err != nil {
			return fmt.Errorf("insert login for %s: %w", member.Source.FullName, err)
		}
		*credentials = append(*credentials, SeedImportCredential{MemberNo: member.MemberNo, FullName: member.Source.FullName, Email: email, Password: password})
	}
	for _, member := range prepared.Members {
		if err := insertSeedRecord(tx, runID, member.Source.Source.Key(), "member", member.ID, member.Source); err != nil {
			return err
		}
	}
	for _, saving := range prepared.Savings {
		id := deterministicID("saving", saving.SourceKey)
		if _, err := tx.Exec(`INSERT INTO saving_records (id,member_id,type,category,amount,record_date,reference_no,note,recorded_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$6 || ' 00:00:00')`, id, saving.MemberID, saving.Type, saving.Category, saving.Amount, saving.RecordDate, saving.Reference, saving.Note, importerID); err != nil {
			return fmt.Errorf("insert saving %s: %w", saving.SourceKey, err)
		}
		if err := insertSeedRecord(tx, runID, saving.SourceKey, "saving", id, saving.Raw); err != nil {
			return err
		}
	}
	for _, loan := range prepared.Loans {
		createdAt := loan.Source.StartDate + " 00:00:00"
		if _, err := tx.Exec(`INSERT INTO loan_requests (id,member_id,requested_amount,duration_months,purpose,status,reviewed_by,reviewed_at,rejection_reason,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,'approved',$6,$7,$8,$7,$7)`, loan.RequestID, loan.MemberID, loan.Principal, loan.Source.DurationMonths, "Historical import: "+loan.Source.Purpose, importerID, createdAt, "Historical source request; no source approval records"); err != nil {
			return fmt.Errorf("insert historical loan request %s: %w", loan.Source.Source.Key(), err)
		}
		finalDate := loan.Source.EndDate
		nextDate := ""
		if len(loan.Schedule) > 0 {
			nextDate = loan.Schedule[0].DueDate
			finalDate = loan.Schedule[len(loan.Schedule)-1].DueDate
		}
		for _, installment := range loan.Schedule {
			if installment.Paid < installment.Scheduled {
				nextDate = installment.DueDate
				break
			}
		}
		if _, err := tx.Exec(`INSERT INTO loans (id,loan_request_id,member_id,approved_amount,duration_months,monthly_installment,remaining_balance,status,approved_by,approved_at,start_date,interest_rate_bps,total_interest,total_obligation,next_due_date,final_due_date,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,0,$11,$12,$13,$14,$10,$10)`, loan.ID, loan.RequestID, loan.MemberID, loan.Principal, loan.Source.DurationMonths, loan.Installment, loan.Remaining, loan.Status, importerID, createdAt, loan.AdminFee, loan.Obligation, nextDate, finalDate); err != nil {
			return fmt.Errorf("insert historical loan %s: %w", loan.Source.Source.Key(), err)
		}
		for _, installment := range loan.Schedule {
			if _, err := tx.Exec(`INSERT INTO loan_installments (id,loan_id,installment_no,due_date,scheduled_amount,paid_amount,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`, deterministicID("installment", fmt.Sprintf("%s:%d", loan.ID, installment.Number)), loan.ID, installment.Number, installment.DueDate, installment.Scheduled, installment.Paid, installment.DueDate+" 00:00:00"); err != nil {
				return fmt.Errorf("insert installment %s/%d: %w", loan.Source.Source.Key(), installment.Number, err)
			}
		}
		for _, repayment := range loan.Repayments {
			id := deterministicID("repayment", repayment.SourceKey)
			if _, err := tx.Exec(`INSERT INTO loan_repayments (id,loan_id,member_id,amount,record_date,reference_no,note,recorded_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$5 || ' 00:00:00')`, id, loan.ID, loan.MemberID, repayment.Amount, repayment.RecordDate, "seed-"+repayment.SourceKey, repayment.Note, importerID); err != nil {
				return fmt.Errorf("insert repayment %s: %w", repayment.SourceKey, err)
			}
			if err := insertSeedRecord(tx, runID, repayment.SourceKey, "loan_repayment", id, repayment.Raw); err != nil {
				return err
			}
		}
		if err := insertSeedRecord(tx, runID, loan.Source.Source.Key(), "loan", loan.ID, loan.Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func assignImportedLoanTypes(db *sql.DB, loans []preparedLoan) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, loan := range loans {
		if _, err := tx.Exec(`UPDATE loans SET loan_type=$1 WHERE id=$2`, loan.Source.LoanType, loan.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE loan_requests SET loan_type=$1 WHERE id=$2`, loan.Source.LoanType, loan.RequestID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func updateImportedMemberTypes(db *sql.DB, members []preparedMember) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, member := range members {
		if _, err := tx.Exec(`UPDATE members SET member_type=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, member.MemberType, member.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureHistoricalImporter(tx *sql.Tx) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM users WHERE email=$1`, seedImporterEmail).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = deterministicID("user", seedImporterEmail)
	if _, err := tx.Exec(`INSERT INTO users (id,email,password_hash,role,member_id,full_name,active,must_change_password,historical_identity) VALUES ($1,$2,$3,'member',NULL,'Historical Importer',FALSE,FALSE,TRUE)`, id, seedImporterEmail, seedImporterPassword); err != nil {
		return "", err
	}
	return id, nil
}

func persistSeedManifestRecords(db *sql.DB, runID string, manifest seeddata.Manifest) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	insert := func(sourceKey, entity string, raw any) error {
		return insertSeedRecord(tx, runID, sourceKey+":source:"+entity, entity, deterministicID("seed-source", sourceKey+":"+entity), raw)
	}
	for _, member := range manifest.Members {
		if err := insert(member.Source.Key(), "member_source", member); err != nil {
			return err
		}
	}
	for _, saving := range manifest.Savings {
		if err := insert(saving.Source.Key(), "saving_source", saving); err != nil {
			return err
		}
	}
	for _, loan := range manifest.Loans {
		if err := insert(loan.Source.Key(), "loan_source", loan); err != nil {
			return err
		}
	}
	evidenceBySource := map[string][]seeddata.LoanEvidence{}
	for _, evidence := range manifest.LoanEvidence {
		key := evidence.Source.Key()
		evidenceBySource[key] = append(evidenceBySource[key], evidence)
	}
	evidenceKeys := make([]string, 0, len(evidenceBySource))
	for key := range evidenceBySource {
		evidenceKeys = append(evidenceKeys, key)
	}
	sort.Strings(evidenceKeys)
	for _, key := range evidenceKeys {
		raw := any(evidenceBySource[key][0])
		if len(evidenceBySource[key]) > 1 {
			raw = evidenceBySource[key]
		}
		if err := insert(key, "loan_evidence", raw); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertSeedRecord(tx *sql.Tx, runID, sourceKey, entity, entityID string, raw any) error {
	payload, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO seed_import_records (run_id,source_key,entity,entity_id,raw_payload) VALUES ($1,$2,$3,$4,$5)`, runID, sourceKey, entity, entityID, string(payload))
	return err
}

func ensureSeedAuditTables(db *sql.DB) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS seed_import_runs (id TEXT PRIMARY KEY, snapshot_id TEXT NOT NULL, manifest_hash TEXT NOT NULL, status TEXT NOT NULL, started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, finished_at TIMESTAMP NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_seed_import_runs_snapshot ON seed_import_runs(snapshot_id, status)`,
		`CREATE TABLE IF NOT EXISTS seed_import_issues (id TEXT PRIMARY KEY, run_id TEXT NOT NULL, severity TEXT NOT NULL, blocker BOOLEAN NOT NULL, entity TEXT NOT NULL, source_key TEXT NOT NULL, reason TEXT NOT NULL, raw_payload TEXT NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (run_id) REFERENCES seed_import_runs(id))`,
		`CREATE TABLE IF NOT EXISTS seed_import_records (run_id TEXT NOT NULL, source_key TEXT NOT NULL, entity TEXT NOT NULL, entity_id TEXT NOT NULL, raw_payload TEXT NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (run_id, source_key), FOREIGN KEY (run_id) REFERENCES seed_import_runs(id))`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("create seed audit table: %w", err)
		}
	}
	return nil
}

func successfulSeedSnapshot(db *sql.DB, snapshotID string) (string, bool, error) {
	var hash string
	err := db.QueryRow(`SELECT manifest_hash FROM seed_import_runs WHERE snapshot_id=$1 AND status='succeeded' ORDER BY finished_at DESC, started_at DESC LIMIT 1`, snapshotID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return hash, err == nil, err
}

func currentSchemaVersion(db *sql.DB) (int, error) {
	var version sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

func insertSeedRun(db *sql.DB, report SeedImportReport) error {
	_, err := db.Exec(`INSERT INTO seed_import_runs (id,snapshot_id,manifest_hash,status) VALUES ($1,$2,$3,$4)`, report.RunID, report.SnapshotID, report.ManifestHash, report.Status)
	return err
}

func updateSeedRun(db *sql.DB, runID, status string) error {
	_, err := db.Exec(`UPDATE seed_import_runs SET status=$1,finished_at=CASE WHEN $1 IN ('succeeded','validation_failed','failed','dry_run') THEN CURRENT_TIMESTAMP ELSE finished_at END WHERE id=$2`, status, runID)
	return err
}

func persistSeedIssues(db *sql.DB, runID string, issues []SeedImportIssue) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for index, issue := range issues {
		id := deterministicID("seed-issue", fmt.Sprintf("%s:%d:%s:%s", runID, index, issue.SourceKey, issue.Reason))
		if _, err := tx.Exec(`INSERT INTO seed_import_issues (id,run_id,severity,blocker,entity,source_key,reason,raw_payload) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, runID, issue.Severity, issue.Blocker, issue.Entity, issue.SourceKey, issue.Reason, issue.RawPayload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateEmptyOperationalDatabase(db *sql.DB) error {
	for _, table := range []string{"members", "saving_records", "loan_requests", "loans", "loan_repayments", "loan_installments"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("table %s already contains %d operational rows", table, count)
		}
	}
	return nil
}

func hasBlockers(issues []SeedImportIssue) bool {
	for _, issue := range issues {
		if issue.Blocker {
			return true
		}
	}
	return false
}

func seedCounts(prepared preparedSeed, issues []SeedImportIssue) map[string]int {
	counts := map[string]int{
		"members": len(prepared.Members), "savings": len(prepared.Savings),
		"loans": len(prepared.Loans), "repayments": 0, "installments": 0,
		"accounts": 0, "issues": len(issues), "blockers": 0, "warnings": 0,
	}
	for _, member := range prepared.Members {
		if member.Status == "active" {
			counts["accounts"]++
		}
	}
	for _, loan := range prepared.Loans {
		counts["repayments"] += len(loan.Repayments)
		counts["installments"] += len(loan.Schedule)
	}
	for _, issue := range issues {
		if issue.Blocker {
			counts["blockers"]++
		} else if issue.Severity == "warning" {
			counts["warnings"]++
		}
	}
	return counts
}

func buildSeedSchedule(startDate string, duration int, obligation int64, repayments []preparedRepayment, firstDueDateSameMonth bool) []preparedInstallment {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil || duration < 1 || obligation < 1 {
		return nil
	}
	base := obligation / int64(duration)
	remainder := obligation % int64(duration)
	schedule := make([]preparedInstallment, 0, duration)
	firstDueOffset := 1
	if firstDueDateSameMonth {
		firstDueOffset = 0
	}
	for number := 1; number <= duration; number++ {
		amount := base
		if number == duration {
			amount += remainder
		}
		schedule = append(schedule, preparedInstallment{Number: number, DueDate: start.AddDate(0, firstDueOffset+number-1, 0).Format("2006-01-02"), Scheduled: amount})
	}
	sort.Slice(repayments, func(i, j int) bool { return repayments[i].RecordDate < repayments[j].RecordDate })
	for _, repayment := range repayments {
		remaining := repayment.Amount
		for index := range schedule {
			available := schedule[index].Scheduled - schedule[index].Paid
			if available <= 0 {
				continue
			}
			paid := remaining
			if paid > available {
				paid = available
			}
			schedule[index].Paid += paid
			remaining -= paid
			if remaining == 0 {
				break
			}
		}
	}
	return schedule
}

func mapMemberSource(sourceStatus string) (string, string) {
	status := strings.ToLower(strings.TrimSpace(sourceStatus))
	memberStatus := "active"
	if strings.HasPrefix(status, "purna bakti") {
		memberStatus = "inactive"
	}
	memberType := "employee"
	if strings.Contains(status, "phl") {
		memberType = "daily_worker"
	} else if status == "nasabah" {
		memberType = "self_employed"
	}
	return memberStatus, memberType
}

func normalizedName(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func resolveSeedAlias(name string, members map[string]preparedMember) string {
	key := normalizedName(name)
	aliases := map[string]string{"i gede mustika": "i gede mustika", "ari wibowo - 00579": "ari wibowo - 00579"}
	if candidate, ok := aliases[key]; ok {
		if _, exists := members[candidate]; exists {
			return candidate
		}
	}
	return ""
}

func normalizeMemberNo(value string) string { return slugSeed(value) }

func isPlaceholderNPP(value string) bool {
	return value == "0" || strings.HasPrefix(value, "phl") || strings.HasPrefix(value, "nasabah")
}

func slugSeed(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
		} else if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func savingNote(row seeddata.SavingRow) string {
	origin := strings.TrimSpace(row.Source.Sheet)
	if origin == "" {
		origin = "Detail Simpanan"
	}
	parts := []string{"Historical import from " + origin}
	if row.SourceMarker != "" {
		parts = append(parts, "source="+row.SourceMarker)
	}
	if row.Month != "" {
		parts = append(parts, "month="+row.Month)
	}
	if row.Category != "" {
		parts = append(parts, "category="+row.Category)
	}
	return strings.Join(parts, "; ")
}

func parseSeedMoney(raw string) (int64, bool, error) {
	rat, err := parseSeedMoneyRat(raw)
	if err != nil {
		return 0, false, err
	}
	fractional := rat.Denom().Cmp(big.NewInt(1)) != 0
	if fractional {
		return 0, true, nil
	}
	if !rat.Num().IsInt64() {
		return 0, false, fmt.Errorf("amount overflows int64")
	}
	return rat.Num().Int64(), false, nil
}

func parseSeedMoneyRounded(raw string) (int64, bool, error) {
	rat, err := parseSeedMoneyRat(raw)
	if err != nil {
		return 0, false, err
	}
	if rat.Denom().Cmp(big.NewInt(1)) == 0 {
		if !rat.Num().IsInt64() {
			return 0, false, fmt.Errorf("amount overflows int64")
		}
		return rat.Num().Int64(), false, nil
	}

	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(rat.Num(), rat.Denom(), remainder)
	doubleRemainder := new(big.Int).Abs(remainder)
	doubleRemainder.Lsh(doubleRemainder, 1)
	if doubleRemainder.Cmp(rat.Denom()) >= 0 {
		if rat.Num().Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, false, fmt.Errorf("amount overflows int64")
	}
	return quotient.Int64(), true, nil
}

func parseSeedMoneyRat(raw string) (*big.Rat, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "Rp"), "rp")
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "."))
	if raw == "" || raw == "-" || raw == "—" {
		return new(big.Rat), nil
	}
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, "\u00a0", "")
	sign := ""
	parenthesized := strings.HasPrefix(raw, "(") && strings.HasSuffix(raw, ")")
	if parenthesized {
		raw = strings.TrimSuffix(strings.TrimPrefix(raw, "("), ")")
		sign = "-"
	}
	if strings.HasPrefix(raw, "-") || strings.HasPrefix(raw, "+") {
		if sign == "" {
			sign = raw[:1]
		}
		raw = raw[1:]
	}
	raw = strings.ReplaceAll(raw, "Rp", "")
	raw = strings.ReplaceAll(raw, "rp", "")
	commaCount, dotCount := strings.Count(raw, ","), strings.Count(raw, ".")
	switch {
	case commaCount > 0 && dotCount > 0:
		// The last separator is the decimal separator; the other one is a
		// thousands separator. This preserves fractional source values so they
		// can be quarantined instead of silently rounded.
		if strings.LastIndex(raw, ".") > strings.LastIndex(raw, ",") {
			raw = strings.ReplaceAll(raw, ",", "")
		} else {
			raw = strings.ReplaceAll(raw, ".", "")
			raw = strings.Replace(raw, ",", ".", 1)
		}
	case commaCount > 1:
		raw = strings.ReplaceAll(raw, ",", "")
	case dotCount > 1:
		raw = strings.ReplaceAll(raw, ".", "")
	case commaCount == 1:
		parts := strings.SplitN(raw, ",", 2)
		if len(parts[1]) == 3 {
			raw = strings.ReplaceAll(raw, ",", "")
		} else {
			raw = strings.Replace(raw, ",", ".", 1)
		}
	case dotCount == 1:
		parts := strings.SplitN(raw, ".", 2)
		if len(parts[1]) == 3 {
			raw = strings.ReplaceAll(raw, ".", "")
		}
	}
	if sign == "-" {
		raw = "-" + raw
	}
	rat, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, fmt.Errorf("cannot parse %q", raw)
	}
	return rat, nil
}

func deterministicID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}

func randomTemporaryPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%"
	result := make([]byte, 16)
	for i := range result {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result[i] = alphabet[index.Int64()]
	}
	return string(result), nil
}

func writeSeedCredentials(path string, credentials []SeedImportCredential) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"member_no", "full_name", "email", "temporary_password"}); err != nil {
		return err
	}
	sort.Slice(credentials, func(i, j int) bool { return credentials[i].MemberNo < credentials[j].MemberNo })
	for _, credential := range credentials {
		if err := writer.Write([]string{credential.MemberNo, credential.FullName, credential.Email, credential.Password}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeSeedReport(path string, report SeedImportReport) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}
