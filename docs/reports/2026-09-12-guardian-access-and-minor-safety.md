# Accès numérique guardian et sécurité des mineurs — 12 septembre 2026

## Diagnostic initial

Dépôt propre avant modification. HEAD et origin/main pointaient sur
`d4d738b Add public self registration` (vérification distante par `git ls-remote`).
Les trois commandes initiales `git status --short`, `git log --oneline -5`,
`go test ./...` ont réussi. Aucun commit ni push effectué.

La contrainte User unique par Person et le login indépendant d'une adhésion
existaient déjà. La primitive de création User était cependant incorporée à
l'approbation Membership. Les relations person_guardians ne représentaient
aucune autorisation numérique explicite.

## Diagnostic de reprise

La reprise a trouvé six fichiers suivis modifiés et quatorze fichiers non suivis,
dont cette migration et ce rapport. HEAD reste d4d738b. Tous les changements locaux
ont été conservés, sans reset, restore, checkout destructeur, commit ni push.

Avant toute modification, les commandes imposées ont été exécutées :
git status --short, git diff --stat, git diff --check, git diff,
git log --oneline -5 et go test ./.... Cette suite de reprise a réussi
(application : 146,473 s), sans erreur actuelle ; diff --check était propre.

Déjà terminés : migration 0022, SQL/sqlc, primitives accounts/provisioning,
activation guardian, guardianaccess, calcul civil partagé, minorsafety et câblage
Application. Six tests PostgreSQL d'intégration et une table de tests unitaires
étaient présents. Les tests ciblés, dont le pool limité à une connexion, passaient.
Une suite race précédente passait également, mais ne couvrait pas encore toutes
les dernières modifications : elle est relancée sur l'état final.

Partiellement terminé : inventaire de validation dans le rapport. Manquaient
l'exemple explicite à 14 ans et une assertion d'accès après suppression de la
relation en maintenant l'enfant mineur. Ces deux compléments ont été ajoutés
sans réimplémenter les services ni recréer la migration.

Non commencé et volontairement différé : UI guardian et parcours enfant.
Aucun élément backend requis ne reste à construire.

## Architecture

Person ≠ User ≠ Membership :

- Person porte l'identité.
- User porte l'accès authentifié, éventuellement sans aucune Membership.
- Membership porte l'adhésion associative.
- person_guardians porte une relation familiale/administrative.
- guardian_access_grants porte l'autorisation numérique relative à un enfant.

Aucun rôle RBAC guardian, aucune permission globale supplémentaire. Les services
administratifs exigent persons.write et un User actif, activé avec mot de passe.
Les nouveaux points d'entrée prennent leur acteur depuis auth.UserID(ctx),
jamais depuis un paramètre du navigateur.

## Migration 0022 et audit

`0022_guardian_digital_access.sql` ajoute guardian_access_grants :

| Colonne | Usage |
| --- | --- |
| id | identifiant historique bigint |
| child_person_id, guardian_person_id | Persons concernées, distinctes |
| granted_at, granted_by_user_id | date et acteur d'origine |
| revoked_at, revoked_by_user_id | révocation définitive de cette occurrence |
| created_at | date de création |

Les acteurs sont des FK users, nullables pour un futur workflow public vérifié.
Les services actuels renseignent toujours l'administrateur authentifié. Un grant
actif par couple est garanti par index unique partiel. Plusieurs guardians d'un
enfant restent possibles, indépendamment de is_primary_contact.

Un trigger exige et verrouille la relation à l'insertion. Les grants référencent
les Persons, pas la ligne de relation supprimable. Un autre trigger empêche la
suppression ou le changement du couple d'une relation avec grant actif.
RemoveRelationship révoque explicitement puis supprime la relation dans une même
transaction. Les anciennes opérations de suppression directe doivent donc être
précédées d'une révocation lorsqu'un grant actif existe.

L'historique interdit DELETE, TRUNCATE et les réécritures. Seule la première
révocation peut modifier une ligne, sans changer son origine. Re-grant crée une
nouvelle ligne. Grant répété sur un couple déjà autorisé retourne le même grant,
sans réécrire son acteur. Revoke répété est idempotent. Les références historiques
aux Persons et Users sont conservées ; une suppression physique de ces identités
reste soumise aux FK.

Down fonctionne à vide et refuse toute donnée, grant actif comme révoqué.
Aucune migration antérieure modifiée.

## User indépendant de Membership

accounts/provisioning.EnsureUserForPersonTx est la primitive transactionnelle
commune. Elle verrouille Person, retrouve son User ou utilise le générateur
historique de username, avec suffixes numériques en cas de collision.
La contrainte unique users.person_id reste la protection DB finale.
Les requêtes de verrouillage et création sont générées par sqlc.

accounts.EnsureUserForPerson fournit le point d'entrée administratif. Il ne crée
ni adhésion ni code d'activation et réutilise un User existant sans le réactiver.
Membership approval appelle la même primitive, puis conserve sa réactivation
explicite et sa préparation d'activation antérieures. Les wrappers
memberships.UsernameBase et memberships.IsMinor conservent leur API.

