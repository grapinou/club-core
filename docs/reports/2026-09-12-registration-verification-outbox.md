# Outbox de vérification email d’inscription — 12 septembre 2026

## Diagnostic initial et périmètre

Le dépôt était propre au début du chantier. Les trois premières commandes ont été,
dans l’ordre, `git status --short`, `git log --oneline -5`, `go test ./...`.
La suite initiale passait. HEAD était `0be055c Add registration email verification` ;
`git ls-remote origin refs/heads/main` a confirmé le même SHA complet sur le distant.
Aucun commit ni push n’a été effectué pendant ce chantier.

Auparavant, `CreateSubmission` persistait la soumission, préparait un challenge,
puis appelait SMTP avant de retourner. Une identité sans candidat évitait ce dernier
appel : DNS, connexion, STARTTLS, relais ou timeout pouvaient donc rendre une
identité éligible temporellement reconnaissable.

Le changement concerne seulement la vérification d’un email connu pour une
registration submission. Aucun formulaire public ni route de soumission n’est
ajouté. Les activations et renvois d’activation User restent synchrones dans leurs
flux existants. Aucun autre service de messagerie, création Membership ou moteur
de matching n’est introduit.

## Architecture et contrat public

```text
CreateSubmission
  transaction PostgreSQL
    submission + candidats historiques
    relecture de l’éligibilité existante
    contrôle de cadence du destinataire
    statut interne + intention outbox éventuelle
  COMMIT
  {"status":"submission accepted"}

indépendamment : worker
  claim durable, COMMIT
  verrou consultatif de session sur le dossier
  transaction de préparation : revalidation + nouveau challenge + compteur
  COMMIT
  dernier contrôle de validité
  rendu en mémoire + SMTP
  transaction de finalisation / retry
  libération du verrou de session
```

Le submitter ne possède plus de dépendance `Mailer`, d’expéditeur ou de base URL.
Il ne prépare aucun challenge et ne réalise aucun I/O SMTP. La seule barrière
synchrone est le commit PostgreSQL. La réponse d’une soumission persistée ne
contient aucun identifiant, match, candidat, email, état d’envoi, User ou Person.
Une erreur transactionnelle annule aussi candidats, statut et intention : elle
reste `ErrUnavailable`, sans erreur SQL sensible exposée par ce service interne.

La garantie est l’absence de dépendance de la réponse à SMTP. Un worker indépendant
peut commencer juste après le commit, avant que le réseau HTTP ait effectivement
livré la réponse au navigateur ; aucune synchronisation avec cette livraison HTTP
n’est nécessaire. Ce chantier ne promet pas des latences PostgreSQL identiques
pour tous les cas de matching.

## Migration et schéma

Nouvelle migration : `migrations/0020_registration_verification_outbox.sql`.
Aucune migration antérieure n’est modifiée.

Table spécialisée `registration_verification_outbox` :

| Colonne | Usage |
| --- | --- |
| `id` | Identité bigint du job |
| `submission_id` | FK vers le dossier, sans copie d’identité |
| `status` | `pending`, `processing`, `sent`, `dead`, `cancelled` |
| `attempt_count` | Tentatives préparées et committées, de 0 à 3 |
| `available_at` | Date d’éligibilité au claim / retry |
| `lease_until` | Date limite du lease, seulement en processing |
| `lease_version` | Génération croissante pour refuser les finalisations obsolètes |
| `created_at`, `updated_at` | Dates opérationnelles |
| `sent_at` | Acceptation SMTP connue, seulement en sent |
| `finished_at` | Fin historique des états terminaux |
| `last_error_code` | Catégorie sûre contrôlée par CHECK |
| `recipient_hash` | SHA-256 sur l’email normalisé, 32 octets |

Index partiel unique : au plus un job `pending` ou `processing` par soumission.
Les jobs terminés ne bloquent pas un éventuel futur job du même dossier au niveau
du schéma ; ce chantier n’expose aucune fonctionnalité de renvoi.
Index partiels supplémentaires pour `available_at` et `lease_until`, et index
sur `(recipient_hash, created_at)` pour le quota.

