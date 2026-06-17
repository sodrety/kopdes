# Use Go, Gin, PostgreSQL, and a layered backend

Accepted. Build the cooperative MVP with Go, Gin, PostgreSQL, pgx, migrations, and a simple handler-service-repository structure because the core complexity is durable business rules over relational cooperative records. This favors explicit SQL, testable service modules, and predictable transactions over a heavier framework or ORM that would hide important financial-record behavior.

**Considered Options**

- Go with Gin and pgx.
- A full-stack JavaScript application.
- An ORM-first backend.

**Consequences**

- Business rules should live in service modules with stable interfaces.
- Repository code should keep SQL explicit and parameterized.
- Transaction boundaries for loan approval and repayment recording must be visible in the backend.
