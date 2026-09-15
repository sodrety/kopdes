# Historical seed import notes

This directory is the controlled source for the initial historical import. The
workbooks are normalized into `generated/seed-manifest.json`; the application
injector consumes that manifest and does not parse Excel during import.

## Included source data

- `Master Anggota` is the member authority. Pivot/helper sheets are excluded.
- `Detail Simpanan` is imported row-for-row. Positive values become deposits;
  negative values become withdrawals. `Saldo Awal` uses `2025-12-31`.
- `Data Base Pinjaman` is the loan master. `Detail Pinjaman` is evidence and
  only `3. Cicilan` rows become repayment records.
- `Pinjaman Barang Sekunder`, `Jaket Touring`, and `Pinjaman Daging Event
  Lebaran` map to `secondary_goods`.
- `Pinjaman Barang Primer Harian` and `Voucher Belanja KOKA` map to
  `goods_purchase_paylater`; the latter remains explicitly flagged for business
  confirmation.
- Empty, pivot, helper, and summary sheets are excluded.

## Identity and status rules

- Current/new NPP becomes the canonical `member_no`; old NPP remains source
  provenance.
- Duplicate or placeholder NPPs (`PHL`, `Nasabah`, `0`, and variants) receive a
  deterministic `-r<source-row>` suffix.
- Known name aliases are explicit: `I Gede mustika` → `I Gede Mustika` and
  `ari Wibowo - 00579` → `Ari Wibowo - 00579`. Other unresolved names block
  validation.
- `Aktif`/`Baru` → active; `Purna Bakti ...` → inactive.
- `PHL*` → `daily_worker`; `Nasabah` → `customer`; `Pegawai` → `employee`; and
  `PKWT` → `contract_worker`. Other legacy member types fall back to `employee`.
- Inactive members remain in the import so historical savings and loan records
  retain their member links; only active members receive login accounts.
- Generated accounts use the `member` role and the email form
  `<canonical-npp-slug>@koperasidj.id`. Accounts are created only for active
  members, with a random temporary password and forced first-login reset.

## Amount and category rules

- Operational monetary fields are whole Rupiah integers. Fractional source
  values are preserved in the manifest/audit payload and quarantined; they are
  never rounded.
- `SIMPANAN WAJIB` → `wajib`; `SIMPANAN MANASUKA` → `sukarela`.
- `POKOK` from `Master Anggota` becomes an opening `pokok` record on
  `2025-12-31`.
- The primary loan workbook has no total-obligation column, so the injector
  derives it as the exact integer sum `Pokok + Admin`; it never rounds. When
  the source `Cicilan` cell contains `Lunas` or zero, the base installment is
  derived from obligation/tenor and any exact remainder is placed in the last
  generated installment.
- Two-digit hyphenated dates in the source are interpreted using the workbook's
  `MM-DD-YY` convention (for example `02-04-25` → `2025-02-04`); unparseable
  dates remain blockers. An absent member join date is normalized to January 1
  of the current year for both active and inactive members so inactive members
  can remain available for historical savings and loan links. This fallback is
  synthetic and must not be interpreted as the actual join date.
- `SHU`, ledger `KHUSUS`, master `KHUSUS`, and `PKPRI` are preserved exactly in
  quarantine until the business mapping is confirmed. They block final import.
- Master `WAJIB`, `MANA SUKA`, and `POKOK` values are reconciliation checkpoints
  against the imported ledger.

## Loan provenance

- Historical loans preserve source principal, admin fee, obligation, tenor,
  installment, dates, and derived balance. They use legacy provenance even for
  secondary/pay-later product types because current calculators cannot recreate
  source terms.
- Each loan receives a synthetic legacy request only to satisfy the domain
  relationship. No approval events are fabricated.
- Installment schedules are generated from source start date/tenor/obligation;
  primary-loan `Mulai dipotong` is the first due month, while a secondary
  workbook's disbursement date begins its schedule in the following month;
  repayments are allocated oldest-first. `Detail Pinjaman`'s `No` is treated
  as a detail-row number, so repayments are matched by member and source date
  window; every repayment must still resolve to exactly one source loan.
  Multiple active loans per member are valid. Positive repayment amounts
  become operational repayments. Negative amounts are preserved as refund or
  adjustment events and excluded from normal repayment totals until adjustment
  support is modeled. Zero amounts are preserved as `penangguhan` events and
  are not operational repayments. Every original row remains in the audit or
  quarantine payload.
