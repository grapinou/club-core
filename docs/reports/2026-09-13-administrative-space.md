# Espace administratif V1 — 13 septembre 2026

## État initial et reprise

Début du chantier sur HEAD `be99ae5 Add self-service account management`, dépôt propre. Les commandes demandées status, log -8, diff --check et go test ./... ont été exécutées avant modification ; suite verte. Les espaces personnel/familial et self-service étaient déjà committés. Rapports personnel/familial, self-service, RBAC et administration des adhésions consultés.

Une nouvelle instruction de reprise est arrivée pendant les validations. Nouvel audit effectué dans l’ordre : git status --short, git diff --stat, git diff --check, git diff intégral, git log --oneline -8, go test ./.... HEAD inchangé. État trouvé : 15 fichiers suivis modifiés (167 insertions, 14 suppressions) et 10 entrées non suivies, dont administration et un harnais visuel temporaire. Aucun fichier indexé. Les fichiers nouveaux ont également été examinés, car git diff ne les affiche pas. Aucun TODO bloquant identifié. Aucun rapport administratif du 13 septembre n’existait encore.

Déjà réalisés et conservés à la reprise : migration 0025, projections sqlc, service administration, handlers/pages, navigation, personnes/recherche/notes/relations, essais, création d’adhésion, affectations aux groupes, audit transactionnel, quatre tests PostgreSQL/HTTP. Les tests ciblés passaient (8,315 s). Le premier passage global a réussi (application 199,270 s, database 27,118 s) ; le passage demandé à la reprise a réussi avec cache, journal /tmp/club-admin-resume-tests.log.

Erreurs réellement rencontrées pendant le développement : nouvelles fixtures sans valid_from de créneau ou statut explicite obligatoire, helper CSRF visant une page sans formulaire, sélection de revision absente de l’ancienne projection GetTrial. Corrections limitées aux fixtures/helper de test. Le rendu réel a révélé un email long débordant sur mobile dans la liste Persons : retour à la ligne Bootstrap ajouté. Navigation redondante supprimée et confirmation de création Membership raccordée au mécanisme notice existant. Aucun défaut préexistant attribué sans preuve à ce chantier.

À la reprise restaient : confirmation visuelle sur l’interface corrigée, couverture additionnelle de concurrence/rollback de création Membership, contrôle race et rapport complet. La reprise a conservé les fonctionnalités, ajouté la projection minimale du compte lié à Person et les tests de concurrence/rollback de création Membership, couvert explicitement les rôles treasurer/coach/president sur les nouvelles routes et les notes invalides conservées, aligné l’ordre RBAC → CSRF sur les routes existantes. Le dernier contrôle de notes longues a corrigé le retour à la ligne du paragraphe admin_note dans la vue Membership existante (classe reading-text). Les validations sont détaillées en fin de document.

## Architecture et permissions

Aucun second RBAC. Tables roles/user_roles, service authorization et middleware Access.RequirePermission conservés. Les rôles stables sont president, secretary, treasurer, coach ; les noms inconnus n’accordent rien. Les droits sont relus depuis PostgreSQL à chaque requête/opération ; une révocation n’interrompt pas rétroactivement une opération déjà autorisée.

| Capacité utilisée | Usage V1 | Président | Secrétaire |
|---|---|---|---|
| persons.read | Accueil, recherche, fiches, essais | oui | oui |
| persons.write | Notes Person, programmation/statut/notes d’essai | oui | oui |
| memberships.read | Dossiers/historique/groupes | oui | oui |
| memberships.approve | Demande d’adhésion, notes, affectations, approbation existante | oui | oui |
| activation.resend | Renvoi existant depuis dossier | oui | oui |
| registrations.review | Vérifications existantes, lien et compteur conservés | oui | oui |
| roles.read / roles.manage | Capacités existantes, aucune gestion Web ajoutée | oui | non |

Treasurer et coach restent sans droits dans cette V1, conformément au mapping existant. Aucun droit financier ou sportif inventé. Les capacités Persons regroupent actuellement le suivi des contacts/essais ; memberships.approve couvre le traitement administratif des adhésions. Pas de changement du mapping. Une séparation future plus fine demandera une décision produit explicite.

Le package administration compose trials et memberships, vérifie un acteur actif/activé issu exclusivement du contexte et les capacités, gère transactions et audit. Les domaines gardent leurs validations ; aucun SQL de mutation dans les nouveaux handlers. Les projections dédiées ne sélectionnent aucun secret User. Les structures administratives ne sont jamais réutilisées dans personalspace. Le branchement optionnel de la liste Persons dans router.New conserve les routes et tests historiques du routeur.

