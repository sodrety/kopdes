# Saving and Loan Cooperative

This context describes a cooperative system that records member savings, loan requests, loan approvals, and repayments after those activities are verified outside the application.

## Language

**Cooperative**:
A member-owned organization that manages member savings and loans.
_Avoid_: Bank, payment processor

**Member**:
A person registered with the cooperative who may hold savings and request loans.
_Avoid_: Customer, client, borrower as a general replacement

**Admin**:
A cooperative operator who records verified activity and reviews member loan requests.
_Avoid_: Teller, superuser

**User**:
A login identity used to access the system as either an **Admin** or a **Member**.
_Avoid_: Account when referring to login identity

**Saving Record**:
A traceable record of a verified saving deposit or withdrawal for a **Member**.
_Avoid_: Payment, transaction, ledger entry

**Saving Balance**:
The derived current savings amount for a **Member** after deposits and withdrawals are applied.
_Avoid_: Account balance, wallet balance

**Loan Request**:
A **Member** request for cooperative financing that is pending, approved, or rejected.
_Avoid_: Loan application when the status record is meant

**Loan**:
An approved obligation created from a **Loan Request**.
_Avoid_: Loan request

**Repayment Record**:
A traceable record of a verified payment made against a **Loan**.
_Avoid_: Payment transaction, installment transaction

**Remaining Balance**:
The unpaid amount still owed on a **Loan**.
_Avoid_: Outstanding account balance

**Reference Number**:
An optional external identifier copied from the manual verification source.
_Avoid_: Transaction ID when the system did not process the transaction

## Relationships

- A **User** has exactly one role: **Admin** or **Member**.
- A **Member** may have one linked member **User**.
- An **Admin** records many **Saving Records** and **Repayment Records**.
- A **Member** owns many **Saving Records**.
- A **Member** may have many **Loan Requests**.
- A **Loan Request** may create zero or one **Loan**.
- A **Member** may have at most one active **Loan**.
- A **Loan** has many **Repayment Records**.
- A **Saving Balance** is derived from **Saving Records**.
- A **Remaining Balance** is reduced by **Repayment Records**.

## Example dialogue

> **Dev:** "When a **Member** transfers money to the cooperative bank account, do we create a payment?"
> **Domain expert:** "No. The app does not process money. After an **Admin** verifies the transfer outside the app, they create a **Saving Record** with the external **Reference Number**."

## Flagged ambiguities

- "account" can mean either **User** or a financial balance. Resolved: use **User** for login identity, **Saving Balance** for member savings, and **Remaining Balance** for loans.
- "payment" can imply the system moved money. Resolved: use **Saving Record** or **Repayment Record** because the system records verified external activity only.
