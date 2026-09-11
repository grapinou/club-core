# Résolution d'identité des soumissions d'inscription

Date : 11 septembre 2026.

## Diagnostic initial

Dépôt propre au démarrage. `git log --oneline -5` commence par
`13235ff Add membership administration interface`, puis RBAC, activation HTTP,
approbation métier et contacts/consentements. HEAD correspond à `origin/main` et
au SHA distant vérifié par `git ls-remote` :
`13235ff8b54d91e980671c24b25a4993b642a457`.
`go test ./...` initial réussi. Aucun commit ni push effectué.

L'application possède déjà les sessions, permissions, CSRF, navigation, vues
HTML et requêtes sqlc. La création administrative existante d'une Person valide
les noms et nettoie les champs facultatifs avant `CreatePerson` ; aucun service
Person distinct n'existe. Cette même requête et ces règles sont réutilisées dans
la transaction de résolution. Les domaines Membership et User sont conservés.

## Pourquoi un sas

Une déclaration Internet n'est pas une identité durable ni une preuve de
possession d'un compte. `Submitter.CreateSubmission` conserve la déclaration et
les raisons de rapprochement avant toute création ou sélection de Person.
Aucune route publique n'est ajoutée. Même sans candidat, aucune Person n'est
créée automatiquement : la soumission reste à traiter par l'administration.
Aucune Membership n'est créée dans ce chantier.

## Schéma et migration

`0018_registration_identity_resolution.sql` ajoute deux tables, sans modifier
les migrations antérieures.

`registration_submissions` : identifiant, statut, prénom/nom obligatoires,
naissance/email/téléphone/adresse facultatifs, dates de création et mise à jour,
Person finale, type de résolution, date et User responsable de la décision.
Les champs déclarés restent inchangés, y compris espaces et casse.

Statuts :

- `received` : aucun candidat détecté, décision humaine encore nécessaire ;
- `awaiting_identity_review` : au moins un candidat snapshoté ;
- `resolved` : Person finale sélectionnée ou créée ;
- `cancelled` : réservé, aucune action d'annulation fournie.

Types de résolution : `existing_person`, `new_person`. Une contrainte impose
Person/type/date présents pour `resolved`, et tous les champs de résolution
absents pour les autres états. L'acteur est nullable dans le schéma pour une
future résolution par preuve de canal ; tous les services de cette V1 imposent
un acteur actif, activé, autorisé et enregistré.

`registration_submission_candidates` : paire unique soumission/Person,
`confidence`, quatre indicateurs `matched_name`, `matched_birth_date`,
`matched_email`, `matched_phone`, date de détection. Aucune copie des
coordonnées de la Person candidate. Les références empêchent la suppression
accidentelle d'identités nécessaires à l'audit.

Des triggers interdisent la modification des données déclarées, la suppression
des soumissions, la modification/suppression des raisons snapshotées et tout
UPDATE d'une soumission résolue/annulée. Le rollback de migration refuse de
supprimer un historique non vide ; il fonctionne sur une installation vide.

## Normalisation et règles exactes

La normalisation sert seulement à comparer, jamais à modifier les sources.
Le générateur de username est volontairement distinct : ses transformations
ne constituent pas une comparaison d'identité.

- Noms : espaces périphériques supprimés, suites d'espaces Unicode réduites
  à un espace, minuscules, normalisation Unicode NFC. Accents et ponctuation
  restent significatifs. `Rémi` et `RÉMI` correspondent ; `Rémi` et `Remi`
  ne correspondent pas. Aucun rapprochement partiel ou prénom interverti.
- Email : trim puis minuscules sur l'adresse entière, convention pratique
  documentée pour ce projet. Pas de suppression de points ni suffixes `+`.
- Téléphone : chiffres ASCII, retrait des espaces et séparateurs `- . ( )`,
  `+` seulement initial ; `00` international retiré. Un numéro français à
  dix chiffres commençant par zéro devient `33` suivi des neuf autres chiffres.
  Un résultat de moins de 7 ou plus de 15 chiffres, une extension ou un autre
  caractère ne participe pas au rapprochement. Aucun pays n'est inféré hors
  de cette convention française.
