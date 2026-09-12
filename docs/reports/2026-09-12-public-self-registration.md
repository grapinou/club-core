# Adhésion publique pour soi-même — 12 septembre 2026

## Diagnostic initial

Les premières commandes ont été, dans l’ordre demandé : `git status --short`,
`git log --oneline -5`, `go test ./...`. Le dépôt était propre et la suite initiale
passait. HEAD était `0026ea3 Add durable registration verification outbox` ;
`git ls-remote origin refs/heads/main` a confirmé le même commit sur le distant.
Aucun commit ni push n’a été effectué pendant ce chantier.

Le dépôt disposait du staging d’identité, de sa revue, des preuves email et de
l’outbox, ainsi que d’un domaine Membership avec création pending, snapshots de
consentements et approbation. Il manquait le formulaire public et le lien durable
entre une identité déclarée et les choix d’adhésion à finaliser après résolution.
Aucune route `/join` existante n’entrait en conflit.

## Architecture

`registration_submissions` conserve l’identité déclarée et les candidats du resolver
existant. `registration_applications` conserve l’intention d’adhésion : saison,
type, activités, décisions sur les consentements présentés et Membership obtenue.
Aucune nouvelle identité ou règle de matching n’est déduite de cette application.

```text
GET /join → identité + choix + consentements présentés
POST /join action=review → récapitulatif serveur, aucune écriture métier
POST /join action=submit
  transaction READ COMMITTED
    validation + clé d’idempotence
    submission + candidats + outbox éventuelle
    application + activités + consentements
    zéro candidat : Person + résolution new_person + Membership pending
  COMMIT → 303 /join/submitted

identité connue / ambiguë
  preuve email via outbox / décision administrative
  transaction de résolution
    identité résolue
    finalizer, avec savepoint de création Membership
      succès → membership_created
      conflit / choix indisponibles → needs_review
  COMMIT
```

Le package `internal/registrationapplications` orchestre ce flux. Il ne réalise
aucun matching ni SMTP : il appelle le submitter d’identité existant et la primitive
transactionnelle commune du domaine Membership. Les handlers parsèrent les champs
HTTP et présentent les résultats ; ils ne rapprochent aucune Person.

## Migration et modèle

Nouvelle migration `0021_public_registration_applications.sql`, sans modification
d’une migration antérieure.

`registration_applications` contient :

- `id`, `submission_id` unique ;
- `request_key` unique, empreinte de nonce de formulaire, sans PII ;
- `season_id`, `membership_type_id` ;
- `status` : `awaiting_identity`, `membership_created`, `needs_review`, `cancelled` ;
- `membership_id` nullable et unique ;
- `created_at`, `updated_at`, `finalized_at` ;
- `last_error_code`, catégorie contrôlée réservée à `needs_review`.

Aucun état intermédiaire `ready` n’est nécessaire : une résolution déclenche la
finalisation dans sa transaction. `cancelled` est réservé au modèle ; aucun parcours
public d’annulation n’est introduit.

`registration_application_activities` conserve `(application_id, activity_id)`,
avec unicité du couple et FK. Un trigger différé impose au moins une activité au
commit, tout en permettant l’insertion atomique du parent puis des enfants.

`registration_application_consents` conserve `(application_id,
consent_definition_id, decision, presented_at)`. Seuls `granted` et `refused` sont
acceptés, avec une réponse unique par définition.

Les choix d’origine sont immuables. Les applications finales et les snapshots ne
peuvent être modifiés/supprimés par les opérations UPDATE/DELETE. Les textes des
définitions sont déjà immuables depuis 0015. Les applications restent conservées
après création Membership. Le Down réussit à vide et refuse de détruire une
application, même déjà finalisée.

## Routes et UX publique

- `GET /join` : formulaire public accessible sans compte ;
- `POST /join`, action `review` : validation et récapitulatif HTML ;
- même POST, action `edit` : retour aux champs conservés ;
- même POST, action `submit` : validation et persistance, puis PRG ;
- `GET /join/submitted` : confirmation générique, sans identifiant ni données saisies ;
- routes `/registration/verify` existantes conservées.

