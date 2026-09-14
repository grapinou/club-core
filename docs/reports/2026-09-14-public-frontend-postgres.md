# Frontend public alimenté par PostgreSQL — 14 septembre 2026

## État initial et audit

Dépôt propre à la reprise, HEAD `5f7bd8e Refine Budokan demo schedule`. Les commandes imposées ont été exécutées dans l’ordre avant modification : `git status --short`, `git log --oneline -10`, `git diff --check`, `go test ./...`. Toutes réussies ; tests initiaux en cache, journal `/tmp/club-public-initial-tests.log`.

Lecture des rapports Organization/seed, administration et espace personnel/familial. Inspection de l’assemblage Application, du routeur, des handlers/vues/templates, de config/, du modèle 0026, du seed et du service Organization. Aucun travail local antérieur à préserver. Le socle métier et les clarifications Budokan sont committés. Aucun AGENTS.md applicable trouvé.

Avant ce chantier : six pages GET publiques, `/`, `/club`, `/contact`, `/where`, `/when`, `/rules`, rendent Config.SiteName et PageConfig/ContactConfig depuis JSON. Le layout base partagé fournit navigation, titre, lang=fr, Bootstrap, skip-link et liens personnels/admin ; son logo est une image fictive locale. `/join` et `/join/child` sont des demandes d’adhésion, pas des réservations d’essai. Trials n’a aucun parcours public visiteur → créneau → réservation. `/tarifs` et `/essai` n’existent pas.

Organization.Identity et Catalogue sont des lectures opérateur incluant lieux/liens inactifs, IDs et descriptions internes. Schedule sélectionne une saison explicite, sans choix courant ni filtre de validité au jour demandé. Les réutiliser directement dans un template exposerait des détails inutiles. Le groupe technique du lundi est une vraie ligne de groups, sans attribut distinguant son nom interne d’un nom publiable.

## Transition retenue et routes

La transition a été annoncée après l’audit et avant l’implémentation : quatre pages principales, une orientation essai, conservation de l’URL règlement et redirections des pages redondantes. Pas de copie du design Wix.

| Route | État final / source |
|---|---|
| GET `/` | Accueil PostgreSQL : identité, activités, lieux et coordonnées |
| GET `/horaires` | Planning courant PostgreSQL, par jour |
| GET `/tarifs` | Types d’adhésion PostgreSQL et absence de montants explicitement annoncée |
| GET `/contact` | Coordonnées, site principal, liens actifs et lieux PostgreSQL |
| GET `/essai` | Orientation vers horaires et prise de contact ; identité PostgreSQL |
| GET `/rules` | Identité PostgreSQL ; courte prose éditoriale temporaire JSON |
| GET `/club` | Redirection permanente 308 vers `/` |
| GET `/where` | Redirection permanente 308 vers `/contact#lieux` |
| GET `/when` | Redirection permanente 308 vers `/horaires` |

HEAD est pris en charge par net/http. Aucun nouveau POST public. `/join`, `/join/child`, login/activation/vérifications, espaces personnel/familial et routes administratives restent fonctionnellement inchangés.

Application utilise explicitement `router.NewWithPublic`. Le constructeur historique `router.New`, ses handlers JSON et leurs tests unitaires sont conservés pour compatibilité des appelants historiques ; ils ne sont plus le chemin d’assemblage du serveur. Il n’existe aucun repli automatique DB → JSON sur une page migrée : une base vide affiche un état vide, une panne affiche une erreur générique.

## Inventaire exhaustif du JSON historique

| Champ / famille | Classement | Traitement |
|---|---|---|
| `site_name` | Ancien nom de club ; désormais libellé du produit pour les écrans historiques | Valeur neutre Club Core ; aucune identité de club pour la vitrine issue de ce champ |
| `home.title`, `home.heading` | Libellés d’interface obsolètes | Titres génériques de page, nom réel depuis Organization |
| `home.description` | Prose fictive TCR obsolète | Retirée du fichier ; Organization.description si présente, sinon phrase neutre d’accueil |
| `home.image`, `home.image_alt` | Champs possibles de présentation, pas de données métier nécessaires | Aucun asset chargé par les nouvelles pages |
| `club.title`, `club.heading` | Titre / identité obsolètes | Route redirigée vers l’accueil DB |
| `club.description`, `club.image`, `club.image_alt` | Contenu fictif / assets historiques obsolètes | Retirés du fichier, pas repris dans la vitrine |
| `contact.title`, `contact.heading`, `contact.description` | Libellés/prose UI obsolètes | Remplacés par l’interface générique |
| `contact.email_address`, `contact.phone_number` | Métier désormais DB | Organization.public_email/public_phone uniquement |
| `where.title`, `where.heading` | Libellés obsolètes | Redirection contact/lieux |
| `where.description` | Adresse métier fictive en prose | Retirée ; Locations comme source |
| `where.image`, `where.image_alt` | Champs possibles inutilisés | Pas repris |
| `when.title`, `when.heading` | Libellés obsolètes | Redirection horaires |
| `when.description` | Horaires métier fictifs en prose | Retirés ; GroupSlots uniquement |
| `when.image`, `when.image_alt` | Champs possibles inutilisés | Pas repris |
| `rules.title`, `rules.heading` | Libellés UI | Titre générique de la route conservée |
| `rules.description` | Éditorial temporaire | Seul contenu public encore en JSON : invitation neutre à demander le règlement au club |
| `rules.image`, `rules.image_alt` | Champs possibles inutilisés | Pas repris |