Les CHECK imposent les correspondances entre état, lease et dates terminales.
Un trigger interdit la suppression des jobs, la modification d’une intention
(id, dossier, hash et création) et toute modification d’un job terminal.
Un trigger sur la fermeture d’une soumission annule atomiquement ses jobs ouverts.

Le Down réussit à vide et refuse dès qu’une ligne d’outbox existe, quel que soit
son statut. Un rollback ne supprime donc ni intention en cours ni preuve historique
d’acceptation SMTP ou d’abandon. Une installation contenant des preuves de la
migration 0019 continue d’appliquer les protections de rollback de cette migration.

## États

| État | Sémantique |
| --- | --- |
| `pending` | Intention disponible à `available_at`, éventuellement après échec |
| `processing` | Job loué ; un crash permet une reprise après `lease_until` |
| `sent` | SMTP a accepté DATA, sans garantie de réception dans la boîte |
| `dead` | Abandon définitif ; dossier ouvert remis en revue humaine |
| `cancelled` | Dossier résolu ou annulé autrement |

Catégories persistées : `smtp_failed`, `ineligible`, `attempts_exhausted`,
`mailer_disabled`, `submission_closed`. Aucun texte d’exception ou de relais SMTP
n’est enregistré dans cette colonne.

## Enqueue et matching

`EmailService.Enqueue` reçoit la transaction de création et réutilise `eligible`.
Les règles restent celles de 0019 : un seul candidat total, `strong`, Person
présente et non archivée, email soumis égal à l’email Person après normalisation,
matching toujours fort à la relecture, aucune nouvelle ambiguïté, même faible.
Le moteur `match` et les preuves candidates existantes ne sont pas modifiés.

Une soumission éligible et sous quota devient `awaiting_email_verification` avec
son intention dans la même transaction. Les autres deviennent
`awaiting_identity_review`, sans mail et sans distinction dans l’acceptation.
Une erreur d’insertion outbox annule entièrement la création.

La création avec email utilise `READ COMMITTED` : après attente du verrou de quota,
la requête suivante doit voir l’enqueue committé par la transaction précédente.
L’éligibilité est relue dans la transaction. Le staging interne sans email conserve
son isolation `REPEATABLE READ` et son usage pour les workflows internes.

## Worker, claim et lease

`internal/outbox.Worker` expose `ProcessOne(ctx) (bool, error)` et `Run(ctx) error`.
PostgreSQL est la seule file ; il n’existe ni channel métier ni scheduler externe.

`ClaimRegistrationVerificationJob`, générée par sqlc, sélectionne une ligne avec
`FOR UPDATE SKIP LOCKED` puis met à jour le lease dans la même instruction
atomique. Un pending arrivé à échéance ou un processing expiré est sélectionnable.
Le claim fixe `lease_until = clock_timestamp() + 60 secondes` et incrémente
`lease_version`. Il ne garde aucune transaction ouverte après l’instruction.

Le worker conserve ensuite une connexion avec un verrou consultatif de session
(namespace 7210, identifiant du dossier), mais aucune transaction pendant SMTP.
Ce verrou est partagé avec les décisions administratives et `VerifyEmail`, qui
utilisent le verrou transactionnel correspondant avant de verrouiller le dossier.
L’ordre de verrouillage métier est dossier, job, puis preuves ; le claim ne prend
que le verrou du job et le relâche immédiatement.

Un ancien worker dont le lease a été repris ne peut finaliser une génération plus
récente. Le verrou de session empêche aussi deux SMTP concurrents si cet ancien
worker est toujours vivant. Si le nouveau claimant ne peut prendre ce verrou, il
laisse le job récupérable à la prochaine expiration, sans compter de tentative.
Un crash libère automatiquement le verrou avec la session PostgreSQL. Une erreur
lors de sa libération ferme la connexion au lieu de la rendre verrouillée au pool.
Une erreur pendant son acquisition ferme également la connexion : une réponse
perdue ne permet pas de savoir si PostgreSQL a effectivement pris le verrou.

