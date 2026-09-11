# Interface administrative des adhésions

Date : 2026-09-11.

## Diagnostic initial et reprise

Avant le début du chantier, le dépôt était propre, les tests réussissaient et le commit 6d382a7 « Add RBAC and secure administrative routes » était confirmé sur le distant (6d382a7adafaeb72fed62ebc27e9b564585dbe69).

Au moment de la reprise demandée :
- trois fichiers suivis étaient modifiés : queries/memberships.sql, dbsqlc/memberships.sql.go et memberships/service.go ;
- memberships/list.go était déjà créé et non suivi ;
- git diff --check réussissait ;
- git status --short, git diff --stat, git diff complet et les cinq derniers commits ont été examinés ;
- go test ./... réussissait : application 17,752 s et database 25,136 s ;
- aucune erreur actuelle n’était à corriger avant poursuite.

Déjà implémenté et préservé :
- lecture enrichie de liste et tri pending ;
- lecture groupée des faits nécessaires à la complétude ;
- évaluation Go partagée entre détail, approbation et liste ;
- List en deux requêtes sous transaction read-only repeatable-read ;
- enrichissement de GetDetails avec guardians et contacts d’urgence ;
- noms des répondants aux consentements snapshotés ;
- requête de vérification du lien Membership/User destinée au retour après renvoi.

Restait à faire :
- routes HTTP, handlers, view models et templates ;
- navigation conditionnelle ;
- branchement des opérations accounts avec PRG ;
- tests propres à cette interface ;
- rapport, qui n’existait pas encore.

Les tests métier/RBAC/activation/email existants ont été conservés. Aucun reset, restore, suppression des modifications locales, commit ou push.

## Routes et permissions

| Route | Permission | Mutation / CSRF |
| --- | --- | --- |
| GET /memberships | memberships.read | lecture, no-store |
| GET /memberships/{id} | memberships.read | lecture, no-store, token fourni aux formulaires |
| POST /memberships/{id}/approve | memberships.approve | CSRF commun obligatoire |
| POST /users/{id}/resend-activation | activation.resend | CSRF commun obligatoire |

Les routes sont enregistrées par MembershipHandler.Register sur le ServeMux existant, dans le point de composition application.NewWithMailer. Elles utilisent les mêmes Access.RequirePermission et websecurity.CSRF que Persons.

President et secretary disposent des permissions nécessaires selon le mapping V1 existant. Aucun nouveau rôle ou droit n’est introduit. Le lien Adhésions du layout commun dépend de memberships.read, indépendamment du lien Personnes.

## Liste administrative

GET /memberships affiche :
- nom et prénom avec lien vers le dossier ;
- saison et type d’adhésion ;
- statut français accompagné de l’identifiant technique pending/active/ended/cancelled ;
- date de demande et date d’approbation si renseignée ;
- Complet ou À compléter, nombre de blocages et de conseils ;
- état du compte : absent, désactivé, activé ou activation nécessaire.

Les pending sont mises en évidence et classées de la plus ancienne à la plus récente. Les autres statuts suivent, par date de demande décroissante. L’ID départage les égalités. Une liste vide affiche un message explicite.

Le service memberships.List fait deux requêtes, indépendamment du nombre d’adhésions :
1. lignes enrichies par les jointures Person, Season, MembershipType et User ;
2. faits de complétude de tous les IDs sélectionnés.

La date administrative est commune aux lignes et les données proviennent d’une seule transaction repeatable-read. Aucun appel GetDetails dans une boucle de liste. Le nombre de requêtes RBAC/session reste également indépendant du nombre de lignes.

## Dossier administratif

GET /memberships/{id} réutilise memberships.GetDetails. Cette lecture reste propriétaire de l’assemblage cohérent et du calcul de complétude.

