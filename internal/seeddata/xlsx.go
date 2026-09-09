package seeddata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
)

const (
	openingBalanceDate = "2025-12-31"
	fallbackJoinDate   = "2026-09-07"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

func Normalize(primaryPath, secondaryPath string) (Manifest, error) {
	primary, err := excelize.OpenFile(primaryPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open primary workbook: %w", err)
	}
	defer primary.Close()

	if hasWorkbookSheet(primary, "01_Anggota") && hasWorkbookSheet(primary, "03_Simpanan") {
		return normalizeSimpananTemplate(primaryPath, primary)
	}
	if strings.TrimSpace(secondaryPath) == "" {
		return Manifest{}, fmt.Errorf("secondary workbook is required for the legacy seed workbook")
	}

	secondary, err := excelize.OpenFile(secondaryPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open secondary workbook: %w", err)
	}
	defer secondary.Close()

	sources, err := sourceMetadata(primaryPath, secondaryPath)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Version: ManifestVersion, Sources: sources}
	manifest.SnapshotID = SnapshotID(sources)

	if err := parseMembers(primaryPath, primary, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := parseSavings(primaryPath, primary, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := parsePrimaryLoans(primaryPath, primary, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := parseLoanEvidence(primaryPath, primary, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := parseSecondaryLoans(secondaryPath, secondary, &manifest); err != nil {
		return Manifest{}, err
	}

	manifest.ExcludedSources = []Source{
		{Name: "primary:Pivo Simpanan/Pivo Pinjaman/Sheet1/Sheet2", SHA256: "derived-or-helper"},
		{Name: "secondary:Sheet1/Rekapan Untuk Ikrom/Pinjaman Barang Primer", SHA256: "derived-or-empty"},
	}
	return manifest, nil
}

func hasWorkbookSheet(file *excelize.File, wanted string) bool {
	for _, sheet := range file.GetSheetList() {
		if sheet == wanted {
			return true
		}
	}
	return false
}

func normalizeSimpananTemplate(sourcePath string, file *excelize.File) (Manifest, error) {
	sources, err := sourceMetadata(sourcePath)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Version: ManifestVersion, Sources: sources}
	manifest.SnapshotID = SnapshotID(sources)
	if err := parseTemplateMembers(sourcePath, file, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := parseTemplateSavings(sourcePath, file, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func sourceMetadata(paths ...string) ([]Source, error) {
	result := make([]Source, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read source workbook %s: %w", path, err)
		}
		sum := sha256.Sum256(content)
		result = append(result, Source{Name: filepathBase(path), SHA256: hex.EncodeToString(sum[:])})
	}
	return result, nil
}

func filepathBase(path string) string {
	path = strings.TrimRight(path, string(os.PathSeparator))
	if index := strings.LastIndexAny(path, `/\\`); index >= 0 {
		return path[index+1:]
	}
	return path
}

func parseMembers(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getRows(file, "Master Anggota")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama") >= 0 && countHeader(row, "npp") >= 2
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find Master Anggota header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := cell(row, findColumn(headers, "nama"))
		if strings.TrimSpace(name) == "" || strings.EqualFold(strings.TrimSpace(name), "nama") {
			continue
		}
		nppColumns := findAllColumns(headers, "npp")
		oldNPP := cell(row, firstOr(nppColumns, -1))
		currentNPP := cell(row, lastOr(nppColumns, -1))
		if strings.TrimSpace(currentNPP) == "" {
			currentNPP = oldNPP
		}
		joinDate := parseDate(cell(row, findDateColumn(headers, "tanggal bergabung", "tanggal")))
		if joinDate == "" {
			joinDate = fallbackJoinDate
		}
		manifest.Members = append(manifest.Members, Member{
			Source:       SourceRef{Source: filepathBase(sourcePath), Sheet: "Master Anggota", Row: rowIndex + 1},
			OldNPP:       strings.TrimSpace(oldNPP),
			CurrentNPP:   strings.TrimSpace(currentNPP),
			FullName:     strings.TrimSpace(name),
			SourceStatus: strings.TrimSpace(cell(row, findColumn(headers, "status"))),
			JoinDate:     joinDate,
			Area:         strings.TrimSpace(cell(row, findColumn(headers, "area"))),
			Balances: MemberAmounts{
				Pokok:    cell(row, findColumn(headers, "pokok")),
				Wajib:    cell(row, findColumn(headers, "wajib")),
				Sukarela: cell(row, findColumn(headers, "mana suka", "manasuka")),
				PKPRI:    cell(row, findColumn(headers, "pkpri")),
				Khusus:   joinColumns(row, findAllColumns(headers, "khusus")),
			},
		})
	}
	return nil
}

func parseSavings(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getRows(file, "Detail Simpanan")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama") >= 0 && (findColumn(row, "simpanan wajib", "simpan wajib") >= 0 || findColumn(row, "shu") >= 0)
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find Detail Simpanan header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := strings.TrimSpace(cell(row, findColumn(headers, "nama")))
		if name == "" {
			continue
		}
		marker := strings.TrimSpace(cell(row, findColumn(headers, "keterangan")))
		manifest.Savings = append(manifest.Savings, SavingRow{
			Source:       SourceRef{Source: filepathBase(sourcePath), Sheet: "Detail Simpanan", Row: rowIndex + 1},
			MemberName:   name,
			SourceNPP:    strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
			RecordDate:   parseDateOrOpening(marker),
			SourceMarker: marker,
			Month:        strings.TrimSpace(cell(row, findColumn(headers, "bulan"))),
			Description:  strings.TrimSpace(marker),
			Category:     strings.TrimSpace(cell(row, findColumn(headers, "kategori"))),
			Pokok:        cell(row, findColumn(headers, "pokok")),
			Khusus:       cell(row, findColumn(headers, "khusus")),
			SHU:          cell(row, findColumn(headers, "shu")),
			Wajib:        cell(row, findColumn(headers, "simpanan wajib", "simpan wajib")),
			Sukarela:     cell(row, findColumn(headers, "simpanan manasuka", "simpan manasuka")),
		})
	}
	return nil
}

func parseTemplateMembers(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getStreamingRows(file, "01_Anggota")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama lengkap") >= 0 && findColumn(row, "npp koperasi") >= 0 && findColumn(row, "status") >= 0
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find 01_Anggota header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := strings.TrimSpace(cell(row, findColumn(headers, "nama lengkap")))
		if name == "" || strings.EqualFold(name, "nama lengkap") {
			continue
		}
		oldNPP := strings.TrimSpace(cell(row, findColumn(headers, "npp lama")))
		currentNPP := strings.TrimSpace(cell(row, findColumn(headers, "npp koperasi")))
		if currentNPP == "" {
			currentNPP = oldNPP
		}
		joinDate := parseDate(cell(row, findColumn(headers, "tanggal bergabung")))
		if joinDate == "" {
			joinDate = fallbackJoinDate
		}
		manifest.Members = append(manifest.Members, Member{
			Source:       SourceRef{Source: filepathBase(sourcePath), Sheet: "01_Anggota", Row: rowIndex + 1},
			OldNPP:       oldNPP,
			CurrentNPP:   currentNPP,
			FullName:     name,
			SourceStatus: strings.TrimSpace(cell(row, findColumn(headers, "status"))),
			MemberType:   templateMemberType(cell(row, findColumn(headers, "jenis anggota"))),
			JoinDate:     joinDate,
			Area:         strings.TrimSpace(cell(row, findColumn(headers, "area"))),
		})
	}
	return nil
}

func parseTemplateSavings(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getStreamingRows(file, "03_Simpanan")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama anggota") >= 0 && findColumn(row, "tanggal") >= 0 && findColumn(row, "wajib") >= 0
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find 03_Simpanan header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := strings.TrimSpace(cell(row, findColumn(headers, "nama anggota")))
		if name == "" {
			continue
		}
		marker := strings.TrimSpace(cell(row, findColumn(headers, "catatan")))
		recordDate := parseDateOrOpening(marker)
		if recordDate == "" {
			recordDate = parseDate(cell(row, findColumn(headers, "tanggal")))
		}
		manifest.Savings = append(manifest.Savings, SavingRow{
			Source:       SourceRef{Source: filepathBase(sourcePath), Sheet: "03_Simpanan", Row: rowIndex + 1},
			MemberName:   name,
			SourceNPP:    strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
			RecordDate:   recordDate,
			SourceMarker: marker,
			Description:  marker,
			Pokok:        cell(row, findColumn(headers, "pokok")),
			Khusus:       cell(row, findColumn(headers, "khusus")),
			SHU:          cell(row, findColumn(headers, "shu")),
			Wajib:        cell(row, findColumn(headers, "wajib")),
			Sukarela:     cell(row, findColumn(headers, "manasuka", "mana suka")),
		})
	}
	return nil
}

func getStreamingRows(file *excelize.File, sheet string) ([][]string, error) {
	iterator, err := file.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("read sheet %s: %w", sheet, err)
	}
	defer iterator.Close()
	rows := make([][]string, 0)
	for iterator.Next() {
		// The template formats money as whole Rupiah, but some underlying cells
		// still contain fractions. Keep raw values so validation can quarantine
		// those rows instead of silently importing a display-rounded amount.
		row, err := iterator.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, fmt.Errorf("read sheet %s row: %w", sheet, err)
		}
		rows = append(rows, row)
	}
	if err := iterator.Error(); err != nil {
		return nil, fmt.Errorf("read sheet %s: %w", sheet, err)
	}
	return rows, nil
}

