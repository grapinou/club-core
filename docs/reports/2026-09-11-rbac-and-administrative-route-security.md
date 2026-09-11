# RBAC et protection des routes administratives

Date : 2026-09-11.

## Diagnostic initial

Le dépôt était propre : git status --short vide. Le commit 831b2f2 « Add account activation and username authentication » est présent sur main et sa présence distante a été confirmée par git ls-remote (831b2f29b2c79cbd7ae0a7c55161ecf81a1e02c6).

Les cinq derniers commits : 831b2f2, 4eb841b, 69c8b06, 6148c4a, 667adb8. Le go test ./... initial réussissait avec résultats en cache.

L’audit initial a constaté :
- authentification username/bcrypt et sessions opaques locales fonctionnelles ;
- identité User déjà placée dans le contexte par le middleware de session ;
- tables roles et user_roles existantes, avec unicité de roles.name et clé primaire composite user_id/role_id ;
- huit routes Persons non protégées ;
- protection CSRF limitée aux handlers d’authentification ;
- opérations applicatives d’approbation et de renvoi sans RBAC ;
- aucune autre route administrative enregistrée.

## Migration et rôles stables

Migration ajoutée : migrations/0017_administrative_roles.sql.

Elle garantit president, secretary, treasurer et coach par INSERT ... ON CONFLICT (name) DO NOTHING dans la table roles existante. Elle n’attribue aucun rôle à un User.

Les rôles et affectations historiques sont conservés. Un nom historique inconnu du mapping ne donne aucun droit. Aucun rôle member, rôle personnalisable, table supplémentaire de rôles ou table dynamique de permissions.

Le Down conserve volontairement les lignes de référence : elles peuvent préexister à 0017 ou avoir été attribuées depuis. Il ne supprime ni rôle ni affectation. La réapplication est idempotente. Les migrations antérieures sont inchangées.

## Modèle de permissions

internal/authorization définit Permission, les constantes de capabilities, le mapping privé centralisé et un service :
- Permissions(ctx, userID) ;
- HasPermission(ctx, userID, permission).

Les IDs utilisent int32 conformément au schéma PostgreSQL/sqlc existant.

| Permission | president | secretary | treasurer | coach |
| --- | --- | --- | --- | --- |
| persons.read | oui | oui | non | non |
| persons.write | oui | oui | non | non |
| memberships.read | oui | oui | non | non |
| memberships.approve | oui | oui | non | non |
| activation.resend | oui | oui | non | non |
| roles.read | oui | non | non | non |
| roles.manage | oui | non | non | non |

Les permissions de plusieurs rôles s’unissent. Aucun rôle ou permission inconnu ne donne de droit. Un User sans rôle a un ensemble vide.

Le service relit PostgreSQL à chaque appel via ListUserRoles ; aucun cache persistant ou de session. Le middleware de navigation réalise sa lecture pour l’affichage, et le middleware de route vérifie indépendamment la permission avant le handler. Les handlers et templates ne comparent aucun nom de rôle.

Requêtes sqlc ajoutées :
- ListUserRoles ;
- ListAvailableRoles ;
- GetRoleByName ;
- AssignUserRole ;
- RevokeUserRole.

AssignUserRole utilise la contrainte composite existante avec ON CONFLICT DO NOTHING. RevokeUserRole est également idempotent en cas d’affectation absente.

## Middleware HTTP et navigation

RequireAuthenticated lit exclusivement auth.UserID depuis le contexte déjà produit par la session :
- anonyme : 303 vers /login ;
- aucune lecture d’un username ou actorID fourni par le navigateur ;
- aucun paramètre next, donc pas de nouvelle redirection ouverte.

Access.RequirePermission(permission, handler) :
1. exige l’authentification ;
2. relit la permission via le service ;
3. retourne 403 avec une page HTML générique si le droit manque ;
4. retourne 500 générique en cas d’erreur de lecture, sans laisser passer ;
5. continue uniquement si autorisé.

Les réponses protégées ne sont pas mises en cache. Une permission est vérifiée au début de chaque opération ; la révocation n’annule pas une opération déjà en cours.

Le middleware Navigation fournit un booléen CanReadPersons à toutes les vues utilisant le layout commun. Le lien Personnes est affiché seulement avec persons.read. En cas d’erreur de lecture, le lien est masqué. Le contrôle de route reste indépendant et souverain.

