# P2.1.2 — Configuration initiale et premier administrateur

Date : 25 septembre 2026.

## État initial

Au début du chantier, HEAD était `4a5af3c` et le working tree contenait déjà les modifications non commitées P2.1 et P2.1.1. Elles ont été préservées. P2.1.1 fournissait la liste des utilisateurs, la gestion web des rôles, les permissions `RolesRead` et `RolesManage`, le CSRF, l’audit des rôles et la protection du dernier gestionnaire. Aucun état d’installation ni secret de setup n’existait. `clubctl grant-role` nécessitait déjà un utilisateur ; créer le tout premier compte pouvait nécessiter SQL.

L’audit a confirmé que `users.person_id` référence `persons`, que `birth_date` est désormais facultatif, que l’authentification normale emploie le username, bcrypt, `is_active` et `activated_at`, et que l’activation par code est destinée aux comptes issus d’autres workflows. `auth.HashPassword` fournit la politique commune de mot de passe (12 à 72 octets). `websecurity.CSRF` et `handlers.AttemptLimiter` protègent déjà les formulaires sensibles. `administrative_events` exige un `actor_user_id` ; aucun acteur utilisateur n’existe avant le setup.

## Architecture retenue

Une commande technique locale `clubctl setup-secret` émet un secret aléatoire. La nouvelle ligne singleton `installation_setup` conserve son empreinte, son émission et l’état d’initialisation. Le responsable saisit le code et ses informations sur `/setup`. Le service `initialsetup` verrouille cette ligne, revérifie l’absence de tout rôle conférant `RolesManage`, vérifie le code, crée une `persons`, crée un `users` actif et activé, lui attribue `president`, efface l’empreinte et inscrit `initialized_at` dans **une même transaction**. Il renvoie ensuite à la connexion normale.

Le rôle initial est explicitement `president` pour la V1, mais le service vérifie avant génération et création que la politique actuelle lui confère réellement `RolesManage`. Le contrôle d’absence de gestionnaire utilise `authorization.RolesWith(RolesManage)` ; il ne dépend pas d’un nom de rôle dans sa requête. Le compte obtenu est un utilisateur normal, soumis ensuite au garde-fou P2.1.1.

## Modèle de données

La migration `0030_initial_setup.sql` crée `installation_setup`, limitée à une ligne par clé booléenne. Elle conserve `initialized_at`, `first_admin_user_id`, `secret_hash` (32 octets), `secret_issued_at` et un compteur de générations. Le compteur conserve la trace d’une rotation, sans conserver les anciennes empreintes. Une contrainte interdit une empreinte active après initialisation. Lors d’une mise à niveau, **toute base contenant déjà un utilisateur est marquée initialisée** : la migration n’ouvre donc pas `/setup` sur une installation existante. Une base vierge est non initialisée, mais sans code utilisable tant que l’opérateur n’en a pas émis un.

L’état lui-même trace la date et l’utilisateur initial créé. `administrative_events` n’a pas été détourné pour fabriquer un acteur système fictif. La migration refuse un rollback qui supprimerait un historique de génération ou un premier administrateur créé par setup. Le contrôle « base de démonstration vide » du seed exclut désormais `installation_setup`, qui est une métadonnée présente même sur une base métier vierge ; les protections contre les vraies données métier restent en place.

## Génération du secret

`crypto/rand` génère 32 octets, affichés comme 64 caractères hexadécimaux faciles à copier. Seul `SHA-256(secret)` est stocké en PostgreSQL ; les 256 bits d’entropie rendent une attaque hors ligne sur cette empreinte irréaliste. La comparaison utilise `crypto/subtle.ConstantTimeCompare`. Le secret est retourné à la commande locale **après commit** et imprimé une seule fois sur sa sortie standard, avec l’adresse `/setup`. Il n’est ni dans le dépôt, ni dans une URL, ni dans les logs applicatifs, ni dans les données de démonstration, ni dans ce rapport.