La préparation verrouille et relit le dossier et l’outbox, revalide l’éligibilité,
invalide les anciens challenges encore ouverts, puis génère une nouvelle référence
aléatoire et un nouveau code avec les primitives existantes. Le compteur augmente
dans cette transaction, avant SMTP : un crash après préparation consomme donc une
tentative. Les échecs SQL annulant la préparation ne consomment pas de tentative
SMTP ; ils restent récupérables grâce au lease.

Juste avant SMTP, le worker vérifie à nouveau statut, génération, lease, référence
et validité du challenge. Il n’envoie pas si ce contrôle échoue. Le rendu et l’appel
`Mailer.Send` ont lieu après le commit de préparation.

## Retry, épuisement et transport désactivé

Politique centralisée dans `internal/outbox/worker.go` :

- maximum de trois préparations committées ;
- première erreur SMTP : pending, disponible dans une minute ;
- deuxième erreur : pending, disponible dans cinq minutes ;
- troisième erreur, ou reprise après un crash à la troisième tentative : dead,
  invalidation du challenge restant et retour en revue ;
- timeout d’envoi de trente secondes, inférieur au lease de soixante secondes.

Après une erreur SMTP, `InvalidateDeliveryAttempt` invalide exclusivement la
référence associée à cette tentative, en conservant l’historique. L’invalidation
et la transition retry/dead sont atomiques. Une compensation échouant en base laisse
le job processing : il sera repris après expiration, avec remplacement de l’ancien
challenge ou abandon si son budget est épuisé.

`mailer.Disabled` est reconnu avant préparation : dead, zéro tentative SMTP,
`mailer_disabled`, dossier en revue. Un autre mailer retournant `ErrDisabled` est
également terminal, sans retry. Cette situation est normale en développement.

La finalisation après un envoi annulé utilise un contexte de nettoyage indépendant,
limité à dix secondes. Elle peut donc invalider et programmer le retry même lorsque
le contexte du processus vient d’être annulé.

## Crash recovery et garantie de livraison

- Crash après claim, avant préparation : reprise du lease, aucun code à retrouver.
- Crash après préparation, avant SMTP : ancien challenge invalidé et nouveau code.
- Crash après acceptation DATA, avant `sent` : même reprise et nouvel email ; le code
  du premier email peut donc devenir invalide.
- Ancien propriétaire revenant après reclaim : sa génération ne peut plus finaliser
  le job du nouveau propriétaire.
- Épuisement après crash : dead et revue, sans quatrième email.

La garantie est **at-least-once delivery attempt**, avec tentatives bornées,
et **pas exactly-once email**. Elle suppose un worker et PostgreSQL disponibles ;
elle ne garantit ni réception finale ni livraison après épuisement du budget.
Le relais SMTP n’est pas participant à la transaction PostgreSQL.

## Administration, vérification et expiration

Si une résolution administrative gagne avant l’envoi, le trigger annule les jobs
pending/processing dans sa transaction. Le worker ne les expédie plus. Si SMTP
avait déjà commencé, l’administration attend le verrou de session ; l’envoi et
sa finalisation précèdent alors la résolution. On ne prétend pas rappeler un email
déjà accepté. L’attente est normalement bornée par le timeout SMTP et la finalisation.
Les writers administratifs futurs devront respecter ce même verrou avant décision.

`GET /registration/verify` et `POST /registration/verify` restent inchangés.
`VerifyEmail` vérifie le challenge effectivement généré pour le mail, conserve
expiration, protection contre le rejeu, relecture d’identité et audit, et partage
le verrou d’arbitrage avec admin/worker. Les protections HTTP, CSRF, limitation
d’essais et réponses d’échec génériques restent en place.

Les lectures administratives continuent de remettre en revue les challenges
expirés sans preuve utilisable. Elles excluent désormais les intentions ouvertes,
pour ne pas abandonner prématurément un dossier avant traitement ou pendant retry.
Après `sent`, l’expiration remet le dossier en revue et conserve le job `sent`.
Aucun renvoi automatique après expiration n’est ajouté. L’expiration reste déclenchée
par les lectures administratives ou une tentative de vérification, comme en 0019.

