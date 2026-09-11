# Vérification d'identité par email connu

Date : 12 septembre 2026.

## Diagnostic et reprise

Le chantier précédent est `2e593be Add registration identity resolution`.
Au démarrage initial, le dépôt était propre, les tests passaient et le commit
`2e593bea87c4dba39a8e261bda0cc7a4db69faa5` était confirmé sur le distant.

À la demande de reprise, les changements locaux ont été conservés intégralement.
Les commandes `git status --short`, `git diff --stat`, `git diff --check`,
`git diff`, `git log --oneline -5`, puis `go test ./...` ont été exécutées avant
nouvelle modification. État trouvé : 12 fichiers suivis modifiés, 9 nouveaux
fichiers, aucun conflit ni défaut d'espacement. La suite globale a réussi
(application : 61,092 s ; database : 25,873 s).

Déjà présents et fonctionnels : migration 0019, challenge, préparation,
vérification transactionnelle, orchestration après soumission, compensation
SMTP, TTL, page publique, expiration synchrone, compteur et audit administratifs.
Neuf tests d'intégration email, un test de template et les contrôles de
configuration existaient ; les neuf tests dédiés passaient (16,478 s).
Le rapport demandé n'existait pas.

Compléments de reprise :

- compensation liée explicitement au couple soumission/référence ;
- retour immédiat en revue si une re-préparation devient inéligible ;
- vérification du nombre de résolutions réellement mises à jour ;
- nouvelle lecture de validité sous verrou avant expiration d'un dossier ;
- signal opérationnel constant si la compensation SQL échoue, sans détails
  SMTP/SQL, référence, code ou coordonnées dans les logs ;
- TTL dans `.env.example`, clarification du point d'entrée public préparé ;
- tests d'atomicité, contraintes SQL, compteur, refus HTTP identiques,
  panne de compensation et absence de secrets dans les logs ;
- rédaction du présent rapport et validations finales.

Aucun reset, suppression du travail antérieur, commit ou push.

## Portée et éligibilité

La preuve concerne uniquement la Person représentée par la soumission. Elle
n'ouvre pas de session, ne crée aucune Membership, ne modifie aucune Person et
ne crée/réactive aucun User. Aucun formulaire public d'inscription n'est ajouté.

`EmailService.eligible` réutilise directement `match` et les normalisations de
0018. Aucun second moteur de matching. Conditions cumulatives :

1. soumission ouverte (`received`, `awaiting_identity_review` ou
   `awaiting_email_verification` pour une nouvelle préparation) ;
2. exactement un candidat snapshoté, de confiance `strong` ;
3. aucun candidat supplémentaire, même faible : choix volontairement plus
   conservateur qu'un simple comptage des seuls `strong` ;
4. Person toujours présente et non archivée ;
5. email déclaré et email Person non vides et identiques après trim/minuscules ;
6. email connu syntaxiquement utilisable comme adresse seule, sans nom d'affichage ;
7. match toujours `strong` en relisant la Person sous verrou ;
8. aucun nouveau candidat détecté depuis le snapshot lors d'une nouvelle lecture
   utilisant le même matching.

Le match `strong` de 0018 reste : prénom/nom normalisés identiques, date de
naissance exacte et email ou téléphone identique. Ici, l'email identique est
une condition supplémentaire obligatoire, même si le téléphone avait rendu le
match fort. Un nouvel email déclaré différent de celui connu entraîne une revue
humaine, sans tentative vers l'ancien email.

Les noms sont normalisés comme en 0018 (espaces/casse/NFC, accents conservés),
l'email par trim/minuscules et le téléphone selon la convention déjà existante.
Un rapprochement ne vaut jamais preuve. La résolution automatique attend un code
valide transmis au canal connu. Toute ambiguïté reste administrative.

## Migration 0019 et états

`migrations/0019_registration_email_verification.sql` ajoute l'état
`awaiting_email_verification` sans modifier une migration antérieure.

| État | Sens | Compteur administratif |
|---|---|---|
| `received` | Déclaration reçue, notamment anciennes soumissions sans candidat | Oui |
| `awaiting_identity_review` | Décision humaine nécessaire | Oui |
| `awaiting_email_verification` | Preuve de possession du canal en cours | Non |
| `resolved` | Identité définitivement résolue | Non |
| `cancelled` | État réservé, pas d'action ajoutée | Non |