Un second `setup-secret` sans option refuse d’écraser le code. Avant initialisation uniquement, `setup-secret --rotate` remplace l’empreinte et invalide immédiatement l’ancien code ; l’ancien secret n’est pas récupérable. Après initialisation, les deux formes refusent toute émission. L’installateur doit remettre le code par un canal privé et éviter de rediriger la sortie vers un journal persistant.

## Route `/setup`

`GET /setup` présente un formulaire court si le code a été émis ; avant émission, il demande de contacter l’installateur. Après configuration, il redirige vers `/login`. Le formulaire demande code, prénom, nom, identifiant, email, mot de passe et confirmation. `POST /setup` ne met jamais le code dans l’URL, ne le réaffiche pas après erreur et ne conserve pas le mot de passe dans le HTML. Un code incorrect produit un message compréhensible ; une installation déjà configurée produit une page explicite. Un succès redirige vers `/login?setup=1`, puis l’authentification standard crée la session.

## Premier compte

La transaction crée une personne liée au nouvel utilisateur. Les noms respectent les limites utilisées par les identités du projet ; l’identifiant est court et explicite ; l’email est validé et conservé sur la personne et le compte. Le mot de passe utilise `auth.HashPassword` et donc bcrypt comme les autres comptes. `is_active=true` et `activated_at` est renseigné immédiatement. La possession du secret de configuration local remplace ici l’étape d’activation habituelle ; aucune vérification email artificielle n’est ajoutée. Le premier compte peut ensuite accéder à `/admin/users` et attribuer les rôles ordinaires.

## Sécurité

- État explicite en base : un simple `COUNT(admins)=0` ne rouvre jamais une installation initialisée.
- Secret fort, empreinte seule en base, comparaison à temps constant, aucun secret dans le GET, l’URL ou les réponses d’erreur.
- Validation et autorisation revérifiées dans la transaction finale ; la ligne singleton est verrouillée `FOR UPDATE`.
- Création de la personne, du compte, du rôle et consommation du code atomiques ; toute erreur annule l’ensemble.
- CSRF commun, protection d’origine, limite de taille du corps, cookies et en-têtes de sécurité existants.
- Limiteur local existant : 10 POST par IP sur 15 minutes et 120 POST globaux par minute, basé sur l’adresse du pair direct. Le code aléatoire de 256 bits constitue la protection principale contre le devinage.
- `clubctl grant-role` écrit désormais son attribution sous le même verrou d’installation que `/setup`, puis marque l’installation initialisée si un rôle gestionnaire est présent et efface un éventuel code en attente. Une révocation ultérieure ne peut pas rouvrir `/setup`.

## Protection contre la concurrence

`IssueSecret`, sa rotation, `Complete` et `clubctl grant-role` verrouillent la même ligne `installation_setup` en transaction. Deux POST valides avec le même code se succèdent au verrou : le premier crée le compte et efface l’empreinte ; le second voit `initialized_at` et reçoit « Cette installation a déjà été configurée ». Le test PostgreSQL lance deux navigateurs simultanément et constate un seul succès, un seul utilisateur, une seule personne et un seul rôle. Le chemin CLI ne peut pas intercaler une attribution entre la vérification et le commit web.

## CLI

`clubctl setup-secret` est réservé à l’opérateur technique après les migrations. `clubctl setup-secret --rotate` sert uniquement si le code a été perdu avant la première configuration. `grant-role`, `revoke-role` et `list-roles` demeurent disponibles pour la maintenance et la récupération exceptionnelle. Le rôle attribué localement ferme le setup, sans modifier la politique de rôles quotidienne.

## UX non technique

Le responsable reçoit une adresse et un code, ouvre `/setup`, remplit le formulaire, puis se connecte. Il ne manipule ni Docker, ni SQL, ni `clubctl`. La gestion suivante des accès reste dans **Administration → Utilisateurs**. Le README sépare maintenant cette procédure de l’installation technique et de la récupération.

