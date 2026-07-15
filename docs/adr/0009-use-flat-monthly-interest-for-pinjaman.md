# Use flat monthly Bunga for Pinjaman

Partially superseded by ADR 0014. The Bunga terminology, flat-rate policy, default rate, and Officer override are no longer accepted. The decision to generate an expected **Installment Schedule** separately from verified **Repayment Records**, which may be partial, exact, or extra, remains accepted.

**Consequences**

- Loan approval must capture a Bunga rate or use the 1.00% monthly default before an installment schedule can be produced.
- Angsuran per month should include principal installment plus flat monthly Bunga.
- Angsuran views should distinguish expected schedule rows from actual verified Repayment Records.
- Remaining Balance should represent the total unpaid obligation, including unpaid principal and scheduled Bunga.
- This does not introduce penalties, declining-balance interest, payment processing, or a full accounting ledger.
