# Parcours public « J’inscris mon enfant » — 12 septembre 2026

## Diagnostic initial

Les trois premières commandes ont été `git status --short`, `git log --oneline -5` et `go test ./...`. Le dépôt était propre. HEAD et la référence locale origin/main pointaient sur `91c8c17 Add guardian access and minor safety`. La suite initiale passait. Aucun commit, push ou changement de migration antérieure n’a été effectué. La lecture distante `git ls-remote origin refs/heads/main` confirme également `91c8c175e707b1acb681f7f2955bf3310692188d` sur main : le chantier précédent est bien poussé.

## Architecture et séparation des identités

Le parcours réutilise le service `registrationapplications`, le matcher `identityresolution`, la présentation signée existante, la création Membership et les domaines consent, guardianaccess et accounts. `registration_submissions` contient uniquement l’identité de l’enfant. `guardian_identity_claims` contient uniquement la déclaration du guardian. Aucune adresse, aucun email ni téléphone guardian ne sont copiés dans la Person enfant. L’email enfant est facultatif ; celui du guardian est obligatoire. Aucune preuve email guardian ne résout l’identité enfant.

La migration `0023_public_child_registration.sql` ajoute trois tables :

- `guardian_identity_claims` : staging reçu, à vérifier, résolu ou annulé ; déclaration, Person finale, type de résolution, dates et actor administratif ;
- `guardian_identity_claim_candidates` : candidats et raisons déterministes figées, sans copie des anciennes coordonnées Person ;
- `child_registration_applications` : extension 1:1 de l’application, claim unique, relation revendiquée, choix urgence et confirmation datée/attribuée.

Saison, type, activités, consentements, Membership et état restent exclusivement dans les tables communes d’application. Les triggers conservent déclarations, candidats et décisions finales. DELETE et TRUNCATE des nouvelles tables sont interdits. Down fonctionne à vide et refuse explicitement de supprimer des claims ou applications enfant.

## Matching et création des Persons

Le claim guardian appelle directement la même fonction `match` que les soumissions : normalisation NFC du nom, email normalisé, téléphone normalisé et niveaux strong/possible/weak inchangés. Il n’existe aucun second moteur.

Zéro candidat crée automatiquement une Person à partir de la seule déclaration correspondante : claim guardian résolu/new_person ou soumission enfant résolue/new_person, sans actor administratif inventé. Avec un ou plusieurs candidats, chaque identité reste en revue, même avec un candidat strong. Le submitter enfant est utilisé sans service email : aucun outbox de vérification enfant n’est créé. Les deux résolutions sont indépendantes.

L’administration peut sélectionner uniquement un candidat du snapshot ou créer une nouvelle Person déclarée. Aucun rattachement ne modifie les données d’une Person existante et aucune fusion n’est effectuée. La création sans candidat de l’enfant utilise `ResolveUnmatchedApplication`, primitive partagée avec l’API adulte conservée.

## Minorité

La date enfant est obligatoire, finie, non future et doit désigner un mineur selon la date civile du club. `civildate` reste la référence : majorité au dix-huitième anniversaire, anniversaire du 29 février au 1er mars les années non bissextiles. Aucun âge ni booléen enfant n’est stocké. Aucun seuil particulier à 15 ans. À 18 ans, une erreur de saisie oriente vers `/join`. La Person finale est revérifiée et verrouillée avant confirmation/finalisation.

## Relation et choix de sécurité

Une déclaration publique ne crée jamais une nouvelle relation ni un grant. Une nouvelle relation exige l’action administrative distincte « Confirmer ce guardian pour cet enfant ». C’est un choix de sécurité Club Core ; aucune certification de filiation ni obligation juridique universelle n’est revendiquée.

Après résolution des deux identités, une relation `person_guardians` existante suffit sans nouvelle confirmation humaine. Son type est conservé, même s’il diffère du type revendiqué ; les deux valeurs apparaissent dans la revue. Une relation absente produit `needs_review / guardian_confirmation_required`, affiché « Lien guardian à confirmer » dans la file.

La confirmation utilise l’actor de session et exige `registrations.review` ainsi que les contrôles `persons.write` du domaine guardianaccess. Elle verrouille soumission/application, claim et Persons, contrôle minorité, archivage et absence d’auto-relation, puis confirme relation, grant, urgence et création Membership dans la transaction métier.

## Primary contact et urgence

Règle V1 : la nouvelle relation devient primary_contact seulement si aucun primary n’existe pour l’enfant. Un primary existant n’est jamais remplacé. Une relation déjà existante conserve son rôle. Le primary n’est pas une permission numérique.

Le formulaire exige un choix Oui/Non explicite pour le contact d’urgence. Oui ajoute `person_emergency_contacts` après confirmation, seulement si absent : priorité 1 sans contact existant, sinon max(priority)+1. Aucun contact existant n’est écrasé. Non n’ajoute rien : la Membership peut rester pending avec `minor_missing_emergency` bloquant son approbation. Les autres guardians, grants et contacts sont conservés.