## Anti-abus IP

`NewRegistrationSubmissionLimiter` réutilise `AttemptLimiter`, avec une instance
séparée exposée par `Application.SubmissionLimiter` : cinq soumissions par IP sur
quinze minutes, plafond global de soixante par minute. Les limites de login et
verify ne sont pas modifiées. `AllowRequest` prend uniquement `RemoteAddr` :
`X-Forwarded-For` est ignoré. Même borne mémoire et mêmes fenêtres que l’infrastructure
existante. Aucune nouvelle route n’est branchée ; le futur handler devra appeler
ce composant et appliquer les protections habituelles des POST publics.

Ces limiteurs sont locaux au processus et se réinitialisent au redémarrage.
Aucune infrastructure distribuée de sessions ou de compteurs n’est ajoutée.

## Anti-abus destination et familles

Au plus trois intentions par hash dans une heure. Le compteur prend en compte les
intentions créées récemment, les envois acceptés récemment et les jobs encore ouverts,
même anciens. Cette politique volontairement conservatrice compte aussi un échec
ou une annulation récente et empêche de contourner le quota par un backlog.
Les anciens jobs terminés hors fenêtre ne consomment plus de quota.

Un verrou consultatif transactionnel dans un namespace séparé (7211), dérivé des
quatre premiers octets du hash, sérialise la décision entre soumissions concurrentes.
Une collision de clé de verrou ne fait que sérialiser deux adresses ; le comptage
reste comparé sur les trente-deux octets complets. Il ne fusionne aucune identité.

Un dépassement ne refuse pas la création du dossier : revue humaine, aucune nouvelle
intention, acceptation publique identique. Plusieurs enfants avec des noms distincts
et une même adresse parentale peuvent chacun recevoir un job dans le quota.
**Le hash est un contrôle de cadence, jamais une unicité d’identité.** Les retries
d’un job ne sont pas de nouvelles intentions ; les duplications inhérentes à une
acceptation SMTP suivie d’un crash restent possibles dans le budget des tentatives.

## Confidentialité et nettoyage

L’outbox n’enregistre ni email en clair, ni référence publique, ni code, ni corps
d’email. Les codes plaintext existent uniquement dans la mémoire du traitement
qui construit et envoie le mail. La table de preuves continue de stocker seulement
le hash du code. Le hash destinataire réutilise la normalisation/SHA-256 existante ;
il est une donnée pseudonymisée susceptible de recherche par dictionnaire, pas
une garantie d’anonymisation d’une fuite de base.

Les logs du worker contiennent seulement des événements génériques et catégories
sûres. Aucun email, hash destinataire/code, code, référence, nom ou exception SMTP
brute n’est loggé. `Run` masque les erreurs SQL opérationnelles par une catégorie
`database_unavailable`. `ProcessOne` retourne ses erreurs au seul appelant interne.

L’appel SMTP, le rendu et la compensation ont quitté `CreateSubmission`.
`FailDelivery` et `ReviewAfterPreparationFailure`, devenues inutilisées par les
flux réels après refactor, ont été supprimées. La primitive associant invalidation
et tentative vit dans `identityresolution`, appelée dans la transaction du worker.
Les tests de vérification directe utilisent explicitement cette primitive.
Les compensations d’activation User n’ont pas changé.

## Intégration au processus et observabilité

L’application expose `VerificationOutbox` sans démarrer de goroutine dans son
constructeur. `cmd/server` démarre `Run` avec le contexte SIGINT/SIGTERM, annule le
worker et arrête HTTP proprement. Une file vide ou une erreur SQL attend deux
secondes avant un nouveau polling ; une file occupée est vidée séquentiellement.
Le worker respecte le contexte lors des attentes et de l’envoi. Il ne lance pas
une goroutine détachée autour d’un mailer qui ignorerait l’annulation.

