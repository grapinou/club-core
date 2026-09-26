# Club Core

Club Core est une application Go de gestion d’association. Le Budokan Sud Oise est le jeu de démonstration livré avec le projet ; le code et le modèle restent génériques.

Le site public présente les activités, le planning, les lieux, les contacts et permet de réserver un essai sans compte. L’espace administratif suit les personnes, les essais, les adhésions et les demandes d’inscription. Un espace personnel est également disponible.

Les fonctionnalités destinées au bureau doivent pouvoir être utilisées depuis l’interface web. Le CLI est réservé à l’installation, au développement et à la récupération exceptionnelle, pas aux opérations normales de l’association. Un compte disposant du droit de gérer les accès peut consulter les utilisateurs et attribuer ou retirer leurs rôles dans **Administration → Utilisateurs**.

## Première configuration — responsable de l’association

1. Ouvrez l’adresse Club Core communiquée par la personne qui a installé le serveur, puis `/setup`.
2. Saisissez le code de configuration reçu de cette personne.
3. Indiquez votre nom, votre identifiant, votre email et un mot de passe pour créer le premier compte administrateur.
4. Connectez-vous sur `/login`, puis ouvrez **Administration → Configurer le club**.
5. Renseignez votre association, une saison, un lieu et une activité. Créez ensuite un groupe durable, puis son ou ses horaires pour la saison.
6. Ajoutez, selon vos besoins, les types d’adhésion et leurs tarifs, les informations de première séance, la présentation du règlement intérieur et les liens publics.
7. Utilisez **Voir le site public** pour vérifier l’accueil, les horaires, les tarifs, le contact et la page d’essai. Vous pourrez revenir modifier ces données à tout moment.

Chaque étape indique ce qui est déjà renseigné et ce qu’il reste à faire. Les données anciennes sont conservées lorsque vous désactivez un lieu, une activité, un groupe, une saison, un horaire ou un type d’adhésion. Le responsable peut ensuite gérer les autres accès dans **Administration → Utilisateurs**.

Ce parcours ne demande ni terminal ni accès à PostgreSQL. Le code est utilisable une seule fois. Après la création du compte, `/setup` renvoie vers la connexion.

## Installation technique — opérateur

Après avoir créé la base, définir `DATABASE_URL`, appliquer les migrations, puis lancer localement :

```bash
go run ./cmd/clubctl setup-secret
```

La commande affiche une fois le code et l’adresse `/setup`. Remettre le code au responsable par un canal privé, sans le placer dans un journal, un ticket public ou un fichier commité. Le serveur peut être démarré avant ou après cette commande. Si le code est perdu **avant** la configuration, `go run ./cmd/clubctl setup-secret --rotate` en génère un nouveau et invalide l’ancien. Après la configuration, ces commandes refusent toute émission. Une base déjà dotée de comptes au moment de la migration reste fermée au setup web et suit la procédure de maintenance existante.

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

Une instance Club Core représente une association dans sa propre base. La configuration web utilise les tables métier existantes ; la migration `0031` ajoute un montant facultatif, une devise et une note publique aux types d’adhésion, une présentation du règlement à l’organisation, ainsi que les opérations au journal administratif. La permission `club.configure` est actuellement accordée au rôle `president`. Les images publiques peuvent référencer ou retirer des fichiers déjà présents sous `/static/images/` ; l’envoi de nouveaux fichiers n’est pas encore disponible dans le navigateur.

Le jeu Budokan se charge uniquement dans une base de démonstration vide dont le nom se termine par `_demo` :

```bash
go run ./cmd/clubctl seed-budokan --confirm-empty-demo
```

La commande utilise `DATABASE_URL`. Le seed refuse une base non vide et exige un nom se terminant par `_demo`. La commande `go run ./cmd/clubctl upgrade-budokan-demo --confirm-demo` est réservée à une démonstration Budokan existante ; elle est idempotente. Ne lancez pas ces commandes sur une base de production.

Les textes pratiques de la première séance, les consignes de matériel, le libellé du téléphone et les images publiques sont des données PostgreSQL de l’organisation. Le formulaire d’essai enregistre une demande de matériel dans les notes existantes de l’essai. Si SMTP est configuré, une confirmation simple est envoyée après l’enregistrement ; un échec d’envoi ne supprime pas la réservation.

Pour un relais SMTP réel, renseigner localement `EMAIL_TRANSPORT=smtp`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` et `SMTP_STARTTLS` avant de lancer le script. Le destinataire vient du formulaire d’essai (responsable pour un mineur). Le relais doit proposer STARTTLS si `SMTP_STARTTLS=true` ; ne désactivez ce réglage que pour un SMTP local sans authentification. Conservez les secrets hors du dépôt, par exemple dans un fichier `.env.local` ignoré par Git et chargé par votre shell. Gmail peut être utilisé comme relais SMTP standard avec les paramètres et identifiants fournis par ce service, sans configuration particulière dans Club Core.

## Récupération et maintenance techniques

`clubctl grant-role`, `revoke-role` et `list-roles` restent des outils locaux de secours. Ils exigent `DATABASE_URL` et ne font pas partie du parcours normal du bureau. La perte du mot de passe du dernier administrateur n’a pas encore de récupération autonome par email ; elle nécessite une intervention technique distincte. Un nouveau code de setup ne peut jamais rouvrir une installation déjà configurée.

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
