# Production Deployment

Production uses one VPS for the first launch:

```text
GitHub test workflow
        ↓ success on main
GitHub-hosted build job → GHCR image
        ↓
Hermes self-hosted runner
        ↓ Tailscale SSH
Kopdes production VPS
        ↓
Caddy → Go app → PostgreSQL
```

The VPS does not build application images. It pulls immutable images from GitHub Container Registry (GHCR). Public SSH is disabled; deployment SSH runs from Hermes through Tailscale.

## Production components

| Component | Responsibility |
| --- | --- |
| GitHub Actions | Run tests, build, and publish images |
| GHCR | Store application images |
| Hermes | Run the deploy job and reach production privately |
| Tailscale | Private Hermes-to-Kopdes SSH path |
| Kopdes VPS | Run Docker Compose |
| Caddy | HTTPS, certificates, and reverse proxy |
| PostgreSQL | Private application database |

## DNS

Point these records to the Kopdes VPS public IP:

- koperasidj.id
- www.koperasidj.id

The Caddyfile redirects www.koperasidj.id to koperasidj.id.

Ports 80 and 443 must be reachable from the public internet for HTTP traffic and automatic TLS certificate issuance.

## Kopdes VPS setup

### 1. Install Docker

Install Docker Engine and the Compose plugin using Docker's official instructions:

- Debian: https://docs.docker.com/engine/install/debian/
- Ubuntu: https://docs.docker.com/engine/install/ubuntu/

Verify:

```sh
sudo systemctl enable --now docker
sudo docker run hello-world
sudo docker compose version
```

Allow the non-root deployment user to run Docker:

```sh
sudo usermod -aG docker deploy
```

Start a new login session after changing group membership. Docker group membership grants root-equivalent container control; keep it limited to the deployment operator.

### 2. Clone the repository

The VPS checkout contains Compose, Caddy, and deployment configuration. The application itself comes from GHCR.

```sh
sudo install -d -o deploy -g deploy /opt/kopdes
sudo -u deploy git clone https://github.com/sodrety/kopdes.git /opt/kopdes
```

For a private repository, configure a read-only GitHub deploy key for the deploy user. Do not put a GitHub password or token in the clone URL.

The final application directory is:

```text
/opt/kopdes
```

This is the value used for the GitHub Actions secret VPS_APP_DIR.

### 3. Configure the host firewall

Keep the provider firewall enabled. Publicly allow only:

```text
80/tcp
443/tcp
```

Allow SSH only on the Tailscale interface:

```sh
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow in on tailscale0 to any port 22 proto tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
sudo ufw status verbose
```

Final UFW state must not contain unrestricted 22/tcp. It should contain 22/tcp on tailscale0.

Never expose PostgreSQL port 5432 publicly. Compose keeps PostgreSQL on the private Compose network.

Docker-published ports can bypass some UFW rules. Keep only Caddy's intended public ports published; do not publish PostgreSQL or the application port.

## Tailscale access

Expected device roles:

```text
hermes       deploy runner and private SSH source
kopdes-prod  production VPS and private SSH target
```

Tailscale policy must allow both:

1. Network access from tag:hermes to tag:kopdes-prod on TCP 22.
2. Tailscale SSH access from tag:hermes to tag:kopdes-prod as local user deploy.

Example policy fragments:

```json
{
  "grants": [
    {
      "src": ["tag:hermes"],
      "dst": ["tag:kopdes-prod"],
      "ip": ["tcp:22"]
    }
  ],
  "ssh": [
    {
      "action": "accept",
      "src": ["tag:hermes"],
      "dst": ["tag:kopdes-prod"],
      "users": ["deploy"]
    }
  ]
}
```

For Mac administration, add a separate rule from autogroup:admin to the required tagged devices and target users. Keep production SSH access limited to known devices and users.

Verify from Hermes:

```sh
tailscale ping kopdes-prod
ssh deploy@kopdes-prod 'hostname; id -un; uptime'
```

## Production environment

Create the real env file on Kopdes. Never commit it.

```sh
cd /opt/kopdes
sudo -u deploy cp .env.production.example .env.production
sudo chmod 600 .env.production
sudo -u deploy nano .env.production
```

Required values:

```env
APP_ENV=production
APP_ADDRESS=:8080
DATABASE_DRIVER=pgx
COOKIE_SECURE=true
APP_IMAGE=ghcr.io/sodrety/kopdes:latest

POSTGRES_DB=kopdes
POSTGRES_USER=kopdes
POSTGRES_PASSWORD=<strong-random-database-password>

JWT_SECRET=<at-least-32-random-characters>

KETUA_UTAMA_MEMBER_ID=ketua-utama-bootstrap
KETUA_UTAMA_EMAIL=<production-admin-email>
KETUA_UTAMA_PASSWORD=<temporary-bootstrap-password>
```

Generate random values on the VPS:

```sh
openssl rand -hex 32
openssl rand -hex 32
openssl rand -hex 24
```

APP_IMAGE is a fallback. Automatic deployment overrides it with an immutable commit tag.

Validate without printing env values:

```sh
sudo -u deploy docker compose \
  --env-file /opt/kopdes/.env.production \
  -f /opt/kopdes/compose.production.yml \
  config --quiet
```

If the GHCR package is private, authenticate Docker on Kopdes once with a read-only package token:

```sh
echo "$GHCR_READ_TOKEN" | docker login ghcr.io \
  --username YOUR_GITHUB_USERNAME \
  --password-stdin
```

Keep the token in Docker's credential store, not in .env.production or Git.

## First launch bootstrap

Complete this once before relying on automatic deployment.

### 1. Start PostgreSQL

```sh
cd /opt/kopdes

sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  up -d postgres
```

Wait until healthy:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  ps postgres
```

### 2. Run migrations without bootstrap credentials

The schema must exist before creating the initial Member:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  run --rm \
  -e KETUA_UTAMA_MEMBER_ID= \
  -e KETUA_UTAMA_EMAIL= \
  -e KETUA_UTAMA_PASSWORD= \
  app
```

Stop the one-off app after migrations finish and the server starts with Ctrl+C.

### 3. Create the bootstrap Member

KETUA_UTAMA_MEMBER_ID must identify an existing active Member. An arbitrary ID is not enough; otherwise the app repeatedly logs load Ketua Utama Member: sql: no rows in result set and exits.

The following uses the default POSTGRES_USER=kopdes and POSTGRES_DB=kopdes values:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  exec -T postgres psql -U kopdes -d kopdes <<'SQL'
INSERT INTO members
  (id, member_no, full_name, join_date, status, member_type)
VALUES
  ('ketua-utama-bootstrap', 'DJ-0001', 'Ketua Utama Bootstrap', CURRENT_DATE, 'active', 'employee')
ON CONFLICT (id) DO NOTHING;
SQL
```

If custom database/user values are configured, replace kopdes in the psql command.

### 4. Start the stack

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  up -d
```

After the first successful login:

1. Change the temporary Ketua Utama password in the application.
2. Remove KETUA_UTAMA_PASSWORD from .env.production.
3. Restart the app:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  up -d app
```

## Smoke test

```sh
curl -fsS https://koperasidj.id/health
curl -fsS https://koperasidj.id/ready
```

Then open https://koperasidj.id/login.

Check service state:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  ps
```

Expected: app, caddy, and postgres are running; PostgreSQL is healthy.

## Hermes GitHub Actions runner

The deployment job runs on Hermes because Kopdes is not publicly reachable on SSH.

### 1. Create the runner

In GitHub:

1. Open sodrety/kopdes.
2. Open Settings → Actions → Runners.
3. Select New self-hosted runner.
4. Select Linux and x64.

Do not select macOS x64. A Linux runner binary must be an ELF executable, not a Mach-O executable.

### 2. Install as a dedicated user on Hermes

```sh
sudo adduser --system --group \
  --home /opt/actions-runner \
  --shell /bin/bash \
  github-runner

sudo install -d -o github-runner -g github-runner \
  /opt/actions-runner-linux-x64

sudo -iu github-runner
cd /opt/actions-runner-linux-x64
```

Run the Linux x64 download/extract commands shown by GitHub, then configure with the one-time token shown by GitHub:

```sh
./config.sh \
  --url https://github.com/sodrety/kopdes \
  --token <one-time-runner-token> \
  --name hermes-deploy \
  --labels hermes,deploy \
  --work _work
```

Install the service:

```sh
exit
cd /opt/actions-runner-linux-x64
sudo ./svc.sh install github-runner
sudo ./svc.sh start
sudo ./svc.sh status
```

Expected status includes:

```text
Active: active (running)
Connected to GitHub
Listening for Jobs
```

Verify the runner is not root:

```sh
sudo systemctl show \
  actions.runner.sodrety-kopdes.hermes-deploy.service \
  -p User
```

Expected:

```text
User=github-runner
```

Pre-approve the Kopdes host key for the runner account:

```sh
sudo -u github-runner -H bash -lc '
  mkdir -p "$HOME/.ssh"
  chmod 700 "$HOME/.ssh"
  ssh -o BatchMode=yes \
    -o StrictHostKeyChecking=accept-new \
    deploy@kopdes-prod \
    "hostname; id -un; uptime"
'
```

## GitHub Actions configuration

Workflow file:

```text
.github/workflows/deploy-production.yml
```

It runs after the test workflow succeeds for main:

1. GitHub checks out the tested commit.
2. GitHub builds and pushes ghcr.io/sodrety/kopdes:<commit-sha>.
3. Hermes runs the deploy job.
4. Hermes connects to deploy@kopdes-prod through Tailscale SSH.
5. Kopdes pulls the exact commit image and verifies /ready.

Configure these environment secrets under GitHub Settings → Environments → production:

| Secret | Value |
| --- | --- |
| VPS_USER | deploy |
| VPS_APP_DIR | /opt/kopdes |

No public SSH secrets are needed. The workflow hardcodes the private Tailscale hostname kopdes-prod. Do not add VPS_HOST, VPS_SSH_KEY, or VPS_KNOWN_HOSTS for the current workflow.

Do not run deployment jobs from pull requests on the Hermes runner. Self-hosted runners must execute only trusted workflows.

## Automatic updates

Push to main:

```text
test → build/push GHCR image → Hermes deploy → Kopdes readiness check
```

Manual deployment from Kopdes is possible when the image tag already exists in GHCR:

```sh
cd /opt/kopdes

APP_IMAGE=ghcr.io/sodrety/kopdes:<commit-sha> \
DEPLOY_COMMIT=<commit-sha> \
./scripts/deploy-production.sh
```

The script refuses a dirty repository, fetches the selected commit, validates Compose configuration, pulls the app image, waits for PostgreSQL, creates a compressed database backup, starts the stack, and verifies /ready.

## Backups

The deploy script stores local backups under /opt/kopdes/backups. Prepare ownership once:

```sh
sudo install -d -o deploy -g deploy -m 700 /opt/kopdes/backups
```

Manual backup:

```sh
cd /opt/kopdes

sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  exec -T postgres \
  sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"' | \
  gzip -9 > "backups/kopdes-$(date -u +%Y%m%d-%H%M%S).sql.gz"
```

Copy backups off the VPS. A backup that exists only on the VPS is not a production backup.

## Restore

Warning: restore overwrites database state. Confirm the backup and target database first.

Stop the app and proxy; keep PostgreSQL running:

```sh
cd /opt/kopdes
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  stop app caddy
```

Restore the selected backup:

```sh
gunzip -c backups/kopdes-YYYYMMDD-HHMMSS.sql.gz | \
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  exec -T postgres \
  sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

Start the app and proxy:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  up -d app caddy
```

Never use docker compose down -v during routine deployment or restore; it can delete the database volume.

## Troubleshooting

### compose.production.yml missing

The VPS clone is missing deployment files. Pull the committed deployment configuration:

```sh
cd /opt/kopdes
sudo -u deploy git pull --ff-only origin main
```

### backups permission error

The backup directory is not owned by deploy:

```sh
sudo chown deploy:deploy /opt/kopdes/backups
sudo chmod 700 /opt/kopdes/backups
```

### App shows Restarting (1)

Read app logs first:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  logs --tail=200 app
```

If logs contain load Ketua Utama Member: sql: no rows in result set, KETUA_UTAMA_MEMBER_ID does not identify an existing active Member. Complete the bootstrap SQL step above, then restart the app.

### /ready fails

Check the app before debugging Caddy:

```sh
sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  ps

sudo -u deploy docker compose \
  --env-file .env.production \
  -f compose.production.yml \
  logs --tail=100 app caddy
```

/ready requires the app to be running and connected to PostgreSQL. Caddy TLS errors can be secondary to an app crash or certificate issuance still in progress.

### Hermes runner reports Exec format error

Check:

```sh
uname -m
file /opt/actions-runner-linux-x64/bin/Runner.Listener
```

For Hermes x86_64, use the GitHub Linux x64 runner. Expected binary type: ELF ... x86-64. Mach-O means the macOS package was downloaded by mistake.

### Deployment job stays queued

Check the Hermes runner is online and labels match:

```text
self-hosted, linux, x64, hermes
```

The workflow deploy job requires all four labels.