La navigation publique comporte « Adhérer ». Le formulaire utilise Bootstrap et
HTML serveur, sans wizard ni JavaScript obligatoire pour compléter le parcours.
Labels, fieldsets, legends, liens entre champs et erreurs, autocomplétion et types
HTML adaptés sont présents. Les erreurs sont proches des champs ; les valeurs
saisies sont conservées et échappées. Un contrôle visuel Chromium à 390 px a été
réalisé sur le rendu du formulaire. Les fichiers temporaires de prévisualisation
ont été supprimés.

Champs collectés : prénom, nom, date de naissance, email, téléphone et adresse.
Prénom/nom/date/email sont obligatoires. Téléphone et adresse restent facultatifs,
avec les limites déjà appliquées par le domaine (80 et 2 000 caractères).
L’email est validé syntaxiquement, sans lookup DNS ; il n’est pas un identifiant
de connexion. Aucun username, mot de passe, groupe, créneau ou document n’est demandé.

Une seule saison active est affichée directement avec un champ caché revalidé.
Plusieurs saisons actives donnent un select. Le modèle n’a pas de période propre
d’ouverture des inscriptions : V1 utilise `seasons.is_active`, comme Membership,
sans inventer de calendrier supplémentaire. Les types et activités proposés sont
actifs. Le modèle actuel expose leur nom, sans description de type d’adhésion.
Plusieurs activités peuvent être sélectionnées ; au moins une est obligatoire.
Si saison, type ou activité disponible manque entièrement, le GET explique que
les inscriptions ne sont pas disponibles.

## Majorité et contacts

`memberships.Service.IsAdult` réutilise `memberships.IsMinor` et la date civile
`time.Now().In(location)` du domaine Membership. La convention des naissances du
29 février reste celle du domaine (anniversaire au 1er mars les années non bissextiles).
Aucun âge ni booléen de minorité n’est stocké.

Une date manquante, invalide, future ou correspondant à un mineur bloque le parcours
avant toute écriture de Person/submission/application. Le formulaire indique que
le parcours « J’inscris mon enfant » n’est pas encore disponible. La Person finale
retenue par l’administration est aussi vérifiée : si elle ne relève pas du parcours
adulte, l’identité reste résolue et l’application passe en revue.

Les adhésions mineures restent autorisées dans le domaine Membership et ses
workflows existants. Aucune règle globale d’interdiction n’est ajoutée.

Choix A pour le contact d’urgence : aucune collecte ni faux contact textuel.
La Membership adulte conserve l’avertissement non bloquant `adult_missing_emergency`.
Aucun guardian, lien familial ou compte parent n’est créé.

## Consentements réellement présentés

Le GET charge en quatre requêtes les saisons, types, activités et définitions
actifs, sans requête par option. Chaque consentement affiche titre, version,
description complète et deux radios indépendants « J’accepte » / « Je refuse ».
Aucune case « tout accepter », aucune sélection par défaut et aucun refus stylé
comme erreur. Une réponse manque : validation échouée. Une réponse refusée : valide.

Le service signe une présentation contenant les IDs exacts montrés, l’instant de
présentation, un nonce aléatoire et le jeton CSRF du navigateur. La signature HMAC
SHA-256 est vérifiée côté serveur ; le jeton ne contient aucune donnée personnelle.
Validité d’une heure. Une présentation falsifiée, expirée ou copiée dans un autre
contexte CSRF est rejetée. Lors d’un renouvellement forcé de présentation, les
anciennes réponses sont effacées pour ne pas les transférer vers d’autres versions.

Le POST relit les définitions signées en base et exige exactement une décision
`granted`/`refused` pour chacune. IDs supplémentaires, inactifs jamais présentés,
réponses multiples et `withdrawn` sont rejetés. Une définition désactivée après
son affichage reste une présentation authentique : son texte immuable est relu
et accepté dans la durée de validité du formulaire. Une nouvelle version publiée
n’est jamais acceptée implicitement à sa place.