La nouvelle orchestration place également les nouvelles soumissions sans
candidat en revue administrative. Le test antérieur attendant `received` a été
adapté à cette évolution explicite du contrat, sans retirer sa vérification
« aucun candidat / aucune création de Person ».

## Challenge et secrets

Table `registration_email_verifications` :

- `id` bigint interne ; `submission_id`, `person_id` ;
- FK composite vers le candidat snapshoté de cette soumission ;
- `public_reference` unique, texte de 64 caractères ;
- `code_hash`, `recipient_hash` : SHA-256, 32 octets ;
- `expires_at`, `used_at`, `invalidated_at`, `created_at`.

La référence est générée à partir de 32 octets `crypto/rand`, encodés en hexadécimal
(256 bits). Le code contient 20 chiffres générés uniformément via `crypto/rand.Int`
(environ 66,4 bits). La référence seule ne permet pas de vérifier l'identité.
Le code n'est placé dans aucune URL et n'est persisté qu'après SHA-256.
`recipient_hash` est l'empreinte de l'email normalisé ; aucune copie de l'email
n'est ajoutée à la table. L'empreinte est une trace de canal, pas un chiffrement
de l'adresse.

Le code en clair circule seulement comme valeur temporaire de livraison, contenu
du mail et entrée de vérification. Aucun écran administratif ni log ne le reçoit.
La comparaison du hash utilise `subtle.ConstantTimeCompare`.

L'index unique partiel `registration_email_one_open` impose au plus un challenge
non utilisé/non invalidé par soumission, y compris s'il vient d'expirer. Toute
nouvelle préparation invalide d'abord les anciens challenges. L'historique reste
présent. Un index partiel par expiration facilite les recherches des preuves
encore ouvertes.

Les triggers interdisent la suppression des preuves, la modification de leur
contenu et tout changement après consommation/invalidation. Les dates et hashes
ne peuvent donc pas être réécrits pour prolonger ou réutiliser un challenge.

### Down

Le Down refuse explicitement si un challenge quelconque existe ou si une
soumission attend un email. Il ne détruit donc jamais une preuve utilisée ni
un historique d'échec. Sur une installation sans ces données, il retire la table
et rétablit l'ancienne contrainte de statuts. Les tests de migrations existants
continuent de couvrir le rollback/rejeu à vide.

## TTL et configuration

`REGISTRATION_VERIFICATION_TTL` est lu depuis l'environnement. Défaut centralisé :
`config.DefaultRegistrationVerificationTTL = time.Hour`. La durée doit être
strictement positive ; zéro, négatif et texte invalide font échouer la configuration.
Le constructeur du service refuse également une durée non positive.

`.env.example` contient `REGISTRATION_VERIFICATION_TTL=1h`. Aucun secret ajouté.
L'application reçoit la durée depuis `Runtime`, puis l'injecte dans le service.
Le calcul et la validation de l'expiration utilisent l'horloge PostgreSQL ; les
attentes de tests s'appuient sur cette même horloge.

## Préparation et orchestration

`Application.Submissions.CreateSubmission` est le point d'entrée préparé pour le
futur formulaire public. Il utilise `NewEmailSubmitter`. Le constructeur historique
`NewSubmitter` conserve la primitive interne de staging, notamment pour les tests ;
le futur HTTP ne doit pas décider lui-même s'il appelle le mailer.

Ordre exact :

1. transaction de création de soumission + snapshot des candidats ; commit ;
2. `PrepareEmailVerification` : verrouillage de la soumission, éligibilité,
   verrou partagé sur la Person, génération, invalidation antérieure,
   insertion du challenge, nouvel état ; commit ;
3. construction du message et `Mailer.Send`, sans transaction PostgreSQL ouverte.

Un échec de préparation laisse ou remet le dossier en revue. Si une soumission
était déjà en attente et que la nouvelle vérification d'éligibilité échoue,
la preuve précédente est invalidée et le dossier revient immédiatement en revue.
Une erreur SQL transactionnelle n'entraîne pas d'insertion partielle de challenge.

