# Club Core

Club Core est une application Go de gestion d’association. Le Budokan Sud Oise est le jeu de démonstration livré avec le projet ; le code et le modèle restent génériques.

Le site public présente les activités, le planning, les lieux, les contacts et permet de réserver un essai sans compte. L’espace administratif suit les personnes, les essais, les adhésions et les demandes d’inscription. Un espace personnel est également disponible.

## Démarrage local

Prérequis : Go, Docker, `curl` et [Goose](https://pressly.github.io/goose/) dans le `PATH`.

```bash
./scripts/run-dev.sh
```

Ce script conserve une base PostgreSQL Docker locale `clubcore_demo` sur le port 5433, configure `DATABASE_URL`, `APP_BASE_URL` et `APP_TIMEZONE`, applique les migrations Goose, charge le seed Budokan si les tables métier sont vides, puis lance le serveur sur `http://localhost:8080`. Sur une ancienne démonstration Budokan, il complète seulement les informations publiques manquantes et rattache la séance du lundi au groupe public sans modifier les personnes et essais existants. Les lancements suivants conservent les données.

Par défaut, le script lance aussi `club-core-mailpit` : interface locale `http://localhost:8025`, SMTP `localhost:1025`. Les emails d’essai sont capturés dans Mailpit, sans compte ni secret. Ses ports sont publiés uniquement sur l’interface locale. Définir explicitement `EMAIL_TRANSPORT=disabled` ou `EMAIL_TRANSPORT=smtp` empêche cette configuration automatique et permet d’utiliser vos propres paramètres.

Pour utiliser une PostgreSQL existante, définir au minimum `DATABASE_URL`, `APP_BASE_URL` et `APP_TIMEZONE`, appliquer les migrations puis lancer :

```bash
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/server
```

Les migrations sont dans `migrations/`. Les requêtes source de [sqlc](https://sqlc.dev/) sont dans `internal/database/queries/` ; `sqlc generate` met à jour `internal/database/dbsqlc/` après une modification SQL. Le fichier `config/config.json` conserve des réglages éditoriaux historiques ; les données publiques du club viennent de PostgreSQL.

Le jeu Budokan se charge uniquement dans une base de démonstration vide dont le nom se termine par `_demo` :

```bash
go run ./cmd/clubctl seed-budokan --confirm-empty-demo
```

La commande utilise `DATABASE_URL`. Le seed refuse une base non vide et exige un nom se terminant par `_demo`. La commande `go run ./cmd/clubctl upgrade-budokan-demo --confirm-demo` est réservée à une démonstration Budokan existante ; elle est idempotente. Ne lancez pas ces commandes sur une base de production.

Les textes pratiques de la première séance, les consignes de matériel, le libellé du téléphone et les images publiques sont des données PostgreSQL de l’organisation. Le formulaire d’essai enregistre une demande de matériel dans les notes existantes de l’essai. Si SMTP est configuré, une confirmation simple est envoyée après l’enregistrement ; un échec d’envoi ne supprime pas la réservation.

Pour un relais SMTP réel, renseigner localement `EMAIL_TRANSPORT=smtp`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` et `SMTP_STARTTLS` avant de lancer le script. Le destinataire vient du formulaire d’essai (responsable pour un mineur). Le relais doit proposer STARTTLS si `SMTP_STARTTLS=true` ; ne désactivez ce réglage que pour un SMTP local sans authentification. Conservez les secrets hors du dépôt, par exemple dans un fichier `.env.local` ignoré par Git et chargé par votre shell. Gmail peut être utilisé comme relais SMTP standard avec les paramètres et identifiants fournis par ce service, sans configuration particulière dans Club Core.

## Vérification

Les tests d’intégration utilisent PostgreSQL 16 via Testcontainers et nécessitent Docker :

```bash
go test ./...
go vet ./...
git diff --check
```

## Structure

- `internal/application` assemble les services, handlers et routes.
- `internal/organization` fournit les données publiques de l’organisation.
- `internal/trials` valide et crée les essais ; le parcours public y réutilise les mêmes règles de programmation que l’administration.
- `internal/administration` et `internal/handlers` servent le bureau et les pages HTTP.
- `internal/views` contient les templates Go ; `static/` contient les styles et illustrations.
- `internal/demodata/budokan.sql` décrit le club de démonstration.

Les rapports de jalons sont dans `docs/reports/`.
