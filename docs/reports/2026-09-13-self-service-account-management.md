# Gestion personnelle du compte — 13 septembre 2026

## Diagnostic initial

Commandes initiales exécutées dans l’ordre : `git status --short` (vide), `git log --oneline -5` (HEAD `acc4ce8 Add personal and family space`), `go test ./...` (succès, cache). Dépôt propre avant ce chantier. Aucun commit ni push pendant cette intervention. Le précédent espace personnel est conservé, avec ses autorisations et son historique familial.

Inspection : bcrypt coût standard, politique d’activation 12–72 octets, codes numériques crypto/rand de 20 chiffres, limiteur local AttemptLimiter, CSRF/CrossOriginProtection communs. Sessions stockées en mémoire par processus, avec tokens aléatoires hachés ; auparavant aucune liaison à la version du mot de passe. Person ne possédait pas updated_at. Normalisation téléphone déjà présente dans identityresolution ; adresse texte facultative, limites de validation communes (80 caractères téléphone, 2000 adresse). Audit existant spécialisé dans inscriptions/activation, sans journal de sécurité générique adapté au compte.

## Routes et frontière de propriété

| Routes | Objet |
|---|---|
| GET /me/account | Identité, coordonnées, connexion/sécurité, fonctions au club |
| GET et POST /me/account/profile | Téléphone et adresse |
| GET et POST /me/account/email | Demande de nouvelle adresse avec mot de passe actuel |
| GET et POST /me/account/email/verify | Consommation du code de la demande du compte connecté |
| GET et POST /me/account/password | Remplacement du mot de passe avec preuve de l’actuel |

Service `accounts.SelfService` dédié, distinct du service administratif de gestion des personnes. Les handlers ne passent aucun ID navigateur au service : celui-ci tire UserID du contexte authentifié, verrouille le User éligible et sa Person, puis emploie exclusivement son person_id. Les rôles ne participent pas à ce choix. Un secrétaire ne peut pas choisir une Person étrangère via ces routes. Les champs supplémentaires person_id/user_id/prénom/username sont ignorés.

## Person / User et coordonnées

Person conserve email, phone_number, address, identité et naissance. User conserve username, password_hash et état du compte. Aucun déplacement ni duplication nouvelle d’email dans User. Prénom, nom, date de naissance et username sont affichés sans formulaire de modification ; correction auprès du club uniquement. Memberships, guardians, consentements et suppression de compte restent hors périmètre.

Téléphone facultatif : trim, validation existante des tailles/encodages, normalisation `identityresolution.NormalizePhone`, simple exposition de l’algorithme existant de matching. Ainsi 06 12 34 56 78 devient 33612345678. Une entrée non vide dont la normalisation échoue est refusée ; vide devient SQL NULL. Adresse : texte existant conservé, trim, validation commune 2000 caractères/UTF-8/NUL, vide devient NULL. Aucun second normaliseur ni nouvelle décomposition postale. Les données invalides restent dans le formulaire ; aucun mot de passe/code n’y est réaffiché.

La migration 0024 ajoute persons.updated_at, mis à jour par les mutations personnelles de coordonnées/email. Elle ne refactore pas les anciens handlers administratifs. L’événement générique profile_contact_updated est transactionnel et n’archive aucune ancienne/nouvelle coordonnée.

## Changement email

1. Nouvelle adresse validée (adresse simple, longueur limitée, sans CR/LF), mot de passe actuel comparé avec bcrypt sous verrou du User.
2. Budget de trois demandes créées par User dans l’heure glissante, calculé en PostgreSQL sous le même verrou.
3. Invalidation de l’ancienne demande non consommée, même expirée ; insertion de la nouvelle et de l’événement email_change_requested dans la transaction.
4. Commit puis livraison du code uniquement à la nouvelle adresse. Person.email n’est pas encore modifié.
5. Vérification dans la session : demande du User et de sa Person, non utilisée, non invalidée, non expirée, hash correct. Consommation, modification Person.email et événement email_changed atomiques.
6. Notification générique à l’ancienne adresse après commit, sans ancienne/nouvelle adresse dans le texte. Une erreur de livraison de cette notification ne rollback pas le changement.

