# P2.1.1 — Utilisateurs, rôles et premier administrateur

Date : 25 septembre 2026.

## État initial

Le dépôt contenait déjà des modifications non commitées de P2.1 dans l’administration, ses requêtes, ses tests, le CSS et un rapport. Elles ont été conservées. HEAD était `4a5af3c`. `users` référence exactement une `persons` par `person_id`, porte `username`, un éventuel `login_email`, `is_active`, `activated_at` et `password_hash`. L’authentification utilise le username, bcrypt, une session liée à l’empreinte du hash et refuse un compte inactif ou non activé. La vérification email concerne les inscriptions publiques ; l’activation d’un compte utilise un code à usage unique généré après approbation d’adhésion ou création d’accès responsable. Le changement de mot de passe et d’email existe dans « Mon compte », avec mot de passe actuel. Aucune récupération autonome par email d’un mot de passe perdu n’existe.

Les tables `roles` et `user_roles` et les rôles `president`, `secretary`, `treasurer`, `coach` existaient. La politique de permissions est en code dans `internal/authorization`, sans cache. `clubctl grant-role`, `revoke-role`, `list-roles` manipulent ces mêmes tables depuis un accès PostgreSQL local. Aucun écran de gestion des rôles ni premier compte administrateur automatique n’existait. Sur une base vierge, `clubctl grant-role` exige déjà un compte : cette commande seule ne résout donc pas la création initiale du compte.

## Audit de sécurité

`RolesRead` et `RolesManage` étaient déjà définies. `president` possède les deux ; `secretary` gère personnes, adhésions, activation et revue des inscriptions, sans droit de gestion des rôles ; `treasurer` et `coach` n’ont pas ces capacités administratives dans la politique actuelle. Les handlers existants appliquent `RequirePermission` et le CSRF commun ; les services sensibles revérifient l’acteur. Le journal `administrative_events` conserve acteur, action, ressource et date. Les requêtes de liste et fiche ajoutées ne sélectionnent jamais le hash ou les codes.

Le service `useraccess` revérifie l’état du compte et `RolesManage` dans sa transaction de modification. Il écrit `user_roles` et `administrative_events` dans le même commit. La migration 0029 étend le journal existant avec les actions `role_granted` / `role_revoked`, la ressource `user` et `role_name`. Une mutation sans effet n’ajoute pas un faux événement. Les routes POST ont la même protection CSRF et les mêmes cookies que les autres formulaires administratifs. L’affichage des liens reste une simple indication ; les routes et le service protègent réellement l’accès.

## UX retenue

« Utilisateurs » figure dans la navigation administrative des comptes ayant `RolesRead`, ainsi que dans leur espace personnel. La liste présente personne, identifiant, email de connexion s’il existe, état du compte et rôles en français. Une recherche simple couvre nom, identifiant et email, avec pages de 50 comptes. La fiche montre l’identité et chaque rôle avec « Attribuer le rôle » ou « Retirer le rôle ». Les actions sont des formulaires courts, explicites, sans jargon de permission. Le compte en attente d’activation et le compte désactivé sont distingués.

## Permission de gestion des rôles

La politique déjà présente est conservée : `president` dispose de `RolesRead` et `RolesManage`, `secretary` n’en dispose pas. C’est une décision de délégation sensible déjà inscrite dans le projet, indépendante d’une règle spéciale BSO. La capacité reste associée à une permission ; le contrôle de verrouillage demande à la même politique quels rôles la confèrent. Une évolution ultérieure de la politique ne nécessite pas de coder le nom `president` dans le service.

## Protection du dernier gestionnaire

Les modifications web prennent un verrou consultatif PostgreSQL transactionnel commun. Après retrait, le service compte les utilisateurs **actifs, activés, dotés d’un mot de passe et possédant un rôle qui confère `RolesManage`**. Si le nombre devient zéro, la transaction est annulée et la fiche explique la conséquence. Ce calcul fonctionne pour son propre compte comme pour un autre. Deux retraits simultanés sont sérialisés : un seul peut réussir si deux gestionnaires existaient. Le test d’intégration vérifie le retrait refusé du dernier, puis accepté après attribution à un second compte, ainsi que deux retraits concurrents.

## Premier administrateur

Le bootstrap web **n’a pas été implémenté**. L’installation ne contient ni secret de prise de contrôle généré par l’installeur, ni marqueur de configuration initiale, ni procédure garantissant un canal fiable pour authentifier l’opérateur avant l’existence d’un gestionnaire. SMTP est facultatif. Une route ouverte basée sur « aucun administrateur n’existe » permettrait au premier visiteur Internet de s’emparer du site. L’activation actuelle ne prouve pas à elle seule que son titulaire est l’installateur.

Sur une installation vierge, aucun compte administrateur n’est créé et aucune route `/setup` n’est ouverte. La prise de contrôle initiale demande temporairement un opérateur ayant accès local à PostgreSQL : créer une personne et un compte selon le modèle existant, l’activer par une procédure locale sûre, puis utiliser `clubctl grant-role <username> president`. Il ne faut pas attribuer automatiquement le rôle au premier inscrit. Le parcours exact de création et d’activation du premier compte n’est **pas encore outillé** : c’est le verrou produit principal restant, et il peut actuellement nécessiter SQL. Le workflow quotidien des rôles après cette étape est web.

