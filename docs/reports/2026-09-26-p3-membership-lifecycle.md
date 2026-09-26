# P3 — Cycle complet de l’adhésion

## État initial

Travail effectué le 26 septembre 2026, depuis `/home/sighto/club-core`, HEAD initial `4a5af3c` (`Polish public trial flow and email testing`). Le dépôt contenait déjà 30 fichiers suivis modifiés et des fichiers non suivis correspondant notamment à P2.1, aux utilisateurs/rôles, au setup initial et à P2.2 (migrations 0029–0031). Ces modifications ont été conservées. Le diff global ne représente donc pas uniquement P3.

Avant toute modification : `git status --short`, `git diff --stat`, `git diff --check`, `git diff`, `git log --oneline -10`, puis `go test ./...`. La référence était verte, y compris PostgreSQL/Testcontainers ; `internal/database` a terminé en 34,502 s et plusieurs autres packages étaient en cache. Aucun commit ni push n’a été effectué.

## Audit de l’existant

Lecture des migrations, services memberships/trials/accounts/provisioning/personalspace/guardianaccess/administration, handlers, routes, templates, permissions et requêtes sqlc, ainsi que des suites d’intégration associées.

Constats déterminants :

- `memberships` possède déjà les états `pending`, `active`, `ended`, `cancelled`, et une contrainte unique personne/saison, tous statuts confondus.
- `CreateRequest` et ses variantes transactionnelles possèdent les validations de naissance, activités, saison/type actifs et consentements présentés.
- `ApproveMembership` verrouille la personne et l’adhésion, revérifie la complétude, puis compose compte/activation et passage à `active` dans la transaction. L’envoi email intervient après commit via accounts.
- Le raccord administratif depuis l’essai vérifiait déjà personne/activité, mais ne stockait pas l’origine. Les autres appels directs au service memberships n’avaient pas cette validation.
- Les pages d’affectation aux groupes, l’historique et les protections contre chevauchements existaient déjà. Elles n’ont pas été réimplémentées.
- `GuardianAccess` distingue relation familiale et autorisation numérique. L’espace personnel applique cette autorisation sans dérogation administrative.
- L’approbation créait historiquement un User pour toute personne, y compris un enfant. Le parcours familial n’a pas besoin de ce compte enfant.
- Après un essai public mineur, le responsable est lié, mais il manquait des actions administratives accessibles pour le choisir comme contact d’urgence et gérer son accès familial.

## Décisions métier

1. **Origine facultative et précise** : `memberships.source_trial_id`, FK nullable vers `trial_registrations`, avec index unique partiel. Ce modèle suffit au lien demandé ; aucune abstraction d’événements ou d’origines multiples.
2. **Un essai, une conversion** : une même séance ne peut être origine que d’une adhésion, même dans une autre saison. Un renouvellement éventuel est une nouvelle demande sans réutiliser l’origine de l’inscription initiale. La contrainte personne/saison existante reste applicable, y compris aux dossiers terminés ou annulés.
3. **Présence préalable pour une conversion** : le service exige `attended`, la même personne et la présence de l’activité de l’essai parmi les activités choisies. Une adhésion directe reste possible à tout moment selon les règles existantes.
4. **Séance conservée** : un essai utilisé comme origine ne peut plus être reprogrammé par les services trials. Les notes et corrections de résultat restent possibles et auditées ; elles ne suppriment pas l’origine et ne révoquent pas automatiquement l’adhésion.
5. **Blocages inchangés à l’approbation** : naissance manquante/non finie, absence d’activité, absence de responsable ou de contact d’urgence pour un mineur, réponse initiale manquante à un consentement présenté. L’absence de contact d’urgence adulte est un conseil. Les refus de consentement ne deviennent pas des blocages.
6. **Groupes facultatifs et explicites** : aucun groupe n’est créé, copié ou affecté depuis l’essai. Le nom du groupe d’essai est une indication affichée ; l’affectation conserve son action dédiée, ses dates et son historique.
7. **Mineur sans nouveau compte enfant** : l’approbation n’en crée plus. Un compte enfant historique est conservé sans réactivation ni nouvelle activation automatique. Pour les adultes, création/réutilisation/réactivation restent inchangées. Le responsable utilise son propre compte et une autorisation `GuardianAccess` explicite.
8. **Pas de rattachement historique déduit** : les anciennes adhésions conservent une origine NULL. NULL signifie « aucune origine essai enregistrée » ; pour l’historique, cela ne prouve pas qu’aucun essai n’a eu lieu.