Après persistance, la finalisation lit uniquement le snapshot de l’application,
même si toutes les définitions actives ont changé. Le même ID/version et le même
`presented_at` sont copiés vers `membership_consent_requirements`. Les décisions
sont enregistrées dans `membership_consents`, avec `given_by_person_id` égal à la
Person adulte membre. Aucune décision n’est attribuée à un parent implicite.

La clé de signature est générée par instance applicative. Un redémarrage invalide
les formulaires encore non soumis, mais pas les applications déjà persistées ni
leur finalisation différée. Une future distribution HTTP entre plusieurs instances
nécessitera une clé partagée ou une affinité de session pour ces formulaires.

## Validation et protection HTTP

Les routes utilisent le CSRF commun : CrossOriginProtection, token/cookie, body
limité à 8 192 octets, no-store, no-referrer, nosniff et CSP existante. Les PII
restent dans le corps du POST et le rendu HTML, jamais dans la query string.
Les descriptions et valeurs saisies passent par `html/template`.

La validation métier est également dans le service, pas seulement dans le handler :
limites/encodage/trim partagés avec identityresolution, naissance valide, majorité,
email syntaxiquement valide, disponibilité des choix, activités non dupliquées,
présentation signée et réponses exactes. Les champs scalaires multiples et les IDs
malformés sont rejetés à la frontière HTTP. Les règles Membership sont relues au
moment de la création finale sous les verrous du domaine.

Le POST `/join` utilise exactement `Application.SubmissionLimiter`, avant parsing
et travail métier : 5 tentatives/IP/15 minutes et 60 globalement/minute, uniquement
sur `RemoteAddr`. `X-Forwarded-For` est ignoré. Un dépassement retourne 429 avec
`Retry-After: 900` et un message générique. Les fenêtres et budgets de login/verify
restent séparés et inchangés. Le quota destinataire durable de l’outbox reste actif.

## Création, atomicité et idempotence de soumission

La création complète utilise une transaction READ COMMITTED, nécessaire à la
sérialisation du quota destinataire existant après attente de son verrou.
Submission, preuves candidates, décision email/outbox, application, activités et
consentements sont cohérents au commit.

Une empreinte du nonce signé est une clé d’idempotence unique en base. Un verrou
consultatif transactionnel dédié sérialise deux POST du même formulaire ; le second
retourne l’acceptation de la même application, sans recommencer le resolver.
Une clé déjà persistée reste reconnue même si la disponibilité des choix a changé
entre-temps, tant que la présentation est encore authentique et valide.

### Zéro candidat

Le resolver et son snapshot existants sont réutilisés. Pour une application publique
complète et valide seulement, zéro candidat autorise `ResolveUnmatchedSelf` :
création Person à partir des données déclarées, résolution `new_person` avec
`resolved_by_user_id = NULL`, puis Membership pending et finalisation d’application.

Ces opérations, les activités et les deux couches de consentement font partie de
la transaction de soumission. Une erreur pendant la création Membership ou le
marquage de l’application annule aussi la nouvelle Person, les candidats et toute
la soumission. Aucun nouvel adhérent ne reste orphelin d’une Membership attendue.
Les anciennes soumissions internes d’identité seule ne prennent pas ce chemin.

### Identité existante vérifiable

Un seul strong, email connu identique, Person éligible et quota disponible :
application `awaiting_identity`, soumission `awaiting_email_verification`, outbox
pending. Aucun challenge ni SMTP dans le POST. Le worker existant prépare et
expédie la preuve indépendamment. `VerifyEmail` enchaîne le finalizer avant commit.

### Ambiguïté ou vérification indisponible