La dépendance vers le sous-package provisioning évite un cycle accounts ↔
memberships et n'introduit pas un second moteur de création User.

## EnsureGuardianUser et activation

L'opération exige un administrateur authentifié, la relation réelle, le grant
actif, les deux Persons non archivées et une date connue indiquant un enfant
mineur. Elle verrouille les Persons dans l'ordre des IDs, puis la relation.
User et code sont préparés dans la même transaction, SMTP après commit.
Les lectures d'authentification et RBAC de cette préparation utilisent aussi
cette transaction, afin de fonctionner avec un pool limité à une connexion.

Aucune Membership, aucun User enfant, aucune attribution de rôle ne sont créés.
Un User désactivé n'est pas réactivé par cette opération. Un User activé est
réutilisé sans nouvel envoi. Un User non activé reçoit une nouvelle préparation,
comme le mécanisme de renvoi existant.

PreparePersonOnlyTx partage intégralement le cycle cryptographique existant et
supprime seulement le fallback de destinataire pour ce cas. Seul l'email propre
du guardian, syntaxiquement utilisable, peut servir. Aucun email enfant ou autre
relation ne peut être emprunté. Sans canal, le User reste non activé et le résultat
interne est no_email_channel. Le code demeure uniquement en mémoire ; son hash
est stocké dans user_activation_codes.

Le mailer et les résultats sent/send_failed/not_required existants sont
réutilisés. Un échec SMTP conserve le User et la préparation committés, comme le
workflow d'activation existant. Pas de migration opportuniste vers l'outbox de
registration. L'opération peut attendre SMTP ; elle n'est pas le POST public /join.

## GuardianAccess et majorité

CanManageChild et ListManagedChildren utilisent l'acteur authentifié et relisent
User → Person → relation → grant. User doit être actif et activé, avec mot de
passe ; guardian et enfant doivent être non archivés. Aucun cache de droits.

Le calcul civil commun vit dans internal/civildate et reste exactement celui de
Membership : dix-huitième anniversaire, report au 1er mars pour une naissance le
29 février lorsque l'année de majorité n'est pas bissextile. La date courante est
convertie dans le fuseau métier configuré. Une horloge injectable permet de tester
les frontières sans attente.

À 18 ans, les droits cessent dès l'appel suivant, sans cron ni suppression des
relations/grants. Birth_date absente ou infinie refuse les droits.
**15 ans n'est pas une majorité numérique générale** et n'intervient dans aucune
décision. Aucun âge ou indicateur is_minor/is_child n'est persisté.

ListActiveGuardiansForChild est accessible à l'enfant authentifié lui-même ou à
un administrateur persons.write. Le résultat minimal contient Person ID,
prénom/nom, relation et date de grant, sans données User ni secrets.
Il liste les accès effectivement utilisables : aucun guardian désactivé,
non activé, archivé, révoqué, ni droits sur un enfant majeur/archivé.

## Politique minorsafety

**Choix de sécurité Club Core, pas obligation légale.**

EvaluatePrivateConversation reçoit les Users participants, exige que l'acteur
authentifié en fasse partie, puis charge identités et grants dans un snapshot
PostgreSQL cohérent, en deux requêtes groupées. Aucun âge, indicateur guardian,
acteur ou booléen d'autorisation soumis par le frontend ne fait autorité.

Les résultats typés sont allowed, denied, supervision_required et
not_applicable ; ils portent RuleClubCoreSafety et un indicateur Supervised.
RuleLegal et RuleOfficialRecommendation sont seulement des catégories disponibles,
sans prétention juridique ni base de règles légales.

| Participants | Décision de cette policy |
| --- | --- |
| adulte et adulte | not_applicable |
| mineur et mineur | not_applicable |
| enfant et son guardian adulte autorisé | allowed |
| adulte non-guardian et mineur seuls, dans les deux sens | supervision_required |
| mineur + adulte non-guardian + son guardian adulte autorisé | allowed, Supervised |
| plusieurs adultes quelconques + mineur sans son guardian | supervision_required |
| enfant devenu majeur | règle mineur non applicable |
| naissance inconnue, User indisponible, acteur absent, IDs dupliqués | denied |

Dans un canal mixte avec plusieurs enfants, chacun doit avoir son propre
guardian adulte autorisé parmi les participants. L'ordre et l'initiateur ne
changent pas le résultat. Une simple relation sans grant ne suffit pas.

Allowed n'autorise pas l'accès à toutes les données d'un enfant et
not_applicable ne dispense pas de la future autorisation métier. Les futurs
domaines devront appeler cette policy à chaque action sensible et modification
des participants ; ils devront intégrer cette vérification à leur propre
frontière transactionnelle. Aucun résultat ne doit être conservé comme un droit
permanent. Aucun canal, message ou dispositif de surveillance n'est créé.

## Administration et transparence

Services câblés dans Application.GuardianAccess, Application.MinorSafety et
Application.Accounts. Pas de nouvelle route ni UI guardian dans ce chantier.
La future UI utilisera CSRF, la session, PRG et persons.write. L'acteur guardian
restera toujours lui-même sur une ressource enfant : aucune impersonation.