Le fichier final conserve uniquement `site_name: Club Core` et `rules.description`. La prose fictive « cat rider » n’est pas présentée comme le règlement Budokan. Les structures Config historiques restent compatibles ; pas de suppression générale du chargeur. Les écrans d’authentification/adhésion et privés conservent leur libellé de produit historique, désormais neutre dans la configuration livrée ; leur identité/session métier n’est pas modifiée. Le serveur charge encore ce petit fichier pour cette compatibilité et le règlement éditorial.

DATABASE_URL, APP_BASE_URL, APP_TIMEZONE, SMTP, sessions/cookies, timeouts, transport mail et environnement restent entièrement hors DB. Aucun tarif, quota, horaire, adresse ou liste d’activités n’est ajouté à une constante Go ou un fichier statique de présentation.

## Architecture services, handlers et vues

Extension du service `internal/organization` par `PublicIdentity`, `PublicActivities`, `PublicMembershipTypes`, `PublicSchedule`. PublicIdentity réutilise Identity, puis filtre les objets actifs et réduit les données. Les listes de noms réutilisent les requêtes existantes d’activités/types. Deux queries sqlc spécifiques sélectionnent les saisons courantes et le planning public.

DTO publics dédiés : PublicClub, PublicLocation, PublicLink, PublicSlot, PublicTimetable. Aucun modèle administratif transmis au frontend. PublicHandler appelle les services, choisit le titre et transforme ces DTO en view models `views.PublicClubView`, `PublicLocationView`, `PublicLinkView`, `PublicSlotView`, `PublicDayView` et PublicPage. Le handler n’exécute pas de SQL. PublicPage ne porte que les petites collections utiles à sa page ; pas de données Persons, adhésions individuelles, consentements juridiques, notes ou comptes.

Le rendu HTML utilise html/template et un buffer avant écriture. Les strings sont échappées, les URLs bénéficient aussi du filtrage html/template. Les erreurs de lecture produisent un 503 générique, sans diagnostic SQL. Aucun cache de données ni du HTML (`Cache-Control: no-store`) : renommage/désactivation visibles à la requête suivante. Comme Identity existant, les petites lectures sont successives, sans transaction de snapshot globale ; une modification concurrente peut être observée entre deux lectures, sans prétention de cohérence transactionnelle d’une page entière.

Les lieux/liens affichés appartiennent à l’organisation active. Le planning exclut les lieux référencés désactivés et les lieux d’une autre organisation. Les activités/groupes/types historiques restent des référentiels d’instance : ce chantier ne crée pas d’isolation multi-tenant.

## Publication du nom de groupe et lundi provisoire

La migration **0027_public_group_name.sql** ajoute un unique booléen `groups.show_name_publicly`, vrai par défaut. Cet ajout explicite est nécessaire pour ne pas déduire la publication d’un nom à partir de sa graphie, de sa description libre ou d’un ID Budokan. Aucun deuxième nom de groupe n’est stocké. Le seed met ce booléen à faux uniquement sur le rattachement technique du lundi.

La projection SQL remplace le nom non publiable par une chaîne vide, avant même de construire les DTO publics. Le créneau conserve horaire, activité, practice_label et lieu. Les descriptions de groupes ne sont jamais sélectionnées par la projection publique, notamment celles expliquant les interprétations techniques. Les lectures opérateur restent complètes. Un nom de groupe renommé arbitrairement reste masqué si son indicateur vaut faux.