func templateMemberType(value string) string {
	switch normalizeHeader(value) {
	case "phl":
		return "daily_worker"
	case "nasabah":
		return "self_employed"
	case "pegawai", "pkwt":
		return "employee"
	default:
		return ""
	}
}

func parsePrimaryLoans(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getRows(file, "Data Base Pinjaman")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama") >= 0 && (findColumn(row, "tenor") >= 0 || findColumn(row, "lama") >= 0)
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find Data Base Pinjaman header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := strings.TrimSpace(cell(row, findColumn(headers, "nama")))
		if name == "" {
			continue
		}
		cicilanCell := cell(row, findExactColumn(headers, "cicilan"))
		manifest.Loans = append(manifest.Loans, Loan{
			Source:             SourceRef{Source: filepathBase(sourcePath), Sheet: "Data Base Pinjaman", Row: rowIndex + 1},
			LoanType:           "regular",
			MemberName:         name,
			SourceNPP:          strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
			SourceHint:         strings.TrimSpace(cell(row, findExactColumn(headers, "no", "nomor"))),
			Principal:          moneyCellExact(row, headers, "pokok"),
			AdminFee:           moneyCellExact(row, headers, "admin"),
			TotalObligation:    "",
			SourceRemaining:    moneyCell(row, headers, "sisa pinjaman", "sisa"),
			MonthlyInstallment: moneyCellExact(row, headers, "cicilan"),
			DurationMonths:     intCellExact(row, headers, "lama cicilan"),
			StartDate:          parseDate(cell(row, findExactColumn(headers, "mulai dipotong"))),
			EndDate:            parseDate(cell(row, findExactColumn(headers, "terakhir dipotong"))),
			SourceStatus:       sourceLoanStatus(cicilanCell),
			Purpose:            strings.TrimSpace(cell(row, findColumn(headers, "keterangan", "keperluan", "jenis"))),
		})
	}
	return nil
}

