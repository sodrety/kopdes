package app

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sodrety/kopdes/internal/seeddata"
)

type SeedEmailRenameOptions struct {
	DryRun          bool
	CredentialsPath string
	OutputPath      string
}

type SeedEmailRename struct {
	MemberNo string `json:"member_no"`
	FullName string `json:"full_name"`
	OldEmail string `json:"old_email"`
	NewEmail string `json:"new_email"`
}

type SeedEmailRenameReport struct {
	Status     string            `json:"status"`
	Updated    int               `json:"updated"`
	Mappings   []SeedEmailRename `json:"mappings"`
	OutputPath string            `json:"output_path,omitempty"`
}

type seedEmailRenameTarget struct {
	Credential SeedImportCredential
	MemberID   string
	UserID     string
	FullName   string
	OldEmail   string
}

func RunSeedEmailRename(db *sql.DB, options SeedEmailRenameOptions) (SeedEmailRenameReport, error) {
	if strings.TrimSpace(options.CredentialsPath) == "" {
		return SeedEmailRenameReport{}, errors.New("credentials path is required")
	}
	credentials, err := readSeedCredentials(options.CredentialsPath)
	if err != nil {
		return SeedEmailRenameReport{}, err
	}
	if !options.DryRun && strings.TrimSpace(options.OutputPath) == "" {
		return SeedEmailRenameReport{}, errors.New("output path is required for a non-dry run")
	}

	tx, err := db.Begin()
	if err != nil {
		return SeedEmailRenameReport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	targets := make([]seedEmailRenameTarget, 0, len(credentials))
	for _, credential := range credentials {
		var target seedEmailRenameTarget
		target.Credential = credential
		err := tx.QueryRow(`SELECT m.id,m.full_name,u.id,u.email FROM members m INNER JOIN users u ON u.member_id=m.id AND u.historical_identity=FALSE WHERE LOWER(m.member_no)=LOWER($1)`, credential.MemberNo).Scan(&target.MemberID, &target.FullName, &target.UserID, &target.OldEmail)
		if errors.Is(err, sql.ErrNoRows) {
			return SeedEmailRenameReport{}, fmt.Errorf("no active member login found for member number %s", credential.MemberNo)
		}
		if err != nil {
			return SeedEmailRenameReport{}, err
		}
		if emailKey(credential.Email) != emailKey(target.OldEmail) {
			return SeedEmailRenameReport{}, fmt.Errorf("credential CSV email for %s does not match database email", credential.MemberNo)
		}
		targets = append(targets, target)
	}
	preparedMembers := make([]preparedMember, 0, len(targets))
	duplicateNames := map[string]int{}
	for _, target := range targets {
		preparedMembers = append(preparedMembers, preparedMember{Source: seeddata.Member{FullName: target.FullName}, MemberNo: target.Credential.MemberNo})
		duplicateNames[normalizedName(target.FullName)]++
	}
	allocator, err := newSeedEmailAllocator(tx, preparedMembers)
	if err != nil {
		return SeedEmailRenameReport{}, err
	}
	for _, target := range targets {
		delete(allocator.used, emailKey(target.OldEmail))
	}
	allocator.duplicateNames = duplicateNames
	report := SeedEmailRenameReport{Status: "dry_run", Mappings: make([]SeedEmailRename, 0, len(targets))}
	for _, target := range targets {
		newEmail := allocator.next(target.FullName, target.Credential.MemberNo)
		report.Mappings = append(report.Mappings, SeedEmailRename{MemberNo: target.Credential.MemberNo, FullName: target.FullName, OldEmail: target.OldEmail, NewEmail: newEmail})
		if !options.DryRun {
			if _, err := tx.Exec(`UPDATE users SET email=$1,updated_at=CURRENT_TIMESTAMP WHERE id=$2`, newEmail, target.UserID); err != nil {
				return SeedEmailRenameReport{}, fmt.Errorf("rename email for %s: %w", target.Credential.MemberNo, err)
			}
		}
	}
	if options.DryRun {
		return report, nil
	}
	if err := tx.Commit(); err != nil {
		return SeedEmailRenameReport{}, err
	}
	updatedCredentials := make([]SeedImportCredential, len(credentials))
	newEmailByMemberNo := make(map[string]string, len(report.Mappings))
	for _, mapping := range report.Mappings {
		newEmailByMemberNo[strings.ToLower(strings.TrimSpace(mapping.MemberNo))] = mapping.NewEmail
	}
	for index, credential := range credentials {
		credential.Email = newEmailByMemberNo[strings.ToLower(strings.TrimSpace(credential.MemberNo))]
		updatedCredentials[index] = credential
	}
	if err := writeSeedCredentials(options.OutputPath, updatedCredentials); err != nil {
		return SeedEmailRenameReport{}, fmt.Errorf("write renamed credentials: %w", err)
	}
	report.Status = "succeeded"
	report.Updated = len(report.Mappings)
	report.OutputPath = options.OutputPath
	return report, nil
}

func readSeedCredentials(path string) ([]SeedImportCredential, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	wantHeader := []string{"member_no", "full_name", "email", "temporary_password"}
	if len(header) != len(wantHeader) {
		return nil, fmt.Errorf("credentials CSV header has %d columns, want %d", len(header), len(wantHeader))
	}
	for index := range wantHeader {
		if header[index] != wantHeader[index] {
			return nil, fmt.Errorf("credentials CSV header column %d is %q, want %q", index+1, header[index], wantHeader[index])
		}
	}
	result := make([]SeedImportCredential, 0)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) != 4 || strings.TrimSpace(row[0]) == "" || strings.TrimSpace(row[2]) == "" {
			return nil, fmt.Errorf("invalid credentials CSV row: %v", row)
		}
		result = append(result, SeedImportCredential{MemberNo: row[0], FullName: row[1], Email: row[2], Password: row[3]})
	}
	return result, nil
}

func WriteSeedEmailRenameReport(path string, report SeedEmailRenameReport) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}