- Naissance : même date civile exacte, connue et finie dans les deux sources.
- Des champs vides ne produisent jamais un match email/téléphone.

Les deux noms normalisés (prénom ET nom) doivent correspondre pour être candidat :

| Niveau | Condition supplémentaire |
|---|---|
| `strong` | Naissance exacte ET (email identique OU téléphone identique) |
| `possible` | Naissance exacte OU email identique OU téléphone identique |
| `weak` | Aucun de ces éléments supplémentaires |

Une naissance contradictoire ne bloque donc pas un candidat `possible` dont
les noms et le canal correspondent ; la différence est visible à la revue.
Une adresse familiale partagée avec un prénom différent ne suffit pas.
Les Persons archivées sont incluses : leur identité reste durable.
Les homonymes sont conservés séparément, triés par confiance puis ID.
Un match, y compris `strong`, n'authentifie jamais et ne rattache jamais seul.

## API publique générique et transaction de soumission

`Application.Submissions` expose uniquement `Submitter.CreateSubmission`.
Le résultat sérialisé est toujours exactement :

```json
{"status":"submission accepted"}
```

Il ne contient ni ID de soumission ou de Person, ni nombre de candidats, ni
coordonnées connues, ni niveau de rapprochement. Les erreurs d'entrée sont
`ErrInvalidSubmission` ; toutes les erreurs de persistance sont ramenées à
`ErrUnavailable` sans détail SQL. Aucun log de données personnelles n'est ajouté.

Validation : prénom/nom non vides, UTF-8 valide, absence de NUL ; limites de
200 caractères par nom, 254 pour email, 80 pour téléphone, 2000 pour adresse ;
naissance facultative mais finie. Cela valide une déclaration, pas une preuve
que ses coordonnées sont joignables.

Une transaction `REPEATABLE READ` insère la déclaration, lit les Persons en une
requête, calcule et insère les candidats, fixe l'état et commit. Tout échec
annule déclaration et candidats. Le snapshot reflète la vue transactionnelle,
pas des Persons apparues après ce point. Les IDs ne sont pas utilisés comme
preuve publique et les séquences peuvent naturellement avoir des trous après
rollback.

## Services administratifs, résolution et audit

`Application.Reviews` expose `List`, `CountOpen`, `GetDetails`, `LinkPerson` et
`CreatePerson`. Chaque méthode contrôle `registrations.review` via PostgreSQL,
sans cache long, et vérifie l'état du User opérateur. `GetDetails` assemble
soumission et candidats dans une lecture transactionnelle cohérente.

Rattachement : verrou `FOR UPDATE` sur la soumission, vérification d'état ouvert,
exigence du candidat snapshoté, vérification/verrou de référence Person,
écriture de la résolution puis commit. Aucun UPDATE de Person, User ou Membership.

Nouvelle Person : même verrou de soumission, même vérification d'état,
`CreatePerson` avec les données déclarées (noms trim, champs facultatifs
trim/NULL), puis résolution `new_person` dans la même transaction. Les notes
Person ne sont pas inventées. Un échec de création ou de résolution rollbacke
tout, sans Person orpheline.

Deux administrateurs concurrents attendent le même verrou : une seule décision
réussit ; la seconde reçoit `ErrClosed`. Une résolution n'est pas réouvrable
par l'UI. Soumission, candidats, acteur, date, type et Person finale restent
consultables. Les coordonnées actuelles des candidats sont relues pour la
comparaison ; les indicateurs historiques de match ne sont pas recalculés.
L'audit n'est donc pas une copie historique des anciennes coordonnées Person.

## RBAC, routes et interface

Nouvelle permission centralisée `registrations.review` accordée à `president`
et `secretary`, refusée à `treasurer`, `coach` et aux Users sans rôle.
Aucune nouvelle table de permissions ou notion de rôle.

| Route | Permission | CSRF | Résultat |
|---|---|---|---|
| GET `/registration-reviews` | registrations.review | Fournit le token commun | Liste |
| GET `/registration-reviews/{id}` | registrations.review | Fournit le token commun | Détail |
| POST `/registration-reviews/{id}/link-person` | registrations.review | Obligatoire | Rattachement puis PRG |
| POST `/registration-reviews/{id}/create-person` | registrations.review | Obligatoire | Création puis PRG |