## Test manuel

Un serveur Club Core réel a été lancé sur une base PostgreSQL 16 Testcontainers **vierge et migrée**, distincte de `clubcore_demo`. La vraie commande `clubctl setup-secret` a émis un code ; un second appel ordinaire a été refusé et `--rotate` a invalidé l’ancien. Le GET `/setup` a montré le formulaire. Le POST avec l’ancien code a répondu 422, avec zéro personne, utilisateur et rôle. Le POST avec le nouveau a répondu 303 vers `/login?setup=1`, avec exactement une personne, un utilisateur et un rôle. Ensuite, GET `/setup` a redirigé vers `/login` et le second POST a répondu 409. Le premier compte s’est connecté normalement et a ouvert `/admin` puis `/admin/users` (200).

Un second compte fictif a été créé dans **cette seule base isolée**, puis activé via la route existante `/activate` et connecté. `/persons` lui a d’abord répondu 403. Le premier administrateur lui a attribué « Secrétaire » depuis la route web P2.1.1 ; `/persons` a alors répondu 200. Le retrait du dernier rôle « Président » a été refusé avec le message de protection attendu. Une tentative de régénération du code après setup a échoué. Le serveur, la base et les fichiers temporaires ont été supprimés ; la démonstration persistante n’a pas été modifiée.

## Mobile

La vraie page `/setup` avant configuration a été rendue dans Chromium à 390 × 1000. Le formulaire est en une colonne, les libellés et champs sont lisibles, sans débordement horizontal observé. Le bas du formulaire se rejoint par défilement normal. La vue reprend la feuille de style et le layout communs ; aucun parcours mobile distinct n’a été ajouté.

## Tests

`initial_setup_integration_test.go` couvre : état vierge, absence de formulaire actif sans code, empreinte seule en base, émission unique, rotation, mauvais code, absence de lignes partielles, mauvais mot de passe/email/username, CSRF, création complète, hash bcrypt, activation immédiate, rôle `president`, fermeture durable de `/setup`, code consommé, connexion, attribution web d’un second rôle, protection du dernier gestionnaire, deux POST concurrents, identifiant dupliqué, limiteur de tentatives et clôture après attribution locale d’un rôle gestionnaire. Les suites P1, P2.1 et P2.1.1 restent exécutées.

## Vérifications

- `go test ./...` : réussi, y compris PostgreSQL/Testcontainers, le setup et les suites P1/P2.1/P2.1.1.
- `go vet ./...` : réussi.
- `git diff --check` : réussi.
- Aucun script shell n’a été modifié ; `bash -n` ne s’applique pas.

## Limites restantes

La génération du code appartient encore à l’installateur et nécessite `DATABASE_URL` ainsi que la commande locale ; l’association n’en a pas besoin. Le code est affiché sur la sortie standard : l’environnement d’installation doit éviter une capture persistante de cette sortie. Le limiteur est en mémoire par processus ; la très forte entropie du code évite de dépendre de lui comme protection principale. Une installation déjà dotée d’utilisateurs lors de la migration est fermée au setup web par prudence et reste soumise à la procédure technique de récupération. La récupération autonome de mot de passe par email n’existe toujours pas. Le seed de démonstration, l’installation Docker complète, les invitations et les permissions personnalisées restent hors périmètre.

## Suite recommandée

**Oui**, pour une installation techniquement préparée et dont le code a été remis de manière privée, une association peut désormais prendre possession de Club Core sans terminal : elle crée son premier administrateur dans le navigateur et administre ensuite les rôles dans l’interface. L’installateur garde la responsabilité de la base, des migrations et de la transmission du code ; une procédure séparée reste nécessaire pour un mot de passe administrateur perdu. Le prochain chantier produit peut être **P2.2 — administration de l’organisation**. Il n’a pas été commencé ici.