- Source status and source remaining balance are audit checkpoints. Members may
  have more than one outstanding active loan; each repayment must still map to
  one source loan by explicit loan hint or an unambiguous source date window. A
  source-`Lunas` loan
  whose positive repayment records exceed its obligation is staged as
  `adjustment_due` with a non-blocking reconciliation warning; the difference
  remains available for refund/overpayment review.
- Blank `Metod` rows in `Detail Pinjaman` are excluded and reported as
  unclassified/pending source data.

## Import safety

- Run `go run ./cmd/seed normalize` to regenerate the deterministic manifest.
- Run `go run ./cmd/seed import --dry-run` to validate without operational
  inserts; the default report is written to
  `output/seed/<snapshot-id>-report.json`.
- An initial real import must start before migrations 15–19 so legacy source
  loans can be staged safely; the injector applies the remaining migrations
  after staging.
- A production database already at schema 20 uses `import --append
  --historical-loans`. This loan-focused mode preserves the complete source
  audit, does not replay existing savings, and inserts legacy loan terms and
  positive repayments transactionally. It is PostgreSQL-only.
- The injector is transactional for operational data, content-addresses the
  snapshot, refuses conflicting snapshots, and records runs, source rows, raw
  payloads, and quarantine issues in the database.
- Existing operational rows cause a conflict instead of being overwritten.
- New temporary credentials are written once to a Git-ignored, mode-0600 CSV;
  reruns never reset or regenerate existing passwords. The report contains the
  credential-file path, while the passwords themselves remain only in that
  restricted CSV.

The current workbook snapshot intentionally does not pass final validation yet.
Resolve the quarantined business mappings and source-date/reconciliation issues,
then regenerate the manifest and rerun the dry-run report before importing.

## Proposed clean workbook

Use the generated `seed-source-template.xlsx` as the recommended shape when the
source owner prepares a corrected workbook. The tabs are intentionally
relational:

- `Members`: one stable `member_id` per person; keep active and inactive
  members, preserve NPP as text, and record account eligibility.
- `Member_Crosswalk`: map every source name/NPP to one canonical member or an
  approved exclusion.
- `Savings`: one movement per row with positive `amount_rp`, explicit
  deposit/withdrawal direction, source category, and application category.
- `Loans`: one loan per row with a unique `loan_id`, exact amounts, dates,
  source status, and member link.
- `Repayments`: one repayment per row linked to exactly one `loan_id`.
- `Exclusions`: source rows intentionally not imported, with reason and
  approval.
- `Decisions`: unresolved business questions and their approved answers.
- `Checks`: completeness checks for missing relationships and decisions.

The current injector still consumes `seed-manifest.json`; this workbook is the
recommended preparation format and does not replace the normalizer until a
direct workbook adapter is added. Source sheet and row references must remain
in every data tab so corrections stay auditable.

For a non-technical data owner, use
`seed-source-template-bahasa.xlsx`. It uses plain Bahasa Indonesia labels,
visible NPP and nomor pinjaman fields instead of database IDs, yellow cells for
inputs, dropdown choices, prewritten questions, and a simple pemeriksaan tab.
The `00_Petunjuk` tab is the starting point. The `09_Contoh_Isian` tab contains
mock examples for each data type; those rows are for guidance only and must be
deleted or ignored before sending the completed workbook. This version is also
a preparation format; the current injector still consumes the normalized
manifest.

The Bahasa workbook is simplified for data entry:

- Headers ending in `*` are mandatory. Fill these first.
- Blue cells are automatic; do not type over them. For example, NPP is filled
  from the selected member and member details are filled from the selected
  loan number.
- In `02_Pencocokan_Nama`, `03_Simpanan`, and `04_Pinjaman`, choose the member
  name from the dropdown sourced from `01_Anggota` instead of retyping it.