Ordre applicatif : middleware de session → navigation → route → authentification/permission → CSRF → handler métier.

## CSRF commun

Le mécanisme du chantier précédent est extrait dans internal/websecurity, au lieu d’en écrire un second.

Le composant commun conserve :
- token aléatoire de 256 bits ;
- cookie HttpOnly, SameSite=Strict, Path=/, sans Domain ; Secure et préfixe __Host- en HTTPS ;
- comparaison en temps constant avec le champ caché csrf_token ;
- http.CrossOriginProtection sans exception d’origine ;
- token obligatoire même sans Origin/Sec-Fetch-Site ;
- limite de corps POST à 8 Kio ;
- Cache-Control no-store, Referrer-Policy no-referrer, nosniff et CSP anti-framing/form-action.

Le middleware place le token dans le contexte de la requête. Les vues des formulaires création/modification, ainsi que chaque formulaire d’archivage/restauration des listes, reçoivent ce token. Les formulaires d’authentification utilisent le même mécanisme et les mêmes cookies.

Tous les POST Persons passent par cette protection. Elle s’exécute après le contrôle d’accès, de sorte qu’un anonyme reçoit la redirection login et un User sans droit reçoit 403, avant lecture des données du formulaire.

Le limiteur login/activation existant reste indépendant et inchangé dans sa politique ; aucun quota d’authentification n’est imposé aux formulaires administratifs Persons.

## Séparation authentification, autorisation et Membership

L’authentification et ses critères User actif/activé/hash présent restent inchangés.

Aucune lecture de Membership n’intervient dans le mapping ou les contrôles RBAC :
- une Membership active sans rôle ne donne pas accès aux Persons ;
- un président ou secrétaire peut accéder aux Persons sans Membership personnelle courante ;
- aucun rôle n’est attribué au login, à la création de User ou à l’approbation d’une Membership.

Le retrait d’un rôle ne touche pas les sessions. La même session reste authentifiée, mais la requête protégée suivante relit les rôles et refuse le droit supprimé. Une désactivation de User continue à invalider sa session via le mécanisme existant.

## Approbation et renvoi d’activation

Les contrôles sont ajoutés à internal/accounts, couche d’orchestration applicative, pas au domaine Membership.

- ApproveMembership(ctx, membershipID, approverID, note) exige memberships.approve pour l’approver.
- ResendActivation(ctx, actorID, targetUserID) exige activation.resend pour l’acteur ; son API distingue désormais acteur et destinataire.

L’acteur doit être un User existant, actif, activé et doté d’un hash, puis posséder la permission. Un refus intervient avant préparation de code, mutation ou email.

Un futur handler devra fournir l’actorID depuis auth.UserID(r.Context()), jamais depuis un champ de formulaire. Il n’existe toujours aucune route HTTP pour ces opérations.

Le service internal/memberships ne dépend d’aucun rôle ou permission et reste inchangé. Les transactions métier et l’ordre commit → email sont conservés.

## Bootstrap et administration locale

cmd/clubctl utilise uniquement DATABASE_URL et le code testable internal/clubctl.

Exemples, avec DATABASE_URL déjà fourni dans l’environnement :

~~~bash
go run ./cmd/clubctl grant-role remi.dupont president
go run ./cmd/clubctl list-roles remi.dupont
go run ./cmd/clubctl revoke-role remi.dupont president
~~~

Le binaire compilé s’utilise de la même manière : clubctl grant-role <username> president.

Le programme :
- exige un User existant ;
- n’accepte à l’attribution/révocation que les quatre identifiants de rôles V1 présents en base ;
- attribue sans doublon et révoque proprement même si déjà absent ;
- liste les rôles persistés dans un ordre déterministe ;
- affiche des résultats courts, sans email, mot de passe, hash ou chaîne de connexion ;
- utilise un contexte borné à 30 secondes et retourne un code de sortie non nul en cas d’erreur.

Le CLI est un outil local privilégié par l’accès DATABASE_URL, volontairement indépendant d’un rôle Web préexistant pour permettre le bootstrap. Il n’est pas exposé en HTTP et n’attribue jamais automatiquement le rôle au premier inscrit. Aucun compte, mot de passe spécial ou backdoor n’est créé.

## Audit final exhaustif des routes