## Grant, User et activation

`guardianaccess.GrantTx` est la variante transactionnelle partagée par `Grant` et le finalizer. Elle conserve les contrôles actor/RBAC, minorité, archivage, relation, verrous et réutilisation du grant actif. Aucun handler n’insère un grant en SQL.

Après commit métier, `AfterResolution` appelle `EnsureGuardianUser`. Le User guardian est créé/réutilisé sans Membership guardian. L’option `KeepPendingActivation` conserve une activation encore valide pour les reprises automatiques ; les actions explicites de renvoi gardent leur sémantique existante. Username, codes, cryptographie et mailer sont réutilisés. Seule la boîte propre à la Person guardian retenue est utilisée.

Un échec SMTP ou de provisioning ne défait jamais relation, grant ou Membership. La revue affiche User absent/non activé/activé/désactivé et une action de préparation/renvoi guardian. L’échec de livraison est journalisé sans PII et le renvoi explicite utilise le service accounts existant.

Aucun User enfant n’est créé par ce parcours, y compris à 16 ou 17 ans. Les tests vérifient cette absence avant toute création exceptionnelle de User enfant dans leurs seules fixtures minorsafety.

## Consentements et Membership

Le formulaire réutilise `Present`, `PresentedCatalog` et le HMAC existant : nonce, versions et date de présentation liés au cookie CSRF. Les textes complets, versions et décisions granted/refused sont présentés dans le formulaire puis le récapitulatif. Les décisions restent dans `registration_application_consents` jusqu’à résolution des deux identités et relation confirmée.

Le finalizer commun construit le même `memberships.Request` et appelle `CreateRequestWithPresentedConsentsTx`. Pour l’enfant, `given_by_person_id` est la Person guardian finale ; la relation existe avant la validation consent du domaine. Une version publiée après présentation ne remplace pas le snapshot.

La Membership appartient uniquement à la Person enfant, conserve activités et choix originaux et reste pending. Le finalizer conserve son savepoint : un doublon Person/saison laisse identités et relation résolues, sans rattachement silencieux, avec `needs_review / membership_already_exists`. Les choix devenus indisponibles et les identités devenues inéligibles sont signalés pour revue.

## Routes, revue et audit

Routes publiques : GET/POST `/join/child`, GET `/join/child/submitted`. HTML serveur, review/edit/submit, sans wizard JavaScript. Le formulaire adulte propose désormais le lien enfant.

La revue existante contient les données enfant, candidats et Person finale ; les données guardian, raisons de matching, Person finale, résolution datée/attribuée et état User ; relation revendiquée/existante, confirmation, choix urgence ; application, activités et consentements communs. Le staging n’est pas supprimé après finalisation.

Actions POST supplémentaires sous `/registration-reviews/{id}` : `link-guardian`, `create-guardian`, `confirm-guardian`, `guardian-activation`. Les actions enfant `link-person`, `create-person` et `finalize-application` sont réutilisées. Actor de session, RBAC, CSRF et redirection PRG protègent les mutations acceptées.

## Atomicité, idempotence et concurrence

La transaction publique contient claim, candidats guardian, soumission/candidats enfant, Persons sans candidat, application, extension, activités et consentements. Le verrou advisory du request_key existant couvre l’ensemble : même formulaire retransmis, même résultat, aucune double identité/application.

Résolution guardian, résolution enfant, confirmation et finalizers partagent l’ordre du verrou de décision puis soumission/application. Le claim est verrouillé ; les Persons de la paire sont verrouillées par ordre d’ID. Les contraintes uniques couvrent relation, primary, grant actif, contact urgence et Membership Person/saison. Les états fermés empêchent une deuxième résolution créatrice. Les reprises de confirmation/finalisation ne créent aucune Membership supplémentaire.

La préparation d’activation reste après commit et sérialise son contrôle du code existant avec les verrous accounts/guardianaccess. Un crash entre commit métier et préparation d’activation nécessite une reprise administrative : aucune nouvelle outbox d’activation n’a été inventée.

## Confidentialité et sécurité HTTP

Toute soumission acceptée redirige en 303 vers la même page générique, sans identifiant, candidat, relation connue, User existant ni PII dans la query string. Les erreurs publiques portent uniquement sur la saisie. Les templates `html/template` échappent déclarations, textes et récapitulatifs.

Réutilisation du SubmissionLimiter, CSRF, CrossOriginProtection, no-store, no-referrer, CSP et nosniff. La limite commune CSRF `MaxFormBodyBytes` vaut 32 KiB : deux identités et adresses encodées, plus les identifiants de consentements signés. Les limites de champs restent applicables ; le texte complet des consentements n’est pas envoyé comme donnée de confiance. Les tests de dépassement existants sont adaptés à cette limite commune.

## SQL et fichiers

