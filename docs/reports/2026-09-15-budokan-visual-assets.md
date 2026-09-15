# Intégration des assets visuels Budokan — 15 septembre 2026

## Périmètre et état initial

Le chantier intègre l’identité illustrée Budokan dans les pages publiques existantes sans modifier leurs routes, leurs parcours, leurs données ni les domaines métier. PostgreSQL reste la source de vérité pour l’identité du club, les activités, les lieux, les coordonnées, les adhésions et le planning. Aucun modèle média/CMS, formulaire, POST public ou texte métier n’a été ajouté.

Avant modification, les commandes demandées ont été exécutées : `git status --short`, `git log --oneline -10`, `git diff --check` et `go test ./...`. Le diff était valide et la suite Go réussissait. L’état local à préserver contenait déjà la suppression de `static/images/club.jpeg` et `static/images/logo.jpeg`, ainsi que le nouveau dossier non suivi `static/images/budokan/`. Ces fichiers fournis n’ont été ni écrasés ni transformés.

## Fichiers modifiés

- `internal/views/templates/pages/public.html` : composition du hero, illustrations de l’accueil et de la page Essai, attributs d’image.
- `static/css/style.css` : palette Budokan, grilles image/texte et cadrages responsive réutilisables.
- `internal/application/public_frontend_integration_test.go` : présence des chemins d’assets attendus dans les pages concernées.
- `internal/application/rbac_integration_test.go` : service HTTP réel des quatre PNG référencés.
- `docs/reports/2026-09-15-budokan-visual-assets.md` : présent rapport.

Les PNG sous `static/images/budokan/` étaient fournis au démarrage du chantier et restent inchangés.

## Images réellement trouvées

Tous les noms annoncés existent exactement, sans variante de chemin ou de casse.

| Asset réel | Format et dimensions | Utilisation |
|---|---:|---|
| `static/images/budokan/hero/hero-bureau-mascots.png` | PNG RGB, 1774 × 887 | Hero de `/` |
| `static/images/budokan/illustrations/training-jjb.png` | PNG RGB, 1536 × 1024 | Section des activités PostgreSQL sur `/` |
| `static/images/budokan/illustrations/club-spirit.png` | PNG RGB, 1536 × 1024 | Bloc existant de contact/accueil du club sur `/` |
| `static/images/budokan/illustrations/trial-jjb.png` | PNG RGB, 1536 × 1024 | Visuel principal de `/essai` |
| `static/images/budokan/illustrations/kids-jjb.png` | PNG RGB, 1536 × 1024 | Non utilisé |
| `static/images/budokan/illustrations/adults-jjb.png` | PNG RGB, 1536 × 1024 | Non utilisé |
| `static/images/budokan/illustrations/schedule-jjb.png` | PNG RGB, 1536 × 1024 | Non utilisé |
| `static/images/budokan/mascots/tiger.png` | PNG RGBA, 253 × 702 | Non utilisé |
| `static/images/budokan/mascots/panda.png` | PNG RGBA, 253 × 702 | Non utilisé |
| `static/images/budokan/mascots/fennec.png` | PNG RGBA, 230 × 702 | Non utilisé |
| `static/images/budokan/mascots/cat.png` | PNG RGBA, 230 × 702 | Non utilisé |
| `static/images/budokan/mascots/otter.png` | PNG RGBA, 265 × 705 | Non utilisé |

## Placement et hiérarchie

L’accueil utilise une grille texte/image dans le hero. Le nom du club, sa description et les CTA Horaires, Faire un essai et Contact restent du HTML indépendant de l’illustration. Le visuel conserve son ratio panoramique 2:1 et n’est jamais placé derrière le texte.

La section « À pratiquer au club » conserve exclusivement les activités lues dans PostgreSQL. `training-jjb.png` occupe la colonne secondaire sans devenir une activité ou un libellé de pratique. `club-spirit.png` accompagne le panneau existant « Une question avant de venir ? » après les informations pratiques. Ces deux images utilisent `loading="lazy"` et ne sont chargées que sur l’accueil.

Sur `/essai`, `trial-jjb.png` partage le panneau principal avec les deux paragraphes et les CTA existants. Aucun formulaire, quota ou réservation n’est introduit. Le visuel est immédiatement visible sur cette page et n’est donc pas différé.

`/horaires` reste entièrement centré sur le planning PostgreSQL. `schedule-jjb.png` n’a pas été retenu : l’image contient des phrases illustrées qui pourraient être interprétées comme des promesses ou règles, et sa hauteur aurait repoussé le planning. `/contact`, `/tarifs` et `/rules` restent sans illustration afin de garder leur contenu direct.

## Cadrage responsive

À partir de 768 px, les compositions utilisent deux colonnes avec des pistes `minmax(0, …)` pour empêcher les contenus de forcer la largeur. Le hero garde son ratio 2:1. Les illustrations secondaires utilisent un ratio 3:2, `object-fit: cover`, une hauteur maximale et des coins cohérents avec les panneaux existants.