## Routes

Les racines établies /persons, /memberships et /registration-reviews sont préservées. /admin est l’accueil, /trials le nouveau suivi des essais.

| Méthode et route | Fonction |
|---|---|
| GET /admin | Travail du jour et dossiers en attente |
| GET /persons | Liste bornée, pagination par 50 |
| POST /persons/search | Recherche sans coordonnées en query string |
| GET /persons/{id} | Fiche administrative agrégée |
| POST /persons/{id}/notes | Notes internes Person |
| GET /trials | Tous les essais, date ou à venir |
| GET /trials/{id} | Détail et actions |
| GET, POST /persons/{id}/trials/new | Programmation |
| POST /trials/{id}/reschedule | Reprogrammation |
| POST /trials/{id}/status | Résultat |
| POST /trials/{id}/notes | Notes |
| GET, POST /persons/{id}/memberships/new | Demande avec identité existante et décisions recueillies |
| GET /memberships/{id}/groups | Affectations/historique |
| POST /memberships/{id}/groups | Nouvelle période d’affectation |
| POST /memberships/{id}/groups/{assignment}/close | Date de sortie, sans supprimer la ligne |
| GET, POST /memberships/{id}/notes | Notes administratives Membership |

Routes existantes réutilisées : création/édition/archivage/restauration Persons, liste/détail/approbation Membership, renvoi d’activation, vérifications d’inscriptions. Aucun alias administratif de /me, aucune impersonation.

## Dashboard et recherche

Accueil : essais registered aujourd’hui, registered futurs, Memberships pending, huit Persons récentes ; liens vers listes/date/dossier. Date civile selon APP_TIMEZONE. Compteur de vérifications existant conservé dans la navigation. Deux lectures métier fixes pour l’accueil ; aucun appel par ligne.

Recherche par sous-chaîne littérale insensible à la casse sur identité/email/téléphone, normalisation téléphone identityresolution réutilisée pour une recherche française formatée dans les valeurs normalisées. Les caractères % et _ ne deviennent pas des jokers SQL. Liste active limitée à 50 plus une sentinelle, pagination pour la liste complète ; recherche limitée à 50 avec invitation à affiner. Les termes restent dans le corps POST et le HTML privé, jamais dans l’URL. La liste globale d’essais est limitée à 100 plus sentinelle et filtrable par date ; la fiche Person charge au plus 101 essais par sa projection bornée. Les adhésions de la Person couvrent toutes les saisons. Les projections et jointures sont groupées ; pas de boucle SQL par essai/personne/membership affiché.

## Persons, notes et relations

La fiche rassemble identité, coordonnées, naissance, notes, compte éventuel (username et état actif/désactivé/en attente d’activation), relations familiales, essais et adhésions de toutes les saisons avec groupes actuels. La projection du compte ne sélectionne ni hash ni code ni session. Les rôles d’autrui ne sont pas ajoutés à cette fiche : roles.read est distinct et réservé au président, tandis que cette fiche est accessible au secrétaire ; le CLI existant reste disponible pour l’administration locale explicite des rôles. Une Person peut exister avant une Membership. La création/modification des données de contact/identité réutilise les handlers historiques protégés ; aucune nouvelle duplication de Person dans le parcours essai → adhésion.

Le schéma réel nomme les notes persons.notes et memberships.admin_note. Mutation explicite de chaque type, trim, UTF-8 valide, absence de NUL, plafond de 10 000 caractères ; vide devient NULL. updated_at renseigné. Valeurs conservées en erreur, html/template assure l’échappement. Aucun contenu de note journalisé ou copié dans l’audit. Les notes restent absentes de /me.

Relations affichées dans les deux sens : nom/lien de la Person, relationship_type traduit, contact principal et archivage. Une mention distingue clairement relation et autorisation numérique. Aucun grant créé ni révoqué, aucune nouvelle gestion de guardians. L’état effectif de GuardianAccess reste calculé par son domaine à chaque accès personnel ; aucune approximation administrative ne prétend accorder cet accès.

## Essais

Service trials réutilisé avec variantes Tx pour composer validation et audit. Person cible rechargée, non archivée à la programmation ; activité active, groupe actif lié à l’activité, créneau actif lié au groupe, date finite, weekday, validité et saison vérifiés par le domaine existant sous ses verrous. Pas de duplication dans les handlers.

