# Use live-calculated Tagihan

Accepted. Tagihan is generated from current Member, Simpanan, Loan, and Installment Schedule state rather than stored as a durable statement snapshot. When a returned Tagihan is imported, the system reads the returned paid or unpaid row status but recalculates what is still due or still unrecorded for the Tagihan Statement Month at import time, preventing duplicate Saving Records and Repayment Records while keeping the first version of Tagihan simpler to operate.

**Consequences**

- A returned Tagihan may not exactly reproduce the amounts originally exported if cooperative records changed before import.
- Import results must explain rows or components that were skipped because they were already satisfied or already recorded.
- Tagihan-created records need Tagihan-derived traceability, such as statement month and Member Identifier, because there is no stored statement snapshot to reference.
- The first Tagihan exchange format is Excel `.xlsx` to match the cooperative's existing company-facing workflow.
