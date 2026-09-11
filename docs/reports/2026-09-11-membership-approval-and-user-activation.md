# Demande d’adhésion, validation administrative et activation du compte

Date : 2026-09-11.

## Diagnostic initial

- Dépôt initialement propre : `git status --short` vide.
- HEAD et branche distante `main` : `69c8b06411f243b8b29b69a96cb8ea25e9fae444`, « Add emergency contacts and membership consents ».
- Présence distante confirmée par `git ls-remote origin refs/heads/main`.
- Les cinq derniers commits : `69c8b06`, `6148c4a`, `667adb8`, `63522d5`, `9fcec22`.
- `go test ./...` initial réussi, résultats en cache.
- Migration disponible suivante : 0016. Aucun changement à la migration 0015 ni au service existant de consentements.
- Aucun commit ni push effectué.

## Workflow métier et services

`Person → CreateRequest → Membership pending → ApproveMembership → Membership active → User durable créé/réutilisé/réactivé → activation préparée si nécessaire`.

`memberships.New(pool, activationValidity, administrativeLocation)` reçoit une durée strictement positive et un fuseau administratif explicite, par exemple Europe/Paris. Il n’existe pas de durée d’expiration implicite en production.

`CreateRequest(ctx, Request)` contrôle la personne, sa date de naissance finie et connue, la saison et le type actifs, ainsi qu’au moins une activité existante et active. Les activités et réponses dupliquées sont refusées. La contrainte PostgreSQL existante garantit l’unicité personne/saison. Les références inexistantes sont retournées comme erreurs de lecture PostgreSQL ; les incohérences métier sont refusées par le service.

La création de la Membership pending, de ses activités, du snapshot et des réponses est atomique. `requested_at` correspond à la soumission. Aucune création de User ni affectation de groupe n’intervient.

## Consentements

`membership_consent_requirements(membership_id, consent_definition_id, presented_at)` possède une clé primaire composite, des références étrangères et une protection contre modification/suppression. Les définitions 0015 étant immuables, référencer leur ID conserve exactement le texte et la version.

Un verrou SHARE sur le catalogue des définitions bloque brièvement publication et désactivation pendant la soumission. Toutes les définitions actives sont requises, et chacune doit avoir exactement une réponse explicite `granted` ou `refused`. Une définition inconnue, supplémentaire, omise ou une première réponse `withdrawn` annule la transaction.

Le donneur doit être la personne adhérente ou un guardian autorisé, avec verrou du lien, selon les règles 0015. Les écritures utilisent la requête sqlc existante de consentements. Les retraits ultérieurs passent toujours par le service `consents`, qui exige un accord courant avant retrait.

À la validation, seules les exigences snapshotées sont consultées. La première décision chronologique doit être granted/refused ; l’historique immuable autorise ensuite granted, refused ou withdrawn sans blocage. Un retrait injecté sans réponse initiale ne suffit pas. Une publication ou désactivation ultérieure n’affecte pas le dossier.

Pour les anciennes Memberships, la présentation initiale est impossible à reconstruire exactement. Le backfill conserve seulement les définitions attestées par des décisions existantes, avec leur première date de décision comme `presented_at`. Il n’invente pas d’exigences à partir du catalogue actuel. Les anciens dossiers sans décisions ont donc un snapshot vide ; une éventuelle reprise administrative est une décision de données historiques distincte.

## Complétude et majorité

La date de naissance reste facultative sur Person, mais obligatoire à la soumission et à l’approbation. Ni âge ni minorité ne sont stockés.

Le seuil est le dix-huitième anniversaire, comparé en dates civiles dans le fuseau configuré. Convention explicite : pour une naissance le 29 février, le dix-huitième anniversaire tombe le 1er mars d’une année non bissextile. Le groupe de pratique n’intervient jamais.

`GetDetails` retourne `Completeness` :
- `BlockingIssues` : missing_birth_date, missing_activity, minor_missing_guardian, minor_missing_emergency, unanswered_consent ;
- `Warnings` : adult_missing_emergency ;
- `IsMinor` nullable si la naissance est inconnue ;
- IDs des exigences sans réponse initiale valide.

Un mineur doit avoir au moins un guardian et un contact d’urgence. Un adulte sans contact d’urgence peut être approuvé. Les états sont recalculés, jamais persistés.

## Approbation et traçabilité

`ApproveMembership(ctx, membershipID, approverUserID, adminNote)` :
1. Verrouille la Person puis la Membership, vérifie pending et la stabilité de la référence Person.
2. Protège les activités et liens de contact observés contre suppression concurrente.
3. Vérifie que l’approver est un User existant, sans imposer de rôle.
4. Calcule la complétude à la date administrative actuelle ; une erreur typée `IncompleteError` expose les blocages.
5. Crée/réutilise/réactive le User et prépare éventuellement une activation dans la même transaction.
6. Enregistre active, approved_at, approved_by_user_id, admin_note et updated_at. joined_at est renseigné seulement s’il était NULL.
7. Retourne Membership, User, complétude et livraison temporaire seulement après commit.

