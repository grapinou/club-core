#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

export DATABASE_PATH="${DATABASE_PATH:-data/clubcore_demo.db}"
export APP_BASE_URL="${APP_BASE_URL:-http://localhost:8080}"
export APP_TIMEZONE="${APP_TIMEZONE:-Europe/Paris}"
export EMAIL_TRANSPORT="${EMAIL_TRANSPORT:-disabled}"

# The guarded command seeds an empty demo or upgrades existing Budokan data.
# It handles files already created by a previous migration/startup attempt.
go run ./cmd/clubctl prepare-budokan-demo --confirm-demo

echo "Club Core — environnement de développement"
echo "Site : $APP_BASE_URL"
echo "Base SQLite : $DATABASE_PATH"
echo "Transport email : $EMAIL_TRANSPORT"
echo "Ctrl+C arrête le serveur. La base est conservée."

exec go run ./cmd/server
