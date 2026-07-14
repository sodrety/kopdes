# KKSUK PD Dharma Jaya

KKSUK PD Dharma Jaya is a saving and loan cooperative system. The MVP records member, saving, loan request, loan approval, and repayment activity after those activities have been verified outside the application.

## Current Slice

Implemented foundation:

- Go HTTP server using Gin.
- PostgreSQL connection through pgx.
- Initial users schema migration.
- Admin user bootstrap from environment variables.
- JWT login for admin users.
- Role-protected admin dashboard API.
- Admin member management and member-linked login creation.
- Member profile API and server-rendered member profile page.
- Server-rendered login and admin dashboard pages enhanced with htmx.
- Wise-inspired CSS tokens from `DESIGN-wise.md`.

## Environment

Required:

```sh
DATABASE_DRIVER=pgx
DATABASE_URL=postgres://postgres:<database-password>@db.mdnuzqzohiewvbbtspko.supabase.co:5432/postgres?sslmode=require
JWT_SECRET=change_this_secret
```

Optional:

```sh
APP_ADDRESS=:8080
APP_ENV=development
COOKIE_SECURE=false
SERVICE_NAME=kopdes
SERVICE_VERSION=development
METRICS_ENABLED=true
TRACING_ENABLED=false
TRACING_EXPORTER=stdout
TRACING_ENDPOINT=
TRACING_INSECURE=false
KETUA_UTAMA_MEMBER_ID=<existing-active-member-id>
KETUA_UTAMA_EMAIL=ketua-utama@coop.test
KETUA_UTAMA_PASSWORD=password
LEGACY_OFFICER_MEMBER_MAPPINGS='{"legacy-officer@coop.test":"existing-active-member-id"}'
```

`KETUA_UTAMA_MEMBER_ID` must identify an existing active Member. If that Member already has a login, the app keeps the existing credentials and assigns the Ketua Utama appointment. Otherwise, `KETUA_UTAMA_EMAIL` and `KETUA_UTAMA_PASSWORD` create the Member's initial login. Ketua Utama can then assign other Officer appointments to existing Members in the application.

Before migration 13 runs against a database containing legacy standalone Officer users, set `LEGACY_OFFICER_MEMBER_MAPPINGS` to a JSON object whose keys are legacy user IDs or emails and whose values are existing active Member IDs. The migration fails safely when any legacy Officer lacks an explicit mapping.
`APP_ENV=staging` or `APP_ENV=production` enables secure auth cookies by default. Set `COOKIE_SECURE` explicitly to override that default.

## Run

```sh
go run ./cmd/api
```

Open `http://localhost:8080/login`.

For local development against Supabase, create a private env file from the template:

```sh
cp .env.supabase.example .env.supabase
$EDITOR .env.supabase
./scripts/run-supabase-local.sh
```

Use the Supabase database password from the project settings. If the password contains special characters, URL-encode it in `DATABASE_URL`.
The app runs migrations on startup and records applied versions in `schema_migrations`. Migration 13 converts Officer users into Member-backed appointments, so production deployments must prepare the explicit legacy mapping before starting the new release.

## Observability

- `/health` is a liveness check.
- `/ready` checks database connectivity.
- `/metrics` exposes Prometheus metrics when `METRICS_ENABLED` is not set to a false value such as `false`, `0`, or `off`.
- Requests include an `X-Request-ID` response header and emit structured JSON logs.
- Endpoint and database traces are emitted when `TRACING_ENABLED=true`. Use `TRACING_EXPORTER=otlp`, `TRACING_ENDPOINT=<collector-host>:4317`, and `TRACING_INSECURE=true` for a private OTLP gRPC collector such as Grafana Tempo or the OpenTelemetry Collector.

## Test

```sh
go test ./...
```

## Frontend Assets

Browser pages use server-rendered HTML enhanced with local pinned runtime assets:

- htmx `2.0.10` at `/static/vendor/htmx-2.0.10.min.js`
- lucide `0.468.0` at `/static/vendor/lucide-0.468.0.min.js`

Update these files intentionally when upgrading frontend runtime behavior.

## Staging

Use `render.yaml` to create the staging web service and PostgreSQL database on Render. See `docs/deployment/staging.md` for setup and smoke-test steps.