Candidat possible/weak, pluralité de candidats, quota épuisé ou inéligibilité email :
la soumission attend la revue et l’application attend son identité. Aucun nouveau
Person/Member n’est créé à ce stade. `LinkPerson` et `CreatePerson` administratifs
appellent le même finalizer dans leur transaction de résolution.
Un job outbox abandonné ou disabled continue de remettre l’identité en revue selon
le mécanisme précédent ; l’intention d’adhésion reste conservée.

## Finalizer et règles Membership partagées

`registrationapplications.Finalize` offre une entrée idempotente. Le hook
`FinalizeSubmission(ctx, tx, submissionID)` est configuré sur les services de
vérification email et de revue ; il ne fait rien pour une soumission sans application.

Le finalizer verrouille l’application, retourne immédiatement si une Membership
est déjà associée, puis lit la résolution immuable. Il charge les activités et le
snapshot de consentements, et utilise
`memberships.CreateRequestWithPresentedConsentsTx`.

`memberships.CreateRequest` conserve son interface existante et utilise la même
primitive interne. Le chemin existant snapshotte toujours les définitions actives ;
le chemin public fournit ses définitions déjà présentées. Les règles communes
restent : Person et naissance présentes, saison/type/activités actifs, activité
obligatoire, unicité Person/saison, décisions et donneur autorisés, état pending.
Aucun second moteur d’adhésion n’est ajouté.

Pour une identité existante ou résolue administrativement, un savepoint couvre la
Membership, ses activités, ses consentements et le marquage final de l’application.
Une erreur annule ce bloc, puis `needs_review` est enregistré dans la transaction
qui conserve l’identité résolue. Le visiteur ne doit pas refaire sa preuve.

Catégories internes :

| Code | Sens |
| --- | --- |
| `membership_already_exists` | L’unicité Person/saison empêche une deuxième Membership |
| `choices_unavailable` | Saison, type, activité ou donnée requise plus disponible |
| `member_not_adult` | Identité finale incompatible avec ce parcours adulte |
| `membership_unavailable` | Échec technique récupérable de création/finalisation |

Les exceptions brutes ne sont jamais affichées. Un retry de finalisation peut
réussir après restauration des choix originaux, sans refaire l’identité. Le doublon
ne rattache pas silencieusement l’application à une adhésion préexistante.

## Concurrence et User

Les décisions email/admin conservent le verrou de dossier déjà partagé avec l’outbox.
L’application a son verrou propre. Les verrous Person de création Membership et de
relecture email utilisent `FOR NO KEY UPDATE`, pour sérialiser les traitements tout
en restant compatibles avec les FK/key-share de résolution. Cela évite une montée
de verrous SHARE → UPDATE entre deux vérifications de la même Person.

La contrainte Membership Person/saison reste la protection finale entre applications
concurrentes. Deux preuves valides peuvent donc toutes deux résoudre leur identité,
avec une Membership créée et une application `needs_review` pour le doublon.
Deux finalizers concurrents d’une application ne créent qu’une Membership.

Aucun User n’est créé, activé, réactivé ou modifié à la soumission/finalisation.
Les états aucun User, actif, désactivé et non activé sont couverts. Le workflow
existant d’approbation Membership conserve la responsabilité de créer/réutiliser
le User, de définir son username et de lancer son activation.

## Réponses publiques et administration

Toutes les acceptations utilisent `303 /join/submitted`. La page est identique pour
nouvelle Person, match email, ambiguïté, quota atteint et états User différents :

> Votre demande a bien été enregistrée.
>
> Selon votre situation, une vérification par email peut être nécessaire.
> Consultez votre messagerie et vos courriers indésirables.
>
> Si aucune action n'est nécessaire, le club traitera directement votre demande.

Avant preuve, aucun nombre de candidats, compte trouvé, email connu ou état de
Membership n’est communiqué. Les erreurs de formulaire ne concernent que les
champs saisis. Les erreurs opérationnelles HTTP restent génériques.

Après preuve email, la page peut indiquer que l’adhésion est enregistrée ou qu’une
vérification complémentaire du club est nécessaire. La réponse `verified` existante
reste utilisée pour les soumissions sans application. Les redirects ne contiennent
que des catégories de résultat, jamais de référence de preuve ou de PII.