Anonyme : 303 `/login`. Sans permission : 403 générique sans données du dossier.
ID absent/invalide ou Person hors snapshot : 404. Erreur interne : 500 générique.
Les POST réussis redirigent en 303 vers le détail avec un marqueur fixe
`notice=resolved` ; une seconde décision utilise `notice=closed`. Aucune PII
n'est mise dans les query strings. L'actor vient exclusivement de
`auth.UserID(r.Context())` ; aucun champ POST n'est utilisé comme acteur.

La navigation affiche `Vérifications (N)` uniquement aux opérateurs autorisés.
N compte les deux états ouverts, car même l'absence de candidat exige une
intervention dans cette V1. Un comptage par rendu, indépendant du nombre de
lignes ; en cas d'erreur de comptage le compteur est indicatif (zéro), jamais
une autorisation. Les handlers contrôlent leurs propres erreurs.

Liste : dates, noms déclarés, naissance, statut, meilleur niveau et nombre de
candidats. Pas d'email/téléphone/adresse dans les lignes. Les états à vérifier
précèdent les reçues sans candidat, puis l'historique ; les dossiers ouverts
sont triés du plus ancien au plus récent.

Détail : données déclarées, tableau déclaré/existant par candidat, différences
signalées, raisons et date du rapprochement, confiance, archivage et état du
User (absent/actif/désactivé/non activé). Aucun hash ou secret sélectionné par la
requête des candidats. Boutons uniquement sur un dossier ouvert. Résolution
historique avec Person finale, acteur et date, sans action de modification.
Les écarts affichés sont des différences de valeur, y compris de présentation
(casse/espaces), distinctes des règles normalisées de rapprochement.

View models `RegistrationListView`, `RegistrationDetailView`,
`RegistrationCandidateView`, `RegistrationFieldView` : présentation préparée
côté Go ; les templates n'effectuent ni matching, ni autorisation métier.
Style Bootstrap existant conservé, sans framework ajouté.

## Sécurité

Sessions et middleware existants, vérification RBAC sur toutes les routes et
use cases, CSRF commun, limite de corps et en-têtes existants, PRG et
`Cache-Control: no-store`. Échappement `html/template` des valeurs déclarées.
Aucun email, token, secret factice, réactivation User ou changement de Membership.
Les pages publiques existantes restent publiques. Les droits ne dépendent pas
d'une Membership. La révocation de rôle prend effet à la requête suivante.

## SQL et fichiers

Requêtes sqlc nouvelles dans `internal/database/queries/registration_submissions.sql` :
`CreateRegistrationSubmission`, `ListIdentityMatchingPersons`,
`CreateRegistrationCandidate`, `MarkRegistrationForReview`,
`GetRegistrationSubmission`, `LockRegistrationSubmission`,
`GetRegistrationCandidate`, `LockRegistrationPerson`,
`ResolveRegistrationSubmission`, `ListRegistrationReviews`,
`CountOpenRegistrationReviews`, `ListRegistrationCandidates`.
Liste agrégée en une requête ; aucun N+1 par ligne. Détail en deux lectures.

Créés :

- `migrations/0018_registration_identity_resolution.sql` ;
- `internal/database/queries/registration_submissions.sql` ;
- `internal/database/dbsqlc/registration_submissions.sql.go` (généré) ;
- `internal/identityresolution/service.go`, `matching_test.go` ;
- `internal/handlers/registration_reviews.go` ;
- `internal/views/registration_reviews.go` ;
- `internal/views/templates/pages/registration_reviews.html` ;
- `internal/views/templates/pages/registration_review_detail.html` ;
- `internal/application/registration_integration_test.go` ;
- ce rapport.

Modifiés : `internal/application/application.go`,
`internal/authorization/service.go`, `service_test.go`,
`internal/database/dbsqlc/models.go` (généré),
`internal/handlers/authorization.go`, `internal/views/security.go`,
`internal/views/templates/layouts/base.html`.

## Tests

Trois tests unitaires de matching/normalisation/validation, extension du test
de mapping RBAC, neuf tests PostgreSQL 16/Testcontainers sur l'application réelle :

