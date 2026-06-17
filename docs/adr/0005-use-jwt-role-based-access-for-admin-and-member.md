# Use JWT role-based access for admin and member users

Accepted. Use JWT authentication with explicit `admin` and `member` roles because the MVP has a small, clear access model and needs member-facing APIs to isolate data by authenticated member identity. This keeps authorization straightforward while making admin and member route boundaries visible in handlers and tests.

**Considered Options**

- JWT with role middleware.
- Server sessions.
- A third-party identity provider.

**Consequences**

- Member-facing routes must derive member identity from the token, not from user-supplied member IDs.
- Admin routes should be grouped and protected separately from member routes.
- Tests must cover missing token, invalid token, wrong role, and cross-member access attempts.
