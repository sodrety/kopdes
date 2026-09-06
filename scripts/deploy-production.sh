#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="${APP_DIR:-$(cd -- "$SCRIPT_DIR/.." && pwd)}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env.production}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/compose.production.yml}"
DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"
DEPLOY_COMMIT="${DEPLOY_COMMIT:-}"
SITE_URL="${SITE_URL:-https://koperasidj.id}"
READY_URL="${READY_URL:-${SITE_URL%/}/ready}"
BACKUP_DIR="${BACKUP_DIR:-$APP_DIR/backups}"

log() {
  printf '[deploy] %s\n' "$*"
}

die() {
  printf '[deploy] ERROR: %s\n' "$*" >&2
  exit 1
}

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

on_error() {
  local exit_code=$?
  trap - ERR
  printf '[deploy] Deployment failed with exit code %s. Recent service state:\n' "$exit_code" >&2
  compose ps >&2 || true
  compose logs --no-color --tail=100 app postgres caddy >&2 || true
  exit "$exit_code"
}

trap on_error ERR

cd "$APP_DIR"

for command in git docker curl gzip; do
  command -v "$command" >/dev/null 2>&1 || die "Missing required command: $command"
done

[ -f "$ENV_FILE" ] || die "Missing $ENV_FILE"
[ -f "$COMPOSE_FILE" ] || die "Missing $COMPOSE_FILE"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || die "$APP_DIR is not a Git checkout"
[ "$REPO_ROOT" = "$APP_DIR" ] || die "APP_DIR must be the repository root ($REPO_ROOT detected)"

if [ -n "$(git status --porcelain=v1 --untracked-files=all)" ]; then
  die "Working tree is not clean; refusing to deploy local changes"
fi

chmod 600 "$ENV_FILE"
docker compose version >/dev/null

log "Fetching origin/$DEPLOY_BRANCH"
git fetch --prune origin "$DEPLOY_BRANCH"

if [ -n "$DEPLOY_COMMIT" ]; then
  git cat-file -e "$DEPLOY_COMMIT^{commit}" 2>/dev/null || die "Commit $DEPLOY_COMMIT is not available after fetching origin/$DEPLOY_BRANCH"
  TARGET_REF="$DEPLOY_COMMIT"
else
  TARGET_REF="origin/$DEPLOY_BRANCH"
fi

log "Checking out $(git rev-parse --short "$TARGET_REF")"
git checkout --detach --quiet "$TARGET_REF"

log "Validating Compose configuration"
compose config --quiet

log "Pulling the application image"
compose pull app

log "Starting PostgreSQL"
compose up -d postgres

postgres_ready=false
for ((attempt = 1; attempt <= 30; attempt++)); do
  if compose exec -T postgres sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null 2>&1; then
    postgres_ready=true
    break
  fi
  sleep 2
done

[ "$postgres_ready" = true ] || die "PostgreSQL did not become ready"

umask 077
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
BACKUP_PATH="$BACKUP_DIR/kopdes-$(date -u +%Y%m%d-%H%M%S).sql.gz"
log "Creating database backup at $BACKUP_PATH"
compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"' | gzip -9 > "$BACKUP_PATH"
[ -s "$BACKUP_PATH" ] || die "Database backup is empty"

log "Starting the production stack"
compose up -d --remove-orphans

log "Waiting for $READY_URL"
curl --fail --silent --show-error --location \
  --retry 30 --retry-delay 2 --retry-connrefused \
  "$READY_URL" >/dev/null

log "Deployment succeeded at $(git rev-parse --short HEAD)"
log "Database backup: $BACKUP_PATH"
compose ps
