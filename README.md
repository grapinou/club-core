# Club Manager

Club Manager est une application de gestion d'associations développée en Go.

L'objectif du projet est de construire progressivement une application simple, robuste et maintenable pouvant servir de base à différents types d'associations.

Le projet sert également de support d'apprentissage : les choix d'architecture sont introduits progressivement, documentés et conservés dans l'historique Git.

---

## État actuel

La première version publique de Club Manager permet de disposer d'un petit site d'association configurable.

Les pages disponibles sont :

* Accueil ;
* Le club ;
* Où nous trouver ;
* Quand nous trouver ;
* Contact ;
* Règlement intérieur.

Le contenu principal du site est chargé depuis un fichier de configuration JSON.

L'application prend également en charge :

* les templates HTML Go ;
* un layout commun ;
* une navigation responsive avec Bootstrap ;
* les fichiers statiques ;
* les images configurables ;
* les liens de contact par email et téléphone.

---

## Architecture actuelle

Le projet suit une architecture volontairement simple :

```text
config.json
    │
    ▼
 config.Load()
    │
    ▼
   Config
    │
    ▼
   main
    │
    ▼
  router
    │
    ├───────────────┐
    ▼               ▼
handlers          static
    │             files
    ▼
  views
    │
    ▼
templates
    │
    ▼
   HTML
```

Les principales responsabilités sont séparées entre :

* `cmd/server` : point d'entrée de l'application ;
* `internal/config` : chargement et représentation de la configuration ;
* `internal/router` : déclaration des routes HTTP et service des fichiers statiques ;
* `internal/handlers` : préparation des données nécessaires aux pages ;
* `internal/views
  HTML

````

Les principales responsabilités sont séparées entre :

- `cmd/server` : point d'entrée de l'application ;
- `internal/config` : chargement et représentation de la configuration ;
- `internal/router` :` : données de présentation et exécution des templates ;
- `static` : CSS, images et autres ressources statiques.

---

## Configuration

Le contenu du site est défini dans :

```text
config/config.json
````

Exemple :

```json
{
    "site_name": "TCR Club Manager",

    "club": {
        "title": "Le Club",
        "heading": "Team Cat Ride",
        "description": "TCR est là pour t'accompagner et te faire progresser en vélo.",
        "image": "/static/images/club.jpeg",
        "image_alt": "L'ensemble des accompagnateurs"
    }
}
```

Cette approche permet de modifier une partie importante du contenu du site sans modifier le code Go.

---

## Fichiers statiques

Les ressources statiques sont placées dans :

```text
static/
├── css/
└── images/
```

Elles sont accessibles depuis l'URL :

```text
/static/
```

Par exemple :

```text
/static/images/club.jpeg
```

correspond à :

```text
static/images/club.jpeg
```

dans le projet.

---

## Interface

L'interface utilise :

* les templates HTML de Go ;
* Bootstrap pour la mise en page et le responsive ;
* un fichier CSS local pour les personnalisations spécifiques.

Bootstrap est volontairement utilisé de manière simple afin de fournir rapidement une interface propre sans faire du développement frontend le sujet principal du projet.

---

## Lancer le projet

Depuis la racine du dépôt :

```bash
go run ./cmd/server
```

Le serveur est alors disponible à l'adresse :

```text
http://localhost:8080
```

---

## Tests

Pour exécuter les tests :

```bash
go test ./...
```

Le projet peut également être vérifié avec :

```bash
go vet ./...
```

et formaté avec :

```bash
go fmt ./...
```

---

## Technologies actuellement utilisées

* Go ;
* bibliothèque standard `net/http` ;
* templates HTML Go ;
* JSON ;
* Bootstrap ;
* Git.

---

## Évolutions prévues

Club Manager a vocation à évoluer progressivement vers une véritable application de gestion d'association.

Les prochaines étapes pourront notamment introduire :

* PostgreSQL ;
* Goose pour les migrations ;
* sqlc pour l'accès aux données ;
* gestion des adhérents ;
* gestion des rôles et permissions ;
* authentification ;
* cotisations ;
* cours et événements ;
* HTMX pour certaines interactions dynamiques.

Ces fonctionnalités ne font pas encore partie de la version actuelle.

---

## Principe de développement

Le projet suit quelques principes simples :

* privilégier la simplicité avant la complexité ;
* écrire du code lisible et maintenable ;
* séparer clairement les responsabilités ;
* ne créer une abstraction que lorsqu'un besoin réel apparaît ;
* faire évoluer l'application par petites étapes ;
* tester régulièrement ;
* documenter les choix techniques ;
* conserver un historique Git clair.

La démarche générale peut être résumée ainsi :

```text
Écrire simplement
        ↓
Observer les besoins
        ↓
Identifier les responsabilités
        ↓
Faire évoluer l'architecture
```

---

## Objectif à long terme

Club Manager doit devenir une application générique pouvant être adaptée à différentes associations tout en conservant un backend commun.

Le projet constitue également un support pour étudier et mettre en pratique des notions de développement logiciel telles que :

* architecture ;
* HTTP ;
* configuration ;
* templates ;
* bases de données ;
* tests ;
* authentification ;
* autorisation ;
* séparation des responsabilités.