- `03_Simpanan` uses one row per member. Enter the values under `KHUSUS`,
  `MANASUKA`, `WAJIB`, and `SHU`; leave a type blank or enter 0 when there is
  no value.
- `03_Simpanan` no longer asks for `Asal Sheet`, `Baris Asal`, `Masuk / Keluar`,
  or `Masukkan?` because the simple format records the member's value by type.
- `04_Pinjaman` does not ask for `Sisa`, `Cicilan Pertama`, or `Tanggal
  Selesai`. Those values are derived or generated by the importer when the
  historical loan data is normalized.
- `05_Angsuran` contains only the loan link and payment facts. `Asal Sheet`,
  `Baris Asal`, and `Masukkan?` are kept out of this simple input tab.
- In `05_Angsuran`, choose `No Pinjaman` from the dropdown sourced from
  `04_Pinjaman`; the member name and NPP then fill automatically.
- `08_Pemeriksaan` highlights missing required data. On a blank workbook,
  unanswered business questions are expected until `07_Pertanyaan` has been
  completed.

## Questions for the source-data owner

Use these questions to resolve the current quarantine report. Ask for an
answer per source row where applicable, preferably using the workbook name,
sheet, and row number.

### Member identity

1. For each of the 243 source names that do not match `Master Anggota`, which
   canonical member/NPP should the row use? Are any of them not cooperative
   members and therefore intended for exclusion?
2. Should the `Baru` status be treated like `Aktif` for account creation? The
   current importer does so.
3. Is the generated deterministic NPP for a non-numeric PHL value acceptable,
   with no member prefix? The current example is `phl-r<source-row>`.

### Member dates

4. The importer uses January 1 of the current year as the synthetic fallback
   join date for every member whose source date is blank, including `Purna
   Bakti` members retained for historical records. This is a placeholder, not
   a historical fact; replace it later when the actual date is available.

### Savings

5. What application category should `SHU` map to? If it should not be imported,
   may it be excluded while preserving the raw value in quarantine?
6. What application category should `KHUSUS` map to, for both Master Anggota
   and Detail Simpanan? If it should not be imported, may it be excluded while
   preserving the raw value in quarantine?
7. What application category should `PKPRI` map to? If it should not be
   imported, may it be excluded while preserving the raw value in quarantine?
8. In Master Anggota, are `WAJIB` and `MANA SUKA` current balances, opening
   balances, monthly contribution values, or another declared amount? Should
   they reconcile exactly to the imported Detail Simpanan ledger?
9. When Master Anggota and Detail Simpanan disagree, which source is
   authoritative, and should the difference be corrected in the workbook or
   recorded as an adjustment?

### Loans and repayments

10. For each loan with a missing or invalid start date, what is the actual
    disbursement or first-deduction date? A member join date cannot substitute
    for a loan date.
11. For each loan with missing/invalid principal or duration, what are the
    correct whole-Rupiah principal and tenor in months?
12. For primary loans where total obligation is absent, is the approved rule
    `Pokok + Admin` correct for every row?
13. For each of the 221 repayments that cannot be matched to exactly one loan,
    which source loan should receive the repayment? If multiple loans are
    genuinely possible, provide a loan identifier or an explicit allocation.
14. When source status says `Lunas` but repayments do not fully settle the
    derived obligation, which is authoritative: the status, the repayment
    rows, or a corrected remaining balance?
15. Are members allowed to have multiple outstanding loans at the same time?
    If yes, approve changing the application/import rule. If no, provide the
    closure date or loan-to-repayment mapping that resolves each overlap.
16. Should blank `Metod` rows remain excluded, or can the source owner classify
    them as a repayment method?

### Secondary products and dates

17. Confirm that `Voucher Belanja KOKA` is `goods_purchase_paylater`.
18. Confirm that secondary-loan schedules begin in the month after the source
    disbursement date, while primary schedules begin in the source's first
    deduction month.

The importer should remain blocked until every question affecting an imported
amount, identity, loan allocation, or category has an explicit answer. Accepted
exclusions remain in the audit/quarantine records rather than being silently
dropped.