La migration ajoute requested_at (backfill created_at), approved_at nullable, approved_by_user_id nullable avec FK et admin_note nullable. Les anciennes approbations ne reçoivent pas de date ou d’approver inventé. Les workflows ended/cancelled restent inchangés.

## User durable et username

`users.person_id` reste unique. Aucun lien de durée de vie n’est ajouté entre User et Membership.

La migration ajoute username UNIQUE NOT NULL et activated_at nullable. password_hash devient nullable ; login_email devient nullable et perd son unicité. login_email reste conservé comme donnée historique, n’est ni lu pour identifier un compte ni alimenté à partir des guardians.

Backfill déterministe : `legacy.<user_id>` pour chaque ancien User. Cela évite toute collision et ne modifie aucun email ou hash. Les anciens comptes possédant déjà un password_hash NOT NULL sont considérés activés à created_at ; leur is_active est conservé. Cette date est une convention de migration, pas une preuve de date d’activation historique.

Pour les nouveaux comptes : prénom/nom en minuscules, décomposition Unicode NFD et retrait des accents combinés, normalisation des séparateurs en points, quelques ligatures translittérées ; lettres non latines conservées. Une identité vide après normalisation produit `member`. Exemple : Rémi Dupont → remi.dupont.

Les collisions utilisent `INSERT ... ON CONFLICT (username) DO NOTHING RETURNING id`, puis suffixes 2, 3, etc. La contrainte UNIQUE arbitre les courses entre homonymes. Le verrou Person sérialise les approbations concurrentes de saisons différentes pour la même personne.

Un User existant conserve username, hash et activated_at, même après modification du nom de Person ou interruption d’adhésion. Une nouvelle approbation met is_active à true. Aucun code n’est créé s’il est déjà activé ; sinon un nouveau code remplace le précédent.

## Destination d’activation

L’ordre de recherche, identique pour adultes et enfants :
1. Person.email non vide, espaces périphériques retirés ;
2. guardian primary possédant un email non vide ;
3. autre guardian avec email, ordonné par ID du lien puis ID Person ;
4. aucune destination.

Aucune adresse n’est copiée entre Persons. Plusieurs enfants peuvent recevoir leur activation sur la même adresse parentale. Sans email, Membership et User sont créés/activés administrativement ; le compte reste à activer. RecipientEmail et RecipientPersonID NULL dans la livraison signalent l’absence de canal.

## Codes et activation

`activation.New(pool, validity)`, `PrepareTx` et `Activate(ctx, username, code, password)` constituent les primitives.

Les codes sont 20 chiffres tirés uniformément avec crypto/rand, soit environ 66 bits d’entropie. Seul leur SHA-256 (32 octets) est stocké dans user_activation_codes, avec user_id, expires_at, used_at, invalidated_at et created_at. La comparaison des empreintes utilise crypto/subtle. L’entropie élevée permet l’emploi d’une empreinte rapide pour ce secret aléatoire ; les mots de passe utilisent un mécanisme distinct.

La préparation verrouille le User, invalide tous les anciens codes non consommés/non invalidés (y compris expirés), conserve l’historique et insère le nouveau. Un index unique partiel impose au maximum un code non utilisé/non invalidé par User.

La livraison temporaire contient UserID, Username, PlaintextCode et le destinataire nullable. Aucun code en clair n’est persisté ou journalisé par les services. PrepareTx ne doit être utilisé que dans une transaction dont le commit a réussi avant de transmettre la livraison.

Activate vérifie username, compte actif non encore activé, code non consommé/non invalidé/non expiré. La consommation, password_hash et activated_at sont atomiques sous verrou User et code. Une seconde consommation concurrente échoue. L’expiration utilise clock_timestamp, y compris après attente de verrou.

Le projet possédait déjà golang.org/x/crypto en dépendance indirecte : bcrypt est utilisé au coût standard 10. La primitive accepte 12 à 72 octets de mot de passe, borne supérieure imposée par bcrypt. x/crypto et x/text deviennent dépendances directes sans changement de version. Aucun écran HTTP ni SMTP.

## Lectures pour le futur frontend

Nouvelles requêtes sqlc :
- GetMembership ;
- GetMembershipDetails : Membership, Person, Season, MembershipType, identifiant/username/état du User et username de l’approver ;
- ListMembershipActivities ;
- ListMembershipConsentRequirements : version, texte présenté et dernière décision ;
- GetUserByUsername ;
- GetUserByPerson.