- zéro/un/plusieurs candidats, homonymes et archive, classement et snapshot ;
- conservation des données et aucune Person créée par soumission ;
- même résultat JSON public dans tous les cas, structure sans champ sensible,
  erreur SQL publique expurgée ;
- rollback de création de soumission après insertions partielles de candidats ;
- rattachement candidat, refus d'une autre Person existante, audit complet ;
- aucune modification de Person, conservation des différences ;
- résolution nouvelle explicite, aucune Membership/User/email ;
- échec de création Person et échec ultérieur de résolution : rollback complet ;
- concurrence création/création et création/rattachement entre deux opérateurs ;
- deux POST HTTP concurrents avec deux sessions et CSRF : une seule Person ;
- matrice anonyme/sans rôle/trésorier/coach/secrétaire/président ;
- GET, navigation, compteur, comparaison, états de compte, XSS, no-store ;
- POST refusés sans CSRF valide ou sans rôle : aucune mutation ;
- acteur de session malgré champs falsifiés, PRG et double POST refusé ;
- données absentes des 403 et erreurs internes 500 génériques ;
- résolution finale et preuve immuables, contrainte de complétude d'audit,
  candidat unique, soumission annulée non résoluble ;
- révocation immédiate et refus d'un opérateur désactivé.

## Validation finale

- `sqlc generate` : réussi, fichiers générés à jour.
- `gofmt` sur tous les fichiers Go concernés : effectué.
- `go test ./...` : réussi, aucun package en échec ; application 43,881 s,
  identityresolution 0,003 s (les suites inchangées peuvent utiliser le cache).
- `go test ./internal/application -run TestRegistration -count=1` : réussi,
  neuf tests d'intégration, 14,079 s.
- `go test -race ./internal/application -run
  'TestRegistrationConcurrentHTTPCreate|TestRegistrationResolutionRollbackAndConcurrency'
  -count=1` : réussi, 7,701 s, aucune course signalée.
- `git diff --check` : réussi, sortie vide.
- `git status --short --untracked-files=all` : 7 fichiers suivis modifiés et
  11 nouveaux fichiers non suivis, correspondant à la liste ci-dessus.
  Aucun fichier staged, aucun commit et aucun push.

Les migrations ont été appliquées dans les bases PostgreSQL éphémères des tests ;
aucune migration n'a été appliquée à une base de production.

## Limites et prochain chantier

- Détection conservatrice : pas de fuzzy matching, noms sans accent différent,
  erreurs orthographiques ou noms modifiés peuvent manquer un candidat.
  Le classement guide une décision humaine, il ne certifie jamais l'identité.
- Scan des Persons en une requête, traitement Go O(N), liste sans pagination :
  adaptés à cette V1 sans endpoint public. Prévoir index de normalisation et
  limites de charge avant exposition ou augmentation importante du volume.
- Deux soumissions distinctes peuvent encore conduire deux opérateurs à créer
  deux Persons ; le verrou protège une même soumission, pas une unicité humaine
  universelle. Une revue transversale/re-détection pourra compléter ce parcours.
- L'accusé public est identique mais le service n'offre pas de garantie de temps
  constant. Avant route publique : anti-abus, maîtrise de la charge, modèle de
  suivi opaque et preuves de possession de canal à définir.
- La conservation est volontairement stricte dans cette V1. Politique de durée,
  purge/anonymisation et correction auditable d'une décision erronée restent à
  définir ; aucun UPDATE libre ni fusion destructrice.
- L'acteur nullable dans le schéma permet un futur type de résolution automatique,
  mais une évolution devra enregistrer explicitement la preuve et le canal
  déjà connu. Aucun code, email de reprise, SMS ou endpoint de confirmation ici.
- Une soumission représente la personne destinée à devenir membre. Le guardian
  sera résolu séparément et relié par `person_guardians` dans un autre chantier.
  Aucune adresse parentale n'est recopiée sur un enfant.
- Pas de formulaire public, création de Membership, réactivation User,
  réconciliation automatique des coordonnées, ni extension des autres domaines.
- Contraintes de déploiement existantes (sessions locales, reverse proxy) inchangées.