Statuts existants uniquement : registered, attended, cancelled, no_show. Pas de machine de transitions inventée : le bureau peut corriger un résultat parmi ces valeurs. Reprogrammation conserve Person/statut/notes ; notes et statut possèdent des actions séparées. Si aucun groupe/créneau n’est choisi, aucune saison d’essai n’est inventée.

La migration ajoute revision, augmentée par chaque mutation d’essai. Le formulaire transporte cette valeur, sans autorité supplémentaire. Le service verrouille l’essai puis compare la révision ; un formulaire obsolète reçoit 409 et doit être rechargé. Deux soumissions simultanées de la même révision ne peuvent pas s’écraser silencieusement.

## Adhésions et essai → adhésion

Les lectures et l’évaluateur de complétude existants sont conservés : saisons, statut, activités, historique, consentements/version présentée, notes, approbation et compte associé. Un consentement facultatif refusé reste une décision normale. Les nouvelles définitions actives ne remplacent pas le snapshot historique.

Depuis un essai, le formulaire propose une demande pour sa Person existante et présélectionne son activité. Le service transactionnel verrouille Person puis essai, vérifie leur relation et la présence de l’activité de l’essai dans la demande. CreateRequestTx réutilise toutes les validations Membership, la contrainte d’unicité Person/saison, les exigences/snapshots et le contrôle du donneur des consentements. Le bureau doit sélectionner explicitement les décisions réellement recueillies et leur auteur ; aucun accord implicite.

Création pending, puis approbation séparée via accounts existant. Aucun changement automatique du statut d’essai, aucune affectation automatique au groupe, aucun nouveau User enfant. Le modèle ne contient pas de lien durable de provenance Trial → Membership : le contrôle de source sert à l’opération et l’historique reste consultable par Person. La politique sur quel résultat d’essai autoriserait une conversion automatique n’est pas définie ; aucune règle restrictive ou automatique inventée.

## Groupes et historique

groups restent durables, group_slots saisonniers. Les services ajoutés à memberships vérifient Membership pending/active, groupe actif, activité de l’adhésion et date d’entrée dans la saison. Verrou Membership commun aux ajouts/sorties : pas de double période ouverte ou de chevauchement pour le même groupe via ce service.

Une clôture revalide assignmentID ET membershipID ; left_at >= joined_at et absence de clôture existante. La date de sortie représente le premier jour hors du groupe. Les lignes passées ne sont pas supprimées, une réaffectation ultérieure ajoute une ligne. Les groupes personnels actuels continuent à appliquer joined_at <= today et left_at absent ou strictement futur, avec groupe actif ; les créneaux viennent des données existantes.

Aucun nouvel éditeur des groupes, saisons ou créneaux. Aucun paiement. Le statut ended/cancelled ne reçoit pas un formulaire d’ajout ; les anciens dossiers/historiques restent consultables.

## Audit, transactions et confidentialité

Migration 0025_administrative_space.sql : revision d’essai et administrative_events. Audit minimal persistant : acteur User, action autorisée par CHECK, type/ID ressource, date PostgreSQL. Actions notes Person, programmation/reprogrammation/statut/notes Trial, demande/notes Membership, affectation/clôture Group. Ni formulaire, coordonnées, notes, hashes, codes ni jetons.

L’audit et la mutation partagent une transaction ; échec de l’insertion d’audit ou du commit = rollback métier. Le Down refuse d’effacer un audit non vide. Aucune table de permission ajoutée. L’audit existant d’approbation/activation est conservé ; les anciens handlers de création/édition/archivage Persons ne sont pas refactorés pour étendre cet audit générique.

Sessions et permissions personnelles restent indépendantes. A/B/C testent même utilisateur avec ou sans rôle, grant et révocation : être membre/guardian ne donne aucune permission administrative ; administrer une Person ne donne pas son /me. À 18 ans les droits guardians cessent selon la politique civile existante, y compris l’historique.

## Sécurité HTTP et UX

Authentification et RBAC serveur obligatoires, puis CSRF commun, CrossOriginProtection, cookies et session existants. Aucune exception /admin ou /me. Corps POST limité à 32 Kio. No-store, CSP, no-referrer et nosniff. Erreurs absentes/ID invalides 404, permission 403, formulaire incompatible 422, conflit 409, indisponibilité technique générique 503. Aucun diagnostic SQL brut dans HTML/logs.

