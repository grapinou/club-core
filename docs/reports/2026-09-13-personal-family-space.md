# Espace personnel et familial — 13 septembre 2026

## Diagnostic initial et reprise

Reprise sur HEAD `05c2a62 Consolidate application UX`. Les six commandes demandées ont été exécutées avant toute modification : `git status --short`, `git diff --stat`, `git diff --check`, `git diff`, `git log --oneline -5`, `go test ./...`. Aucun changement local supprimé, aucune remise à zéro, aucun commit/push.

État trouvé : six fichiers suivis modifiés (27 insertions, 49 suppressions) et sept entrées non suivies, dont le package personalspace. Le rapport de ce chantier était absent ; celui de consolidation UX a été lu. Aucun TODO/FIXME dans les nouveaux fichiers. Aucune preuve de contrôle visuel personnel final disponible ; les captures temporaires du chantier UX précédent n’existaient plus.

Déjà présents : routes personnelles et alias /me, service personalspace, DTO minimaux, templates personnels dédiés, six projections SQL sqlc, détail des consentements, groupes/créneaux actuels, contrôle de propriété SQL, GuardianAccess sur chaque requête enfant, dashboard groupé, trois tests d’intégration personnels. Ces implémentations ont été conservées.

La première suite globale a échoué dans application ; les autres packages testés passaient. Les tests personnels existants contenaient des fixtures invalides : `TestPersonalAccountOwnershipAndHistory` et `TestPersonalGroupsAndDashboardBatching` inséraient des Memberships sans le statut obligatoire (SQLSTATE 23502) ; `TestPersonalFamilyAuthorizationPrivacyAndSnapshots` créait ensuite une adhésion adulte sans répondre à la nouvelle définition de consentement active (« unanswered or unexpected consent »). Ces erreurs appartenaient au chantier local, pas à des régressions constatées dans les parcours historiques. Inspection du code et des contraintes avant correction : statut pending explicite et désactivation des définitions après les assertions de snapshot, avant la fixture adulte indépendante.

Complété pendant la reprise : fonctions administratives du compte traduites, indication personnelle de contact d’urgence sans coordonnées, assertions de retrait immédiat d’une fonction, états vides du dashboard et security headers. Tests personnels ciblés ensuite passants (6,180 s). Génération SQL et formatage repris. Contrôle réel et validation finale détaillés ci-dessous.

## Routes et architecture

| GET | Fonction |
|---|---|
| /dashboard | Mon espace, mes adhésions et enfants réellement gérables |
| /me | Redirection 303 vers /dashboard, aucune page dupliquée |
| /me/account | Compte connecté |
| /me/memberships/{id} | Adhésion appartenant à la Person connectée |
| /me/children/{childID} | Enfant autorisé par GuardianAccess |
| /me/children/{childID}/memberships/{membershipID} | Autorisation enfant ET propriété de l’adhésion |

`internal/personalspace.Service` propose GetMyAccount, GetDashboard, GetMyMembership, GetManagedChild et GetManagedChildMembership. Les listes d’adhésions sont incluses dans les projections du dashboard et de l’enfant, sans méthodes ni routes supplémentaires inutiles. Les handlers parsèment les IDs, appellent le service et rendent HTML. Ressource absente, étrangère ou ID malformé : 404 uniforme ; anonyme : 303 login ; panne technique : message générique 503.

DTO dédiés Account, Summary, Dashboard, Child, Consent, Group, Slot et Membership. View models PersonalAccountView, ChildOverviewView, PersonalMembershipView (ChildID pour la variante enfant), composés dans PersonalPage. Aucune structure administrative complète ne passe au template. Les IDs internes servent uniquement aux liens et contrôles, sans affichage décoratif.

## Séparation personnel / administration et HTTP

Aucun rôle administratif ne donne de dérogation à la propriété ni à GuardianAccess. Un secrétaire non guardian reçoit 404 sur la route personnelle enfant, alors que sa route administrative Membership reste disponible selon RBAC. Les liens d’administration restent dans une navigation/section explicitement séparée. Les détails administratifs existants ne sont pas réutilisés comme pages personnelles.

Authentification/session obligatoires, Cache-Control no-store sur succès et refus, middleware de sécurité et CSRF existants conservés. Aucune PII dans les liens/query strings générés. Aucun POST métier ajouté, aucun User enfant créé, aucune écriture métier dans personalspace. Le rôle guardian et un rôle membership ne sont pas introduits.

