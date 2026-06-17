# PRD: Saving and Loan Cooperative MVP

## Problem Statement

Cooperative admins need a simple, traceable way to record member data, savings, loan requests, loan approvals, and loan repayments. Today these activities happen outside the system, such as through manual bank transfers and offline approval flows, which makes it hard for admins and members to see accurate balances, request status, repayment history, and basic cooperative totals.

Members need member-facing access to view their profile, savings, loan request status, active loan, and repayment history. Admins need admin-facing access to create members, record financial activity that already happened outside the system, review loan requests, record repayments, and view operational summaries.

The MVP must avoid acting like a payment processor or full accounting ledger. It records cooperative activity after an admin has verified it manually.

## Solution

Build a saving and loan cooperative MVP with a Go backend, PostgreSQL database, JWT authentication, role-based APIs, and simple HTML/CSS/JavaScript pages. The system will support two roles: `admin` and `member`.

Admins will manage members, manually record saving deposits and withdrawals, approve or reject pending loan requests, manually record repayments, and view a dashboard summary. Members will log in to view their own cooperative data, submit loan requests, and track savings, loan status, and repayments.

The backend will use a layered structure where handlers receive HTTP requests, services enforce business rules, repositories persist records, and PostgreSQL stores the source of truth. Business-critical operations such as loan approval and repayment recording will run in database transactions.

## User Stories

1. As an admin, I want to log in with my admin account, so that I can access cooperative management features.
2. As a member, I want to log in with my member account, so that I can view my own cooperative records.
3. As a logged-out user, I want invalid credentials to be rejected, so that unauthorized users cannot access cooperative data.
4. As an authenticated user, I want my API access controlled by role, so that admin-only and member-only data stay separated.
5. As an admin, I want to create a member with member number, full name, phone, address, and join date, so that new cooperative members can be registered.
6. As an admin, I want member numbers to be unique, so that member records can be identified reliably.
7. As an admin, I want to see a member list, so that I can find cooperative members quickly.
8. As an admin, I want to view member detail, so that I can review a member's profile and cooperative activity.
9. As a member, I want to view my profile, so that I can confirm the cooperative has my correct information.
10. As an admin, I want members to have statuses of active, inactive, or suspended, so that cooperative rules can distinguish eligible and ineligible members.
11. As an admin, I want to record a saving deposit for an active member, so that verified manual payments are reflected in the system.
12. As an admin, I want to record a saving withdrawal for an active member, so that verified withdrawals are reflected in the system.
13. As an admin, I want saving amounts to require positive values, so that invalid records are not stored.
14. As an admin, I want saving records to include record date, reference number, note, and recorder, so that each saving record is traceable.
15. As an admin, I want withdrawals that exceed current saving balance to be rejected, so that a member's saving balance cannot become negative.
16. As a member, I want to view my saving history, so that I can see all deposits and withdrawals recorded for me.
17. As a member, I want to view my saving summary, so that I can see total deposits, total withdrawals, and current balance.
18. As a member, I want to submit a loan request with amount, duration, and purpose, so that I can ask the cooperative for financing.
19. As a member, I want only active members to be allowed to request loans, so that cooperative policy is enforced consistently.
20. As a member, I want duplicate pending loan requests to be rejected, so that I do not accidentally create multiple open requests.
21. As a member, I want loan requests to be rejected when I already have an active loan, so that the one-active-loan rule is enforced.
22. As a member, I want to view my loan request history, so that I can track past and current requests.
23. As a member, I want to see loan request status, so that I know whether a request is pending, approved, or rejected.
24. As a member, I want to see rejection reasons, so that I understand why a request was declined.
25. As an admin, I want to list pending loan requests, so that I can review requests waiting for a decision.
26. As an admin, I want to view the requested amount, duration, purpose, and member details for a loan request, so that I can make an approval decision.
27. As an admin, I want to approve a pending loan request, so that an approved loan can be created.
28. As an admin, I want approved amount to be equal to or lower than requested amount, so that approvals cannot exceed the member request.
29. As an admin, I want monthly installment to be calculated as approved amount divided by duration months, so that the MVP has a simple no-interest repayment schedule.
30. As an admin, I want loan approval to update the request and create the loan atomically, so that request and loan records cannot become inconsistent.
31. As an admin, I want approved loans to start with remaining balance equal to approved amount, so that repayment tracking starts from the correct principal.
32. As an admin, I want to reject a pending loan request with a reason, so that the member has a traceable outcome.
33. As an admin, I want rejected loan requests not to create loan records, so that declined requests do not affect outstanding loan totals.
34. As a member, I want to view my active loan, so that I can see approved amount, installment amount, and remaining balance.
35. As an admin, I want to list active loans, so that I can monitor outstanding cooperative loans.
36. As an admin, I want to record a repayment for an active loan, so that verified manual repayments are reflected in the system.
37. As an admin, I want repayment amounts to require positive values, so that invalid repayment records are not stored.
38. As an admin, I want repayments that exceed remaining loan balance to be rejected, so that loans cannot be overpaid in the MVP.
39. As an admin, I want repayment recording to create a repayment record and reduce remaining balance atomically, so that repayment history and loan balance stay consistent.
40. As an admin, I want a loan to become paid when remaining balance reaches zero, so that completed loans are clearly closed.
41. As a member, I want to view my repayment history, so that I can track repayments recorded against my loan.
42. As a member, I want my pages to show only my own profile, savings, loan requests, loans, and repayments, so that other members' data stays private.
43. As an admin, I want a dashboard summary with total members, active members, total savings, active loans, total outstanding loan, and pending loan requests, so that I can monitor cooperative operations.
44. As a member, I want a dashboard summary with saving balance, active loan, remaining loan balance, latest saving records, and latest repayments, so that I can understand my current cooperative position.
45. As an API client, I want validation and business rule errors to use a consistent error response shape, so that frontend behavior can be predictable.
46. As a developer, I want common error codes such as validation, unauthorized, forbidden, not found, duplicate data, business rule violation, and internal server error, so that API failures are clear and testable.
47. As a developer, I want password hashes rather than plain text passwords, so that account credentials are not stored unsafely.
48. As a developer, I want JWT secrets and database settings to come from environment variables, so that deployment settings are not hard-coded.
49. As a developer, I want database queries to be parameterized, so that input cannot be interpolated into SQL unsafely.
50. As a developer, I want manual financial activity to preserve reference numbers, notes, dates, and recorded-by users, so that records remain auditable.