Succès POST → 303 → GET ; marqueurs non sensibles saved=1 ou notice=requested selon convention existante. Recherche POST est une lecture sans mutation et sans redirection de PII. Formulaires sans JavaScript, libellés français, composants Bootstrap/section-panel/page-header/actions/empty-state/focus partagés, largeur de lecture existante. Navigation Mon espace et Administration distincte, même session et identité.

## Tests automatisés et multi-session

Tests PostgreSQL 16/Testcontainers via application réelle, comptes fictifs : administrative_space_integration_test.go. A membre normal ; B secrétaire/président ; C guardian, puis doté temporairement d’un rôle et privé de ce rôle à la requête suivante. Accès personnels et administratifs indépendants, refus via URL/IDs modifiés, absence de secrets, service direct protégé.

Person : liste bornée/pagination/recherche/normalisation, notes/XSS/audit, relations, absence et identifiants invalides. Trial : programmation et reprogrammation, dates/weekday/saison, activité/groupe/créneau inactifs ou incompatibles, statut/notes, conflits de révision et audit rollback. Membership : source Trial étrangère, décisions/auteur invalides, absence de duplication Person, historique/snapshot/refus, groupes et sortie étrangère, chevauchements/dates, notes absentes du personnel. HTTP : CSRF sur chaque nouvelle mutation, origine étrangère, body trop grand, no-store/headers et PRG. Concurrence : deux sessions pour la même révision Trial, deux affectations au même groupe, deux demandes d’adhésion depuis le même essai (une seule création). Rollback de la création Membership et de ses activités si l’audit échoue. Ordre d’authentification vérifié sur POST anonyme ; notes dépassant 10 000 caractères refusées et conservées en formulaire sans nouvel audit. Les cinq tests administratifs ciblés ont réussi en 9,736 s avant la dernière assertion complémentaire de notes.

Les suites existantes restent la couverture de non-régression /join, /join/child, GuardianAccess/minorsafety, compte/email/password/sessions, approval, activation, vérifications, RBAC et UX. Les packages sans fichiers de tests propres sont exercés par application.

## Contrôle visuel

Application réelle sur localhost, PostgreSQL jetable newFixture, mailer factice sans SMTP, mot de passe visuel aléatoire conservé uniquement dans /tmp. Aucun secret local de développement modifié. Chromium/Playwright à 390×900 et 1365×900 : accueil, personnes, fiche et état vide, essais et état vide, essai/actions, création d’essai, création d’adhésion, adhésion, groupes/historique, notes, création Person, erreur de planning et confirmation de notes.

Premier passage : 30 captures, un débordement d’email long sur la liste mobile constaté et corrigé avec text-break. Captures réellement examinées pour hiérarchie, labels, formulaires, retours à la ligne, groupes historiques et décisions. Passe finale sur l’état corrigé : 30 captures plus 8 captures avec noms/notes longs, JavaScript désactivé. audit.json et audit-long.json indiquent scrollWidth égal au viewport sur les 38 rendus, réponses 200 hors erreur de formulaire volontaire 422. Les captures clés ont été examinées : boutons accessibles, navigation distincte, hiérarchie, états vides, valeurs conservées en erreur, confirmation, historique et textes longs lisibles. Un second débordement dans le paragraphe de note Membership a été découvert par le cas sans espaces, puis corrigé avec reading-text et revérifié sur les deux largeurs. Preuves temporaires dans /tmp/club-admin-visual/ (audit.json, audit-long.json, captures), sans données réelles.

Le serveur visuel et sa base jetable ont été arrêtés. Le harnais temporaire a été conservé hors dépôt dans /tmp/club-admin-visual-fixture.go et /tmp/club-admin-visual-fixture-final.go ; aucun fichier métier abandonné. Le script Playwright et le binaire visuel restent uniquement dans /tmp, aucune fixture avec mot de passe de démonstration fixe ajoutée au dépôt.

## Limites et préparation des comptes réels

Pas de politique financière, trésorerie, transitions Membership ended/cancelled nouvelles, gestion Web des rôles, gestion de grants, comptes mineurs, impersonation, CMS/Budokan, vitrine, configuration en tables, documents, paiement, messagerie, agenda complexe, import ni notifications générales.

