# Activation HTTP, email et connexion par username

Date : 2026-09-11.

## Diagnostic initial

Le dépôt était propre. Les cinq derniers commits étaient :
- 4eb841b Add membership approval and user activation
- 69c8b06 Add emergency contacts and membership consents
- 6148c4a Add trial scheduling with groups and slots
- 667adb8 Add groups, memberships and recurring slots
- 63522d5 Add person notes and guardian relationships

La présence distante de 4eb841b708e4a20ca74d55abfa8371ec57593a70 sur main a été vérifiée avec git ls-remote. Le go test ./... initial réussissait, avec résultats en cache.

L’inspection du routeur, des handlers, des vues, de la configuration et des requêtes a constaté :
- aucun handler ni template de login ;
- aucune session, aucun cookie d’authentification et aucun middleware d’authentification ;
- aucune protection CSRF existante ;
- net/http.ServeMux, handlers Go et templates html/template intégrés via embed ;
- GetUserByUsername et les primitives memberships/activation déjà disponibles ;
- login_email conservé dans le schéma/modèle, sans mécanisme de connexion existant qui l’utilise ;
- configuration éditoriale JSON et DATABASE_URL fourni par environnement ;
- routes historiques de gestion des Persons accessibles sans contrôle d’accès.

Le chantier ajoute une première authentification dans l’architecture existante. Il ne remplace ni ne réécrit le cœur métier d’adhésion ou d’activation.

## Architecture et branchement

internal/application construit le mailer, les services existants, l’orchestration accounts, l’authentification, les sessions et les handlers. cmd/server/main.go utilise son Handler et configure des délais HTTP.

Le ServeMux et les templates de base existants sont conservés. Un lien Connexion apparaît dans la navigation.

Application.Accounts expose les deux opérations destinées au futur appelant administratif :
- ApproveMembership(ctx, membershipID, approverUserID, note) ;
- ResendActivation(ctx, userID).

Aucune route publique d’approbation ou de renvoi administratif n’est créée. Le futur appelant devra appliquer ses autorisations puis utiliser cette orchestration pour obtenir l’envoi après approbation.

Aucune migration, nouvelle dépendance ou modification des services internal/memberships et internal/activation. La seule requête sqlc ajoutée est GetUserByID, pour revalider les sessions.

## Configuration

La configuration technique est chargée par config.LoadRuntime depuis l’environnement, séparément du JSON éditorial. .env.example fournit les noms et des valeurs de développement sans secret ; il n’est pas chargé automatiquement.

| Variable | Comportement |
| --- | --- |
| APP_BASE_URL | Origine publique, défaut http://localhost:8080 ; HTTPS requis hors loopback, sans identifiants, chemin, query ou fragment |
| EMAIL_TRANSPORT | disabled par défaut, ou smtp |
| SMTP_HOST | Hôte du relais, requis en mode smtp |
| SMTP_PORT | Port explicite entre 1 et 65535 |
| SMTP_USERNAME | Identifiant facultatif, requis avec SMTP_PASSWORD |
| SMTP_PASSWORD | Secret uniquement fourni par environnement ; jamais journalisé |
| SMTP_FROM | Adresse d’expédition valide, éventuellement avec nom d’affichage |
| SMTP_STARTTLS | true par défaut ; false permis uniquement sur loopback et sans authentification |
| ACTIVATION_CODE_TTL | Durée fournie aux primitives existantes ; défaut centralisé 24h, strictement positive |
| APP_TIMEZONE | Fuseau administratif, défaut Europe/Paris |

Une configuration SMTP activée mais invalide bloque le démarrage avant la connexion PostgreSQL. Aucun secret SMTP n’est ajouté au JSON, au dépôt ou au rapport. Les erreurs de configuration ne reproduisent pas les valeurs reçues.

En mode disabled, une tentative d’envoi retourne ErrDisabled ; elle n’est jamais annoncée comme envoyée. Les pages d’activation/login fonctionnent sans SMTP.

Pour le développement HTTP, le serveur écoute sur 127.0.0.1:8080. En configuration HTTPS, il écoute sur :8080 derrière un reverse proxy TLS ; ce port backend doit rester privé. Les attributs Secure des cookies proviennent de APP_BASE_URL, sans confiance implicite dans les en-têtes Forwarded.

## Mailer et template

Interface minimale :
- Message : From, To, Subject, Text ;
- Mailer.Send(ctx, message) error.