func parseLoanEvidence(sourcePath string, file *excelize.File, manifest *Manifest) error {
	rows, err := getRows(file, "Detail Pinjaman")
	if err != nil {
		return err
	}
	headerRow, headers := findHeader(rows, func(row []string) bool {
		return findColumn(row, "nama") >= 0 && findColumn(row, "metod") >= 0 && findColumn(row, "nilai") >= 0
	})
	if headerRow < 0 {
		return fmt.Errorf("%s: could not find Detail Pinjaman header", sourcePath)
	}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		name := strings.TrimSpace(cell(row, findColumn(headers, "nama")))
		if name == "" {
			continue
		}
		method := strings.TrimSpace(cell(row, findColumn(headers, "metod")))
		manifest.LoanEvidence = append(manifest.LoanEvidence, LoanEvidence{
			Source:      SourceRef{Source: filepathBase(sourcePath), Sheet: "Detail Pinjaman", Row: rowIndex + 1},
			MemberName:  name,
			SourceNPP:   strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
			LoanHint:    strings.TrimSpace(cell(row, findColumn(headers, "no", "nomor"))),
			Method:      method,
			Amount:      moneyCell(row, headers, "nilai"),
			RecordDate:  parseDate(cell(row, findDateColumn(headers, "tanggal", "tgl", "bulan"))),
			Description: strings.TrimSpace(cell(row, findColumn(headers, "keterangan", "kategori"))),
		})
	}
	return nil
}

