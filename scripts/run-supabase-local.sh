#!/usr/bin/env sh
set -eu

ENV_FILE="${ENV_FILE:-.env.supabase}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing $ENV_FILE. Copy .env.supabase.example to $ENV_FILE and set DATABASE_URL." >&2
  exit 1
fi

set -a
. "$ENV_FILE"
set +a

exec go run ./cmd/api
