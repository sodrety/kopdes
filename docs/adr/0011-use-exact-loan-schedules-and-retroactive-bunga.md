# Use exact Loan schedules and retroactive Bunga

Accepted. Extends ADR 0009 and supersedes the remaining principal-only behavior described by ADR 0006.

An Admin records the exact Loan start/disbursement date during approval. The first Installment Schedule deadline is one calendar month later. Every later deadline is calculated from the original start day, clamped to the target month's final valid day (for example, 31 January produces 29 February in a leap year, 31 March, and 30 April). The final deadline is the last installment deadline.

Flat monthly **Bunga** defaults to 1.00% and may be set by an Admin from 0.00% through 10.00%, stored as integer basis points (`100` = 1.00%). Total obligation is approved principal plus flat Bunga across the full tenor. Money is stored in whole Rupiah; division remainder is assigned to the final installment.

Total Bunga is calculated once from principal, monthly basis-point rate, and tenor, then rounded to the nearest whole Rupiah using half-up rounding. Installment division happens only after this rounding.

Repayment Records are allocated to the oldest unpaid scheduled installment first. A partially covered installment remains unpaid and becomes overdue immediately after its exact deadline. This status creates neither a penalty nor additional Bunga. Scheduled rows remain expectations and are never evidence that money was received.

All existing Loans receive 1.00% monthly Bunga retroactively across their original tenor. Their start date is the Jakarta calendar date of their approval timestamp. Existing immutable Repayment Records are allocated oldest-first. A previously paid Loan whose recalculated balance is positive receives the internal `adjustment_due` status; it does not consume the one-active-Loan slot, but it blocks a new Loan Request until every outstanding balance is zero.

Database `TIMESTAMP` values that do not carry an offset are interpreted as UTC before deriving an Asia/Jakarta calendar date. Offset-bearing timestamps retain their stated instant. This convention matches `CURRENT_TIMESTAMP` and avoids host-time-zone-dependent migration or approval results.

Cancelled Loans are void obligations and are excluded from retroactive Bunga and schedule generation. Migration preserves their `cancelled` status, clears financial obligation and Remaining Balance to zero, and they never block a new Loan Request.

An Admin may correct a start date only for an `active` or `adjustment_due` Loan and only before any Repayment Record exists. Paid and cancelled Loans are immutable. Corrections are audited. A start date must be a valid `YYYY-MM-DD` date, no earlier than the Loan Request date and no later than the current Jakarta date.

Tenor is limited to 1–120 months in the request service, approval service, schedule calculator, browser controls, and database enforcement for future writes. Database migration preserves any pre-existing longer-tenor row so deployment is safe, but such legacy data cannot be newly inserted or updated to another out-of-range value.

When the pending-request uniqueness migration encounters legacy duplicates, it keeps the oldest request by `(created_at, id)` pending and marks every later duplicate rejected before installing the unique index. This deterministic policy avoids silently selecting a newer request.

**Consequences**

- Remaining Balance includes principal and all scheduled Bunga.
- A Member can have one active Loan plus multiple historical `adjustment_due` Loans.
- Exact next and final deadlines are persisted for efficient lists and exports; schedule rows remain the source for allocation and overdue state.
- Member Loan Requests are rejected while any Loan has a positive Remaining Balance.
- Reports and exports must include exact dates, Bunga, total obligation, and Remaining Balance.
- The dashboard's “Active loans” count remains status-specific (`active` only). Reopened historical `adjustment_due` debt is included in outstanding balances, not mislabeled as an active Loan.
