# Saving and Loan Cooperative

This context describes a cooperative system that records member savings, loan requests, loan approvals, and repayments after those activities are verified outside the application.

## Language

**Cooperative**:
A member-owned organization that manages member savings and loans.
_Avoid_: Bank, payment processor

**Member**:
A person registered with the cooperative who may hold savings and request loans.
_Avoid_: Customer, client, borrower as a general replacement

**Admin Area**:
The cooperative back-office area available to officers according to their **Officer Role**.
_Avoid_: Treating Admin as an assignable role or a universally authorized superuser

**Officer Role**:
An officer's position in the cooperative authority hierarchy: **Manager**, **Ketua I**, **Ketua II**, or **Ketua Utama**.
_Avoid_: Admin, staff role, access level

**Officer**:
A cooperative operator who holds exactly one **Officer Role**.
_Avoid_: Admin, assuming an Officer Role can be held by only one person

**Manager**:
The first **Officer Role** in the cooperative authority hierarchy.

**Ketua I**:
The **Officer Role** immediately above **Manager** and below **Ketua II**.
_Avoid_: Ketua 1

**Ketua II**:
The **Officer Role** immediately above **Ketua I** and below **Ketua Utama**.
_Avoid_: Ketua 2

**Ketua Utama**:
The highest **Officer Role** in the cooperative authority hierarchy.

**Approval Chain**:
The ordered review of a **Loan Request** or **Penarikan** by **Manager**, **Ketua I**, **Ketua II**, and **Ketua Utama**, regardless of the requested amount.
_Avoid_: Single-admin approval, skipped approval stage

**Rejection**:
The terminal decision by the current officer that ends an **Approval Chain** with a mandatory reason.
_Avoid_: Returning a request to an earlier approval stage, reopening the same request for correction

**Cancellation**:
The terminal decision by a Member to stop their own pending **Loan Request** or **Penarikan** before **Final Approval**.
_Avoid_: Officer Rejection, deleting the request or its Approval History

**Final Approval**:
The **Ketua Utama** decision that completes an **Approval Chain** and authorizes creation of the resulting financial record.
_Avoid_: Treating an earlier-stage approval as permission to create a Loan or withdrawal Saving Record

**Approval Stage**:
The single approve-or-reject decision assigned to one **Officer Role** within an **Approval Chain**.
_Avoid_: Requiring every Officer with the same Officer Role to decide

**Current Approval Stage**:
The **Officer Role** presently responsible for the next decision on a pending **Approval Chain**.
_Avoid_: Treating an earlier or later Officer Role as authorized to act

**Approval History**:
The ordered record of completed **Approval Stages**, including each deciding Officer, decision, decision time, and any decision note.
_Avoid_: Overwriting earlier approvals when the chain advances

**Operational Permission**:
An authority to perform a specific cooperative back-office activity, assigned to an **Officer Role** independently of its place in the approval hierarchy.
_Avoid_: Assuming higher approval authority grants every lower role's operational access

**User**:
A login identity used to access the system as either a **Member** or an officer with exactly one **Officer Role**.
_Avoid_: Account when referring to login identity

**Saving Record**:
A traceable record of a verified saving deposit or withdrawal for a **Member** in exactly one **Simpanan** category.
_Also called in UI_: Simpanan record
_Avoid_: Payment, transaction, ledger entry

**Saving Balance**:
The derived current savings amount for a **Member** after deposits and withdrawals are applied.
_Also called in UI_: Saldo Simpanan
_Avoid_: Account balance, wallet balance

**Available Withdrawal Balance**:
The **Simpanan Sukarela** balance remaining after amounts reserved by pending **Penarikan** requests are excluded.
_Avoid_: Treating reserved amounts as already withdrawn

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
A withdrawal request from a **Member** against **Simpanan Sukarela** that follows the full **Approval Chain** after external verification.
_Avoid_: Withdrawal from **Simpanan Pokok** or **Simpanan Wajib**, payout, disbursement

**Withdrawal Reservation**:
The amount of a pending **Penarikan** excluded from the Member's **Available Withdrawal Balance** until the request is rejected or finally approved.
_Avoid_: Saving Record, completed withdrawal

**Loan Request**:
A **Member** request for cooperative financing that is pending, approved, or rejected.
_Also called in UI_: Permintaan Pinjaman
_Avoid_: Loan application when the status record is meant

