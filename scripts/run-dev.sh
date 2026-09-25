#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

CONTAINER="club-core-postgres"

export DATABASE_URL="postgres://clubcore:clubcore@localhost:5433/clubcore?sslmode=disable"
export APP_BASE_URL="http://localhost:8080"
export APP_TIMEZONE="Europe/Paris"

echo
echo "Club Core — environnement de développement"
echo "──────────────────────────────────────────"

# Vérification Docker
if ! docker info >/dev/null 2>&1; then
    echo "✗ Docker n'est pas disponible."
    exit 1
fi
echo "✓ Docker"

# Démarrage/création de PostgreSQL
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
    if [ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" != "true" ]; then
        echo "→ Démarrage de PostgreSQL..."
        docker start "$CONTAINER" >/dev/null
    fi
else
    echo "→ Création de PostgreSQL..."
    docker run -d \
        --name "$CONTAINER" \
        -e POSTGRES_USER=clubcore \
        -e POSTGRES_PASSWORD=clubcore \
        -e POSTGRES_DB=clubcore \
        -p 5433:5432 \
        postgres:16-alpine >/dev/null
fi

# Attente PostgreSQL
echo "→ Attente de PostgreSQL..."
until docker exec "$CONTAINER" pg_isready -U clubcore -d clubcore >/dev/null 2>&1; do
    sleep 1
done

echo "✓ PostgreSQL prêt"

# Migrations
if ! command -v goose >/dev/null 2>&1; then
    echo "✗ Goose n'est pas installé ou n'est pas dans PATH."
    exit 1
fi

echo "→ Migrations..."
goose -dir migrations postgres "$DATABASE_URL" up
echo "✓ Migrations"

echo
echo "──────────────────────────────────────────"
echo " Club Core est prêt"
echo
echo " http://localhost:8080"
echo "──────────────────────────────────────────"
echo

exec go run ./cmd/server