L’implémentation réelle utilise net/smtp avec :
- connexion annulable et délai total maximal de 30 secondes ;
- STARTTLS obligatoire lorsqu’activé, sans repli silencieux en clair ;
- validation des certificats et TLS 1.2 minimum ;
- authentification PLAIN uniquement sous TLS ;
- vérification des adresses et rejet des injections CR/LF dans les en-têtes ;
- corps UTF-8 quoted-printable et sujet encodé MIME ;
- erreurs par étape sans reproduire les réponses potentiellement sensibles du relais.

La confirmation DATA vaut acceptation par le relais, pas preuve de réception dans la boîte. Un échec ultérieur de QUIT n’annule pas cette acceptation.

internal/mailer/templates/activation.txt est intégré avec embed et rendu via text/template. Le message contient Club Core, le username, le code et le lien APP_BASE_URL/activate. Il rappelle que compte et adhésion sont distincts, et invite à contacter le club en cas de problème. Aucun mot de passe ou hash n’entre dans les données de ce template.

Le code n’est jamais ajouté à l’URL. Sa durée est décrite comme limitée sans inventer une date d’expiration absente de la structure ActivationDelivery existante. Aucun journal de message ni mailer « console » n’est ajouté.

## Ordre exact : commit puis email

Approbation :
1. Appel au service memberships.ApproveMembership existant.
2. Retour réussi uniquement après son commit PostgreSQL.
3. ActivationDelivery nil : statut not_required.
4. Livraison sans destinataire : statut no_email_channel.
5. Livraison avec destinataire : rendu du template puis Mailer.Send.
6. Succès : statut sent ; échec : statut send_failed et DeliveryError.

L’orchestration ne transmet au futur écran ni plaintext_code ni password_hash : elle retourne Membership, UserID, complétude et DeliveryStatus.

Un échec SMTP produit exactement :
« Adhésion validée, mais email d’activation non envoyé. »

DeliveryError.ApprovalCommitted vaut true. L’erreur sous-jacente reste classifiable via errors.Is/Unwrap, tandis que le message de l’erreur est sûr à afficher. Membership active, User et code restent persistés. Aucun rollback ni passage à un statut d’adhésion invalide.

## Renvoi d’activation

ResendActivation :
1. Ouvre une transaction et verrouille le User.
2. Refuse un User désactivé ou déjà activé avec des erreurs métier distinctes pour l’appelant administratif.
3. Appelle activation.PrepareTx existant.
4. Cette primitive résout à nouveau Person.email, guardian primary puis autre guardian déterministe, invalide l’ancien code et prépare le nouveau.
5. Commit.
6. Envoi hors transaction, ou résultat no_email_channel.

Sans canal, le compte n’est pas corrompu et aucun mail n’est tenté. Le nouveau code et l’invalidation de l’ancien sont néanmoins committés, conformément à l’ordre de préparation retenu ; un futur renvoi après correction de l’adresse créera un nouveau code.

Si le SMTP échoue après renvoi, le résultat est send_failed et le message :
« Nouveau code préparé, mais email d’activation non envoyé. »

Aucune adresse n’est copiée entre Persons ou vers User. La résolution est entièrement déléguée à la primitive existante.

## Routes et vues HTTP

| Route | Comportement |
| --- | --- |
| GET /activate | Formulaire public : identifiant, code, nouveau mot de passe, confirmation |
| POST /activate | Confirmation vérifiée, puis appel direct à activation.Activate |
| GET /login | Formulaire Identifiant / Mot de passe, ou état connecté et bouton de déconnexion |
| POST /login | Recherche par username, vérification bcrypt et création de session |
| POST /logout | Révocation de la session locale puis redirection /login |

Les POST activation/login suivent PRG :
- activation réussie : 303 vers /login?activated=1, avec « Votre compte est activé. Vous pouvez maintenant vous connecter. » ;
- activation refusée : 303 vers /activate?error=1, puis message unique « Impossible d’activer le compte avec ces informations. » ;
- login refusé : 303 vers /login?error=1, puis « Identifiant ou mot de passe incorrect. » ;
- login réussi : 303 vers /.

Les paramètres de redirection ne contiennent aucun identifiant, code ou mot de passe. Les formulaires ne réaffichent pas les valeurs soumises. Le GET ne recherche aucun username.

La page rappelle minimum 12 caractères et la borne de 72 octets. La primitive existante reste l’autorité : elle applique actuellement 12–72 octets, pas un comptage Unicode des caractères. Ce contrat métier n’a pas été changé ; le minlength HTML est une aide de saisie.