Les droits futurs de treasurer/coach et l’éventuelle conversion automatique d’un essai nécessitent une décision métier. Pour les affectations nouvellement exposées, la V1 retient explicitement une règle conservatrice : entrée dans la saison et adhésion pending/active. L’éventuel besoin d’affectations hors saison devra être confirmé avant d’étendre cette règle. Les notes libres Person/Membership utilisent une écriture atomique avec dernier enregistrement gagnant ; elles n’ont pas le mécanisme de révision des essais. Il n’existe pas encore d’écran de consultation de l’audit générique ni d’état des grants sur la fiche administrative. Les horaires/groupes/saisons/types/consentements doivent être configurés via l’existant avant une campagne réelle ; aucun paramètre Budokan codé en dur. Recherche simple, limite des résultats et dépendance Bootstrap CDN existante conservées. Pas d’audit WCAG formel ou matrice multi-navigateurs.

Campagne réelle non commencée. Prévoir DB isolée, comptes activés avec usernames distincts, adresses fournies localement hors Git/rapports/logs, mots de passe non fixes, profils navigateurs séparés. A sans rôle, B rôle secretary explicitement attribué via clubctl existant, C avec relation ET grant valide si nécessaire. Tester coordonnées self-service visibles au bureau, adhésion créée/traitée au bureau visible uniquement au membre concerné, révocation du rôle et du grant indépendantes. Ne jamais attribuer un rôle parce qu’une Membership est active.

## Validation finale et inventaire

Génération sqlc réussie et stabilité des fichiers générés vérifiée par SHA-256 avant/après (identiques). gofmt appliqué aux 16 fichiers Go concernés ; git diff --check réussi. Suite finale go test ./... réussie (code de sortie 0), application 208,971 s, handlers 0,102 s, router 0,007 s, views 0,006 s ; autres packages verts/cache ou sans tests propres. Journal complet : /tmp/club-admin-verified-tests.log. La suite avait aussi réussi lors de la reprise et avant l’ultime correction de texte long.

Contrôle race final étendu réussi (code de sortie 0), aucune course détectée : application 376,350 s, handlers 2,171 s, views 1,028 s, router 1,033 s, autres packages cache ou sans tests propres. Journal complet /tmp/club-admin-verified-race.log. Le premier passage avait également réussi (application 378,553 s, database 39,598 s). La dernière exécution porte bien sur l’ultime correction de texte long et sur l’assertion de conservation des notes invalides.

Commande race finale :
```bash
go test -race \
  ./internal/application ./internal/accounts ./internal/personalspace \
  ./internal/guardianaccess ./internal/memberships ./internal/authorization \
  ./internal/auth ./internal/handlers ./internal/administration \
  ./internal/trials ./internal/views ./internal/router ./internal/database
```

Les contrôles finaux sqlc generate, gofmt sur tous les fichiers Go concernés, go test ./..., git diff --check et git status --short ont été exécutés. Aucun généré obsolète, aucun fichier Go restant à formater. La suite complète couvre également les cinq nouveaux scénarios administratifs PostgreSQL/HTTP. Toutes les validations sont réussies ; aucune correction fonctionnelle en attente.

Aucun commit, push, reset, clean, modification de secret ni suppression de travail métier local. Le harnais visuel temporaire est archivé dans /tmp.

### Fichiers créés


- `docs/reports/2026-09-13-administrative-space.md`
- `internal/administration/service.go`
- `internal/application/administrative_space_integration_test.go`
- `internal/database/dbsqlc/administration.sql.go`
- `internal/database/queries/administration.sql`
- `internal/handlers/administration.go`
- `internal/memberships/groups.go`
- `internal/views/administration.go`
- `internal/views/templates/pages/administration.html`
- `migrations/0025_administrative_space.sql`

### Fichiers suivis modifiés

- `internal/application/application.go`
- `internal/database/dbsqlc/membership_groups.sql.go`
- `internal/database/dbsqlc/models.go`
- `internal/database/dbsqlc/trial_registrations.sql.go`
- `internal/database/queries/membership_groups.sql`
- `internal/database/queries/trial_registrations.sql`
- `internal/handlers/memberships.go`
- `internal/memberships/service.go`
- `internal/router/001_router.go`
- `internal/trials/service.go`
- `internal/views/memberships.go`
- `internal/views/status.go`
- `internal/views/templates/layouts/base.html`
- `internal/views/templates/pages/dashboard.html`
- `internal/views/templates/pages/membership_detail.html`

### État Git constaté après modifications

15 fichiers suivis modifiés, 10 nouveaux fichiers (le statut abrège le package administration en répertoire), aucun fichier indexé.