Username, IDs, rôles, Memberships et GuardianAccess ne changent pas. Aucune contrainte UNIQUE sur persons.email : les adresses familiales partagées restent possibles. Aucun snapshot d’inscription, de matching ou de consentement n’est réécrit.

### Stockage, code et TTL

Migration `0024_self_service_account.sql` : user_email_change_requests contient User/Person, nouvelle adresse en clair pour application/livraison, forme normalisée et SHA-256 de cette forme, SHA-256 du code, création/expiration/utilisation/invalidation. Index unique partiel : une demande non consommée et non invalidée par User. Historique conservé par les parcours ; rollback de migration refusé en présence d’historique de sécurité.

`activation.GenerateCode` extrait la primitive existante : 20 chiffres générés indépendamment via crypto/rand, plus de 66 bits d’entropie. Aucun code en clair persistant ; comparaison des empreintes en temps constant. `EMAIL_CHANGE_TTL`, défaut central d’une heure, documenté dans .env.example, validé au chargement (strictement positif). L’expiration utilise l’horloge PostgreSQL et est revérifiée lors de la consommation.

Transport désactivé/erreur : réponse générique d’indisponibilité, aucune prétention de livraison réussie, aucun code affiché. La demande déjà créée reste soumise au TTL et au budget ; l’ancienne adresse demeure inchangée. L’erreur brute SMTP n’est pas propagée à HTML/logs. Livraison directe après commit, sans nouvelle outbox. Une panne entre commit et livraison peut nécessiter une nouvelle demande ; limite explicitement conservée.

### Rate limiting et concurrence

AttemptLimiter existant réutilisé pour les POST sensibles, avec clés internes User/opération : dix tentatives par quinze minutes et plafond global de 120/minute pour ce limiteur. Ne dépend pas d’un email, d’une adresse IP fournie dans le formulaire ou d’un header proxy. En supplément, le budget PostgreSQL de trois demandes email par User/heure survit aux redémarrages et est sérialisé entre instances. Le hash email est stocké ; pas de quota global sur une adresse partagée dans cette V1.

Les demandes et vérifications verrouillent User/Person avant la demande. Index unique et invalidation transactionnelle empêchent deux demandes utilisables. Deux demandes simultanées peuvent être livrées dans un ordre différent : seule la dernière créée est vérifiable. Deux validations concurrentes du même code : une seule réussite/événement. Une nouvelle demande invalide le code précédent. Test de rollback avec échec forcé de l’audit : aucune consommation ni modification partielle d’email.

## Mot de passe et sessions

Mot de passe actuel obligatoire et revérifié sous verrou, nouveau et confirmation identiques, politique partagée avec activation via auth.ValidPassword/HashPassword : 12–72 octets, bcrypt coût standard. Réutiliser l’actuel est refusé. Deux changements avec le même ancien mot de passe : un seul réussit ; le second compare le nouveau hash et échoue. Le changement invalide aussi les demandes email en attente.

Après commit, RotateAccount supprime les sessions locales de ce User et crée un nouveau token crypto/rand pour la session courante. Cookie HttpOnly, SameSite Lax, Secure en HTTPS, durée existante. L’ancien token est supprimé. Si la rotation échoue exceptionnellement, la session courante est supprimée et retour login.

La connexion lie désormais chaque session de production à l’empreinte du hash bcrypt effectivement vérifié. Le middleware compare cette empreinte au hash courant lu en base : une session d’un autre processus ou un login tardif vérifié avant le changement est rejeté, même s’il échappe à la suppression locale. Aucun hash/empreinte dans le cookie ou les templates. La primitive Create sans empreinte demeure pour les fixtures internes existantes ; le login de production utilise CreateAuthenticated. Stockage de sessions toujours local, aucune infrastructure distribuée ajoutée. Une requête déjà authentifiée en cours n’est pas interrompue ; les opérations sensibles revérifient le mot de passe sous verrou.