## Compte, adhésions et historique

Compte : prénom, nom, username, email, téléphone présent, état Actif (la projection exige un compte actif/activé et une Person non archivée). Fonctions existantes traduites : Présidence, Secrétariat, Trésorerie, Encadrement sportif ; relues à chaque accès. Aucun hash, code d’activation ou jeton n’est sélectionné pour affichage.

Adhésion : saison, type, statut français, activités, dates utiles, état du dossier, groupes/créneaux actuels et consentements. Toutes les saisons sont listées, de la plus récente à la plus ancienne, y compris les adhésions terminées. GetPersonalMembership contraint simultanément l’ID et person_id avant toute lecture de détail dépendante.

La politique de complétude memberships existante est réutilisée, puis réduite à des booléens utiles. Affichage « Dossier complet », « Une information doit encore être complétée par le club » et, si nécessaire pour un mineur, contact d’urgence manquant. Aucun code de blocage interne ne passe au template. Un refus de consentement est une décision enregistrée, pas une erreur ; l’absence de contact d’urgence adulte ne devient pas un blocage.

## Enfant, GuardianAccess et majorité

Enfants du dashboard issus exclusivement de ListManagedChildren de GuardianAccess. Chaque requête de page enfant ou d’adhésion enfant recalcule CanManageChild. Grant actif, relation existante, compte guardian actif et activé, Persons non archivées et minorité sont nécessaires. Ni primary_contact, ni coordonnées communes, ni rôle admin ne remplacent le grant.

La frontière de majorité reste la politique de date civile existante : dès le jour des 18 ans, tous les accès guardian sont retirés, y compris aux anciennes saisons. Révocation, suppression de relation, archivage ou désactivation prennent effet sur les requêtes suivantes.

Page enfant : prénom/nom, date de naissance, liste d’adhésions et liens vers les détails. Le détail d’adhésion enfant exige à la fois le droit de gérer cet enfant et membership.person_id == childID. Aucune exception pour un ID valide d’un autre enfant ou adulte.

## Confidentialité et consentements

Masqués : notes Person et administration, coordonnées enfant, compte enfant, activation/sessions, audit administratif, résolution d’identité, candidats de matching, safe_error_code, identités/coordonnées des autres guardians et contacts d’urgence. Les projections sélectionnent seulement les données utiles.

Urgence : booléen enregistré/manquant ; si le viewer est contact, « Vous êtes enregistré comme contact d’urgence ». Les coordonnées d’autrui ne sont jamais chargées par la projection enfant.

Consentements : définition/version liée au snapshot des exigences de cette adhésion, titre et texte, dernière décision, date dans le fuseau métier. Les nouvelles versions actives ne remplacent pas le snapshot historique. Refusé et Retiré sont traduits normalement ; aucune décision et aucun consentement disposent d’un état explicite. Auteur réduit en SQL à un booléen : « par vous » ou « par un responsable ». Aucun nom/ID du donneur transmis au template. Échappement html/template vérifié avec un texte contenant script.

## SQL, groupes et créneaux

Six queries dans personal_space.sql, code sqlc généré, aucune migration. ListPersonalMembershipSummaries prend un tableau d’IDs autorisés et couvre toutes les saisons. Une seule lecture des Memberships pour le compte et tous ses enfants : N+1 Membership du dashboard corrigé dans le travail repris et conservé. Test avec zéro puis quatre enfants : une requête de liste, associations correctes, enfant sans adhésion conservé. GuardianAccess garde sa propre lecture groupée ; aucun refactor général.

Groupes actifs avec joined_at <= aujourd’hui et left_at absent ou strictement futur ; un groupe quitté aujourd’hui n’est pas actuel. Créneaux actifs de la saison de l’adhésion, validité inclusive autour de la date métier. Jour français, HH:MM, lieu ; état explicite sans groupe ou sans créneau. Pas d’agenda ni nouvel historique de groupes.

## Navigation et UX

Mon espace et Mon compte conservés dans la navigation partagée. Cards, section-panel, page-header, badges et empty-state du vocabulaire consolidé ; largeur content-readable, listes plutôt que grosses tables, focus visible partagé. Retour au dashboard et à la page enfant. Aucun bouton d’écriture administrative dans les sections personnelles.