**Proposed Loan Terms**:
The approved amount, duration, interest rate, and start date fixed by **Manager** for the remaining **Approval Stages** of a **Loan Request**.
_Avoid_: Allowing a later Officer to alter terms that earlier Officers approved

**Loan**:
An approved obligation created from a **Loan Request**.
_Also called in UI_: Pinjaman
_Avoid_: Loan request

**Pinjaman**:
The Bahasa-first product label for the cooperative loan area, including **Loan Requests** and approved **Loans**.
_Avoid_: Using Pinjaman alone when the distinction between a **Loan Request** and a **Loan** matters

**Bunga**:
A flat monthly interest amount calculated once from a **Loan**'s approved principal, basis-point rate, and tenor, then rounded to whole Rupiah using half-up rounding.
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

**Loan Start Date**:
The exact Jakarta calendar date on which an approved **Loan** is disbursed. Its original day anchors every monthly installment deadline, with invalid month days clamped to the final valid day.
_Avoid_: Request submission date, a timestamp used without Jakarta conversion

**Adjustment Due**:
The internal status for a historically paid **Loan** reopened by retroactive **Bunga**. It carries a Remaining Balance but does not count as the Member's one active Loan.
_Avoid_: Marking multiple historical Loans active

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

- A **User** is either a **Member** user or an **Officer**.
- An **Officer** has exactly one **Officer Role**, and an **Officer Role** may be held by many Officers.
- **Officer Roles** are ordered from lowest to highest authority: **Manager**, **Ketua I**, **Ketua II**, and **Ketua Utama**.
- An Officer accesses the **Admin Area** through the **Operational Permissions** assigned to their **Officer Role**.
- A higher **Officer Role** does not automatically inherit the **Operational Permissions** of a lower Officer Role.
- An **Operational Permission** governs the same activity consistently regardless of how the Officer attempts it.
- A **Loan Request** and **Penarikan** each follow an **Approval Chain** beginning with **Manager**.
- Every **Officer Role** must approve in order, and no approval stage may be skipped.
- Each **Approval Stage** is completed once by any Officer holding its assigned **Officer Role**, with the Officer's identity and decision time retained.
- An approval may include an immutable optional note, while a **Rejection** requires an immutable reason.
- The same Officer may decide more than one **Approval Stage** in an **Approval Chain** when they hold each stage's **Officer Role** at its decision time.
- Each **Approval History** entry retains the Officer Role held at decision time even if the Officer later changes roles.
- A pending **Approval Chain** has exactly one **Current Approval Stage**.
- Completing an approval advances the **Current Approval Stage**, while **Rejection** or **Final Approval** ends it.
- **Cancellation** also ends a pending **Approval Chain** while preserving its completed **Approval History**.
- An **Approval Chain** retains one ordered **Approval History**.
- Officers may view the complete **Approval History**.
- A Member sees the current workflow state and only the latest completed decision, without the deciding Officer's identity.
- **Final Approval** by **Ketua Utama** completes the **Approval Chain**.
- A **Rejection** at any stage ends the **Approval Chain**, and a corrected request must be submitted as a new request.
- Earlier-stage approvals do not create a **Loan**, create a withdrawal **Saving Record**, or change a **Saving Balance**.
- A **Loan** or withdrawal **Saving Record** is created only after **Final Approval**.
- For **Penarikan**, **Final Approval** also confirms that the external withdrawal has been completed and verified; no separate disbursement stage exists.
- For a **Loan Request**, **Final Approval** also confirms that the external loan disbursement and **Loan Start Date** have been verified; no separate disbursement stage exists.
- A **Member** may have one linked member **User**.
- An officer records many **Saving Records** and **Repayment Records** when permitted by their **Officer Role**.
- A **Member** owns many **Saving Records**.
- A **Saving Record** belongs to exactly one **Simpanan** category.
- A **Member** may request many **Penarikan** records against **Simpanan Sukarela**.
- A submitted **Penarikan** remains unchanged; the Member must cancel it and submit a new request to alter its details.
- A **Penarikan** may be submitted only by the Member's linked Member user.
- A pending **Penarikan** holds one **Withdrawal Reservation**.
- A **Withdrawal Reservation** reduces **Available Withdrawal Balance** without changing **Saving Balance**.
- **Rejection** releases the **Withdrawal Reservation**, while **Final Approval** replaces it with a withdrawal **Saving Record**.
- **Cancellation** releases the **Withdrawal Reservation**.
- A **Member** may have many **Loan Requests**.
- A submitted **Loan Request** remains unchanged; the Member must cancel it and submit a new request to alter its requested details.
- **Manager** fixes one set of **Proposed Loan Terms** when approving the first stage of a **Loan Request**.
- **Proposed Loan Terms** remain unchanged throughout the later **Approval Stages**.
- **Proposed Loan Terms** cannot be changed after **Final Approval** through routine operations.
- A **Loan Request** may create zero or one **Loan**.
- A **Member** may have at most one active **Loan**.
- A **Member** may have multiple historical **Adjustment Due** Loans, and cannot submit a new **Loan Request** while any Loan has a positive **Remaining Balance**.
- A cancelled **Loan** is a void obligation: it has no Bunga, schedule, or Remaining Balance and never blocks a new **Loan Request**.
- A **Loan** has one generated **Installment Schedule**.
- A **Loan** has many **Repayment Records**.
- A **Loan** may include **Bunga** as part of its scheduled **Angsuran**.
- A **Repayment Record** may be partial, exact, or extra relative to scheduled **Angsuran** rows.
- **Repayment Records** cover the oldest unpaid **Installment Schedule** row first; partial coverage remains unpaid and may become overdue without adding penalties or Bunga.
- A **Saving Balance** is derived from **Saving Records**.
- A **Remaining Balance** is reduced by **Repayment Records**.
- **Simpanan** contains **Saving Records** and derived **Saving Balances**.
- **Simpanan** may be categorized as **Simpanan Pokok**, **Simpanan Wajib**, or **Simpanan Sukarela**.
- **Penarikan** records verified external withdrawal activity from **Simpanan Sukarela** only.
- Every withdrawal from **Simpanan Sukarela** originates as a **Penarikan** and completes the full **Approval Chain**.
- An Officer cannot create a withdrawal **Saving Record** directly through routine savings recording.
- **Pinjaman** contains **Loan Requests**, approved **Loans**, and **Angsuran** views.