## Audit et confidentialité

account_security_events : identifiant d’événement, user_id, person_id, timestamp et événement parmi email_change_requested/email_changed/password_changed/profile_contact_updated. Pas de coordonnées, mot de passe, code ou hash. Chaque événement fait partie de la transaction métier. Aucun formulaire ni erreur SMTP brute journalisé ; aucune nouvelle journalisation de PII.

View models minimaux : Account enrichi seulement d’adresse et naissance ; AccountFormView contient coordonnées destinées au formulaire, messages et erreurs, jamais code, mot de passe ni identifiants de ressource. html/template assure l’échappement. Aucun email/ID/code dans les URLs générées. PRG utilise seulement les indicateurs fixes saved=1/sent=1, convention des pages auth existantes ; ces indicateurs affichent un message, ne constituent aucune preuve ni autorisation métier.

## Sécurité HTTP et UX

Authentification obligatoire pour GET/POST, no-store, CSRF, CrossOriginProtection, cookie SameSite Strict pour CSRF, security headers et limite commune de corps 32 Kio conservés. Un POST anonyme retourne au login, une preuve CSRF manquante ou origine étrangère est refusée. Erreurs 422 liées aux champs, quotas 429, indisponibilité générique 503. Succès 303 vers une page GET ; formulaires pleinement utilisables sans JavaScript.

Page compte divisée en identité, coordonnées, connexion/sécurité et fonctions au club si présentes. Les pages personnelles familiales restent lecture seule ; seule la mention obsolète du compte est retirée. Composants partagés page-header, section-panel, boutons, alertes, actions, content-readable et focus existant. Aucun nouveau design system ni framework.

Contrôle réel : serveur Go de l’application sur localhost:8080, PostgreSQL 16 isolé via newFixture et comptes fictifs, mailer de test sans SMTP réel. Chromium/Playwright, JavaScript désactivé, viewports 390×900 et 1365×900. Les cinq pages demandées ont été inspectées dans les deux formats, ainsi que les erreurs de téléphone/mot de passe actuel/code/nouveau mot de passe et les confirmations de coordonnées/email/password. 28 captures et mesures dans `/tmp/club-account-visual/` (`audit.json`). Aucun overflow de document ; tous les champs visibles ont un label. Inspection des captures : boutons entiers, hiérarchie et erreurs lisibles, coordonnées conservées en erreur, secrets effacés, confirmations claires. Aucun changement CSS nécessaire. Le serveur a été arrêté, le harnais temporaire retiré du dépôt ; copie de travail dans /tmp/club-account-visual-fixture.go. Aucun compte ni adresse réelle utilisé.

## Tests et validation

Tests PostgreSQL/HTTP dédiés dans self_service_account_integration_test.go : anonymat, propriété/admin, préremplissage, normalisation/absence/invalidité téléphone et adresse, valeurs conservées, CSRF/origine/taille de corps/PRG, email invalide/mot de passe faux, ancienne adresse conservée, hash du code, code erroné/expiré/réutilisé, remplacement de demande, emails partagés, quota, isolation inter-User, notification échouée sans rollback, mot de passe invalide/confirmation/réutilisation/succès, vrai login HTTP nouveau, ancien refusé, autres sessions révoquées et courante saine. Concurrence email et password, audit transactionnel et rollback forcé, transport désactivé, invalidation des demandes email après password.

Tests auth : rejet d’un login tardif lié à l’ancien hash. Tests config : TTL par défaut et paramètres invalides. Helper de confidentialité existant adapté aux codes de livraison de plusieurs types et aux notifications sans code ; les assertions sur les mots de passe/hashes restent présentes.

Résultats ciblés : six tests SelfService passent (12,410 s) ; auth/config passent. Première exécution ciblée a révélé l’absence de persons.updated_at et l’hypothèse « tout email est une activation » dans le helper de test, corrigées avant validation finale.

