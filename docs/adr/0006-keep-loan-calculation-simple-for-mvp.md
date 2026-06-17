# Keep loan calculation simple for the MVP

Accepted. For the MVP, approved loans use `monthly_installment = approved_amount / duration_months` and `remaining_balance = approved_amount`, with no interest, penalty, amortization, or complex accounting. This is a deliberate scope boundary so the first version can validate cooperative workflows before encoding richer financial policy.

**Consequences**

- Loan approval should create a straightforward principal-only obligation.
- Repayments reduce remaining balance directly.
- Interest, penalty, saving categories, and accounting reports are out of scope until later product decisions define those rules.
