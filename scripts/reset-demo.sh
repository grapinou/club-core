#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

export DATABASE_PATH="${DATABASE_PATH:-data/clubcore_demo.db}"
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
DATABASE_PATH="$(realpath -m -- "$DATABASE_PATH")"
export DATABASE_PATH

# Build first: a compilation failure must not erase an existing demo.
clubctl_binary=$(mktemp)
trap 'rm -f -- "$clubctl_binary"' EXIT
go build -o "$clubctl_binary" ./cmd/clubctl

rm -f -- "$DATABASE_PATH" "$DATABASE_PATH-wal" "$DATABASE_PATH-shm"
"$clubctl_binary" migrate
"$clubctl_binary" seed-budokan --confirm-empty-demo
"$clubctl_binary" prepare-demo-office --confirm-demo
"$clubctl_binary" verify-demo --confirm-demo

printf '\nClub Core — démonstration réinitialisée\n\nBase :\n  %s\n\n' "$DATABASE_PATH"
printf 'Association :\n  Budokan Sud Oise\n\n'
printf 'Référentiel :\n  1 saison\n  2 activités\n  5 groupes\n  16 créneaux\n  3 tarifs\n  1 consentement\n\n'
printf 'Comptes :\n  president.demo  → Président\n  secretary.demo  → Secrétaire\n  treasurer.demo  → Trésorier\n\n'
printf 'Mot de passe :\n  %s\n\n' "$CLUBCORE_DEMO_PASSWORD"
printf 'Données métier :\n  0 essai\n  0 adhésion\n  0 membre\n\nLancer :\n  ./scripts/run-dev.sh\n'