## Login et sessions

auth.Service utilise GetUserByUsername, sans fallback email. Le compte doit être is_active, activated_at renseigné et password_hash renseigné, puis bcrypt doit accepter le mot de passe.

Le message public est identique pour username absent, mauvais mot de passe, compte désactivé, compte non activé et hash absent. Une comparaison bcrypt factice limite la différence de coût pour les comptes absents ou inéligibles. Les erreurs internes de lookup ne sont pas exposées.

Les usernames legacy.<id> restent utilisables tels quels, sans mécanisme supplémentaire.

Faute de système préexistant, une session opaque minimale est créée :
- jeton aléatoire de 256 bits, seule son empreinte SHA-256 indexe le stockage serveur ;
- stockage local mutex, borné à 10 000 entrées, expiration fixe à 12 heures ;
- renouvellement du jeton à chaque login réussi et invalidation de l’ancien ;
- révocation au logout ;
- cookie HttpOnly, SameSite=Lax, Path=/, sans Domain ;
- en HTTPS : Secure et préfixe __Host- ;
- relecture du User à chaque utilisation d’une session : désactivation/non-activation/hash absent invalide la session ;
- contexte HTTP ne contenant que l’ID User, jamais le hash.

Aucune consultation de Membership n’intervient dans l’authentification ou les sessions. Les tests couvrent une adhésion ended et un compte n’ayant aucune Membership.

## Sécurité HTTP et limitation des tentatives

Les routes d’authentification utilisent :
- http.CrossOriginProtection de Go, sans trusted origins/bypass ;
- token CSRF aléatoire de 256 bits, cookie HttpOnly SameSite=Strict et champ caché comparés en temps constant ; préfixe __Host- en HTTPS ;
- le token reste obligatoire même lorsque Origin/Sec-Fetch-Site sont absents ;
- lecture du corps POST limitée à 8 Kio, avec extraction depuis PostForm uniquement ;
- Cache-Control: no-store, Referrer-Policy: no-referrer, X-Content-Type-Options: nosniff ;
- CSP frame-ancestors 'none', form-action 'self', base-uri 'self' ;
- échappement automatique html/template ;
- aucune journalisation de code, mot de passe, hash ou identifiants SMTP.

Un limiteur local partagé entre POST /activate, /login et /logout autorise 10 tentatives par IP sur 15 minutes et 120 tentatives globales par minute. Il fonctionne avant bcrypt et la lecture du formulaire. Sa mémoire est bornée, les fenêtres expirent, les rejets retournent 429 et Retry-After. Il utilise RemoteAddr, ignore X-Forwarded-For, et son horloge est testable.

Le serveur reçoit aussi des délais ReadHeader, Read, Write et Idle.

## Tests

PostgreSQL 16/Testcontainers :
- parcours complet approbation → email capturé → activation HTTP → bcrypt/activated_at → login username → session → logout ;
- vérification à partir d’une connexion PostgreSQL indépendante que Membership et code sont committés au moment exact de Send ;
- erreur différée au commit : aucun mail et Membership toujours pending ;
- SMTP en échec : Membership toujours active, erreur opérationnelle et cause classifiable ;
- compte activé renouvelé : aucun mail ;
- absence d’email : approbation valide et résultat explicite ;
- renvoi : ancien code invalidé avant Send, adresse Person réévaluée, fallback primary puis autre guardian, refus des comptes désactivés/activés ;
- erreurs HTTP : confirmation différente, mot de passe court, code faux, expiré, invalidé, utilisé, username absent ; message générique et aucune activation partielle ;
- login refusé pour mauvais username/password, désactivation, non-activation, hash absent et email utilisé comme identifiant ;
- login sans Membership courante et sans aucune Membership.

Tests unitaires/HTTP :
- configuration valide, invalide, développement local et absence de secrets affichés ;
- contenu du template et absence de données de mot de passe/hash ;
- vrai dialogue SMTP loopback : DATA accepté, RCPT refusé, erreurs assainies, absence de STARTTLS, annulation pendant greeting ;
- injection d’en-têtes refusée et transport disabled explicite ;
- GET avec cookies/en-têtes de sécurité ;
- rejet CSRF pour cookie/token manquant, mauvais token, Origin étranger ou Sec-Fetch-Site cross-site ;
- corps trop volumineux refusé ;
- capture des logs standard et vérification de l’absence des secrets des requêtes dans logs/réponses/redirections ;
- limitation IP/globale, expiration des fenêtres et impossibilité de contourner par changement du port ou X-Forwarded-For ;
- rotation, expiration, falsification et révocation des sessions, revalidation des comptes désactivés.