## Implementation Decisions

- Build the MVP as a Go application using Gin for HTTP routing.
- Use PostgreSQL as the persistent data store and pgx as the database driver.
- Use golang-migrate for schema migrations.
- Use JWT authentication with role-based middleware for `admin` and `member` routes.
- Start with simple HTML, CSS, and JavaScript for the frontend instead of a JavaScript framework.
- Use a layered backend architecture: handler for HTTP request/response, service for business rules, repository for database access, and database migrations for persistence.
- Model login accounts separately from cooperative member records. Admin users do not require a member reference; member users must be linked to a member.
- Store member status as `active`, `inactive`, or `suspended`.
- Store saving records as immutable financial activity rows with `deposit` or `withdrawal` type, positive amount, record date, optional reference number, optional note, and recording admin.
- Derive saving balance from saving records by treating deposits as increases and withdrawals as decreases.
- Reject withdrawals that would make the derived saving balance negative.
- Store loan requests separately from loans. Loan requests represent member intent and review status; loans represent approved obligations.
- Support loan request statuses of `pending`, `approved`, and `rejected`.
- Enforce that inactive or suspended members cannot request loans.
- Enforce that a member can have only one pending loan request at a time.
- Enforce that a member can have only one active loan at a time.
- Approve only pending loan requests.
- Reject only pending loan requests.
- Require rejection reasons for rejected loan requests.
- On approval, update the loan request and create the loan in a single database transaction.
- Allow approved amount to be equal to or lower than requested amount.
- Calculate monthly installment as `approved_amount / duration_months`.
- Do not calculate interest, penalties, fees, amortization, or compound balances in the MVP.
- Store loan status as `active`, `paid`, or `cancelled`.
- Store repayments as immutable rows with loan, member, positive amount, record date, optional reference number, optional note, and recording admin.
- On repayment, create the repayment record and reduce loan remaining balance in a single database transaction.
- Reject repayment amounts greater than the current remaining balance.
- Mark the loan as paid when remaining balance reaches zero.
- Use consistent API error responses with an `error.code` and `error.message`.
- Expose admin APIs under an admin route namespace and member APIs under a member route namespace.
- Member-facing APIs must resolve data from the authenticated JWT member identity rather than accepting arbitrary member IDs.
- Admin dashboard totals should aggregate from current member, saving, loan, and loan request records.
- Member dashboard totals should aggregate only the authenticated member's data.