Le lundi 20:15–22:00 affiche ainsi Préparation physique / Jiu-Jitsu Brésilien et le lieu, sans inventer son public. Le nom JJB pratiques spécifiques n’apparaît pas en HTML, y compris dans un attribut. Les créneaux No-Gi et Libre affichent leur practice_label et leur groupe JJB Adolescents et Adultes. Ils ne deviennent pas des activités autonomes.

L’indicateur ne désactive pas le groupe et ne retire pas ses créneaux. Il est configuré via SQL dans cette V1, sans nouveau CRUD. Pour une base de démonstration déjà peuplée avant 0027, l’opérateur doit identifier les noms internes à masquer et régler cet indicateur ; la migration ne reconnaît jamais Budokan et le seed conserve son refus d’une base non vide. Le contrôle de ce chantier utilise une base neuve seedée avec la version finale.

## Saison courante et validité des horaires

Date civile calculée dans APP_TIMEZONE, puis passée explicitement au service. Une saison est courante si elle est active et si starts_at ≤ date ≤ ends_at. Aucune année codée en dur dans le frontend. Les saisons historiques/futures ne sont pas sélectionnées en repli.

Zéro saison courante : message « Aucun planning de saison courante… ». Une saison : planning de celle-ci. Plusieurs saisons courantes : erreur contrôlée 503, aucune sélection arbitraire de la première ligne. Les bornes sont inclusives ; les tests couvrent début, fin, historique, absence, désactivation et chevauchement.

Les créneaux actifs doivent également être valides au jour courant (valid_from/valid_until inclusifs). La page montre un planning hebdomadaire actuellement en vigueur, pas un agenda daté ni une promesse de cours pendant les vacances. Saison, activité, groupe, créneau et lieu doivent être actifs. Un nom de groupe masqué reste distinct d’un groupe inactif.

## Accueil, activités et lieu

Accueil sans photo imposée : identité/nom court DB, description si renseignée, sinon phrase légère. CTA Horaires, Faire un essai, Contact. Cartes d’activités actives venant exclusivement de la table activities. Pas d’inférence de discipline à partir des practice_label.

Lieux et coordonnées sont présents dès l’accueil, avec liens vers les détails. Faute de notion de « lieu principal » dans le modèle, tous les lieux actifs sont présentés dans l’ordre de la lecture existante, sans attribuer arbitrairement une priorité. Budokan possède un seul lieu. Aucune duplication d’adresse. Les panneaux et l’introduction pourront accueillir des assets ultérieurs sans que ces assets soient requis par la mise en page.

## Horaires, publics et modalités

Présentation principale par jour, avec ancres accessibles lundi → dimanche. À l’intérieur : heure, libellé de pratique ou nom du groupe, groupe lorsque pertinent, activité, lieu/adresse. Les sept jours restent visibles, avec message explicite pour un jour sans créneau. Pas de tableau large, filtre JS, dépendance au script ou seconde version du planning.

Le jeu Budokan produit seize lignes, répartition 3/3/3/3/1/2/1. No-Gi du samedi et Libre du dimanche désignent bien des modalités du groupe adolescents/adultes. Une courte indication globale explique les âges pédagogiques souples ; aucun seuil d’âge ni interdiction n’est calculé. Le lundi conserve son seul intitulé de séance, sans le groupe technique.

Les détails « environ 45 minutes de renforcement/cardio puis JJB » et « libre surveillé sans cours structuré » restent documentés dans le seed/rapport métier. Le rendu conserve leurs practice_label et ne parse pas leurs mots pour fabriquer un comportement ou une explication propre à Budokan. Aucun domaine de phases/supervision n’est ajouté. Une future rédaction publique propre aux séances nécessitera une source éditoriale explicitement publiable si le club souhaite ces explications supplémentaires.

## Contact, liens, tarifs et essai

Contact : email et téléphone cliquables, site principal depuis Organization, collection de liens étiquetés depuis organization_links, lieux actifs. Aucun nom de réseau dans le template. Zéro lien ne laisse pas de titre orphelin ; plusieurs liens sont une liste simple. Adresses et noms longs reviennent à la ligne.

Tarifs : types d’adhésion DB, puis information honnête « Les montants ne sont pas encore affichés sur ce site », CTA demander les tarifs et lien vers la demande d’adhésion existante. Aucun montant public connu n’est copié dans le frontend. Un prochain modèle d’offres/tarifs saisonniers est nécessaire avant leur affichage calculé ; ni price_text, ni paiement, ni intégration HelloAsso.

Essai : route de conseil uniquement, avec horaires et prise de contact. Aucun formulaire de réservation ou création de Trial. Aucun quota de deux séances codé dans l’UI. Le texte précise qu’une prise de contact ne réserve pas automatiquement une place. Le vrai parcours public Trial reste un jalon distinct, avec ses règles de public, disponibilité et inscription.