Les GET incluent la prise en charge HEAD de net/http avec les mêmes protections.

| Route | Accès | Permission | CSRF |
| --- | --- | --- | --- |
| GET / | public | aucune | sans mutation |
| GET /club | public | aucune | sans mutation |
| GET /contact | public | aucune | sans mutation |
| GET /where | public | aucune | sans mutation |
| GET /when | public | aucune | sans mutation |
| GET /rules | public | aucune | sans mutation |
| GET /static/... | public | aucune | sans mutation |
| GET /login | public | aucune | token fourni |
| POST /login | public | aucune | oui |
| GET /activate | public | aucune | token fourni |
| POST /activate | public | aucune | oui |
| POST /logout | authentifié | aucune | oui |
| GET /persons | authentifié | persons.read | token pour archivage |
| GET /persons/new | authentifié | persons.write | token pour création |
| POST /persons | authentifié | persons.write | oui |
| GET /persons/{id}/edit | authentifié | persons.write | token pour modification |
| POST /persons/{id}/edit | authentifié | persons.write | oui |
| POST /persons/{id}/archive | authentifié | persons.write | oui |
| GET /persons/archived | authentifié | persons.read | token pour restauration |
| POST /persons/{id}/restore | authentifié | persons.write | oui |

Les GET de préparation d’écriture utilisent persons.write. POST /logout exige maintenant explicitement une session ; aucune permission administrative n’est nécessaire. Aucune autre route administrative n’a été identifiée. Aucun écran Memberships ou rôles n’a été ajouté.

## Tests

Tests de mapping :
- les sept permissions du président ;
- les cinq permissions du secrétaire ;
- aucun accès Persons pour trésorier/coach ;
- aucun droit sans rôle ou avec rôle inconnu ;
- union de plusieurs rôles ;
- permission inconnue refusée, absence de cache et propagation d’une erreur PostgreSQL.

PostgreSQL 16/Testcontainers, application réelle :
- les huit routes Persons testées pour anonyme, sans rôle, treasurer, coach, secretary et president ;
- aucune fuite de données personnelles dans les refus ;
- vérification de l’état complet de la table Persons avant/après les POST refusés ;
- aucune mutation si non authentifié, sans permission ou token invalide ;
- mutations création/modification/archivage/restauration réussies avec permission et CSRF ;
- navigation administrative conditionnelle ;
- révocation en base, même cookie de session, accès 403 à la requête suivante, session encore connectée ;
- désactivation de User invalidant toujours la session ;
- Membership active sans rôle et administrateurs sans Membership courante ;
- pages publiques, fichiers statiques et logout anonyme ;
- approbation et renvoi refusés sans droit, sans mutation/email ; réussite avec secretary ;
- attribution/révocation/listage CLI, répétitions idempotentes, username/rôle inconnu et mauvaise syntaxe ;
- migration réappliquée avec rôles historiques et affectations existantes préservés.

Tests CSRF communs et tests d’authentification existants :
- génération du token/cookie, token absent, cookie absent, origine étrangère ;
- POST valide, rejet avant handler ;
- maintien des tests antérieurs des codes, mots de passe, SMTP, sessions et limitation.

Le test routeur historique du formulaire d’édition anonyme attend maintenant 303 et vérifie qu’aucune lecture de Person n’a eu lieu. Les fixtures d’approbation créent explicitement leur rôle président de test.

## Fichiers

Créés :

- cmd/clubctl/main.go
- docs/reports/2026-09-11-rbac-and-administrative-route-security.md
- internal/application/rbac_integration_test.go
- internal/authorization/service.go
- internal/authorization/service_test.go
- internal/clubctl/run.go
- internal/database/dbsqlc/roles.sql.go
- internal/database/queries/roles.sql
- internal/handlers/authorization.go
- internal/router/security_test.go
- internal/views/security.go
- internal/websecurity/csrf.go
- internal/websecurity/csrf_test.go
- migrations/0017_administrative_roles.sql

Modifiés :