La revue administrative affiche saison, type, activités, consentements/version/texte,
état de l’application, lien Membership ou absence de Membership et raison traduite.
Les applications `needs_review` sont comptées dans « Vérifications (N) » et placées
en tête de la liste, même si l’identité est déjà résolue.

Le POST protégé `/registration-reviews/{id}/finalize-application` permet au reviewer
de réessayer les choix originaux après correction de leur disponibilité. Il utilise
les permissions et le CSRF existants. Il ne réécrit jamais l’identité résolue.
L’audit distingue explicitement la nouvelle Person créée automatiquement de la
preuve email ; il ne déduit pas une preuve du seul champ administrateur NULL.

## SQL et fichiers

Nouveau fichier sqlc `registration_applications.sql` : catalogues publics en lots,
relecture des versions présentées, création et verrouillage d’application,
activités/consentements, recherche par clé d’idempotence, transitions, détail
administratif et résultat après preuve email. Le modèle et les requêtes Go sont
régénérés par sqlc. Les requêtes de revue incluent le besoin de revue d’application.
Le SQL métier Membership reste dans sa primitive partagée.

Créés :

- `migrations/0021_public_registration_applications.sql`
- `internal/database/queries/registration_applications.sql`
- `internal/database/dbsqlc/registration_applications.sql.go`
- `internal/registrationapplications/service.go`
- `internal/registrationapplications/presentation_test.go`
- `internal/identityresolution/resolution.go`
- `internal/handlers/join.go`
- `internal/views/join.go`
- `internal/views/templates/pages/join.html`
- `internal/application/public_registration_integration_test.go`
- ce rapport.

Modifiés :

- `internal/application/application.go`
- `internal/database/dbsqlc/models.go`
- `internal/database/queries/registration_submissions.sql`
- `internal/database/dbsqlc/registration_submissions.sql.go`
- `internal/identityresolution/service.go`
- `internal/identityresolution/email.go`
- `internal/identityresolution/matching_test.go`
- `internal/memberships/service.go`
- `internal/handlers/registration_reviews.go`
- `internal/handlers/registration_verification.go`
- `internal/views/registration_reviews.go`
- `internal/views/templates/layouts/base.html`
- `internal/views/templates/pages/registration_review_detail.html`

## Tests et validation

Les tests d’intégration utilisent PostgreSQL 16/Testcontainers et le handler complet :

- GET public, options actives, version/texte complet, labels/fieldset/CSRF et XSS ;
- récapitulatif sans écriture, modification et soumission PRG ;
- validation des champs, limites, date, mineur/futur, email, IDs inconnus/inactifs,
  activités manquantes/dupliquées, consentements manquants/injectés/dupliqués/withdrawn ;
- nouvelle Person, audit automatique, Membership pending, activités, consentements,
  refus valide et warning adulte sans contact d’urgence ;
- rollback complet, y compris Person, si le marquage final d’application échoue ;
- match email → outbox pending → worker explicite → VerifyEmail → Membership ;
- versions présentées conservées après publication d’une nouvelle version, avant
  POST comme pendant l’attente de résolution ; date et donneur corrects ;
- ambiguïté → rattachement ou nouvelle Person administrative → Membership ;
- saison/type/activité désactivés, doublon et erreur SQL de finalisation : identité
  conservée, application needs_review, aucune Membership partielle ; retry admin ;
- page submitted strictement identique pour les différents chemins et états User ;
  Users inchangés avant approbation et absence d’activation prématurée ;
- CSRF, cross-origin, body limit, headers, budget IP/global, instance de limiter
  partagée et ignorance de X-Forwarded-For ;
- deux POST simultanés du même formulaire ; finalisation idempotente ;
- VerifyEmail contre admin ; deux vérifications de la même Person/saison ;
  deux finalizers créant réellement une adhésion après correction métier ;