## Navigation, responsive et accessibilité

Navigation commune : Accueil, Horaires, Adhésions et tarifs, Essayer, Adhérer, Contact ; Connexion ou Mon espace/Mon compte selon la session. Les entrées Administration restent soumises aux permissions existantes. Les assertions historiques de navigation ont seulement été adaptées aux nouvelles destinations ; les tests de révocation et de confidentialité sont conservés. Les anciennes adresses pratiques ont des redirections testées.

Suppression de l’image fictive du logo dans le layout ; les fichiers d’images historiques ne sont pas supprimés. Réemploi des panels, cards, actions, couleurs, focus et largeurs existants. CSS ajouté uniquement pour les grilles de cartes, les jours/horaires et les retours à la ligne. À 390 px, les lignes de créneau empilent heure et contenu ; les jours suivent l’ordre du document. À 1365 px, les jours occupent deux colonnes. Toutes les commandes de navigation restent visibles sans menu JavaScript.

Un h1 par page, sections/h2 par sujet ou jour, sous-titres de lieux/activités, listes sémantiques, éléments time/address, liens explicites, navigation des jours nommée, skip-link et focus conservés. Aucun formulaire nouveau ne demande de label. Lang=fr conservé ; titres construits depuis la page et le nom DB, meta description ajoutée via un champ optionnel de SecurityData partagé. Pas de moteur SEO ou de répétition de l’identité en configuration.

## Sécurité et états vides

Routes en lecture seule ; POST non autorisé. Middleware CSRF/security headers existant réutilisé sur les GET publics (no-store, nosniff, no-referrer, CSP), sans changement des protections des formulaires existants. Les indications de navigation ne donnent aucun droit ; RBAC, GuardianAccess et sessions ne sont pas refactorés. Aucune donnée personnelle chargée par le service public.

État vide par absence d’organisation, activités, lieux, coordonnées, types, saison ou créneaux. Une organisation inactive n’affiche ni ses coordonnées ni ses référentiels. Les liens inactifs sont absents. Description facultative, site/contacts facultatifs, pas d’image vide ou de réseau social avec URL absente.

## Tests HTTP et source de vérité

`public_frontend_integration_test.go` utilise PostgreSQL 16 réelle et Application complète. Le démarrage PostgreSQL/migrations a été extrait du helper de fixture existant pour accepter une base `_demo` vide, sans recréer d’infrastructure ni changer les fixtures métier historiques. Le test seede Budokan, puis rend les réponses via le Handler réel. Les dates de la fixture HTTP sont ajustées relativement au jour de test pour rester reproductibles après 2027 ; le seed versionné ne change pas ses dates et les tests de saison utilisent des dates explicites.

Vérifications : identité, trois activités, lieu, contact, saison et seize créneaux, sept jours, heures, pratique No-Gi/Libre/préparation physique, absence du nom technique, HTML échappé, pas de prix codé en dur, aucun champ JSON sentinelle sur les pages migrées, liens 0/plusieurs, inactifs de chaque type, état sans organisation, types vides, absence de saison courante en HTTP, redirections et refus POST. Un renommage du groupe masqué vérifie que la règle n’est pas liée à son nom initial.

Critère de source de vérité : le même objet Application, le même JSON sentinelle et des requêtes successives ; UPDATE Organization.name/email puis UPDATE Location.name/address → nouveau rendu immédiatement. Aucun redémarrage ni modification du fichier JSON. Les tests de saison du service couvrent l’historique et les bornes, ainsi que les chevauchements refusés. Les scénarios historiques de seed/rollback/concurrence, d’affectation pédagogique, de RBAC et des espaces privés restent dans la suite complète.

## Contrôle réel Chromium et comparaison fonctionnelle

Application Go réellement compilée/lancée sur localhost:8080, PostgreSQL 16 Alpine Docker dédiée `club_public_demo`, port lié exclusivement à 127.0.0.1, migrations Goose 0001–0027 puis commande réelle `go run ./cmd/clubctl seed-budokan --confirm-empty-demo`. Aucun accès à une base métier existante, aucune Person/User créée, transport mail désactivé. Les paramètres temporaires restent sous `/tmp`, pas dans le dépôt.