Prochaine étape précise : faire générer par l’installation technique un secret de setup aléatoire à usage unique, stocké seulement sous forme d’empreinte et communiqué sur un canal local à l’opérateur. Une page web demanderait ce secret, créerait ou activerait le premier compte via les primitives existantes et attribuerait un rôle gestionnaire dans une transaction verrouillée qui vérifierait à nouveau l’absence de gestionnaire et consommerait le secret. Le secret ne serait ni dans les logs ni dans le dépôt ; la route serait fermée après succès. Tester redémarrage, seconde visite et demandes concurrentes avant mise en production. Cette architecture n’est pas improvisée dans P2.1.1.

## CLI

`grant-role`, `revoke-role` et `list-roles` restent disponibles avec les mêmes tables et valeurs que l’interface. Ils sont des outils locaux privilégiés de bootstrap, maintenance et récupération exceptionnelle ; la CLI écrit directement dans `user_roles` et ne suit pas le garde-fou web ni l’audit HTTP, ce qui impose une procédure d’exploitation contrôlée. Si le dernier administrateur existe mais a perdu son mot de passe, aucun reset autonome n’existe : l’accès local à la base reste nécessaire pour rétablir un compte activable ou autorisé. « Mon compte » permet seulement le changement avec mot de passe actuel.

## Généricité

Aucune règle BSO, personne, email ni mot de passe de démonstration n’a été ajouté. Les données existantes de `clubcore_demo` ne sont ni vidées ni modifiées par le chantier ; le seed n’a pas été touché.

## Tests

`users_roles_integration_test.go` utilise PostgreSQL 16/Testcontainers et le vrai handler HTTP. Le second compte suit d’abord l’approbation d’adhésion et l’activation par code existantes. Le test vérifie ensuite : anonyme redirigé, compte sans rôle refusé, gestionnaire admis, navigation et recherche, rôles visibles, absence de hash/secret, CSRF, attribution persistée avec effet immédiat sur l’accès, retrait avec perte immédiate de permission, garde du dernier gestionnaire, second gestionnaire, audit acteur/cible/rôle/action, et retraits concurrents. Les tests préexistants couvrent toujours P1 et P2.1.

## Test manuel

Le scénario A de première installation web ne s’applique pas, faute de bootstrap web. Un serveur HTTP réel a été lancé sur une base PostgreSQL Testcontainers isolée, avec deux comptes fictifs et temporaires. Une session HTTP séparée par compte a permis de constater : anonyme redirigé vers `/login` ; compte sans rôle refusé sur `/admin/users` et `/persons` (403) ; liste administrateur (200) avec les deux noms et « Président » ; attribution de « Secrétaire » (303) puis accès de ce compte à `/persons` (200) ; retrait (303) puis refus (403) ; tentative de retirer le dernier « Président » avec message compréhensible et accès administrateur conservé (200). L’approbation d’adhésion et l’activation par code du second compte ont été exercées dans le test d’intégration, pas dans ce contrôle HTTP manuel. Le serveur, sa base et le harnais temporaires ont été arrêtés et supprimés. `clubcore_demo` n’a pas été touchée.

## Mobile

Les templates réels de liste et fiche ont été inspectés dans Chromium à 390 × 900. La fiche a aussi été inspectée à 390 × 1500 pour voir les quatre actions, puis à 1365 × 1200 sur bureau. Les cartes, textes et boutons restent lisibles et accessibles, sans débordement observé. Ce contrôle visuel statique complète les scénarios HTTP ; il ne mesure pas un parcours tactile réel.

## Vérifications finales

- `go test ./...` : réussi, y compris la suite PostgreSQL/Testcontainers.
- `go vet ./...` : réussi.
- `git diff --check` : réussi.
- `scripts/run-dev.sh` : inchangé ; `bash -n` non applicable.

## Limites restantes

La création du premier compte et le bootstrap web sûr restent à construire. L’écran ne crée pas directement de compte et ne l’active/désactive pas : l’approbation d’adhésion et l’activation existantes restent le parcours normal de compte. La CLI de secours peut contourner le garde-fou de dernier gestionnaire et son usage n’est pas audité par `administrative_events`. La liste de rôles reflète la politique actuelle, sans éditeur de permissions individuelles.

## Suite recommandée

La gestion quotidienne des rôles est maintenant réalisable sans terminal. La prise en main initiale d’une association non technique **n’est pas suffisamment simple** tant que le secret de setup et le premier compte ne disposent pas d’un parcours sûr et complet. Résoudre ce verrou avant d’affirmer que Club Core peut être installé sans accompagnement technique. P2.2 — administration de l’organisation — peut être planifiée, sans la commencer ici, mais sa livraison ne doit pas masquer cette dépendance de première installation.
