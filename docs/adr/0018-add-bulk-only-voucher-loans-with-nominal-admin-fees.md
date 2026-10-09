# 18. Add bulk-only Voucher loans with nominal admin fees

Status: Accepted

## Context

Voucher loans are entered by administrators through Excel bulk import. They use
Primer accounts like Paylater, but their administration fee is an explicit Rupiah
amount rather than a percentage. This extends the type-based fee rules in ADR 14.

## Decision

Use the stable loan type `voucher` with a one-month tenor and the fee policy
`voucher_nominal_one_time_v1`. Template version 2 adds `Biaya Admin (Nominal)`;
Voucher rows require a nonnegative whole Rupiah amount, including zero. Other
loan types leave this column blank. Continue accepting version 1 for existing
products.

Store the imported fee on the request and keep it unchanged when approvers adjust
the principal. The total obligation is the approved principal plus this one-time
fee. Reject Voucher in individual admin and member intake. Preserve the batch
provenance and fee in database guards.

Resolve Voucher journal and Tagihan repayment accounts through existing Paylater
mapping keys while retaining Voucher as the recorded loan type. Include Voucher
in Tagihan and portfolio displays, with English and Indonesian translations.

## Consequences

Administrators can charge different nominal fees per imported row. Approval,
disbursement, corrections, and overrides retain the original fee. SQLite table
rebuilds and PostgreSQL constraints extend existing product guards; historical
loans retain their existing terms.