## Modèle de données

Ajout unique au dossier :

```sql
source_trial_id INTEGER NULL REFERENCES trial_registrations(id)
```

Index `memberships_source_trial_unique`, limité aux valeurs non NULL. La FK empêche la suppression d’un essai référencé. Les services vérifient la cohérence personne/activité ; une intervention SQL privilégiée reste en dehors de ces validations métier.

Les tables personnes, responsables, accès familiaux, comptes, consentements et groupes ne sont pas remplacées. L’ajout d’un responsable comme contact d’urgence utilise `person_emergency_contacts` ; sa trace utilise `administrative_events`.

## Migration éventuelle

Migration ajoutée : `migrations/0032_membership_source_trial.sql`.

- Ajoute la colonne nullable et son index unique, sans backfill.
- Étend les actions autorisées du journal avec `emergency_contact_added`.
- Refuse le retour à 0031 si une origine ou un nouvel événement serait perdu.
- Le test `TestP3SourceMigrationPreservesHistory` vérifie down/up sans données nouvelles, conservation d’un dossier historique et refus atomique d’un rollback destructeur d’origine.

Migration réellement appliquée dans les bases Testcontainers et dans la base navigateur isolée `clubcore_p3`. Elle n’a pas été appliquée à la base persistante `clubcore_demo` pendant cette session. Le lancement habituel via `scripts/run-dev.sh` appliquera les migrations comme auparavant ; le script n’a pas été modifié.

## Relation essai → adhésion

`memberships.Request.SourceTrialID` vaut zéro pour une demande directe. Toutes les variantes `CreateRequest*` passent par la même validation transactionnelle de la source, puis son insertion atomique avec activités et consentements.

Le verrou d’essai est partagé avec les mutations de séance ; l’unicité PostgreSQL protège les conversions concurrentes. Les requêtes de lecture ajoutent le contexte d’origine et retrouvent le dossier lié ou celui de la personne pour la saison de la séance. Sans créneau, la date d’essai sert à proposer une saison ; ce n’est qu’une présélection modifiable.

Depuis un essai présent, le formulaire montre date, activité, groupe et horaire, présélectionne l’activité et propose la saison. Si un dossier existe déjà pour la saison correspondante ou depuis cet essai, le lien ouvre ce dossier. L’ancienne URL de préparation redirige également vers lui. Un groupe n’est jamais affecté automatiquement.

## Adhésion directe

Le formulaire reste accessible depuis la fiche personne sans `trial`. Aucune lecture ni présence d’essai n’est requise par `CreateRequest` dans ce cas. La colonne reste NULL après demande et approbation. Le dossier indique « Demande d’adhésion directe ».

Le formulaire liste les dossiers existants et retire leurs saisons des choix. La contrainte unique PostgreSQL reste l’autorité lors de POST forgés ou concurrents.

## Workflow pending → active

Transitions réellement offertes : création d’une demande `pending`, puis approbation administrative `pending → active`. Une nouvelle approbation est refusée. Les états historiques `ended` et `cancelled` restent lisibles ; P3 n’ajoute pas de nouvelle action de terminaison ou d’annulation d’adhésion.

L’approbation conserve les verrous et la vérification de complétude dans la transaction. L’origine n’est ni effacée ni recalculée. Le navigateur affiche les blocages et désactive le bouton, mais le backend reste l’autorité.

## Groupes

Réutilisation de `/memberships/{id}/groups`, `AssignGroupTx`, `CloseGroupTx` et des requêtes existantes. Conservation du multi-groupe, des périodes `joined_at / left_at` et de la distinction entre groupe durable et appartenance temporaire.

Le dossier principal affiche désormais les affectations et leurs dates, y compris les sorties, ainsi que le lien vers la gestion complète. L’espace personnel continue d’afficher les groupes courants. Chromium a réalisé une affectation volontaire après approbation et vérifié sa visibilité dans l’espace adulte.

## Mineurs et responsables

Sur la fiche d’un enfant, pour un responsable déjà lié, quatre actions minimales sont proposées : choisir comme contact d’urgence, autoriser l’accès familial, préparer/renvoyer l’activation du responsable, retirer l’accès familial.

Le contact d’urgence n’accorde aucun accès numérique. L’autorisation familiale ne valide pas l’adhésion. La préparation de compte requiert cette autorisation et réutilise `EnsureGuardianUser`. L’approbation n’invente ni relation, ni autorisation, ni compte enfant.