Validation globale : `sqlc generate` et gofmt réussis ; `go test ./...` réussi, application 192,330 s et database 27,351 s, puis nouvelle exécution après génération/formatage également réussie (cache). `git diff --check` réussi. Journal global : `/tmp/club-account-final-tests.log`. Contrôle race étendu : réussi (application 345,556 s), sans course détectée ; `/tmp/club-account-family-resume-race.log`. Packages : application, guardianaccess, memberships, accounts, personalspace, auth, activation, identityresolution, config, handlers et views.

Une nouvelle instruction de reprise de l’espace familial a été reçue pendant la validation. Depuis cette instruction, aucun ajout fonctionnel de gestion du compte n’a été effectué ; les changements antérieurs ont été conservés et audités. Voir la section « Nouvel audit de reprise » du [rapport familial](2026-09-13-personal-family-space.md).

## Limites

Pas de modification d’identité/username, suppression de compte, comptes mineurs, gestion de guardians/consentements/adhésions. Pas de nouvelle outbox ni retry automatique de livraison email ; absence de notification générale. Quotas de tentatives locaux selon infrastructure existante, budget de création email persistant. Pas de quota par adresse partagée. Historique des demandes et audit sans politique de purge automatisée ajoutée. Pas de restauration d’un email précédent ni d’historique de mots de passe. updated_at ajouté pour les nouvelles mutations personnelles ; pas de refactor des mutations admin anciennes. Aucune vraie adresse email dans les fixtures, le rapport ou le dépôt.

## Future multi-user administration test

Campagne non construite et aucune simulation administrative réelle exécutée. Préparer ultérieurement un environnement isolé avec : compte A adhérent normal ; compte B secrétaire/membre du bureau ; compte C optionnel guardian et/ou adhérent. Utiliser exclusivement les adresses fournies localement par l’utilisateur, hors Git et logs.

Capacités à vérifier lors de ce futur chantier : comptes activés et usernames distincts ; boîte de livraison accessible pour chaque adresse ; sessions dans profils navigateurs séparés ; rôles administratifs explicites pour B ; Memberships sur plusieurs saisons ; pour C, enfant mineur, relation et grant actif. Comparer propriété personnelle et permissions administratives, changements de coordonnées visibles côté club, remplacement email seulement après preuve, indépendance des comptes, révocation des sessions, audit, maintien des accès familiaux et arrêt à la majorité. Un email partagé ne doit pas fusionner les comptes. Prévoir base/SMTP de test isolés et nettoyage autorisé ; aucune adresse réelle n’est codée à ce stade.

## Décisions restantes

Aucune clarification produit nécessaire à la V1. Les limites ci-dessus et les adresses locales du futur environnement relèvent de chantiers suivants.

## Fichiers créés ou modifiés

- `.env.example`
- `docs/reports/2026-09-13-self-service-account-management.md`
- `internal/accounts/self_service.go`
- `internal/activation/service.go`
- `internal/application/application.go`
- `internal/application/membership_ui_integration_test.go`
- `internal/application/self_service_account_integration_test.go`
- `internal/auth/credential_sessions_test.go`
- `internal/auth/password.go`
- `internal/auth/service.go`
- `internal/auth/sessions.go`
- `internal/config/runtime.go`
- `internal/config/runtime_test.go`
- `internal/database/dbsqlc/memberships.sql.go`
- `internal/database/dbsqlc/models.go`
- `internal/database/dbsqlc/personal_space.sql.go`
- `internal/database/dbsqlc/persons.sql.go`
- `internal/database/dbsqlc/self_service_account.sql.go`
- `internal/database/queries/personal_space.sql`
- `internal/database/queries/self_service_account.sql`
- `internal/handlers/auth.go`
- `internal/handlers/self_service_account.go`
- `internal/identityresolution/service.go`
- `internal/personalspace/service.go`
- `internal/views/account_form.go`
- `internal/views/templates/pages/account_form.html`
- `internal/views/templates/pages/personal_space.html`
- `migrations/0024_self_service_account.sql`

État final après audit familial : 18 fichiers suivis modifiés et 11 nouveaux, tous conservés non committés. `git diff --check` réussi. Aucun commit ni push.