Harnais Playwright temporaire `/tmp/club-public-visual.py`, captures et mesures sous `/tmp/club-public-visual/`. Chromium à 390×900 et 1365×900, JavaScript désactivé. 42 rendus contrôlés : six pages seedées, textes très longs, zéro lien, plusieurs liens, référentiels vides et aucune organisation active. Statuts 200, h1 unique, aucun débordement horizontal aux deux largeurs, chargement Bootstrap, absence du nom technique et accès clavier au skip-link vérifiés. Inspection visuelle des captures principales et des états représentatifs mobile/desktop : cartes, navigation, CTA, contacts et sept jours lisibles. Un échec ponctuel de chargement du CDN Bootstrap a interrompu le premier passage ; le second passage complet, avec contrôle explicite du CSS, a réussi. Les 42 captures et audit.json sont conservés sous /tmp/club-public-visual/ (artefacts locaux temporaires, non versionnés).

Le site [Budokan](https://www.budokansudoise.com/) et sa [page Horaires](https://www.budokansudoise.com/horaires) ont été consultés comme benchmark fonctionnel. Améliorations concrètes proposées : lieu/contact accessibles dès l’accueil, action essai reliée aux horaires/contact, saison issue des dates en DB, lecture du planning par jour avec ancres, publics associés aux modalités No-Gi/Libre, redirections des pages Où/Quand dispersées. Ce n’est pas un test utilisateur comparatif et aucune affirmation de conversion améliorée n’est mesurée. Aucun téléchargement d’image Wix ni reproduction pixel à pixel.

## Limites et prochaines données nécessaires

Restent : public du lundi à confirmer, texte définitif du règlement, assets originaux, modèle de tarification saisonnière et conception du vrai parcours d’essai. La version image_rights de démonstration reste au domaine Consent, sans texte juridique inventé ou rendu comme nouveau règlement. Les enseignants restent hors périmètre ; aucun rôle coach transformé en relation publique d’enseignement.

Dépendance Bootstrap CDN historique conservée ; le contrôle navigateur doit constater son chargement. Aucun audit WCAG exhaustif ni matrice multi-navigateurs. Pas de CMS, média, upload, paiement, document, multi-tenant ou administration générale. La description Organization reste le seul texte de présentation métier déjà disponible. L’information de 45 minutes/surveillance ne devient pas une règle frontend.

## Résultats finaux et fichiers

Toutes les validations ont réussi :

- `sqlc generate`, puis `gofmt` sur tous les fichiers Go concernés.
- `go test ./...` : application 222,289 s, database 34,455 s ; autres packages également réussis ou sans tests.
- `go test -race ./internal/application ./internal/organization ./internal/handlers ./internal/views ./internal/router ./internal/database ./internal/demodata` : application 386,372 s, database 47,184 s ; aucune race détectée. Les tests des services Organization et du seed résident dans les packages d’intégration.
- Migrations et seed réellement exécutés sur la PostgreSQL isolée, puis serveur HTTP et 42 contrôles Chromium sans JavaScript aux deux résolutions. Les changements en base sont visibles sans recharger la configuration.
- `git diff --check` sans erreur. Aucun commit, push, reset ou clean.

Journaux de tests locaux : `/tmp/club-public-final-tests.log` et `/tmp/club-public-race.log`. Le serveur et le conteneur PostgreSQL créés pour cette vérification sont arrêtés à la fin ; aucune base existante n’est modifiée.

Fichiers ajoutés : migration 0027, requêtes publiques et génération sqlc correspondante, projections du service Organization, handler public, view models/template public, tests d’intégration publics et ce rapport. Fichiers ajustés : README, JSON historique, assemblage Application et routeur, helpers/assertions de tests existants, modèles/queries sqlc générés, indicateur de publication du seed, métadonnées/layout partagés et CSS. Aucun changement des domaines personnels, paiements, enseignants ou essais.

Copie de travail prête pour revue manuelle. État Git final (changements de ce jalon uniquement) :

```text
 M README.md
 M config/config.json
 M internal/application/application.go
 M internal/application/integration_test.go
 M internal/application/rbac_integration_test.go
 M internal/application/ux_integration_test.go
 M internal/database/dbsqlc/groups.sql.go
 M internal/database/dbsqlc/models.go
 M internal/demodata/budokan.sql
 M internal/router/001_router.go
 M internal/views/security.go
 M internal/views/templates/layouts/base.html
 M static/css/style.css
?? docs/reports/2026-09-14-public-frontend-postgres.md
?? internal/application/public_frontend_integration_test.go
?? internal/database/dbsqlc/public_organization.sql.go
?? internal/database/queries/public_organization.sql
?? internal/handlers/public.go
?? internal/organization/public.go
?? internal/views/public.go
?? internal/views/templates/pages/public.html
?? migrations/0027_public_group_name.sql
```
