package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sodrety/kopdes/internal/app"
	"github.com/sodrety/kopdes/internal/seeddata"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "normalize":
		if err := normalize(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "import":
		if err := importManifest(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func normalize(args []string) error {
	flags := flag.NewFlagSet("normalize", flag.ContinueOnError)
	primary := flags.String("primary", "docs/seed-data/01. Data Base Simpanan dan Pinjaman Koperasi Dharma Jaya tahun buku 2026 Rev2.xlsx", "primary workbook")
	secondary := flags.String("secondary", "docs/seed-data/Rekap Pinjaman Sekunder KKSUK 2026.xlsx", "secondary workbook")
	out := flags.String("out", "docs/seed-data/generated/seed-manifest.json", "normalized manifest output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, err := seeddata.Normalize(*primary, *secondary)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	hash, err := manifest.Hash()
	if err != nil {
		return err
	}
	fmt.Printf("normalized snapshot=%s manifest_hash=%s members=%d savings=%d loans=%d evidence=%d output=%s\n", manifest.SnapshotID, hash, len(manifest.Members), len(manifest.Savings), len(manifest.Loans), len(manifest.LoanEvidence), *out)
	return nil
}

func importManifest(args []string) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "docs/seed-data/generated/seed-manifest.json", "normalized manifest")
	dryRun := flags.Bool("dry-run", false, "validate and report without inserting operational data")
	credentials := flags.String("credentials", "", "restricted output path for new member credentials")
	report := flags.String("report", "", "output path for the reconciliation report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	payload, err := os.ReadFile(*manifestPath)
	if err != nil {
		return err
	}
	var manifest seeddata.Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if *report == "" {
		*report = filepath.Join("output", "seed", manifest.SnapshotID+"-report.json")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	databaseDriver := os.Getenv("DATABASE_DRIVER")
	if databaseDriver == "" {
		databaseDriver = "pgx"
	}
	db, err := app.OpenDatabase(app.Config{DatabaseDriver: databaseDriver, DatabaseURL: databaseURL})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	result, err := app.RunSeedImport(db, manifest, app.SeedImportOptions{DryRun: *dryRun, CredentialsPath: *credentials, ReportPath: *report})
	encoded, encodeErr := json.MarshalIndent(result, "", "  ")
	if encodeErr == nil {
		fmt.Println(string(encoded))
	}
	if err != nil {
		return err
	}
	return encodeErr
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./cmd/seed normalize|import [options]")
}