La page comprend :
- identité : prénom, nom, naissance, Majeur/Mineur calculé par le backend, téléphone, email, adresse et notes Person ;
- adhésion : saison, type, statut, dates, approver et note administrative ;
- activités liées ;
- responsables légaux avec relation, indicateur de contact principal, téléphone et email ;
- contacts d’urgence dans l’ordre de priorité ;
- consentements présentés lors de cette demande ;
- compte User et actions applicables.

La majorité affichée vient de Completeness.IsMinor, sans nouveau calcul d’âge ni persistance. Si la naissance manque, elle est indiquée comme inconnue. La convention du dix-huitième anniversaire du métier existant est conservée.

Les consentements proviennent exclusivement de membership_consent_requirements : titre, version, description intégrale, dernière décision, nom du répondant et date. Une définition publiée ensuite n’est pas ajoutée au dossier.

Les quatre états se distinguent :
- Accordé (granted) : badge vert ;
- Refusé (refused) : badge neutre ;
- Retiré (withdrawn) : badge neutre ;
- Non renseigné : badge d’attention.

Un refus ou retrait n’est pas présenté comme une erreur administrative dès lors qu’une réponse initiale existe. Le catalogue actif ne sert jamais à reconstruire le dossier.

Le compte affiche username, actif, activé et activation nécessaire. Les view models ne contiennent aucun password_hash, code d’activation ou jeton de session. Le seul token de formulaire est le CSRF commun requis par la sécurité HTTP.

## Complétude et view models

Le calcul de complétude préexistant est partagé dans evaluateCompleteness :
- l’ancienne lecture ponctuelle appelle la nouvelle requête de faits avec un seul ID ;
- GetDetails et ApproveMembership continuent d’utiliser ce calcul ;
- List utilise le même évaluateur pour les faits chargés en groupe.

Aucune règle de majorité ou contact n’est reconstituée dans les templates. Le SQL fournit les faits et les exigences sans réponse initiale ; les conditions administratives restent dans Go.

Traductions :
- missing_birth_date : Date de naissance manquante ;
- missing_activity : Aucune activité renseignée ;
- minor_missing_guardian : Responsable légal manquant pour un mineur ;
- minor_missing_emergency : Contact d’urgence manquant pour un mineur ;
- unanswered_consent : Une autorisation n’a pas reçu de réponse ;
- adult_missing_emergency : Contact d’urgence conseillé.

BlockingIssues produit une section Blocages et « Validation impossible ». Warnings apparaît séparément sous « Conseils — ne bloquent pas l’approbation ». Un pending sans blocage affiche « Dossier prêt à être validé ». Pour un dossier déjà traité, le titre est « Dossier complet » lorsqu’il n’a aucun blocage.

Structures ciblées dans internal/views/memberships.go :
- MembershipListView et MembershipRowView ;
- MembershipDetailView ;
- ContactView, ConsentView et AccountView.

Les dates sont formatées côté Go dans le fuseau administratif configuré. Les données prêtes à afficher, les labels, états et permissions d’action sont transmis au template, sans framework DTO générique.

## Approbation et note administrative

Le formulaire de validation apparaît seulement pour pending avec memberships.approve. Il accepte une note administrative facultative et son bouton est désactivé lorsque le backend indique des blocages. Les warnings seuls ne désactivent pas le bouton.

POST /memberships/{id}/approve :
1. RBAC et CSRF existants ;
2. récupération exclusive de l’acteur depuis auth.UserID(r.Context()) ;
3. lecture de admin_note, espaces périphériques retirés, nil si vide ;
4. appel accounts.ApproveMembership ;
5. résultat après commit et envoi éventuel ;
6. redirection 303 vers le dossier.

La note est enregistrée dans memberships.admin_note, sans toucher persons.notes. Les champs actorID/approver éventuellement envoyés par un navigateur ne sont pas utilisés.

Le backend refait les contrôles transactionnels, même si la page précédente indiquait un dossier complet. Un dossier incomplet reste pending et revient après PRG avec un message lisible ; le GET recalcule les blocages. Une seconde approbation est refusée sans nouveau mail.