Sous 768 px, toutes les compositions passent sur une colonne. L’image du hero vient après le texte et les CTA. Sur Essai, l’image précède le texte pour conserver une entrée visuelle claire. Les images reprennent une hauteur automatique plafonnée à 20 rem ; leur largeur reste à 100 % de leur conteneur. Aucun style inline ni seconde version mobile des contenus n’a été créé.

## Accessibilité

Le hero porte l’alternative concise « Les mascottes du Budokan en tenue de jiu-jitsu brésilien », qui décrit l’univers graphique sans recopier le titre voisin. Les images d’entraînement, d’esprit du club et d’essai sont complémentaires à des contenus HTML déjà complets ; elles utilisent donc `alt=""` pour éviter une narration redondante.

Toutes les images déclarent leurs dimensions intrinsèques afin de réserver leur espace et de limiter les décalages de mise en page. Le hero utilise `fetchpriority="high"`; les décodages sont asynchrones. Le h1 unique par page, le skip-link, les styles de focus, l’ordre du document et la navigation clavier du layout restent inchangés. Aucune information métier n’est portée uniquement par une image.

## Assets volontairement non utilisés

- `kids-jjb.png` et `adults-jjb.png` : les activités publiques ne définissent pas ces segments comme des activités distinctes. Les placer aurait encouragé une association éditoriale non portée par les données.
- `schedule-jjb.png` : contenu textuel interne à l’image et concurrence visuelle avec le planning réel.
- les cinq mascottes isolées : le hero présente déjà clairement cet univers, avec le tigre au centre. Leur répétition dans les cartes aurait alourdi le site et dilué la hiérarchie.

## Tests et vérifications

- État initial : `git diff --check` réussi ; `go test ./...` réussi.
- Après intégration : `gofmt -w internal/application/public_frontend_integration_test.go internal/application/rbac_integration_test.go` réussi.
- Tests ciblés : `go test ./internal/application -run '^(TestPublicFrontendPostgres|TestPublicRoutesAndLogout)$' -count=1 -v` réussi. Ils confirment les pages, les données PostgreSQL, les redirections et POST existants via le test public complet, puis le service HTTP `200 image/png` des quatre assets utilisés.
- `sqlc generate` n’a pas été exécuté : aucun fichier SQL, schéma ou code généré n’a été modifié.
- Validation finale complète et `git diff --check` : résultats consignés ci-dessous après leur exécution finale.

## Vérification Chromium réelle

Une PostgreSQL 16 Alpine jetable dédiée `club_visual_assets_demo` a été créée sur `127.0.0.1:55438`, migrée jusqu’à 0027 puis alimentée par la commande réelle `seed-budokan --confirm-empty-demo`. Le serveur Go réel a été lancé sur `127.0.0.1:8080` avec `APP_TIMEZONE=Europe/Paris` et le transport mail désactivé.

Chromium headless a rendu `/`, `/horaires`, `/essai` et `/contact` à 390 × 900 et 1365 × 900. Deux captures longues supplémentaires de l’accueil ont permis de contrôler les illustrations différées. Les contrôles visuels constatent : ratio et cadrage conservés, textes et CTA lisibles, navigation intacte, aucune image derrière un texte, planning visible dès le premier écran, empilement mobile cohérent et aucun débordement horizontal visible. Les pages Contact restent légères ; le hero et la page Essai gardent une hiérarchie claire entre contenu et illustration.

Les captures temporaires ont été conservées hors des fichiers versionnés. Le serveur et le conteneur jetable ont été arrêtés après la vérification ; aucune base existante n’a été utilisée.

## Validation finale et état Git

`go test ./...` réussit intégralement après les modifications (`internal/application` : 206,687 s ; autres packages réussis, en cache ou sans tests). `git diff --check` réussit également après la rédaction du rapport. Aucun commit, push, reset ou clean n’a été exécuté.

État final de `git status --short` :

```text
 M internal/application/public_frontend_integration_test.go
 M internal/application/rbac_integration_test.go
 M internal/views/templates/pages/public.html
 M static/css/style.css
 D static/images/club.jpeg
 D static/images/logo.jpeg
?? docs/reports/2026-09-15-budokan-visual-assets.md
?? static/images/budokan/
```

Les deux suppressions et le dossier d’images Budokan appartenaient à l’état initial fourni. Les autres lignes correspondent à cette intégration.

## Limites et améliorations futures

Les PNG fournis pèsent environ 2,4 à 2,6 Mio chacun pour le hero et les illustrations, et 256 à 336 Kio pour les mascottes. Le chargement par page et le lazy-loading limitent l’impact actuel, mais des variantes WebP/AVIF et des tailles responsive réduiraient nettement le transfert sans changer le modèle de données. Ce travail suppose de dériver des formats de présentation statiques à partir des originaux, sans créer de modèle média.

Les illustrations enfants/adultes pourront être introduites si une future donnée publique explicite permet de les associer sans déduire un public à partir d’un `practice_label`. Le visuel de planning restera à réévaluer seulement si son texte illustré est validé comme pure présentation et si sa présence ne retarde pas l’accès aux créneaux.