L’ajout d’un contact verrouille les personnes par ordre d’identifiant, vérifie la relation, conserve les autres contacts et alloue une nouvelle priorité. L’écriture et son journal administratif sont atomiques. Les autorisations/révocations conservent leur historique et acteur via le service GuardianAccess existant.

## Consentements

Les définitions présentées, décisions, auteurs et versions restent gérés par les services existants. Aucun accord implicite ; le bureau reporte les décisions effectivement recueillies. L’essai ne vaut pas consentement à l’adhésion.

Les tests et le navigateur ont utilisé un refus adulte non bloquant et une décision de responsable pour l’enfant. L’historique et la règle de réponse initiale sont inchangés.

## Comptes utilisateurs

Adultes : workflow existant création/réutilisation/réactivation et email après commit. Mineurs : approbation sans nouveau User, sans code ni email d’activation enfant. Si un User enfant existait déjà, il est retourné inchangé ; sinon `Approval.User` est vide et `accounts.ApprovalResult.UserID` vaut zéro.

Le compte responsable est créé/réutilisé indépendamment de l’adhésion enfant. Les échecs ou absences de canal email donnent un message distinct ; le bureau peut compléter les coordonnées et relancer la préparation/activation. Les comptes désactivés ne sont pas réactivés silencieusement par cette action familiale.

## Espace personnel / familial

Les projections personalspace et les contrôles d’appartenance existants ont été conservés. Les tests lisent l’adhésion active adulte et l’adhésion active enfant depuis un compte responsable autorisé. Une relation seule ne donne pas accès. Une révocation enlève immédiatement cet accès. Un administrateur et un autre membre obtiennent un 404 sur la route personnelle de l’enfant.

Aucune donnée administrative supplémentaire n’a été injectée dans les vues familiales.

## Administration

`/memberships/{id}` rassemble identité, saison/type, activités, origine, statut, complétude, responsables, urgence, consentements, groupes, compte et validation. Ajout du lien vers la fiche personne.

La fiche personne conserve sa vue globale, avec dates de demande/validation et lien vers l’essai d’origine dans les lignes d’adhésion. Les détails restent dans leurs dossiers respectifs. Aucun identifiant technique n’est utilisé comme libellé utilisateur.

## Routes et permissions

| Parcours | Permission / contrôle |
| --- | --- |
| GET `/memberships`, `/memberships/{id}`, groupes | `memberships.read` existant |
| Demande, approbation, mutations de groupes et notes d’adhésion | `memberships.approve` existant |
| Personnes / essais en lecture | `persons.read` |
| Mutations personnes / essais | `persons.write` |
| POST `/users/{id}/resend-activation` | `activation.resend` existant |
| POST `/persons/{id}/guardians/{guardian}/emergency` | `persons.write`, relation vérifiée par administration |
| POST `/persons/{id}/guardians/{guardian}/grant` et `/revoke` | `persons.write`, service GuardianAccess |
| POST `/persons/{id}/guardians/{guardian}/activation` | `persons.write`, `EnsureGuardianUser` + autorisation familiale existante |
| `/me/children/{id}/memberships/{membership}` | session, appartenance et GuardianAccess ; aucun contournement RBAC |

Les nouveaux chemins familiaux utilisent la permission de gestion de personne déjà exigée par les services familiaux. Le renvoi d’activation générique garde sa permission propre. Aucun test de nom de rôle n’a été ajouté aux handlers.

## Sécurité

CSRF sur les nouvelles mutations, acteur issu du contexte de session, PRG en succès, messages fixes en URL, `Cache-Control: no-store` via les protections existantes. L’identifiant d’essai reçu par formulaire est systématiquement revalidé par le service, même hors HTTP.

L’audit et les mutations de contact sont dans la même transaction ; comptes/activation et autorisations utilisent leurs transactions existantes. L’envoi SMTP n’est pas inclus dans une transaction PostgreSQL. Aucune modification du RBAC, des règles de majorité ou de la politique de sécurité des mineurs.

## UX

Contexte d’essai visible avant enregistrement et origine persistante visible après validation. L’action de préparation depuis un essai n’est proposée que pour un résultat présent ; sinon une demande directe reste accessible. Les dossiers existants sont liés au lieu de proposer un doublon.

Les groupes sont expliqués comme facultatifs et la provenance n’impose pas d’affectation. La fiche mineur distingue relation familiale, contact d’urgence et accès familial. Pas de refonte graphique ni de champs financiers ajoutés.

