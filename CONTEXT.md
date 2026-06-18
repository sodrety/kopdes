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
_Avoid_: Teller, superuser, splitting into Bendahara, Pimpinan, or Manager before those roles are explicitly accepted

**User**:
A login identity used to access the system as either an **Admin** or a **Member**.
_Avoid_: Account when referring to login identity

**Saving Record**:
A traceable record of a verified saving deposit or withdrawal for a **Member** in exactly one **Simpanan** category.
_Also called in UI_: Simpanan record
_Avoid_: Payment, transaction, ledger entry

**Saving Balance**:
The derived current savings amount for a **Member** after deposits and withdrawals are applied.
_Also called in UI_: Saldo Simpanan
_Avoid_: Account balance, wallet balance

**Simpanan**:
The Bahasa-first product label for a **Member**'s cooperative savings area.
_Avoid_: Using Simpanan alone when the distinction between **Saving Record** and **Saving Balance** matters

**Simpanan Pokok**:
A required base saving category associated with cooperative membership.
_Avoid_: Treating it as freely withdrawable while membership is active, assuming it must be exactly one record

**Simpanan Wajib**:
A required recurring saving category for cooperative members.
_Avoid_: Treating it as freely withdrawable while membership is active

**Simpanan Sukarela**:
A voluntary saving category that members may request to withdraw from.
_Avoid_: Mixing it with required saving categories

**Penarikan**:
A withdrawal request from a **Member** against **Simpanan Sukarela**, reviewed by an **Admin** after external verification.
_Avoid_: Withdrawal from **Simpanan Pokok** or **Simpanan Wajib**, payout, disbursement

**Loan Request**:
A **Member** request for cooperative financing that is pending, approved, or rejected.
_Also called in UI_: Permintaan Pinjaman
_Avoid_: Loan application when the status record is meant

**Loan**:
An approved obligation created from a **Loan Request**.
_Also called in UI_: Pinjaman
_Avoid_: Loan request

**Pinjaman**:
The Bahasa-first product label for the cooperative loan area, including **Loan Requests** and approved **Loans**.
_Avoid_: Using Pinjaman alone when the distinction between a **Loan Request** and a **Loan** matters

**Bunga**:
A flat monthly interest amount calculated from a **Loan**'s approved principal.
_Avoid_: Declining-balance interest, penalty, fee

**Repayment Record**:
A traceable record of a verified payment made against a **Loan**.
_Also called in UI_: Angsuran
_Avoid_: Payment transaction, installment transaction

**Angsuran**:
The Bahasa-first product label for scheduled or recorded repayments against a **Loan**.
_Avoid_: Payment when the system only records verified external activity

**Installment Schedule**:
The expected monthly **Angsuran** rows generated from an approved **Loan**'s principal, tenor, and flat monthly **Bunga**.
_Avoid_: Treating scheduled rows as proof that money was received

**Remaining Balance**:
The unpaid amount still owed on a **Loan**, including approved principal and scheduled **Bunga**.
_Avoid_: Outstanding account balance, principal-only balance

**Reference Number**:
An optional external identifier copied from the manual verification source.
_Avoid_: Transaction ID when the system did not process the transaction

**Status Label**:
A Bahasa UI label for an internal workflow state.
_Examples_: Menunggu, Disetujui, Ditolak, Selesai
_Avoid_: Renaming stable internal enum values only to match display text

## Relationships

- A **User** has exactly one role: **Admin** or **Member**.
- A **Member** may have one linked member **User**.
- An **Admin** records many **Saving Records** and **Repayment Records**.
- A **Member** owns many **Saving Records**.
- A **Saving Record** belongs to exactly one **Simpanan** category.
- A **Member** may request many **Penarikan** records against **Simpanan Sukarela**.
- A **Member** may have many **Loan Requests**.
- A **Loan Request** may create zero or one **Loan**.
- A **Member** may have at most one active **Loan**.
- A **Loan** has one generated **Installment Schedule**.
- A **Loan** has many **Repayment Records**.
- A **Loan** may include **Bunga** as part of its scheduled **Angsuran**.
- A **Repayment Record** may be partial, exact, or extra relative to scheduled **Angsuran** rows.
- A **Saving Balance** is derived from **Saving Records**.
- A **Remaining Balance** is reduced by **Repayment Records**.
- **Simpanan** contains **Saving Records** and derived **Saving Balances**.
- **Simpanan** may be categorized as **Simpanan Pokok**, **Simpanan Wajib**, or **Simpanan Sukarela**.
- **Penarikan** records verified external withdrawal activity from **Simpanan Sukarela** only.
- **Pinjaman** contains **Loan Requests**, approved **Loans**, and **Angsuran** views.

## Example dialogue

> **Dev:** "When a **Member** transfers money to the cooperative bank account, do we create a payment?"
> **Domain expert:** "No. The app does not process money. After an **Admin** verifies the transfer outside the app, they create a **Saving Record** with the external **Reference Number**."

## Flagged ambiguities

- "account" can mean either **User** or a financial balance. Resolved: use **User** for login identity, **Saving Balance** for member savings, and **Remaining Balance** for loans.
- "payment" can imply the system moved money. Resolved: use **Saving Record** or **Repayment Record** because the system records verified external activity only.
- "Simpanan" can mean either a saving activity row or the member's derived savings area. Resolved: use **Saving Record** or **Saving Balance** when precision matters, and **Simpanan** for the UI area.
- "Pinjaman" can mean either a request or an approved obligation. Resolved: use **Loan Request** or **Loan** when precision matters, and **Pinjaman** for the UI area.
- KOPKARLYTA-style roles such as Bendahara, Pimpinan, and Manager are not current Kopdes roles. Resolved: keep **Admin** and **Member** until a future workflow explicitly accepts more roles.
- "Bunga" can mean flat interest, declining-balance interest, penalty, or fee. Resolved: **Bunga** is flat monthly interest on the approved **Loan** principal.
- "Remaining Balance" can mean principal-only or total owed. Resolved: **Remaining Balance** includes approved principal and scheduled **Bunga**.
- "Simpanan Pokok" can mean a one-time row or a category total. Resolved: it is a category total that may be built from one or more **Saving Records**.
- "registration" can mean public self-registration or Admin-created membership. Resolved: Kopdes uses Admin-created **Member** registration only until a future decision accepts public intake.
- "Penarikan" can imply in-app money movement or withdrawals from any saving category. Resolved: **Penarikan** is an Admin-reviewed withdrawal request from **Simpanan Sukarela** only, with money movement verified outside the app.
- "status" can mean internal workflow state or visible Bahasa text. Resolved: keep internal enum/code names stable in English, and display Bahasa **Status Labels** such as Menunggu, Disetujui, Ditolak, and Selesai in the browser UI.
- "Angsuran" can mean expected schedule or actual repayment. Resolved: use **Installment Schedule** for expected monthly rows and **Repayment Record** for verified actual repayments, which may be partial, exact, or extra.
