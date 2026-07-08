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
ADMIN_EMAIL=admin@coop.test
ADMIN_PASSWORD=password
```

When `ADMIN_EMAIL` and `ADMIN_PASSWORD` are set, the app creates the admin user if it does not already exist.
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
The app runs migrations on startup and records applied versions in `schema_migrations`.

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
