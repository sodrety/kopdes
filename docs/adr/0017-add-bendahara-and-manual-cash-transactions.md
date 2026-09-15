# 17. Add Bendahara and manual cash transactions

- Status: Accepted
- Date: 2026-09-15

## Context

The Transaksi Kas page previously aggregated verified savings, withdrawals, loans, and repayments but could not represent verified standalone cash movements. The cooperative also needs an operational Bendahara role without adding that role to the loan and withdrawal approval chain.

## Decision

- Add **Bendahara** as an operational Officer Role outside the approval hierarchy.
- Give Bendahara access only to the complete Transaksi Kas page, manual cash-in/cash-out recording, and cash-category management.
- Keep manual cash transactions standalone and immutable. A correction is another opposite-direction transaction with a clear note.
- Allow backdated transaction dates, reject future dates, and warn rather than block a negative cash balance.
- Seed direction-specific Indonesian categories and allow Manager or Bendahara to add, deactivate, and reactivate categories. A category name is immutable after first use.
- Generate a daily `KAS-YYYYMMDD-####` reference from the transaction date when the user leaves the field blank. Manager or Bendahara can edit it to any non-empty unique value.
- Treat removal of any officer assignment as deactivation of the appointment, preserving history and transaction ownership. The existing safeguard for the last active Ketua Utama remains.
- Include manual entries in the Transaksi Kas ledger, balance report, profit/loss totals, and related exports.

## Consequences

The application stores manual transactions and category audit events separately from member financial records. This preserves the distinction between verified cooperative activity and standalone cash adjustments while keeping the cash ledger and reports complete. Because the Bendahara role is operational only, approval workflow and notification rules remain unchanged.