- internal/accounts/service.go
- internal/application/application.go
- internal/application/integration_test.go
- internal/handlers/000_page.go
- internal/handlers/003_contact.go
- internal/handlers/011_person_form.go
- internal/handlers/012_persons_list.go
- internal/handlers/013_person_update_form.go
- internal/handlers/017_archived_persons_list.go
- internal/handlers/auth.go
- internal/handlers/auth_security.go
- internal/router/001.5_unknown_route_test.go
- internal/router/001.6_method_not_allowed_test.go
- internal/router/001_router.go
- internal/router/002_helper_route_test.go
- internal/router/013_person_update_form_route_test.go
- internal/views/000_page.go
- internal/views/003_contact.go
- internal/views/011_person_form.go
- internal/views/012_persons_list.go
- internal/views/013_person_update_form.go
- internal/views/auth.go
- internal/views/templates/layouts/base.html
- internal/views/templates/pages/011_person_form.html
- internal/views/templates/pages/012_persons_list.html
- internal/views/templates/pages/013_person_update_form.html
- internal/views/templates/pages/017_archived_persons_list.html


## Validation finale

- sqlc generate : succès.
- gofmt sur les fichiers Go concernés : succès.
- go test ./... : succès ; internal/application exécuté en 17,831 s, internal/database en 25,325 s, internal/handlers en 0,097 s et internal/router en 0,004 s. Autres packages réussis ou sans fichiers de tests.
- git diff --check : succès, aucune sortie.
- git status --short : 27 fichiers suivis modifiés et 14 nouveaux fichiers, tous limités à ce chantier ; aucun fichier indexé.
- Aucun commit ni push.

État Git final :

~~~text
 M internal/accounts/service.go
 M internal/application/application.go
 M internal/application/integration_test.go
 M internal/handlers/000_page.go
 M internal/handlers/003_contact.go
 M internal/handlers/011_person_form.go
 M internal/handlers/012_persons_list.go
 M internal/handlers/013_person_update_form.go
 M internal/handlers/017_archived_persons_list.go
 M internal/handlers/auth.go
 M internal/handlers/auth_security.go
 M internal/router/001.5_unknown_route_test.go
 M internal/router/001.6_method_not_allowed_test.go
 M internal/router/001_router.go
 M internal/router/002_helper_route_test.go
 M internal/router/013_person_update_form_route_test.go
 M internal/views/000_page.go
 M internal/views/003_contact.go
 M internal/views/011_person_form.go
 M internal/views/012_persons_list.go
 M internal/views/013_person_update_form.go
 M internal/views/auth.go
 M internal/views/templates/layouts/base.html
 M internal/views/templates/pages/011_person_form.html
 M internal/views/templates/pages/012_persons_list.html
 M internal/views/templates/pages/013_person_update_form.html
 M internal/views/templates/pages/017_archived_persons_list.html
?? cmd/clubctl/
?? docs/reports/2026-09-11-rbac-and-administrative-route-security.md
?? internal/application/rbac_integration_test.go
?? internal/authorization/
?? internal/clubctl/
?? internal/database/dbsqlc/roles.sql.go
?? internal/database/queries/roles.sql
?? internal/handlers/authorization.go
?? internal/router/security_test.go
?? internal/views/security.go
?? internal/websecurity/
?? migrations/0017_administrative_roles.sql
~~~

## Limites et décisions restantes

- Le mapping V1 est statique. Treasurer et coach n’ont volontairement aucune permission administrative actuelle. Aucun rôle member.
- Les permissions memberships.read, roles.read et roles.manage sont définies, mais n’ouvrent aucun écran non encore créé.
- Le CLI nécessite un User préexistant et un accès PostgreSQL local privilégié. Il permet aussi de retirer le dernier président ; un opérateur disposant de DATABASE_URL peut réattribuer ce rôle explicitement.
- Un rôle historique inconnu est préservé et visible au CLI, mais n’octroie aucun droit et n’est pas attribuable par les commandes V1.
- La migration Down conserve les rôles pour ne pas détruire des données ou affectations antérieures.
- La révocation est effective à la requête suivante ; aucune annulation rétroactive des opérations déjà autorisées.
- Les sessions et limiteurs restent locaux, avec les limites de proxy déjà documentées. Aucun stockage partagé ni mécanisme de proxy de confiance.
- Les futurs handlers d’approbation/renvoi devront prendre l’identité dans le contexte et appeler l’orchestration sécurisée, pas directement le domaine.
- L’administration financière, espaces coach/membre, interface Web des rôles, formulaire public d’adhésion et autres domaines listés hors périmètre restent absents.

Aucun commit ni push effectué.
