# Use a fixed Officer hierarchy for request approval

Accepted. This decision supersedes ADR 0005's assignable `admin` role and the routine post-approval Loan Start Date correction allowed by ADR 0011. Users are either Members or Officers holding one fixed role: Manager, Ketua I, Ketua II, or Ketua Utama. Every Loan Request and Penarikan follows the same sequential chain for every amount: Manager, Ketua I, Ketua II, then Ketua Utama. Rejection at any stage and Member cancellation are terminal; only Ketua Utama Final Approval creates the Loan or withdrawal Saving Record and confirms the externally completed activity required by ADR 0004.

Approval authority does not imply inherited operational access. Manager retains day-to-day back-office operations and the Manager approval stage. Ketua I and Ketua II receive read-only oversight plus their own approval stages. Ketua Utama receives the same oversight, Final Approval, and exclusive Officer-account administration. Page, API, action, and navigation access use one explicit fixed permission matrix. Multiple Officers may hold a role, and the same person may decide multiple stages only when they hold each current role at decision time.

Requests remain immutable after submission. Manager fixes Proposed Loan Terms during the first approval stage; later Officers approve that snapshot or reject it. Approval History preserves the deciding Officer, role snapshot, decision, time, and immutable optional approval note or mandatory rejection reason. Officers see full history; Members see the current state and latest completed decision without Officer identity. Post-approval Loan Start Date correction is removed from routine operations.

Pending Penarikan reserves Available Withdrawal Balance without changing Saving Balance. Rejection or Member cancellation releases the reservation; Final Approval converts it into a withdrawal Saving Record. Direct routine creation of Sukarela withdrawal records is prohibited, and only linked Member users may submit Penarikan for now.

Ketua Utama creates, deactivates, reactivates, resets credentials for, and assigns roles to Officers. Officer accounts have full name, email, role, and active status; new and reset credentials require a password change on next login. The last active Ketua Utama cannot be demoted or deactivated. Role and active-status changes invalidate existing sessions immediately. Officer administration changes are immutable audit events without password values.

Persistent in-app notifications are created when a request enters a role's stage and when a request reaches a terminal outcome. Notifications are per-user, read/unread, linked to the request, and resolved for other Officers when one Officer decides the stage. Notification events are stored independently from delivery so a future email channel can consume the same events without coupling email to approval logic.

**Migration**

- Existing `admin` users become Manager Officers.
- Existing pending Loan Requests and Penarikan begin at the Manager stage with empty Approval History.
- Existing terminal requests and financial records remain historical and are not retroactively given approval stages.
- The initial Ketua Utama is created through deployment bootstrap credentials.

**Consequences**

- Role identifiers and approval stages are fixed stable English values; browser copy and status labels remain translated in English and Bahasa Indonesia.
- Approval decisions, Officer audit events, notifications, and withdrawal reservations require durable data with concurrency-safe single-stage transitions.
- A missing active Officer at any stage delays the chain; higher roles cannot skip that stage.
- Existing admin-only middleware, routes, templates, tests, migrations, seed data, and exports must move to capability-based authorization.