## Résultats d’approbation et d’activation

Les notifications après PRG distinguent :
- Adhésion validée.
- Adhésion validée et email d’activation envoyé.
- Adhésion validée. Aucun email d’activation n’est disponible.
- Adhésion validée, mais email d’activation non envoyé.

DeliveryError.ApprovalCommitted permet de distinguer explicitement un échec de livraison après validation. Un échec SMTP ne fait jamais repasser l’adhésion en pending.

Après traitement, la page affiche « Validée le … par … » si la traçabilité existe et retire le formulaire d’approbation. Les données du compte reflètent la création, réutilisation ou réactivation effectuée par les services existants.

## Renvoi d’activation

Le formulaire est affiché seulement lorsque le compte existe, est actif, nécessite une activation, et que l’acteur possède activation.resend.

POST /users/{id}/resend-activation appelle accounts.ResendActivation avec l’acteur du contexte. Aucun appel direct à PrepareTx dans le handler.

Le formulaire fournit membership_id uniquement pour le retour vers le dossier. Avant toute opération, GetMembershipIDForUser vérifie que ce dossier appartient bien à la Person du User visé. Aucun champ d’URL de retour n’est accepté.

Les résultats sont :
- Email d’activation renvoyé.
- Nouveau code préparé, mais aucun email n’est disponible.
- Nouveau code préparé, mais email non envoyé.
- message d’action indisponible si le compte a entre-temps été activé ou désactivé.

L’invalidation de l’ancien code, la résolution de l’adresse et l’ordre commit → email restent dans accounts/activation. Aucun plaintext_code n’arrive dans le view model.

## SQL ajouté ou enrichi

Dans internal/database/queries/memberships.sql :
- ListAdministrativeMemberships ;
- ListMembershipCompletenessFacts ;
- GetMembershipIDForUser ;
- ListMembershipConsentRequirements enrichie des prénom/nom du répondant.

Les requêtes existantes ListPersonGuardians et ListPersonEmergencyContacts sont réutilisées par GetDetails. Aucun nouveau schéma ni migration. Les fichiers sqlc sont régénérés.

## Sécurité et erreurs

- Anonyme : 303 vers /login.
- Permission absente : 403 via le middleware existant.
- ID invalide, dossier absent ou association dossier/User invalide : 404.
- Erreur DB : 500 générique, jamais de message PostgreSQL brut.
- Validation métier impossible : PRG vers le dossier, message explicite et complétude recalculée.
- GET administratifs : Cache-Control no-store, protections HTTP du CSRF commun.
- Toutes les mutations : permissions, CSRF, POST, PRG.
- Templates html/template avec échappement automatique, notamment des notes et textes.
- Aucune donnée personnelle, note, code ou username dans les query strings : uniquement un marqueur fixe notice.
- Aucun code, hash, mot de passe ou jeton de session affiché ou journalisé par les nouveaux handlers.

Les marqueurs notice suivent le mécanisme simple déjà employé pour les pages d’authentification : ils ne constituent pas une preuve persistante d’envoi. Le statut réel et les actions affichées viennent toujours du dossier relu en base.

## Tests

Les tests précédents ont été conservés, sans duplication des primitives déjà couvertes.

