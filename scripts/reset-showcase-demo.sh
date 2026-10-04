#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

export DATABASE_PATH="${DATABASE_PATH:-data/clubcore_showcase_demo.db}"
CLUBCORE_DEMO_PASSWORD="${CLUBCORE_DEMO_PASSWORD:-mon-mot-de-passe-de-test}"
export CLUBCORE_DEMO_PASSWORD

# Validate before any deletion. Only explicit local demo filenames are accepted.
case "$DATABASE_PATH" in
  */|*:*|*\?*|*$'\n'*|*$'\r'*) echo 'Reset refusé : chemin local ambigu.' >&2; exit 1 ;;
esac
case "$(basename -- "$DATABASE_PATH")" in
  *_demo.db|*_demo.sqlite|*_demo.sqlite3|*_demo) ;;
  *) echo 'Reset refusé : le nom de la base doit se terminer par _demo (hors extension SQLite).' >&2; exit 1 ;;
esac
for file in "$DATABASE_PATH" "$DATABASE_PATH-wal" "$DATABASE_PATH-shm"; do
  if [[ -L "$file" || ( -e "$file" && ! -f "$file" ) ]]; then
    echo 'Reset refusé : fichier ambigu, lien symbolique ou fichier non régulier.' >&2
    exit 1
  fi
done
# auth.HashPassword applies the same byte-length policy in the Go bootstrap.
password_bytes=$(LC_ALL=C printf '%s' "$CLUBCORE_DEMO_PASSWORD" | wc -c)
if (( password_bytes < 12 || password_bytes > 72 )); then
  echo 'Reset refusé : CLUBCORE_DEMO_PASSWORD doit contenir 12 à 72 octets.' >&2
  exit 1
fi
# Build and run the shared Go guard before resolving or deleting the target.
clubctl_binary=$(mktemp)
trap 'rm -f -- "$clubctl_binary"' EXIT
go build -o "$clubctl_binary" ./cmd/clubctl
"$clubctl_binary" check-demo-reset-path --confirm-demo
DATABASE_PATH="$(realpath -m -- "$DATABASE_PATH")"
export DATABASE_PATH
"$clubctl_binary" check-demo-reset-path --confirm-demo

rm -f -- "$DATABASE_PATH" "$DATABASE_PATH-wal" "$DATABASE_PATH-shm"
"$clubctl_binary" migrate
"$clubctl_binary" seed-budokan --confirm-empty-demo
"$clubctl_binary" prepare-demo-office --confirm-demo
"$clubctl_binary" prepare-showcase-demo --confirm-demo
"$clubctl_binary" verify-showcase-demo --confirm-demo

printf '\nClub Core — showcase réinitialisé\n\nBase :\n  %s\n\n' "$DATABASE_PATH"
printf 'Mot de passe :\n  %s\n\n' "$CLUBCORE_DEMO_PASSWORD"
printf 'Comptes bureau :\n  president.demo\n  secretary.demo\n  treasurer.demo\n\n'
printf 'Comptes de recette :\n  member.demo\n    adulte actif + historique + 2 contacts + consentement\n\n  parent.demo\n    famille avec 4 enfants : actif, en attente, vide, historique seul\n\n  secretary.member.demo\n    espace personnel + rôle secrétaire, adhésion sans groupe\n\n  empty.demo\n    états vides\n\n'
printf 'Intégrité :\n  foreign_key_check : vide\n  integrity_check : ok\n\nLancer :\n  DATABASE_PATH=%q ./scripts/run-dev.sh\n' "$DATABASE_PATH"