## Testing Decisions

- Tests should focus on externally visible behavior: returned results, persisted state, validation errors, business rule failures, role enforcement, and transaction outcomes.
- Tests should avoid depending on internal implementation details such as private helper names, handler wiring internals, or repository query formatting beyond the observable database result.
- Unit tests should cover service-level business rules because services are the deep modules that encapsulate most cooperative logic behind stable interfaces.
- Repository integration tests should cover persistence behavior where SQL constraints, joins, and transactions matter.
- API integration tests should cover authentication, role authorization, request validation, response shape, and end-to-end state changes.
- Authentication tests should cover login success, invalid credentials, JWT generation, JWT validation, missing token, invalid token, and role mismatch.
- Member service tests should cover member creation, duplicate member numbers, member list/detail behavior, and member profile lookup from authenticated identity.
- Saving service tests should cover deposits, withdrawals, withdrawal exceeding balance, inactive member rejection, positive amount validation, saving summary, and saving history.
- Loan request service tests should cover active member eligibility, inactive/suspended rejection, duplicate pending request rejection, active loan rejection, request creation, request history, and pending admin list.
- Loan approval service tests should cover approving pending requests, rejecting non-pending approvals, approved amount validation, approved amount greater than requested amount rejection, active loan conflict, monthly installment calculation, and transactional creation of loan records.
- Loan rejection service tests should cover rejecting pending requests, rejection reason persistence, and prevention of loan creation.
- Repayment service tests should cover repayment creation, remaining balance reduction, overpayment rejection, paid status transition, inactive/non-active loan rejection, and transactional balance updates.
- Dashboard service tests should cover admin summary aggregation and member summary aggregation.
- Integration tests should cover the main happy paths: login, create member, record saving, submit loan request, approve loan request, reject loan request, record repayment, and dashboard summary.
- Manual test flows should verify member login through loan request status updates and admin login through member creation, saving recording, loan review, repayment recording, and dashboard review.
- Because the repo is currently empty, there is no prior test pattern to reuse yet. The first implementation should establish naming, fixtures, transaction cleanup, and API test helper conventions.

## Out of Scope

- Payment gateway integration.
- Automatic bank transfer verification.
- Real-money movement.
- Double-entry bookkeeping.
- Complex accounting reports.
- Interest calculation.
- Penalty calculation.
- Uploading payment proof.
- WhatsApp notifications.
- Email notifications.
- Mobile app.
- Vue.js frontend migration.
- Export to Excel.
- Monthly cooperative reports.
- Full audit log beyond recorded-by fields and timestamps.
- Role permission management beyond `admin` and `member`.
- Docker deployment.
- CI/CD pipeline.

## Further Notes

- This PRD is synthesized from the supplied TDD for the saving and loan cooperative MVP.
- The system records financial activities that already happened outside the application after admin verification.
- The first version should answer: who the members are, how much saving each member has, who requested a loan, which loans are approved, how much each member still owes, and what repayments have been recorded.
