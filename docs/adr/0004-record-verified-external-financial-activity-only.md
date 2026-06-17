# Record verified external financial activity only

Accepted. The MVP records saving and repayment activity after admins verify it outside the system; it does not move money, verify bank transfers, or act as a payment gateway. This boundary keeps the product focused on cooperative recordkeeping and avoids payment-processing, reconciliation, and compliance complexity that would dominate the MVP.

**Consequences**

- Use domain terms such as Saving Record and Repayment Record instead of Payment for persisted activity.
- Store reference numbers and notes as external traceability fields.
- Do not add automatic bank verification, payment proof uploads, or real-money transfer flows without a later ADR.