## Mobile

Chromium réel via Playwright, aux largeurs 1280 et 390 px. Mesure de `document.documentElement.scrollWidth <= innerWidth` sur les pages vérifiées des trois parcours : administration, essais, préparation depuis essai, dossiers pending/active, personnes, groupes, adhésions et espace personnel/familial. Aucun dépassement mesuré.

Captures réellement examinées : préparation adulte à 1280 px, dossier adulte actif à 390 px, fiche enfant avec actions familiales à 390 px, adhésion enfant dans l’espace familial à 390 px. Navigation répartie sur plusieurs lignes, formulaires et boutons lisibles. Les réservations publiques et activations ont aussi été saisies dans des fenêtres de 390 px. Les captures ont été utilisées hors dépôt puis supprimées après contrôle.

## Tests automatisés

Nouveaux tests PostgreSQL/Testcontainers :

- `TestP3MembershipLifecycle` : adulte avec essai programmé puis présent, demande pending, approbation active, origine conservée, email d’activation et connexion personnelle ; adhésion directe avec NULL avant/après approbation ; enfant avec responsable, essai, décision de consentement, refus d’approbation avant contact d’urgence, ajout du contact, autorisation familiale, création/activation du compte responsable, approbation sans compte enfant, visibilité familiale puis révocation. Vérification des liens, groupes non affectés automatiquement et double approbation.
- `TestP3FamilyDossierSecurity` : anonymous, compte sans permission, CSRF sur les quatre nouveaux POST, responsable étranger, préparation de compte sans autorisation, absence de mutation sur refus.
- `TestP3SourceTrialValidationAndConcurrency` : essai inexistant, autre personne, activité incohérente, essai non présent, deux conversions concurrentes dans deux saisons, unicité, refus du doublon, origine après approbation, FK empêchant suppression, reprogrammation refusée et nouvelle demande directe indépendante.
- `TestP3MinorExistingAccountPreserved` : compte enfant historique désactivé conservé sans réactivation ni email.
- `TestP3SourceMigrationPreservesHistory` : historique conservé et garde du rollback.

Suites préexistantes conservées : concurrence d’approbation et de création, rollback d’audit, consentements versionnés, mineurs incomplets, groupes/historique, réactivation/réutilisation de comptes, RBAC, anonymes/CSRF, confidentialité personnelle et familiale, essais publics, configuration de l’association.

Adaptation ciblée de `TestUnactivatedRenewalAndSharedFamilyEmail` : la vérification de réutilisation d’un compte non activé et du canal email familial utilise désormais des personnes adultes. Elle ne doit plus imposer l’ancienne création automatique d’un compte enfant. La nouvelle règle enfant possède ses scénarios séparés.

## Parcours navigateur réel

Base isolée `clubcore_p3`, PostgreSQL 16 dans `clubcore-p3-postgres` (port local 5434), serveur sur `localhost:8080`, Mailpit isolé sur ports 1026/8026. `clubcore_demo` et ses conteneurs n’ont pas été effacés. La structure de l’association fictive « Cercle des découvertes » (Échecs, groupe Découverte, Salle associative, saison/type et consentement) a été préparée en SQL ; le setup administrateur et les parcours suivants ont été exécutés via Chromium, sans injection SQL des personnes/adhésions/permissions familiales.

1. **Adulte Alice** : site public → choix séance/date → réservation → dossier essai → marquer présent → préparer demande → type et refus de consentement → enregistrer → vérifier origine et absence de groupe → approuver → récupérer l’email dans Mailpit → activer le compte → connexion → adhésion active personnelle. Affectation volontaire au groupe après validation, puis vérification du groupe dans l’espace personnel et du lien vers le dossier depuis l’essai.
2. **Adulte David sans essai** : ajout d’une personne dans l’administration → demande directe → décision de consentement → validation → email et activation → adhésion active personnelle avec origine NULL.
3. **Enfant Lina / Camille** : réservation publique mineur avec responsable → présence → demande depuis essai avec décision du responsable → bouton d’approbation désactivé car urgence manquante → fiche enfant → choisir le responsable comme contact d’urgence → autoriser l’accès familial → préparer son activation → réception Mailpit, activation et connexion du responsable → validation de l’enfant → adhésion active dans l’espace familial, sans compte enfant. Accès refusé à Alice et au bureau sur cette route personnelle ; révocation de Camille contrôlée par 404, puis réautorisation et visibilité rétablie.

