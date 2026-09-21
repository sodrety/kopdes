# Use live-calculated Tagihan

Accepted. Tagihan is generated from current Member, Simpanan, Loan, and Installment Schedule state rather than stored as a durable statement snapshot. When a returned Tagihan is imported, the system reads the returned paid or unpaid row status but recalculates what is still due or still unrecorded for the Tagihan Statement Month at import time, preventing duplicate Saving Records and Repayment Records while keeping the first version of Tagihan simpler to operate.

**Consequences**

- A returned Tagihan may not exactly reproduce the amounts originally exported if cooperative records changed before import.
- Import results must explain rows or components that were skipped because they were already satisfied or already recorded.
- Tagihan-created records need Tagihan-derived traceability, such as statement month and Member Identifier, because there is no stored statement snapshot to reference.
- The first Tagihan exchange format is Excel `.xlsx` to match the cooperative's existing company-facing workflow.
- Tagihan has no Simpanan Pokok deduction. Simpanan Wajib and Simpanan Manasuka deductions use each member's latest dated record in that category on or before the statement cutoff, based on imported savings data; only a latest deposit contributes a positive deduction. A same-month deposit in a category suppresses that member-specific deduction.
- Tagihan export breaks loan repayments into Regular, Secondary Goods, and Goods Purchase/Paylater columns while import continues to recalculate and record each loan's outstanding installment balance.