func parseSecondaryLoans(sourcePath string, file *excelize.File, manifest *Manifest) error {
	types := []struct {
		sheet    string
		loanType string
	}{
		{sheet: "Pinjaman Barang Sekunder", loanType: "secondary_goods"},
		{sheet: "Jaket Touring", loanType: "secondary_goods"},
		{sheet: "Pinjaman Daging Event Lebaran", loanType: "secondary_goods"},
		{sheet: "Pinjaman Barang Primer Harian", loanType: "goods_purchase_paylater"},
		{sheet: "Voucher Belanja KOKA", loanType: "goods_purchase_paylater"},
	}
	for _, sourceType := range types {
		sheet, loanType := sourceType.sheet, sourceType.loanType
		rows, err := getRows(file, sheet)
		if err != nil {
			return err
		}
		headerRow, headers := findHeader(rows, func(row []string) bool {
			return findColumn(row, "nama") >= 0
		})
		if headerRow < 0 {
			return fmt.Errorf("%s: could not find %s header", sourcePath, sheet)
		}
		for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
			row := rows[rowIndex]
			name := strings.TrimSpace(cell(row, findColumn(headers, "nama")))
			if name == "" {
				continue
			}
			principalColumn := findExactColumn(headers, "jumlah pinjaman", "total belanja", "total")
			if principalColumn < 0 {
				principalColumn = findColumn(headers, "jumlah pinjaman", "total belanja", "total")
			}
			loan := Loan{
				Source:             SourceRef{Source: filepathBase(sourcePath), Sheet: sheet, Row: rowIndex + 1},
				LoanType:           loanType,
				MemberName:         name,
				SourceNPP:          strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
				SourceHint:         strings.TrimSpace(cell(row, findExactColumn(headers, "no", "nomor"))),
				Principal:          cell(row, principalColumn),
				AdminFee:           cell(row, principalColumn+1),
				TotalObligation:    cell(row, principalColumn+2),
				SourceRemaining:    moneyCell(row, headers, "sisa", "saldo"),
				MonthlyInstallment: moneyCellExact(row, headers, "cicilan perbulan", "cicilan perbulan"),
				DurationMonths:     intCell(row, headers, "tenor", "lama", "jangka"),
				StartDate:          parseDate(cell(row, findColumn(headers, "tgl pinjaman cair", "tanggal pinjaman cair"))),
				EndDate:            parseDate(cell(row, findDateColumn(headers, "tanggal selesai", "tanggal akhir", "akhir"))),
				SourceStatus:       strings.TrimSpace(cell(row, findColumn(headers, "status"))),
				Purpose:            sheet,
			}
			if sheet == "Pinjaman Barang Primer Harian" || sheet == "Voucher Belanja KOKA" {
				loan.Principal = cell(row, findColumn(headers, "total belanja", "total"))
				loan.AdminFee = "0"
				loan.TotalObligation = loan.Principal
				loan.MonthlyInstallment = loan.Principal
				loan.DurationMonths = 1
			}
			manifest.Loans = append(manifest.Loans, loan)
			monthColumns := findMonthColumns(rows, headerRow+1)
			months := make([]int, 0, len(monthColumns))
			for month := range monthColumns {
				months = append(months, month)
			}
			sort.Ints(months)
			for _, month := range months {
				column := monthColumns[month]
				raw := cell(row, column)
				if raw == "" || raw == "0" || raw == "-" {
					continue
				}
				manifest.LoanEvidence = append(manifest.LoanEvidence, LoanEvidence{
					Source:     SourceRef{Source: filepathBase(sourcePath), Sheet: sheet, Row: rowIndex + 1},
					MemberName: name, SourceNPP: strings.TrimSpace(cell(row, findColumn(headers, "npp"))),
					LoanHint: loan.SourceHint, Method: "3. Cicilan", Amount: raw,
					RecordDate: fmt.Sprintf("2026-%02d-01", month), Description: "Secondary workbook monthly payment",
				})
			}
		}
	}
	return nil
}

func getRows(file *excelize.File, sheet string) ([][]string, error) {
	rows, err := file.GetRows(sheet, excelize.Options{RawCellValue: false})
	if err != nil {
		return nil, fmt.Errorf("read sheet %s: %w", sheet, err)
	}
	return rows, nil
}