## Tests et contrôle visuel

Tests PostgreSQL/Testcontainers dans personal_space_integration_test.go : anonyme, compte et secrets, propriété/historique, IDs invalides/inexistants/étrangers, absence de grant et admin non guardian, grant valide, révocation, suppression relation, majorité exacte et historique interdit, enfant/guardian archivés, compte désactivé, Membership autre enfant/adulte, consentements snapshot/refus/retrait/anonymisation/empty state/XSS, groupes/créneaux actuels et batching. Tests complétés pour les fonctions du compte, leur retrait, le contact viewer, les états vides et les headers HTTP. Les tests GuardianAccess existants conservent leurs horloges contrôlées et la politique minorsafety.

Contrôle final réel : application Go complète écoutant sur localhost:8080, PostgreSQL 16 isolé via la fixture existante newFixture/guardianPair/request ; vrais login et cookies Chromium/Playwright, aucun SMTP réel. Le harnais temporaire a été retiré du dépôt après arrêt du serveur ; copie dans /tmp/club-personal-visual-fixture.go. Aucun environnement métier existant modifié.

Douze navigations/captures : dashboard membre et responsable, compte, Membership personnelle, enfant, Membership enfant, chacune à 390 et 1365 px (hauteur de viewport 900). Réponses 200, Bootstrap chargé, scrollWidth égal à la largeur du viewport partout. Les douze captures pleine page ont été examinées : navigation entière, sections hiérarchisées, badges lisibles, empty states visibles, textes non coupés, contenu desktop limité à la largeur partagée. Le consentement refusé apparaît normalement dans un dossier complet et son auteur est anonymisé. Aucune sentinelle de notes/coordonnées sensibles dans les pages. Captures et mesures conservées temporairement dans `/tmp/club-personal-visual/`, fichier `audit.json`. Pas de correction CSS nécessaire.

Validation automatisée finale : `sqlc generate` réussi ; gofmt appliqué à tous les fichiers Go du chantier ; `go test ./...` réussi, application en 178,067 s et les autres packages verts ou sans tests. Les parcours /join, /join/child, guardianaccess, minorsafety, membership approval, RBAC, login, activation, registration reviews, dashboard et UX restent couverts par la suite existante, sans modification de leurs tests. Journal : `/tmp/club-personal-final-tests.log`.

`git diff --check` réussi après la suite globale ; `git status --short` : six fichiers suivis modifiés, huit entrées nouvelles (dont le package personalspace et ce rapport), tout non committé. Le diff --stat standard ne compte pas les fichiers non suivis. Contrôle `go test -race ./internal/application ./internal/guardianaccess ./internal/memberships ./internal/accounts ./internal/personalspace` : réussi, application en 306,203 s, aucune course détectée. Journal : `/tmp/club-personal-race.log`. Les tests d’intégration de guardianaccess/accounts/personalspace résident dans application, même si ces packages affichent « no test files ».

## Limites et suites possibles

Lecture seule intégrale. Pas de gestion de profil, compte mineur, guardian, consentements, adhésions, paiement, documents, notifications ou agenda. Aucune décision produit bloquante pour cette V1. Les futures écritures devront faire l’objet d’un chantier séparé avec leurs propres autorisations et validations ; elles ne sont pas anticipées par des boutons inactifs. Les fonctions affichées sont les quatre fonctions administratives actuellement connues ; de nouvelles fonctions nécessiteront un libellé explicite. Contrôle Chromium, sans audit WCAG formel ou matrice multi-navigateurs.

## Inventaire final du chantier

- `docs/reports/2026-09-13-personal-family-space.md`
- `internal/application/application.go`
- `internal/application/personal_space_integration_test.go`
- `internal/database/dbsqlc/personal_space.sql.go`
- `internal/database/queries/personal_space.sql`
- `internal/handlers/dashboard.go`
- `internal/handlers/personal_space.go`
- `internal/memberships/service.go`
- `internal/personalspace/service.go`
- `internal/views/dashboard.go`
- `internal/views/personal_space.go`
- `internal/views/templates/layouts/base.html`
- `internal/views/templates/pages/dashboard.html`
- `internal/views/templates/pages/personal_space.html`

Dernier `git diff --check` après finalisation du rapport : réussi. Aucun commit ni push. Aucun changement local trouvé à la reprise abandonné.
