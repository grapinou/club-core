#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

CONTAINER="club-core-postgres"
DB_NAME="clubcore_demo"
DB_USER="clubcore"
DB_PASSWORD="clubcore"
DB_PORT="5433"
MAILPIT_CONTAINER="club-core-mailpit"
MAILPIT_SMTP_PORT="1025"
MAILPIT_WEB_PORT="8025"

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

# Capture locale des emails, sauf si un transport est choisi explicitement.
if [ -z "${EMAIL_TRANSPORT:-}" ]; then
    if docker inspect "$MAILPIT_CONTAINER" >/dev/null 2>&1; then
        if [ "$(docker inspect -f '{{.State.Running}}' "$MAILPIT_CONTAINER")" != "true" ]; then
            echo "→ Démarrage de Mailpit..."
            docker start "$MAILPIT_CONTAINER" >/dev/null
        fi
    else
        echo "→ Création du conteneur Mailpit..."
        docker run -d \
            --name "$MAILPIT_CONTAINER" \
            -p "127.0.0.1:${MAILPIT_SMTP_PORT}:1025" \
            -p "127.0.0.1:${MAILPIT_WEB_PORT}:8025" \
            axllent/mailpit:v1.31.1 >/dev/null
    fi
    echo "→ Attente de Mailpit..."
    for attempt in $(seq 1 30); do
        if curl --silent --fail "http://localhost:${MAILPIT_WEB_PORT}/" >/dev/null; then
            break
        fi
        if [ "$attempt" -eq 30 ]; then
            echo "✗ Mailpit ne répond pas sur localhost:${MAILPIT_WEB_PORT}."
            exit 1
        fi
        sleep 1
    done
    export EMAIL_TRANSPORT="smtp"
    export SMTP_HOST="localhost"
    export SMTP_PORT="$MAILPIT_SMTP_PORT"
    export SMTP_FROM="essais@clubcore.test"
    export SMTP_STARTTLS="false"
    unset SMTP_USERNAME SMTP_PASSWORD
    echo "✓ Mailpit prêt"
fi

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
    IS_BUDOKAN="$(
        docker exec "$CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
        "SELECT EXISTS(SELECT 1 FROM organizations WHERE is_active AND name='Budokan Sud Oise' AND public_email='budokansud.oise@gmail.com')"
    )"
    if [ "$IS_BUDOKAN" = "t" ]; then
        go run ./cmd/clubctl upgrade-budokan-demo --confirm-demo >/dev/null
    fi
    echo "✓ Données de démonstration présentes"
fi

echo
echo "──────────────────────────────────────────"
echo " Club Core est prêt"
echo
echo " Site :       http://localhost:8080"
echo " Base :       ${DB_NAME}"
echo " PostgreSQL : localhost:${DB_PORT}"
if [ "${SMTP_HOST:-}" = "localhost" ] && [ "${SMTP_PORT:-}" = "$MAILPIT_SMTP_PORT" ]; then
    echo " Mailpit :    http://localhost:${MAILPIT_WEB_PORT} (SMTP localhost:${MAILPIT_SMTP_PORT})"
fi
echo "──────────────────────────────────────────"
echo
echo "Ctrl+C arrête le serveur."
echo "La base PostgreSQL est conservée."
echo

exec go run ./cmd/server