func findHeader(rows [][]string, predicate func([]string) bool) (int, []string) {
	for i, row := range rows {
		if predicate(row) {
			headers := make([]string, len(row))
			for j, value := range row {
				headers[j] = normalizeHeader(value)
			}
			return i, headers
		}
	}
	return -1, nil
}

func normalizeHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", " ")
	value = nonAlphaNumeric.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func findColumn(headers []string, names ...string) int {
	for i, rawHeader := range headers {
		header := normalizeHeader(rawHeader)
		compactHeader := strings.ReplaceAll(header, " ", "")
		for _, name := range names {
			candidate := normalizeHeader(name)
			compactCandidate := strings.ReplaceAll(candidate, " ", "")
			if header == candidate || strings.Contains(header, candidate) || compactHeader == compactCandidate || strings.Contains(compactHeader, compactCandidate) {
				return i
			}
		}
	}
	return -1
}

func findDateColumn(headers []string, names ...string) int {
	return findColumn(headers, names...)
}

func findAllColumns(headers []string, name string) []int {
	name = normalizeHeader(name)
	var indexes []int
	for i, rawHeader := range headers {
		header := normalizeHeader(rawHeader)
		compactHeader := strings.ReplaceAll(header, " ", "")
		compactName := strings.ReplaceAll(name, " ", "")
		if header == name || strings.Contains(header, name) || compactHeader == compactName || strings.Contains(compactHeader, compactName) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func countHeader(headers []string, name string) int {
	return len(findAllColumns(headers, name))
}

func cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func firstOr(values []int, fallback int) int {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func lastOr(values []int, fallback int) int {
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func joinColumns(row []string, indexes []int) string {
	values := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if value := cell(row, index); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, "|")
}

func moneyCell(row []string, headers []string, names ...string) string {
	return cell(row, findColumn(headers, names...))
}

func moneyCellExact(row []string, headers []string, names ...string) string {
	return cell(row, findExactColumn(headers, names...))
}

func intCell(row []string, headers []string, names ...string) int {
	value := strings.TrimSpace(cell(row, findColumn(headers, names...)))
	if value == "" {
		return 0
	}
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, ",", "")
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func intCellExact(row []string, headers []string, names ...string) int {
	value := strings.TrimSpace(cell(row, findExactColumn(headers, names...)))
	if value == "" {
		return 0
	}
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, ",", "")
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func findExactColumn(headers []string, names ...string) int {
	for i, rawHeader := range headers {
		header := normalizeHeader(rawHeader)
		for _, name := range names {
			if header == normalizeHeader(name) {
				return i
			}
		}
	}
	return -1
}

func sourceLoanStatus(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "lunas") {
		return "Lunas"
	}
	return ""
}

func findMonthColumns(rows [][]string, rowIndex int) map[int]int {
	months := map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "mei": 5, "may": 5, "jun": 6, "jul": 7, "ags": 8, "aug": 8, "sep": 9, "okt": 10, "oct": 10, "nov": 11, "des": 12, "dec": 12}
	result := map[int]int{}
	if rowIndex < 0 || rowIndex >= len(rows) {
		return result
	}
	for column, value := range rows[rowIndex] {
		if month, ok := months[normalizeHeader(value)]; ok {
			result[month] = column
		}
	}
	return result
}

func parseDateOrOpening(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "saldo awal") || strings.Contains(lower, "saldo akhir 2025") {
		return openingBalanceDate
	}
	return parseDate(value)
}

func parseDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	formats := []string{
		"2006-01-02", "02/01/2006", "2/1/2006", "01/02/2006", "1/2/2006",
		"02-01-2006", "2-1-2006", "02 Jan 2006", "2 Jan 2006", "January 2, 2006",
		"02/01/06", "2/1/06", "01-02-06", "1-2-06", "2006 Jan", "2006 Feb", "2006 Mar", "2006 Apr", "2006 May", "2006 Jun", "2006 Jul", "2006 Aug", "2006 Sep", "2006 Oct", "2006 Nov", "2006 Dec", "2006 Jan 2", "2006 Dec 31",
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if serial, err := strconv.ParseFloat(value, 64); err == nil && serial > 20000 && serial < 80000 {
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		return base.Add(time.Duration(serial*24) * time.Hour).Format("2006-01-02")
	}
	return ""
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