Méthode sqlc `CountRegistrationVerificationJobs` pour les compteurs de tous les
états, notamment pending, processing et dead. Diagnostic SQL sans PII :

```sql
SELECT status, count(*)
FROM registration_verification_outbox
GROUP BY status ORDER BY status;

SELECT count(*) AS expired_leases
FROM registration_verification_outbox
WHERE status = 'processing' AND lease_until <= clock_timestamp();

SELECT last_error_code, count(*)
FROM registration_verification_outbox
WHERE status = 'dead'
GROUP BY last_error_code;
```

Aucune UI d’observabilité ni dépendance Prometheus n’est ajoutée. Les dossiers dead
reviennent dans le compteur existant « Vérifications (N) » via leur statut de revue.

## SQL ajouté

- Migration : table, FK/CHECK, index, protection de l’historique, annulation à la
  fermeture et Down prudent.
- Fichier `internal/database/queries/registration_verification_outbox.sql` : claim
  atomique SKIP LOCKED et compteurs, avec code et modèle régénérés par sqlc.
- `identityresolution/outbox.go` : éligibilité réutilisée, verrou de quota, comptage
  temporel, insertion de l’intention, statut transactionnel et verrou de décision.
- `identityresolution/email.go` : invalidation par référence/dossier et exclusion
  des jobs ouverts lors des deux relectures d’expiration.
- `outbox/worker.go` : verrou de session, contrôle de propriété/validité avant envoi,
  incrément des tentatives, transitions sent/retry/dead et invalidation historique.

## Tests

Tests PostgreSQL 16 avec Testcontainers, sans mock de la file durable :

- enqueue éligible/inéligible, absence de challenge avant traitement, statut cohérent,
  contrainte de job actif unique, rollback intégral sur erreur d’insertion/statut ;
- mailer contrôlé par channels : acceptation avant tout appel SMTP, puis deuxième
  soumission éligible pendant que l’envoi du premier dossier reste bloqué ;
- SMTP après commit du challenge et aucune session idle-in-transaction pendant Send ;
- succès, challenge utilisable par VerifyEmail, historique sent préservé après résolution ;
- trois erreurs, backoff, absence de retry prématuré, nouveaux codes/références,
  invalidation des anciens codes, historique conservé et compteur de revue ;
- erreur puis succès, disabled sans challenge ni boucle, inéligibilité après enqueue
  et nouvelle ambiguïté avant retry ;
- claim, durée du lease, absence de reprise d’un lease vivant, reprise après préparation,
  abandon après crash à la dernière tentative ;
- SKIP LOCKED sous verrou SQL explicite et deux workers traitant deux jobs distincts ;
- reprise pendant qu’un ancien worker vit encore : absence de SMTP concurrent et
  impossibilité pour l’ancienne génération de finaliser ;
- résolution admin avant traitement, après claim et concurrence réelle admin/verify
  avec un Send en cours : une seule résolution ;
- shutdown Run pendant SMTP, compensation et libération du verrou ; tests unitaires
  de boucle injectant l’attente pour prouver absence de polling agressif et annulation ;
- quota concurrent entre six membres partageant une adresse, acceptations identiques,
  quota des jobs sent, fin de fenêtre et backlog ancien ;
- schéma et lignes outbox sans matériel de livraison, logs sans PII ni secrets,
  diagnostic par compteurs ;
- Down à vide puis refus avec pending, processing et sent, et suppression refusée ;
- échec SQL pendant préparation : aucun challenge partiel ; reprise puis expiration
  d’un challenge envoyé, avec sent conservé et aucun renvoi ;
- politique IP dédiée, limite/fenêtre par IP, plafond/fenêtre global, isolation du
  budget login et ignorance de X-Forwarded-For ;
- suite existante adaptée à `ProcessOne` explicite : HTTP verify, CSRF, anti-bruteforce,
  expiration, audit, privacy, relecture d’identité et concurrence avec l’administration.

