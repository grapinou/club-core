# Club Core

Club Core est une application Go de gestion d’association. Le Budokan Sud Oise est le jeu de démonstration livré avec le projet ; le code et le modèle restent génériques.

Le site public présente les activités, le planning, les lieux, les contacts et permet de réserver un essai sans compte. L’espace administratif suit les personnes, les essais, les adhésions et les demandes d’inscription. Un espace personnel est également disponible.

## Démarrage local

Prérequis : Go, Docker et [Goose](https://pressly.github.io/goose/) dans le `PATH`.

```bash
./scripts/run-dev.sh
```

Ce script conserve une base PostgreSQL Docker locale sur le port 5433, configure `DATABASE_URL`, `APP_BASE_URL` et `APP_TIMEZONE`, applique les migrations Goose, puis lance le serveur sur `http://localhost:8080`. Il ne charge pas automatiquement les données Budokan.

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

La commande utilise `DATABASE_URL`. Consultez `go run ./cmd/clubctl --help` pour les commandes disponibles. Ne lancez pas le seed sur une base de production.

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