Nouveaux tests PostgreSQL/Testcontainers avec application réelle :
- liste et détails pour anonyme, sans rôle, treasurer, coach, secretary et president ;
- navigation conditionnelle, no-store et absence de fuite de données dans les refus ;
- visibilité des quatre statuts, pending placé avant active plus récent, séparation des Persons/saisons/types ;
- égalité de complétude entre liste groupée et détail pour chaque ligne ;
- 404 des dossiers inexistants ou IDs invalides ;
- identité, minorité, activités, guardians, ordre des contacts d’urgence et répondants ;
- trois décisions explicites, snapshot conservé après nouvelle publication, état non renseigné ;
- blocages mineur, naissance/activité manquantes, warning adulte ;
- notes échappées et séparation Person/admin_note ;
- POST refusés sans permission ou sans CSRF, sans mutation/email ;
- approbation avec warning seul, acteur du contexte malgré champs d’usurpation, métadonnées persistées ;
- User créé ou réutilisé, email envoyé si canal, absence de canal et échec SMTP après commit ;
- double approbation sans nouveau mail ;
- renvoi, ancien code invalidé, compte activé refusé et action absente ;
- absence de canal, SMTP en échec et association dossier/User invalide ;
- absence de plaintext_code, hash, mot de passe ou jeton de session dans les pages ;
- erreur PostgreSQL réelle simulée sur une base isolée : liste et détail retournent 500 générique.

Les nouveaux tests ciblés ont réussi en 12,580 s avant la validation globale finale. Aucun SMTP Internet n’est utilisé.

## Fichiers

Créés :

- docs/reports/2026-09-11-membership-administration-ui.md
- internal/application/membership_ui_integration_test.go
- internal/handlers/memberships.go
- internal/memberships/list.go
- internal/views/memberships.go
- internal/views/templates/pages/membership_detail.html
- internal/views/templates/pages/memberships.html

Modifiés :

- internal/application/application.go
- internal/database/dbsqlc/memberships.sql.go
- internal/database/queries/memberships.sql
- internal/handlers/authorization.go
- internal/memberships/service.go
- internal/views/security.go
- internal/views/templates/layouts/base.html


## Validation finale

- sqlc generate : succès.
- gofmt sur tous les fichiers Go concernés : succès.
- go test ./... : succès ; internal/application exécuté en 29,481 s, internal/handlers en 0,095 s et internal/router en 0,004 s. Autres packages réussis (cache) ou sans fichiers de tests.
- Les tests database avaient aussi été exécutés lors du diagnostic de reprise, en 25,136 s.
- git diff --check : succès, aucune sortie.
- git status --short : sept fichiers suivis modifiés et sept nouveaux fichiers, incluant le travail préservé de la reprise. Aucun fichier indexé.
- Aucun commit ni push.

État Git final :

~~~text
 M internal/application/application.go
 M internal/database/dbsqlc/memberships.sql.go
 M internal/database/queries/memberships.sql
 M internal/handlers/authorization.go
 M internal/memberships/service.go
 M internal/views/security.go
 M internal/views/templates/layouts/base.html
?? docs/reports/2026-09-11-membership-administration-ui.md
?? internal/application/membership_ui_integration_test.go
?? internal/handlers/memberships.go
?? internal/memberships/list.go
?? internal/views/memberships.go
?? internal/views/templates/pages/membership_detail.html
?? internal/views/templates/pages/memberships.html
~~~

## Limites et décisions restantes

- Liste V1 sans pagination, chargée en deux requêtes : une pagination pourra être ajoutée si le volume le justifie.
- Les complétudes affichées sont recalculées à la date de consultation ; la traçabilité d’approbation reste distincte.
- Les informations historiques 0016 dont la présentation initiale n’était pas reconstructible conservent le comportement documenté lors de cette migration.
- Les statuts ended/cancelled sont seulement consultables ; aucun bouton de transition supplémentaire.
- Pas d’édition de guardians, contacts d’urgence ou consentements depuis ce dossier ; les blocages sont visibles mais leur correction reste dans les opérations existantes ou de futurs écrans.
- Les sessions/limiteurs locaux, reverse proxy et livraison SMTP synchrone gardent les limites déjà documentées.
- Pas de formulaire public d’adhésion, nouvelle création publique de Person, fusion de doublons, gestion Web des rôles, paiement, licence, grade, boutique, espace membre/coach, compétition ou récupération de mot de passe.
- Aucun refactor UX global, nouvelle pile de sécurité, commit ou push.
