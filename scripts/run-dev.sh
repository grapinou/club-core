#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

CONTAINER="club-core-postgres"
DB_NAME="clubcore_demo"
DB_USER="clubcore"
DB_PASSWORD="clubcore"
DB_PORT="5433"

export DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@localhost:${DB_PORT}/${DB_NAME}?sslmode=disable"
export APP_BASE_URL="http://localhost:8080"
export APP_TIMEZONE="Europe/Paris"

echo
echo "Club Core — environnement de développement"
echo "──────────────────────────────────────────"

# Docker
if ! docker info >/dev/null 2>&1; then
    echo "✗ Docker n'est pas disponible."
    exit 1
fi
echo "✓ Docker"

# PostgreSQL
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
    if [ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" != "true" ]; then
        echo "→ Démarrage de PostgreSQL..."
        docker start "$CONTAINER" >/dev/null
    fi
else
    echo "→ Création du conteneur PostgreSQL..."
    docker run -d \
        --name "$CONTAINER" \
        -e POSTGRES_USER="$DB_USER" \
        -e POSTGRES_PASSWORD="$DB_PASSWORD" \
        -e POSTGRES_DB="$DB_NAME" \
        -p "${DB_PORT}:5432" \
        postgres:16-alpine >/dev/null
fi

echo "→ Attente de PostgreSQL..."
until docker exec "$CONTAINER" \
    pg_isready -U "$DB_USER" >/dev/null 2>&1; do
    sleep 1
done
echo "✓ PostgreSQL prêt"

# Base de démonstration
DB_EXISTS="$(
    docker exec "$CONTAINER" \
        psql -U "$DB_USER" -d postgres -tAc \
        "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'"
)"

if [ "$DB_EXISTS" != "1" ]; then
    echo "→ Création de la base ${DB_NAME}..."
    docker exec "$CONTAINER" \
        createdb -U "$DB_USER" "$DB_NAME"
fi

echo "✓ Base ${DB_NAME}"

# Goose
if ! command -v goose >/dev/null 2>&1; then
    echo "✗ Goose n'est pas installé ou n'est pas dans PATH."
    exit 1
fi

echo "→ Migrations..."
goose -dir migrations postgres "$DATABASE_URL" up
echo "✓ Migrations"

# Données de démonstration
BUSINESS_ROWS="$(
    docker exec "$CONTAINER" \
        psql -U "$DB_USER" -d "$DB_NAME" -tAc "
            SELECT
                (SELECT COUNT(*) FROM organizations) +
                (SELECT COUNT(*) FROM activities) +
                (SELECT COUNT(*) FROM groups) +
                (SELECT COUNT(*) FROM group_slots) +
                (SELECT COUNT(*) FROM seasons) +
                (SELECT COUNT(*) FROM locations);
        "
)"

if [ "$BUSINESS_ROWS" = "0" ]; then
    echo "→ Chargement des données Budokan..."
    go run ./cmd/clubctl seed-budokan --confirm-empty-demo
    echo "✓ Données Budokan"
else
    echo "✓ Données de démonstration présentes"
fi

echo
echo "──────────────────────────────────────────"
echo " Club Core est prêt"
echo
echo " Site :       http://localhost:8080"
echo " Base :       ${DB_NAME}"
echo " PostgreSQL : localhost:${DB_PORT}"
echo "──────────────────────────────────────────"
echo
echo "Ctrl+C arrête le serveur."
echo "La base PostgreSQL est conservée."
echo

exec go run ./cmd/server