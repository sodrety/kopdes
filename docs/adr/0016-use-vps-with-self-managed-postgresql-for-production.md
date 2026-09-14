# Use a VPS with self-managed PostgreSQL for production

Accepted. Keep staging on the Docker-managed PostgreSQL path from ADR 0007, but deploy production to a single VPS using Docker Compose to run the Dockerized Go service and PostgreSQL on the same host for budget reasons. This accepts more operational responsibility than managed PostgreSQL in exchange for direct control over cost, server configuration, database access, backups, and future production growth.

**Consequences**

- Production must have explicit PostgreSQL backup, restore, upgrade, monitoring, and firewall procedures before launch.
- PostgreSQL must not be exposed to the public internet, and backups must be copied off the VPS.
- Compose should keep the app, PostgreSQL, and Caddy reverse proxy configuration in one minimal deployment unit.
- Production should set `APP_ENV=production`, `DATABASE_DRIVER=pgx`, `COOKIE_SECURE=true`, and a strong `JWT_SECRET`.
- The VPS should terminate HTTPS for `koperasidj.id` at Caddy, redirect `www.koperasidj.id` to `koperasidj.id`, and forward traffic only to the application service.
- The first production launch should create one minimal active Member for Ketua Utama bootstrap; broader existing Member import can happen later as a separate controlled data task.
- The bootstrap Member should be created by a documented one-time SQL seed after migrations create the schema, then the app should be restarted with `KETUA_UTAMA_MEMBER_ID`, `KETUA_UTAMA_EMAIL`, and a temporary `KETUA_UTAMA_PASSWORD`.