Le résultat public réussi reste exactement :

```json
{"status":"submission accepted"}
```

Ni ID, ni référence, ni candidature, ni nombre de candidats, ni information
« email envoyé/existant » ne sont retournés. La référence est communiquée par mail.
Les erreurs de validation/persistance de la déclaration restent les erreurs
publiques expurgées de 0018 ; une fois la déclaration persistée, l'éligibilité
et les résultats SMTP ne modifient pas l'accusé public.

## Email et compensation SMTP

Template versionné `internal/mailer/templates/registration_verification.txt`,
rendu par `mailer.RegistrationMessage`, puis abstraction Mailer existante.
Contenu texte générique, référence, code et URL fixe
`APP_BASE_URL + /registration/verify`. Aucune ancienne coordonnée, information
User/Membership, rôle ou autre candidat. Aucun mot de passe SMTP versionné.
Les tests utilisent le fake existant, sans serveur Internet.

Après échec SMTP : nouvelle transaction, verrou soumission, contrôle du couple
référence/soumission, invalidation du challenge, retour en revue. Une erreur
retardée d'une ancienne livraison ne peut pas invalider son remplacement ni
écraser une décision déjà résolue. Une annulation du contexte HTTP n'empêche pas
la compensation : contexte distinct et borné à dix secondes.

Si la base est elle-même indisponible lors de cette compensation, aucun programme
ne peut garantir une écriture immédiate. Un message opérationnel constant est
journalisé sans détails ni données sensibles. La déclaration reste conservée ;
à expiration du challenge, une lecture administrative la remet en revue après
rétablissement de la base. Il n'y a ni retry automatique ni worker. Un arrêt du
processus entre commit et SMTP est récupéré de la même manière à expiration.

## Vérification transactionnelle

`Application.Verifications.VerifyEmail(reference, code)` :

1. lecture de la soumission associée à la référence ;
2. verrou soumission avant tout verrou sur le challenge ;
3. verrou challenge, contrôle de l'état en attente, non utilisé, non invalidé,
   non expiré ;
4. hash du code comparé en temps constant ;
5. relecture des candidats et de la Person, même éligibilité stricte, Person
   correspondant au challenge et empreinte du canal toujours identique ;
6. `used_at` seulement si l'expiration n'est toujours pas dépassée ;
7. résolution `existing_person`, Person attendue, `resolved_at`, acteur NULL ;
8. contrôle d'une seule ligne résolue puis commit.

Un échec de résolution rollbacke aussi `used_at`. Un code faux ne détruit pas la
preuve encore valable. Un candidat devenu ambigu/inaccessible ou un email modifié
empêche la résolution et renvoie le dossier en revue après présentation du bon code.
Aucun UPDATE de Person/User, aucune création de Membership ou de session.

## Routes, confidentialité et limitation

| Route | Accès | Protection | Résultat |
|---|---|---|---|
| GET `/registration/verify` | Public | En-têtes communs, CSRF fourni, no-store | Référence + code, sans recherche d'identité |
| POST `/registration/verify` | Public | Limiteur partagé, CSRF, contrôle d'origine, corps limité | 303 vers GET avec marqueur fixe |

Le handler est enregistré sur l'AuthHandler existant et utilise son même
`AttemptLimiter` : dix POST par IP sur quinze minutes et 120 POST globaux par
minute, budget partagé avec login/activation/logout. Le contrôle précède le travail
métier et le parsing coûteux. Refus 429 avec `Retry-After: 900`.
`RemoteAddr` est utilisé ; aucun `X-Forwarded-For` n'est interprété.

Le CSRF commun conserve `CrossOriginProtection`, cookies existants, limite de
corps de 8192 octets et en-têtes de sécurité. Toutes les réponses de ces routes
portent `Cache-Control: no-store`.

Échec : redirection fixe `/registration/verify?result=failed`, puis :
« Impossible de vérifier cette demande avec ces informations. »
Même résultat pour référence inconnue, mauvais code, expiration, invalidation,
réutilisation, résolution administrative antérieure ou candidat indisponible.