## Example dialogue

> **Dev:** "When a **Member** transfers money to the cooperative bank account, do we create a payment?"
> **Domain expert:** "No. The app does not process money. After an authorized **Officer** verifies the transfer outside the app, they create a **Saving Record** with the external **Reference Number**."

## Flagged ambiguities

- "account" can mean either **User** or a financial balance. Resolved: use **User** for login identity, **Saving Balance** for member savings, and **Remaining Balance** for loans.
- "payment" can imply the system moved money. Resolved: use **Saving Record** or **Repayment Record** because the system records verified external activity only.
- "Simpanan" can mean either a saving activity row or the member's derived savings area. Resolved: use **Saving Record** or **Saving Balance** when precision matters, and **Simpanan** for the UI area.
- "Pinjaman" can mean either a request or an approved obligation. Resolved: use **Loan Request** or **Loan** when precision matters, and **Pinjaman** for the UI area.
- "Admin" previously meant both an assignable user role and the back-office area. Resolved: use **Officer Role** for **Manager**, **Ketua I**, **Ketua II**, or **Ketua Utama**, and **Admin Area** for the back-office surface.
- Legacy users assigned the former "admin" role become **Manager** Officers; "admin" is not retained as an **Officer Role**.
- "Bunga" can mean flat interest, declining-balance interest, penalty, or fee. Resolved: **Bunga** is flat monthly interest on the approved **Loan** principal.
- "Remaining Balance" can mean principal-only or total owed. Resolved: **Remaining Balance** includes approved principal and scheduled **Bunga**.
- "Simpanan Pokok" can mean a one-time row or a category total. Resolved: it is a category total that may be built from one or more **Saving Records**.
- "registration" can mean public self-registration or Admin-created membership. Resolved: KKSUK PD Dharma Jaya uses Admin-created **Member** registration only until a future decision accepts public intake.
- "Penarikan" can imply in-app money movement or withdrawals from any saving category. Resolved: **Penarikan** is an Admin-reviewed withdrawal request from **Simpanan Sukarela** only, with money movement verified outside the app.
- "status" can mean internal workflow state or visible Bahasa text. Resolved: keep internal enum/code names stable in English, and display Bahasa **Status Labels** such as Menunggu, Disetujui, Ditolak, and Selesai in the browser UI.
- "Angsuran" can mean expected schedule or actual repayment. Resolved: use **Installment Schedule** for expected monthly rows and **Repayment Record** for verified actual repayments, which may be partial, exact, or extra.