SQL ajouté : migration 0023 et `internal/database/queries/child_registration.sql` (création/lecture/verrouillage/résolution claims, candidats et extension, confirmation). La liste commune de revue expose la raison de l’application. Les écritures relation/urgence et lectures d’éligibilité restent dans le service métier enfant, comme les requêtes transactionnelles existantes du projet. `sqlc generate` produit les modèles et queries correspondants.

Nouveaux fichiers métier : `internal/identityresolution/guardian.go`, `internal/registrationapplications/child.go`. Les services et vues existants sont étendus dans accounts, application, guardianaccess, handlers, identityresolution, memberships, registrationapplications, views et websecurity. Tests ajoutés : `internal/application/public_child_registration_integration_test.go`, `internal/civildate/civildate_test.go`. Les tests adultes/auth/email modifiés ne changent que l’attente du message d’orientation ou la taille du dépassement HTTP.

## Tests et vérifications

Les tests enfant utilisent les fixtures PostgreSQL 16 / Testcontainers du dépôt. Ils couvrent formulaire public, review/edit/submit, XSS, validation, email enfant absent, âges 14/15/17/18 et date future ; créations sans candidat, un ou plusieurs candidats, résolutions indépendantes, création administrative et audit ; relation connue ou nouvelle, primary conservé, grant réutilisé, urgence Oui/Non/priorité/doublon, consent giver et snapshot ; Membership pending, doublon saison, absence de User enfant et Membership guardian ; SMTP failure et activation conservée ; admin CSRF, RBAC, actor, CrossOriginProtection ; confidentialité de la page submitted ; Down vide/refus avec staging ; confirmations, POST, résolutions et finalizers concurrents ; intégration guardianaccess/minorsafety après activation.

La convention bissextile est testée directement dans civildate. Les suites existantes couvrent également activation/login, approval, outbox, résolution email, revue et protections guardianaccess/minorsafety.

Vérifications finales :

- `sqlc generate` : réussi.
- `gofmt` sur tous les fichiers Go modifiés/créés : effectué.
- `go test ./...` : réussi ; application PostgreSQL/Testcontainers en 169,639 s, database en 27,008 s.
- `git diff --check` : réussi, aucune erreur.
- `go test -race ./internal/application ./internal/identityresolution ./internal/registrationapplications ./internal/guardianaccess ./internal/minorsafety ./internal/memberships ./internal/accounts` : réussi, aucune race détectée ; application en 285,833 s. guardianaccess et accounts sont instrumentés via les tests d’intégration application, sans fichiers de tests propres.
- `git status --short` : 20 fichiers suivis modifiés et 8 nouveaux fichiers, sans commit ni push. Le dépôt était propre au départ ; les changements du chantier restent dans le workspace à la demande de l’utilisateur.


## Limites et décisions V1

Un seul guardian déclarant par formulaire. Pas de preuve publique automatisée d’une relation nouvelle ; intervention administrative volontaire même avec deux nouvelles Persons. Pas de fuzzy matching, fusion, arbitrage entre guardians, certification juridique, messagerie ni User enfant. Le matching repose sur un snapshot des Persons au moment de chaque staging ; deux formulaires indépendants ne constituent pas une preuve commune d’identité. Le request_key protège les retransmissions du même formulaire.

Les erreurs d’activation nécessitent une reprise explicite dans la revue, et un crash après commit peut laisser un User non préparé. Les règles existantes de disponibilité du mailer, de validité des codes et d’activation des Users ne sont pas remplacées. L’application n’accorde aucun accès guardian effectif à un User non activé ou désactivé.

## Inventaire des fichiers du chantier

- `internal/accounts/guardian.go` — modifié
- `internal/application/application.go` — modifié
- `internal/application/public_registration_integration_test.go` — modifié
- `internal/application/registration_email_integration_test.go` — modifié
- `internal/database/dbsqlc/models.go` — modifié
- `internal/database/dbsqlc/registration_submissions.sql.go` — modifié
- `internal/database/queries/registration_submissions.sql` — modifié
- `internal/guardianaccess/service.go` — modifié
- `internal/handlers/auth_security_test.go` — modifié
- `internal/handlers/join.go` — modifié
- `internal/handlers/registration_reviews.go` — modifié
- `internal/identityresolution/resolution.go` — modifié
- `internal/identityresolution/service.go` — modifié
- `internal/memberships/service.go` — modifié
- `internal/registrationapplications/service.go` — modifié
- `internal/views/join.go` — modifié
- `internal/views/registration_reviews.go` — modifié
- `internal/views/templates/pages/join.html` — modifié
- `internal/views/templates/pages/registration_review_detail.html` — modifié
- `internal/websecurity/csrf.go` — modifié
- `docs/reports/2026-09-12-public-child-registration.md` — créé
- `internal/application/public_child_registration_integration_test.go` — créé
- `internal/civildate/civildate_test.go` — créé
- `internal/database/dbsqlc/child_registration.sql.go` — créé
- `internal/database/queries/child_registration.sql` — créé
- `internal/identityresolution/guardian.go` — créé
- `internal/registrationapplications/child.go` — créé
- `migrations/0023_public_child_registration.sql` — créé