Les grants restent consultables pour audit, par exemple :

```sql
SELECT id, child_person_id, guardian_person_id,
       granted_at, granted_by_user_id, revoked_at, revoked_by_user_id
FROM guardian_access_grants
WHERE child_person_id = $1
ORDER BY id;
```

Aucune modification de /join, des consentements, de l'outbox, de VerifyEmail ou
des rôles administratifs. Aucun email/hash/token n'est journalisé par les nouveaux
services.

## SQL et fichiers

Nouvelles requêtes sqlc :
LockAccountPerson, CreateUserForPersonUsername, LockGuardianRelation,
CreateGuardianAccessGrant, GetActiveGuardianAccess, RevokeGuardianAccessGrant,
GetGuardianAccessFacts, ListActiveGuardiansForChild,
ListManagedChildrenForGuardian, ListInteractionParticipants,
ListInteractionGuardianEdges.

Fichiers créés :

- migrations/0022_guardian_digital_access.sql
- internal/accounts/guardian.go
- internal/accounts/provisioning/{service,username}.go
- internal/civildate/minority.go
- internal/guardianaccess/service.go
- internal/minorsafety/{service,service_test}.go
- internal/database/queries/{account_provisioning,guardian_access}.sql
- internal/database/dbsqlc/{account_provisioning,guardian_access}.sql.go
- internal/application/guardian_access_integration_test.go
- ce rapport.

Fichiers modifiés :

- internal/accounts/service.go
- internal/activation/service.go
- internal/application/application.go
- internal/database/dbsqlc/models.go
- internal/memberships/{service,username}.go

## Tests

Tests PostgreSQL/Testcontainers ajoutés dans application :

- grants, absence de relation, self, unicité, revoke/re-grant, acteurs et dates ;
- interdiction DELETE/TRUNCATE/réécriture et suppression de relation active ;
- droits par ressource, anonymat, absence de grant, plusieurs guardians ;
- archivage des deux Persons, User désactivé, naissance inconnue ;
- veille/jour des 18 ans, convention du 29 février, historique à majorité ;
- transparence enfant et refus de listing non autorisé ;
- User sans Membership, username/collision/réutilisation et non-réactivation ;
- activation réelle guardian puis login sans Membership ;
- SMTP après commit, absence de canal propre, email invalide, aucun fallback ;
- absence de Membership ou User enfant créés ;
- deux Grant, Grant/Revoke et deux EnsureUser simultanés, sans Sleep ;
- provisionnement avec pool limité à une connexion, sans réservation imbriquée ;
- policy avec faits DB et session, révocation, faux guardian et supervision ;
- Down à vide, refus avec grant actif et historique.

Tests unitaires minorsafety : symétrie, 14/15/17/18 ans, guardian, faux guardian,
supervision de chaque mineur, adultes/adultes, mineurs/mineurs, naissance inconnue.
Les tests Membership de calcul civil et username historiques restent présents.

Validation finale effectuée sur l'état de code final :

| Commande | Résultat |
| --- | --- |
| sqlc generate | succès |
| gofmt sur les fichiers Go concernés | effectué |
| go test ./... | succès, application 146,505 s |
| go test -race ./internal/application ./internal/accounts ./internal/guardianaccess ./internal/minorsafety ./internal/memberships | succès, application 249,249 s, aucune course détectée |
| git diff --check | succès, aucune sortie |
| contrôle whitespace des 14 fichiers non suivis | succès |
| git status --short | 6 fichiers suivis modifiés, 14 nouveaux fichiers non suivis |

Les packages accounts et guardianaccess n'ont pas de tests locaux : leurs services
sont exercés avec PostgreSQL par les tests d'application, y compris sous race.
Six tests d'intégration guardian et quatorze cas unitaires minorsafety sont
présents, en complément des tests historiques.

La suite générale valide aussi les non-régressions d'approbation Membership,
activation, login, RBAC, inscription publique, revue administrative, VerifyEmail
et outbox. Aucun commit, push ou changement destructeur du travail local.
L'arbre reste volontairement modifié pour revue.

## Limites et suite

Aucun parcours enfant, arbitrage entre guardians, écran de gestion, compte enfant
automatique, système de capabilities persistées ou messagerie réelle.
Les utilisateurs sans date de naissance ne participent pas à une interaction
évaluée par cette policy avant correction de leurs données.

Un grant peut exister avant activation du User ; il ne produit alors aucun droit
effectif. Le service numérique ne redéfinit pas la validité juridique de la
relation administrative. La supervision multi-participants choisie ici exige un
guardian autorisé pour chaque mineur ; une future politique de canal officiel
devra être décidée explicitement si elle doit accepter une autre supervision.

La protection d'audit vise les opérations DML applicatives, pas un propriétaire DB
capable de supprimer les triggers ou les tables. Comme les autres permissions,
une décision représente les faits observés à l'appel ; les futurs domaines doivent
appliquer leur contrôle au moment de l'action, sans cache durable.
