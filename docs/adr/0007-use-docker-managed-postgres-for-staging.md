# Use Docker and managed PostgreSQL for staging

Accepted. Deploy the staging environment as a Dockerized Go web service backed by managed PostgreSQL. Use the hosting platform for HTTPS, logs, health checks, and database backups.

**Considered Options**

- Docker web service with managed PostgreSQL on Render, Fly.io, or Railway.
- VPS with systemd, Nginx, and self-managed PostgreSQL.
- Buildpack-only deployment without an explicit Dockerfile.

**Decision**

Use a Dockerfile in the repository so staging and later production have one repeatable runtime package. Use managed PostgreSQL for staging to avoid spending MVP time on database operations.

Render is the recommended first staging target because the app is a single HTTP service, already exposes `/health`, and only needs environment variables plus PostgreSQL.

**Consequences**

- Staging must set `DATABASE_DRIVER=pgx` and must not use SQLite.
- Staging must set `APP_ENV=staging` and `COOKIE_SECURE=true` when served over HTTPS.
- Startup migrations are acceptable for staging MVP, but production should add versioned migration tracking.
- CI should run `go test ./...` before deploy.
- Platform logs are enough for staging, with structured application logging deferred until usage grows.