Aucun test ne dépend d’un SMTP Internet. Aucun email réel n’a été envoyé.

## Fichiers créés et modifiés

Créés :
- .env.example
- internal/accounts/service.go
- internal/application/application.go
- internal/application/integration_test.go
- internal/auth/service.go
- internal/auth/sessions.go
- internal/auth/sessions_test.go
- internal/config/runtime.go
- internal/config/runtime_test.go
- internal/handlers/auth.go
- internal/handlers/auth_security.go
- internal/handlers/auth_security_test.go
- internal/mailer/mailer.go
- internal/mailer/smtp.go
- internal/mailer/activation.go
- internal/mailer/smtp_test.go
- internal/mailer/templates/activation.txt
- internal/views/auth.go
- internal/views/templates/pages/auth.html
- docs/reports/2026-09-11-account-activation-http-and-email.md

Modifiés :
- cmd/server/main.go
- internal/database/queries/memberships.sql
- internal/database/dbsqlc/memberships.sql.go (généré)
- internal/views/templates/layouts/base.html

## Résultats finaux

- sqlc generate : succès.
- gofmt sur tous les fichiers Go concernés : succès.
- go test ./... : succès. internal/database exécuté en 24,340 s ; internal/router en 0,004 s ; autres packages réussis ou sans fichiers de tests.
- Les nouveaux tests PostgreSQL de internal/application ont également été exécutés explicitement pendant ce chantier en 9,039 s ; le dernier passage global les a réutilisés depuis le cache.
- git diff --check : succès, aucune sortie.
- git status --short : quatre fichiers suivis modifiés et vingt nouveaux fichiers ; aucun fichier indexé. Tous correspondent au présent chantier.
- Aucun commit ni push ; services métier précédents inchangés.

État Git final :

```text
 M cmd/server/main.go
 M internal/database/dbsqlc/memberships.sql.go
 M internal/database/queries/memberships.sql
 M internal/views/templates/layouts/base.html
?? .env.example
?? docs/reports/2026-09-11-account-activation-http-and-email.md
?? internal/accounts/
?? internal/application/
?? internal/auth/
?? internal/config/runtime.go
?? internal/config/runtime_test.go
?? internal/handlers/auth.go
?? internal/handlers/auth_security.go
?? internal/handlers/auth_security_test.go
?? internal/mailer/
?? internal/views/auth.go
?? internal/views/templates/pages/auth.html
```

## Risques et décisions restant ouverts

- Les sessions et limites sont locales : un redémarrage déconnecte les utilisateurs et réinitialise les compteurs. Une architecture multi-instance demandera un stockage partagé ou une décision explicite de routage.
- Derrière un proxy, RemoteAddr représente le proxy : plusieurs utilisateurs partagent donc le quota local. Une politique de proxies de confiance et/ou une limitation en bordure devra être définie avant exposition à grande échelle.
- Les routes historiques /persons restent sans autorisation, comme constaté initialement. Le middleware de session ne transforme pas un User activé en administrateur. La définition et l’application des droits sur ces routes doivent précéder leur exposition publique.
- L’envoi est synchrone après commit, sans outbox/retry automatique. Un arrêt entre commit et Send nécessite un renvoi. Un timeout SMTP peut laisser incertaine l’acceptation par le relais.
- Deux renvois rapprochés peuvent produire des emails reçus dans un ordre différent ; seul le code le plus récent reste valide. Pas de verrou PostgreSQL tenu pendant SMTP.
- Le TLS public est terminé par un reverse proxy configuré pour APP_BASE_URL, et le backend HTTP doit rester privé. Le transport implémente STARTTLS, pas SMTP TLS implicite sur 465.
- La politique métier existante de longueur du mot de passe compte les octets ; une évolution Unicode relève d’une décision distincte.
- La durée d’activation et le fuseau ont des valeurs par défaut centralisées configurables. L’autorisation administrative des futures opérations d’approbation et de renvoi reste à brancher.
- Aucun changement aux conventions historiques legacy.<id> ou au backfill de migration 0016.

Hors périmètre conservé : formulaire public complet d’adhésion, interface administrative Memberships, mot de passe oublié, changement d’email/username, espace membre, paiements, licences, grades, boutique, alertes de renouvellement, WhatsApp, photos, compétitions et refactor général.

Aucun commit ni push effectué.