Les réservations publiques proposaient le 3 octobre 2026 : le résultat « présent » a été renseigné pour simuler l’issue de ces séances dans cette base de test, sans attendre leur date. Aucun événement réel n’est revendiqué. Une première tentative du script a enregistré un essai adulte supplémentaire avant correction de son assertion de confirmation ; cette donnée temporaire est restée isolée et n’a pas créé d’adhésion supplémentaire.

Contrôle SQL final du navigateur : Alice active, origine renseignée, compte activé, un groupe explicite ; David active, origine NULL, compte activé, aucun groupe ; Lina active, origine renseignée, aucun compte enfant, aucun groupe. Les liens et redirections après POST ont été suivis dans Chromium. Les essais initiaux du script ont aussi nécessité de corriger son extraction du secret de setup et ses attentes de messages ; ces erreurs relevaient du script temporaire, pas de modifications du produit.

## Vérifications finales

Résultats de la dernière vérification du code final :

| Commande | Résultat réel |
| --- | --- |
| `go test ./...` | Succès, code 0 ; `internal/application` 232,612 s, `internal/database` 40,211 s. |
| `go vet ./...` | Succès, code 0, aucun diagnostic. |
| `git diff --check` | Succès, code 0. |
| `sqlc generate` | Exécuté après les changements SQL ; sources générées à jour. |
| `git status --short`, `git diff --stat`, `git diff` | Exécutés et revus ; modifications P3 et travail préexistant laissés non committés. |

Revue explicite de généricité : aucune logique ajoutée dépendant de Budokan, BSO, JJB, Jiu-Jitsu, dojo, kimono ou combat. Les fixtures historiques réutilisées n’ont pas été renommées. Les parcours Chromium sur une association culturelle confirment également l’absence de dépendance à une pratique sportive.

Aucun secret, mot de passe de démonstration, capture, script navigateur temporaire ou artefact local ajouté au dépôt. Le serveur temporaire a été arrêté ; les deux conteneurs isolés et leurs données ont été supprimés après contrôle. La base persistante de démonstration et `scripts/run-dev.sh` sont préservés. Aucun commit/push.

## Fichiers principaux modifiés

Fichiers P3 nouveaux :

- `migrations/0032_membership_source_trial.sql`
- `internal/handlers/family_dossier.go`
- `internal/application/membership_lifecycle_integration_test.go`
- `internal/database/membership_source_integration_test.go`
- ce rapport.

Fichiers existants complétés :

- `internal/memberships/service.go`, `internal/trials/service.go`
- `internal/administration/service.go`
- `internal/handlers/administration.go`, `internal/application/application.go`
- `internal/database/queries/{memberships,administration}.sql`
- sorties sqlc correspondantes et `dbsqlc/models.go`
- `internal/views/memberships.go`
- `internal/views/templates/pages/{membership_detail,administration}.html`
- `internal/database/memberships_integration_test.go`.

Les autres modifications préexistantes visibles dans `git status` ne doivent pas être supprimées ni attribuées à P3.

## Limites connues

- Paiements, comptabilité, Hosted, forum/messagerie, RBAC avancé et renouvellement automatique restent hors périmètre.
- Pas de nouvel écran de fin/annulation d’adhésion ; les états existants sont préservés.
- Pas de reconstitution des origines historiques ni de modification d’une origine après création.
- Les noms d’activité/groupe proviennent des objets liés actuels ; il ne s’agit pas d’un snapshot immuable de leurs libellés.
- Les actions familiales ajoutées concernent un responsable déjà lié, notamment depuis l’essai public. Elles ne constituent pas un CRUD complet des relations familiales ou des autres contacts d’urgence.
- La relation issue d’un formulaire public doit être vérifiée par le bureau avant d’accorder l’accès familial ; P3 ne fournit pas de vérification d’identité automatique.
- Une absence de canal SMTP ou un échec d’envoi ne revient pas sur la validation déjà enregistrée. L’email réel externe n’a pas été testé : seule la capture locale Mailpit l’a été.
- Les services protègent le parcours applicatif ; un opérateur SQL privilégié peut toujours contourner certaines règles métier.
- Aucun compte enfant historique n’a été supprimé ; leur état d’accès existant est conservé.

## Suite recommandée

Faire relire les trois parcours par un responsable associatif sur une instance de recette, puis prioriser les éventuels besoins de gestion des relations/contacts et de fin d’adhésion à partir de cet usage. Aucun chantier suivant n’a été commencé.