GetDetails regroupe ces lectures dans une transaction read-only repeatable-read et expose la complétude ainsi que AccountState (Exists, IsActive, IsActivated, NeedsActivation). Le frontend n’a pas à déduire les règles de majorité ni les états du compte.

La lecture historique 0015 ListCurrentMembershipConsents conserve son contrat (catalogue actif et décisions historiques). Pour afficher le dossier soumis, utiliser la nouvelle lecture snapshotée GetDetails/ListMembershipConsentRequirements.

Les écritures et verrous propres au workflow sont des requêtes pgx internes aux services ; aucune API REST ou couche DTO générale n’est ajoutée.

## Fichiers

Créés :
- migrations/0016_membership_approval_and_user_activation.sql
- internal/memberships/service.go
- internal/memberships/username.go
- internal/memberships/username_test.go
- internal/activation/service.go
- internal/database/queries/memberships.sql
- internal/database/dbsqlc/memberships.sql.go (généré)
- internal/database/memberships_integration_test.go
- docs/reports/2026-09-11-membership-approval-and-user-activation.md

Modifiés :
- internal/database/dbsqlc/models.go (généré)
- internal/database/consents_integration_test.go : rollback ciblé jusqu’à 0014 pour continuer à tester 0015 après l’ajout de 0016
- go.mod : x/crypto et x/text deviennent dépendances directes.

## Tests

PostgreSQL 16 via Testcontainers, sans substitution par mocks :
- matrice de demandes invalides, absence de données partielles, unicité personne/saison ;
- pending sans User, snapshot actif, granted/refused explicites, versions ultérieures et retrait ;
- adultes, warnings, mineurs, guardian donneur, naissance/activité/consentement manquants, approver inexistant, double approbation ;
- anniversaire exact, joined_at conservé, note administrative et groupe indépendant ;
- quatre homonymes, conservation du compte/hash/username/activated_at à travers plusieurs saisons ;
- comptes actifs et désactivés, activés et non activés ;
- tous les chemins email, ordre déterministe des guardians et deux enfants partageant une adresse ;
- empreinte des codes, succès, code faux/expiré/invalidé/utilisé, historique d’invalidation, mot de passe bcrypt ;
- cinq homonymes approuvés simultanément, renouvellements concurrents d’une même Person, consommation concurrente d’un code ;
- échec forcé après préparation du compte/code : rollback complet et aucune livraison retournée ;
- concurrence sur la même Membership, immutabilité du snapshot ;
- migration avec anciens comptes et consentements, aller-retour et refus atomique d’un rollback incompatible.

Tests unitaires : normalisation des usernames, veille/jour/lendemain du dix-huitième anniversaire et convention du 29 février.

## Validation finale

- `sqlc generate` : succès (sqlc v1.31.1).
- `gofmt` sur les fichiers Go concernés : succès.
- `go test ./...` : succès ; internal/database exécuté avec PostgreSQL/Testcontainers en 24,336 s, internal/memberships en 0,002 s. Aucun test ignoré.
- `git diff --check` : succès, aucune sortie.
- `git status --short` : uniquement les changements locaux de ce chantier, trois fichiers suivis modifiés et neuf nouveaux fichiers ; aucun fichier indexé, aucun commit/push.

État Git final :

```text
 M go.mod
 M internal/database/consents_integration_test.go
 M internal/database/dbsqlc/models.go
?? docs/reports/2026-09-11-membership-approval-and-user-activation.md
?? internal/activation/
?? internal/database/dbsqlc/memberships.sql.go
?? internal/database/memberships_integration_test.go
?? internal/database/queries/memberships.sql
?? internal/memberships/
?? migrations/0016_membership_approval_and_user_activation.sql
```

## Limites et décisions hors périmètre

La migration Down refuse les données incompatibles avec les anciennes contraintes (email obligatoire/unique, hash obligatoire), sans inventer d’email/hash ni supprimer les nouveaux comptes.

Le verrou SHARE du catalogue est volontairement simple et bref ; il retarde les publications pendant les demandes. Les écritures métier doivent passer par les services. Les comptes existants créés par un autre chemin concurrent restent protégés par les contraintes UNIQUE ; une violation personne/saison ou person_id est retournée à l’appelant.

La durée de validité et le fuseau sont à fournir lors du futur branchement applicatif. La convention du 29 février et le traitement des dossiers antérieurs sont explicités ci-dessus pour revue métier. La future entrée HTTP devra encadrer les tentatives d’activation et protéger la livraison temporaire ; aucun endpoint public n’est exposé ici.

Restent hors périmètre : UI, SMTP, récupération de mot de passe, alerte de non-renouvellement, espace membre, licences, grades, paiements, boutique, RBAC détaillé, WhatsApp, photos, compétition et affectation automatique aux groupes. Aucun refactor général ni modification du workflow de fin d’adhésion.
