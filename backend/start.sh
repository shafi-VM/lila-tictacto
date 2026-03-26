#!/bin/sh
set -e

# Railway provides postgresql://, Fly.io provides postgres://
# Nakama expects --database.address as user:pass@host:port/db (no scheme, no query string)
DB_ADDR="${DATABASE_URL#postgresql://}" # strip postgresql:// prefix (Railway)
DB_ADDR="${DB_ADDR#postgres://}"        # strip postgres:// prefix (Fly.io) if still present
DB_ADDR="${DB_ADDR%%\?*}"              # strip query string (e.g. ?sslmode=disable)

echo "Running migrations..."
/nakama/nakama migrate up --database.address "$DB_ADDR"

echo "Starting Nakama..."
exec /nakama/nakama \
  --name nakama1 \
  --database.address "$DB_ADDR" \
  --logger.level INFO \
  --runtime.path /nakama/data/modules \
  --runtime.http_key "${NAKAMA_HTTP_KEY:-defaultkey}" \
  --session.token_expiry_sec 7200