```text
 M internal/application/application.go
 M internal/database/dbsqlc/membership_groups.sql.go
 M internal/database/dbsqlc/models.go
 M internal/database/dbsqlc/trial_registrations.sql.go
 M internal/database/queries/membership_groups.sql
 M internal/database/queries/trial_registrations.sql
 M internal/handlers/memberships.go
 M internal/memberships/service.go
 M internal/router/001_router.go
 M internal/trials/service.go
 M internal/views/memberships.go
 M internal/views/status.go
 M internal/views/templates/layouts/base.html
 M internal/views/templates/pages/dashboard.html
 M internal/views/templates/pages/membership_detail.html
?? docs/reports/2026-09-13-administrative-space.md
?? internal/administration/
?? internal/application/administrative_space_integration_test.go
?? internal/database/dbsqlc/administration.sql.go
?? internal/database/queries/administration.sql
?? internal/handlers/administration.go
?? internal/memberships/groups.go
?? internal/views/administration.go
?? internal/views/templates/pages/administration.html
?? migrations/0025_administrative_space.sql
```

### Sorties complètes des suites finales

`go test ./...` — sortie 0 :

```text
?   	github.com/grapinou/club-core/cmd/clubctl	[no test files]
?   	github.com/grapinou/club-core/cmd/server	[no test files]
?   	github.com/grapinou/club-core/internal/accounts	[no test files]
?   	github.com/grapinou/club-core/internal/accounts/provisioning	[no test files]
?   	github.com/grapinou/club-core/internal/activation	[no test files]
?   	github.com/grapinou/club-core/internal/administration	[no test files]
ok  	github.com/grapinou/club-core/internal/application	208.971s
ok  	github.com/grapinou/club-core/internal/auth	(cached)
ok  	github.com/grapinou/club-core/internal/authorization	(cached)
ok  	github.com/grapinou/club-core/internal/civildate	(cached)
?   	github.com/grapinou/club-core/internal/clubctl	[no test files]
ok  	github.com/grapinou/club-core/internal/config	(cached)
?   	github.com/grapinou/club-core/internal/consents	[no test files]
ok  	github.com/grapinou/club-core/internal/database	(cached)
?   	github.com/grapinou/club-core/internal/database/dbsqlc	[no test files]
?   	github.com/grapinou/club-core/internal/guardianaccess	[no test files]
ok  	github.com/grapinou/club-core/internal/handlers	0.102s
ok  	github.com/grapinou/club-core/internal/identityresolution	(cached)
ok  	github.com/grapinou/club-core/internal/mailer	(cached)
ok  	github.com/grapinou/club-core/internal/memberships	(cached)
ok  	github.com/grapinou/club-core/internal/minorsafety	(cached)
ok  	github.com/grapinou/club-core/internal/outbox	(cached)
?   	github.com/grapinou/club-core/internal/personalspace	[no test files]
ok  	github.com/grapinou/club-core/internal/registrationapplications	(cached)
ok  	github.com/grapinou/club-core/internal/router	0.007s
?   	github.com/grapinou/club-core/internal/trials	[no test files]
ok  	github.com/grapinou/club-core/internal/views	0.006s
ok  	github.com/grapinou/club-core/internal/websecurity	(cached)
```

Contrôle race — sortie 0 :

```text
ok  	github.com/grapinou/club-core/internal/application	376.350s
?   	github.com/grapinou/club-core/internal/accounts	[no test files]
?   	github.com/grapinou/club-core/internal/personalspace	[no test files]
?   	github.com/grapinou/club-core/internal/guardianaccess	[no test files]
ok  	github.com/grapinou/club-core/internal/memberships	(cached)
ok  	github.com/grapinou/club-core/internal/authorization	(cached)
ok  	github.com/grapinou/club-core/internal/auth	(cached)
ok  	github.com/grapinou/club-core/internal/handlers	2.171s
?   	github.com/grapinou/club-core/internal/administration	[no test files]
?   	github.com/grapinou/club-core/internal/trials	[no test files]
ok  	github.com/grapinou/club-core/internal/views	1.028s
ok  	github.com/grapinou/club-core/internal/router	1.033s
ok  	github.com/grapinou/club-core/internal/database	(cached)
```

État final : copie de travail prête à la revue manuelle, 15 fichiers suivis modifiés et 10 nouveaux, aucun fichier indexé. L’inventaire Git ci-dessus est inchangé. Aucun commit ni push. La campagne avec adresses réelles reste à organiser séparément.