Succès : `/registration/verify?result=verified`, puis :
« Votre identité a été vérifiée. »
Aucun code ni référence recopié dans les champs après POST, les redirections,
les messages publics ou l'administration. Les paramètres de GET ne déclenchent
aucune recherche ni préremplissage de secret. Les messages PRG sont des messages
d'affichage uniquement, pas une preuve utilisable par un futur parcours.

## Concurrence et expiration

Toutes les opérations mutables commencent par le verrou de la soumission :
préparation, vérification, compensation, résolution administrative, expiration.
Le lookup initial d'une référence ne garde pas de verrou challenge en attendant
la soumission. Les Persons candidates sont verrouillées pour empêcher leur
modification pendant l'éligibilité et la consommation.

Deux préparations se sérialisent et la dernière remplace la première. Deux
vérifications ne consomment qu'une fois. Administration contre email : le premier
commit gagne ; l'autre obtient une erreur de dossier traité ou la réponse publique
générique. L'administration peut traiter un dossier en attente email et invalide
alors ses challenges dans la même transaction.

`ExpirePendingVerifications` est appelé après autorisation dans `CountOpen`,
`List`, `GetDetails`. Il verrouille les dossiers concernés avec `SKIP LOCKED`,
recontrôle l'absence de preuve encore valide sous verrou, invalide les preuves
expirées et remet les soumissions en revue. Une vérification d'un code expiré
fait aussi cette transition. Un dossier occupé sera repris lors d'une lecture
suivante ; aucun cron, polling ou worker n'est nécessaire.

## Administration et audit

Le compteur compte toujours seulement `received` et `awaiting_identity_review`.
Les dossiers en attente email restent listés après ceux nécessitant une décision,
avec le libellé « Vérification email en cours ». Leur détail explique qu'aucune
intervention immédiate n'est requise et qu'une action administrative remplace la
vérification en cours.

La lecture sqlc enrichie expose un booléen `email_verified` fondé sur l'existence
d'un challenge utilisé pour cette soumission et sa Person finale. L'affichage
« Vérification d’un email connu / Intervention administrative : Aucune » exige
ce booléen ET un acteur absent. Il ne repose pas sur NULL seul. La date et la
Person finale restent visibles. Aucun hash, code ou référence n'est rendu.

Les permissions existantes `registrations.review` restent obligatoires : président
et secrétaire autorisés, autres rôles refusés. Aucun droit ne découle d'une Membership.

## SQL et fichiers

SQLC existant enrichi dans `internal/database/queries/registration_submissions.sql` :

- `GetRegistrationSubmission` : existence d'une preuve utilisée ;
- `ResolveRegistrationSubmission` : état email ouvert accepté pour l'administration ;
- `ListRegistrationReviews` : ordre séparant la revue et l'attente email.

`CountOpenRegistrationReviews` reste inchangé : il excluait déjà les autres états.
Modèles et requêtes générés via `sqlc generate`.

Les statements de gestion des challenges sont paramétrés et regroupés dans
`internal/identityresolution/email.go`, selon le pattern transactionnel déjà utilisé
par le service d'activation : verrou Person, insert challenge, invalidation,
transition d'état, lookup référence, consommation, résolution et expiration.
Pas de seconde implémentation des règles de matching en SQL.

Fichiers créés :

- `migrations/0019_registration_email_verification.sql` ;
- `internal/identityresolution/email.go`, `email_test.go` ;
- `internal/application/registration_email_integration_test.go` ;
- `internal/handlers/registration_verification.go` ;
- `internal/mailer/registration.go`, `registration_test.go` ;
- `internal/mailer/templates/registration_verification.txt` ;
- `internal/views/registration_verify.go` ;
- `internal/views/templates/pages/registration_verify.html` ;
- ce rapport.

Fichiers modifiés :

- `.env.example` ;
- `internal/application/application.go`, `integration_test.go`,
  `registration_integration_test.go` ;
- `internal/config/runtime.go`, `runtime_test.go` ;
- `internal/database/queries/registration_submissions.sql` ;
- `internal/database/dbsqlc/models.go`, `registration_submissions.sql.go` ;
- `internal/identityresolution/service.go` ;
- `internal/views/registration_reviews.go` ;
- `internal/views/templates/pages/registration_review_detail.html`,
  `registration_reviews.html`.