Les scénarios métier appellent principalement `ProcessOne`. Les tests concurrents
utilisent channels et observation des verrous PostgreSQL, avec délais maximaux pour
signaler un blocage. Le test d’expiration attend la date réellement persistée ; aucun
`time.Sleep` arbitraire ne sert à deviner l’ordonnancement.

## Validation finale

Résultats obtenus sur le chantier :

- `sqlc generate` : succès.
- `gofmt` sur tous les fichiers Go concernés : appliqué.
- `go test ./...` : succès pour tous les packages ; dernière exécution du package
  application en 96,447 secondes, tests PostgreSQL/Testcontainers inclus.
- `go test -race ./internal/application ./internal/identityresolution ./internal/outbox` :
  succès ; dernière exécution application en 164,983 secondes, aucune course détectée.
- `git diff --check` : succès, aucune sortie.
- `git status --short` : huit fichiers suivis modifiés et huit nouveaux fichiers
  (le répertoire `internal/outbox/` regroupe deux fichiers dans la sortie courte).
  Toutes ces modifications appartiennent au présent chantier et restent non committées.
- Aucun commit ni push effectué. HEAD reste le commit initial `0be055c`.

État Git final :

```text
 M cmd/server/main.go
 M internal/application/application.go
 M internal/application/registration_email_integration_test.go
 M internal/database/dbsqlc/models.go
 M internal/handlers/auth_security.go
 M internal/handlers/auth_security_test.go
 M internal/identityresolution/email.go
 M internal/identityresolution/service.go
?? docs/reports/2026-09-12-registration-verification-outbox.md
?? internal/application/registration_outbox_integration_test.go
?? internal/database/dbsqlc/registration_verification_outbox.sql.go
?? internal/database/queries/registration_verification_outbox.sql
?? internal/identityresolution/outbox.go
?? internal/outbox/
?? migrations/0020_registration_verification_outbox.sql
```

## Fichiers

Créés :

- `migrations/0020_registration_verification_outbox.sql`
- `internal/database/queries/registration_verification_outbox.sql`
- `internal/database/dbsqlc/registration_verification_outbox.sql.go`
- `internal/identityresolution/outbox.go`
- `internal/outbox/worker.go`
- `internal/outbox/worker_test.go`
- `internal/application/registration_outbox_integration_test.go`
- ce rapport.

Modifiés :

- `cmd/server/main.go`
- `internal/application/application.go`
- `internal/application/registration_email_integration_test.go`
- `internal/database/dbsqlc/models.go`
- `internal/handlers/auth_security.go`
- `internal/handlers/auth_security_test.go`
- `internal/identityresolution/email.go`
- `internal/identityresolution/service.go`

## Limites et décisions ouvertes

- Les quotas (5/IP, 60 globaux, 3/destination) sont des choix V1 à ajuster selon
  l’usage réel, sans changer les réponses publiques.
- Le worker séquentiel garde une connexion PostgreSQL pendant l’envoi pour son
  verrou de session. Cela suppose une session stable, comme le pool pgx actuel ;
  un futur proxy en transaction pooling nécessiterait de revoir cette partie.
- Les décisions admin/verify peuvent attendre un envoi déjà commencé ; la soumission
  publique, elle, n’acquiert pas ce verrou d’envoi.
- Le timeout repose sur un Mailer respectant son contexte. Le SMTP de production
  ferme sa connexion à l’annulation ; un fake ou futur transport ignorant le contexte
  ne peut pas être interrompu de force proprement par Go.
- Les pannes SQL persistantes ne consomment pas artificiellement les tentatives SMTP :
  le traitement reste durablement en attente de récupération de PostgreSQL.
- Un email peut être livré plusieurs fois après crash, et un premier code peut être
  remplacé. La V1 ne promet pas exactly-once.
- L’expiration reste déclenchée par les mécanismes existants, sans cron ni renvoi.
- Les limiteurs IP restent locaux au processus ; le quota destinataire est durable
  et partagé via PostgreSQL.
- Aucun endpoint public de soumission n’est encore exposé. Son intégration devra
  utiliser le limiteur dédié et les protections HTTP existantes.
