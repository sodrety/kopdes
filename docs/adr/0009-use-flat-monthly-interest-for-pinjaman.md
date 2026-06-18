# Use flat monthly Bunga for Pinjaman

Accepted. Supersedes ADR 0006 for future Pinjaman work: approved loans should support **Bunga** as flat monthly interest on the approved principal, matching KOPKARLYTA-style Pinjaman and Angsuran expectations. The default Bunga rate is 1.00% per month, and an Admin may override it per approved Loan. Approved Loans should generate an expected monthly **Installment Schedule** from principal, tenor, and flat monthly **Bunga**. Admins still record verified **Repayment Records** separately, and those actual repayments may be partial, exact, or extra relative to the schedule.

**Consequences**

- Loan approval must capture a Bunga rate or use the 1.00% monthly default before an installment schedule can be produced.
- Angsuran per month should include principal installment plus flat monthly Bunga.
- Angsuran views should distinguish expected schedule rows from actual verified Repayment Records.
- Remaining Balance should represent the total unpaid obligation, including unpaid principal and scheduled Bunga.
- This does not introduce penalties, declining-balance interest, payment processing, or a full accounting ledger.