## Tests

Treize tests d'intégration email PostgreSQL/Testcontainers sur l'application réelle,
auxquels s'ajoutent le test du template, le test du constructeur TTL et l'extension
des tests de configuration. Les tests antérieurs sont conservés.

Couverture : éligibilité et inéligibilités demandées, homonyme faible supplémentaire,
archive, conflit apparu après préparation, conservation du snapshot, référence/code,
hashes, index unique, invalidation de remplacement, SMTP après commit, compensation
SMTP et contexte annulé, erreur de compensation expurgée et récupération après panne.

Vérification : réussite, référence inconnue, code faux, expiration, invalidation,
réutilisation, Person modifiée/archivée, résolution avec acteur NULL et preuve utilisée,
aucune mutation Person/User/Membership, rollback de consommation après erreur de
résolution et rollback du challenge après erreur de préparation.

Concurrence : double préparation, double vérification, administration contre email,
gagnants exercés explicitement, une seule résolution finale. Compensation retardée
d'une ancienne preuve et tentative de compensation entre deux soumissions refusées.

HTTP : GET/POST, CSRF, PRG, no-store, IP/global/Retry-After, contrôle d'origine,
corps trop grand, aucun secret réaffiché. Sept causes d'échec comparées avec le même
contenu HTML et la même redirection publique. Compteur excluant l'attente email puis
incluant le dossier expiré, audit automatique et RBAC. Down refusé devant une preuve,
preuve utilisée non supprimable. Aucun serveur SMTP Internet dans les tests.

## Résultats finaux

- `sqlc generate` : réussi.
- `gofmt` sur tous les fichiers Go concernés : effectué.
- `go test ./...` : réussi, aucun package en échec ; application 66,190 s,
  identityresolution 0,003 s. Certaines suites inchangées utilisent le cache.
- `go test -race ./internal/application ./internal/identityresolution` : réussi,
  aucune course détectée ; application 125,806 s, identityresolution 1,012 s.
- Tests complémentaires ciblés : atomicité/compensation/éligibilité passés,
  puis comparaison des refus HTTP et panne de compensation passées (3,693 s).
- `git diff --check` : réussi, sortie vide.
- `git status --short --untracked-files=all` : 13 fichiers suivis modifiés,
  11 nouveaux fichiers non suivis, correspondant à l'inventaire ci-dessus.
  Aucun fichier staged, aucun commit ni push.

La migration est appliquée et testée dans PostgreSQL/Testcontainers. Aucune
migration n'a été appliquée à une base de production.

## Limites et décisions ultérieures

- La règle « aucun autre candidat, même faible » est volontairement stricte et
  peut orienter des homonymes légitimes vers l'administration.
- Un email partagé prouve la possession du canal, pas l'identité civile absolue.
  Aucune règle guardian/enfant n'est ajoutée et aucun email guardian n'est copié.
- Les recherches relisent les Persons avec le moteur déterministe O(N) de 0018.
  Index de normalisation/limites de charge à envisager avant exposition du futur
  formulaire à grande échelle. Les conflits sont évalués sur les données visibles
  lors de la transaction, sans verrou global interdisant toute nouvelle Person.
- Réponse de soumission identique mais pas de promesse de temps constant : l'envoi
  SMTP synchrone peut influer sur la latence. Une politique anti-abus et de cadence
  d'envoi reste nécessaire avant création d'un endpoint public de soumission.
- Pas de garantie de livraison exactement une fois entre commit et SMTP. Arrêt
  processus, panne SQL pendant compensation ou commit indéterminé peuvent retarder
  le retour en revue jusqu'à expiration et prochaine lecture administrative.
- Limiteurs/sessions locaux, budget global partagé, architecture mono-instance et
  absence de proxy de confiance conservés. Aucun stockage distribué introduit.
- Les droits/finalités de conservation et une éventuelle correction auditable de
  décision restent à définir. Aucun effacement automatique de preuve et aucun
  rollback destructeur ne sont fournis.
- Aucun formulaire public complet, création automatique de Person, Membership,
  réactivation User, SMS, vérification téléphone, réconciliation de coordonnées,
  parcours guardian, email administratif ou autre domaine hors périmètre.