- Down à vide et refus de destruction d’audit, snapshots immuables ;
- validation du domaine sans handler ; signature, liaison navigateur, durée de vie
  et non-réutilisation des nonces de présentation.

Les suites existantes couvrent toujours les règles mineures du domaine Membership,
les limites civiles d’âge, la concurrence outbox et l’absence de SMTP synchrone.
Les nouveaux tests n’utilisent aucun sleep arbitraire pour ordonner les courses.

Résultats finaux :

- `sqlc generate` : succès.
- `gofmt` sur les fichiers Go concernés : appliqué.
- `go test ./...` : succès pour tous les packages ; package application en
  135,645 secondes sur la dernière exécution, PostgreSQL/Testcontainers inclus.
- `go test -race ./internal/application ./internal/identityresolution ./internal/outbox ./internal/memberships ./internal/registrationapplications` :
  succès, aucune course détectée ; package application en 222,805 secondes.
- `git diff --check` : succès, aucune sortie.
- `git status --short` : 13 fichiers suivis modifiés, 11 nouveaux fichiers ;
  uniquement le présent chantier. Le répertoire registrationapplications regroupe
  deux nouveaux fichiers dans la sortie courte.
- HEAD reste `0026ea3`. Aucun commit ni push effectué.

État Git final :

```text
 M internal/application/application.go
 M internal/database/dbsqlc/models.go
 M internal/database/dbsqlc/registration_submissions.sql.go
 M internal/database/queries/registration_submissions.sql
 M internal/handlers/registration_reviews.go
 M internal/handlers/registration_verification.go
 M internal/identityresolution/email.go
 M internal/identityresolution/matching_test.go
 M internal/identityresolution/service.go
 M internal/memberships/service.go
 M internal/views/registration_reviews.go
 M internal/views/templates/layouts/base.html
 M internal/views/templates/pages/registration_review_detail.html
?? docs/reports/2026-09-12-public-self-registration.md
?? internal/application/public_registration_integration_test.go
?? internal/database/dbsqlc/registration_applications.sql.go
?? internal/database/queries/registration_applications.sql
?? internal/handlers/join.go
?? internal/identityresolution/resolution.go
?? internal/registrationapplications/
?? internal/views/join.go
?? internal/views/templates/pages/join.html
?? migrations/0021_public_registration_applications.sql
```

## Limites et suites possibles

- Deux formulaires distincts peuvent détecter simultanément zéro candidat avant
  qu’une nouvelle Person soit durablement visible : une déduplication universelle
  de ces nouvelles identités n’est pas promise. Chaque soumission utilise néanmoins
  le resolver et la contrainte Person/saison protège toute identité déjà résolue.
  Deux POST du même formulaire sont sérialisés et idempotents.
- Les quotas IP sont locaux au processus. La signature de formulaire est elle aussi
  locale à l’instance ; les applications et le quota destinataire sont durables en DB.
- Une indisponibilité PostgreSQL empêchant même l’écriture `needs_review` peut faire
  échouer toute la transaction de résolution. Aucun système ne peut conserver une
  nouvelle preuve dans une base entièrement indisponible. Les conflits métier et
  erreurs de création récupérables par savepoint, eux, préservent la preuve.
- Le retry admin conserve les choix immuables ; il ne modifie pas une saison/type,
  ne fusionne pas de Person et ne résout pas automatiquement un doublon Membership.
- Le body limité à 8 KiB et la présentation d’une heure conviennent à cette V1 ; un
  catalogue de consentements beaucoup plus volumineux nécessitera de revoir ces bornes.
- L’absence d’ouverture d’inscriptions distincte de `is_active` est une limite du
  modèle actuel, documentée sans ajouter une nouvelle politique de calendrier.
- Le parcours enfant devra résoudre séparément l’identité de l’enfant et celle du
  guardian. Aucun email parental n’est assimilé à une identité d’enfant ici.
- Aucun paiement, document, groupe/créneau, contact secondaire complexe, guardian,
  mot de passe ou création anticipée de compte n’est ajouté.
