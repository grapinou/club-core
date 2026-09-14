# Campagne réelle multi-utilisateurs — reprise du 14 septembre 2026

## 1. État trouvé et audit initial

HEAD : `6ff3788 Add administrative space`. Les cinq commandes demandées ont été exécutées dans l’ordre avant toute modification : `git status --short`, `git diff --stat`, `git diff --check`, `git diff`, `git log --oneline -8`.

```text
 M .gitignore
?? cmd/multi-user/
```

Le seul diff suivi ajoutait trois lignes à `.gitignore` pour exclure `/runtime/multi-user/`. Le fichier non suivi `cmd/multi-user/main.go` a été lu intégralement. Aucun autre ajout local, aucun répertoire runtime, aucun rapport de campagne ou équivalent plus récent, aucune ancienne preuve visuelle disponible dans /tmp. Aucun AGENTS.md applicable trouvé. Les quatre rapports personnel/familial, self-service, administration et RBAC demandés ont été lus. Les recherches dans cmd/scripts/docs/configuration ont confirmé l’absence d’un second mécanisme de campagne.

Le `go test ./...` initial échouait exclusivement à la compilation du préparateur : import pgx inutilisé, HostIP désormais de type netip.Addr, méthode Port.Int inexistante. Le package application a cependant terminé avec succès (213,332 s). Les rapports antérieurs attestent leurs suites d’intégration, concurrence, race et captures ; ils indiquent explicitement que cette campagne n’avait pas commencé. Aucune preuve de campagne réussie avant cette reprise n’a été retrouvée.

## 2. Travail antérieur conservé

Le préparateur existant est conservé : commandes init/start, fichier privé d’identités, PostgreSQL Testcontainers, migrations Goose, Mailpit local épinglé par digest, comptes activés par le domaine, rôles via clubctl, relation familiale sans grant, données Membership/Trial, commandes grant/revoke et grant-role/revoke-role, audit sans données privées. Aucun nouveau moteur de fixture, aucune refonte du domaine ou de l’application.

Les fonctionnalités administratives déjà committées restent inchangées. Aucun commit, push, reset, clean, checkout, stash ou effacement de fichier local effectué.

## 3. Environnement et reproductibilité

Exécution réelle de l’application Go sur `http://localhost:8090`, PostgreSQL 16 Alpine jetable, migrations du dépôt et Mailpit. Ports Docker explicitement liés à 127.0.0.1, serveur HTTP également limité au loopback. Chaque start crée une nouvelle base et un nouveau sous-répertoire privé run-*. Le marqueur campaign_fixture_guard et la connexion loopback/base club_campaign protègent les commandes locales contre une autre base.

Plusieurs démarrages complets ont réussi pendant la mise au point du harnais. Les anciens processus de campagne ont reçu SIGTERM ; leur gestionnaire arrête uniquement leurs conteneurs jetables. Leurs fichiers privés et preuves ont été conservés. Le port occupé produit un refus ; aucun serveur tiers n’est arrêté par start. PostgreSQL, migrations, activation des comptes et semis sont donc effectivement reproductibles.

## 4. Injection locale des identités et lancement

Depuis la racine :

```bash
go run ./cmd/multi-user init
# Éditer localement runtime/multi-user/identities.json si souhaité.
go run ./cmd/multi-user start
```

`init` crée un objet Emails avec les clés A/B/C et des adresses fictives, mode 600 ; il refuse tout écrasement. Le répertoire est créé en mode 700. Les adresses sont validées sans réaffichage. Aucun besoin de fournir une vraie adresse pour démontrer les autorisations. Cette campagne utilise uniquement les valeurs fictives ; aucune adresse réelle reçue ou injectée.

Les mots de passe aléatoires et la connexion restent dans le state.json privé dont start affiche seulement le chemin. Ne pas copier son contenu dans Git, terminal partagé, rapport ou conversation. `git check-ignore` confirme l’exclusion d’identities.json et state.json ; modes 700/600 vérifiés. Les scripts ne contiennent aucune identité réelle ou secret fixe. Les erreurs Playwright brutes ne sont pas imprimées, car leurs journaux pourraient reprendre les valeurs remplies.

Le navigateur se lance avec Python Playwright installé localement et Chromium :

```bash
python3 scripts/multi-user-campaign.py runtime/multi-user/run-EXEMPLE/state.json
```

Remplacer le chemin par celui annoncé au démarrage. Ici l’interpréteur disponible après installation locale est `runtime/multi-user/venv/bin/python`. Python système n’avait ni Playwright ni ensurepip ; les dépendances ont été installées exclusivement dans runtime ignoré. Chromium était déjà présent dans le cache utilisateur. Prérequis habituels : Go, Docker, Python/Playwright et navigateur Chromium ; accès au CDN Bootstrap existant pour le contrôle de son chargement.

Le script effectue des mutations et doit être lancé **une fois sur chaque base fraîche**. Un deuxième lancement sur une campagne déjà traitée n’est pas une opération idempotente. Le redémarrage crée une nouvelle base ; il ne réinitialise pas une base métier existante.

## 5. User A

Compte adulte activé, sans rôle administratif, Membership initialement pending. Vrai formulaire de connexion ; /me redirige vers son dashboard, compte propre et Membership personnelle accessibles. Modification de l’adresse par le formulaire self-service, entièrement sans JavaScript. /admin refusé 403. A n’a aucun grant pour l’enfant de C et reçoit 404.

## 6. User B

Compte distinct, sans adhésion personnelle, secretary attribué explicitement par le clubctl existant. Accès /admin, /persons, fiche de A, /trials, détail d’essai, Membership, notes et groupes. Le compte et /me restent ceux de B. L’URL personnelle de la Membership de A est refusée 404 malgré son rôle ; l’enfant de C aussi.

Une query person_id visant A sur /me ne change pas l’identité. Le formulaire self-service de B a également été soumis avec person_id et user_id forgés visant A : seules les coordonnées de B changent, l’adresse de A reste celle enregistrée par A.

## 7. User C et enfant

C est adulte, actif, activé, sans rôle administratif, avec une Membership active. L’enfant est mineur et ne possède aucun User. Relation PersonGuardian et contact d’urgence créés ; grant initialement absent. Aucun rôle guardian n’est inventé.

## 8. Sessions réellement séparées

Trois contextes Chromium Playwright indépendants, trois pages, trois connexions avec les formulaires /login réels. Aucun transfert ni remplacement manuel de cookie. Distinction des cookies de session vérifiée en mémoire sans les imprimer ni les sauvegarder. Les trois contextes restent ouverts pendant toutes les opérations et révocations ; leurs cookies de session initiaux restent identiques en fin de parcours.

A fonctionne avec JavaScript désactivé pendant l’intégralité de la campagne. B et C utilisent chacun leur contexte normal. Aucun endpoint d’impersonation, aucun changement d’identité dans les cookies.

## 9. Self-service A → administration B

A saisit une adresse fictive et soumet /me/account/profile. B ouvre /persons, recherche User A par POST, clique sa fiche existante : la nouvelle adresse est visible. Ni recréation de Person ni reconnexion. La même donnée apparaît aussi dans le dossier administratif Membership. Le test d’IDs forgés décrit plus haut confirme ensuite que B ne peut modifier A par le self-service.

## 10. Notes administratives

B écrit une note Person contenant une balise script fictive et une sentinelle. Le HTML est échappé, aucun script supplémentaire injecté. B fournit également une note à l’approbation de la Membership puis la modifie par l’écran de notes dédié.

La sentinelle est absente du HTML rendu personnel de A sur /dashboard, /me, /me/account et sa Membership. Les captures montrent les notes uniquement côté bureau, en texte inerte. Les données d’audit ne contiennent pas le contenu des notes.

## 11–12. GuardianAccess et révocation

Relation seule : C reçoit 404 sur la page enfant, tout comme A et B. La commande locale grant authentifie B avec son propre compte, construit son contexte authentifié via les sessions existantes et appelle guardianaccess.Grant ; elle ne manipule pas les sessions navigateur et n’expose aucune route HTTP.

Grant actif : C reçoit 200 sur la page enfant et sa Membership, voit l’enfant dans son dashboard et reste sans accès administratif. A et B conservent leur refus 404.

Révocation via le domaine : la requête suivante du même contexte C reçoit 404, puis la Membership enfant reçoit aussi 404. Son propre /me et compte restent utilisables, /admin reste 403. Le refus est le 404 texte minimal existant : aucune identité ou donnée enfant divulguée.

## 13–14. RBAC et révocation secretary

Dans le contexte B toujours connecté, /admin est accessible. La commande revoke-role passe par clubctl et retire secretary en base. La prochaine requête /admin reçoit 403, la fiche Person de A reçoit 403, la navigation administrative disparaît. /me et /me/account restent ceux de B et accessibles ; l’enfant reste interdit.

Réattribution explicite de secretary en fin de campagne : /admin retourne 200, l’enfant reste 404. Les permissions n’ont donc pas été figées dans la session et n’accordent pas GuardianAccess.

## 15. Membership traitée par B

B approuve la demande pending existante de A, avec note administrative, modifie cette note, puis affecte explicitement le groupe et la date du jour via l’écran existant. A retrouve immédiatement une adhésion Active, la saison/type, l’activité, le groupe et son créneau, ainsi que le consentement refusé. A ne reçoit ni note administrative ni audit ni coordonnées d’autrui.

## 16. Trial → Membership

B ouvre l’essai de A et suit son lien de création de Membership. Il sélectionne explicitement la saison suivante, le type et le donneur A, conserve l’activité préremplie et choisit Refusé pour le consentement. Création pending réussie ; cette deuxième adhésion est visible dans l’espace de A. Aucun groupe automatique.

Audit SQL final : toujours quatre Persons, trois Users, aucun User enfant ; Trial toujours registered et conservé dans la fiche. La nouvelle Membership réutilise la Person A. Le consentement refusé est visible chez A. Aucun statut Trial ni consentement automatique ajouté.

## 17. Matrice réellement observée

| État | /me propre | Enfant de C | /admin |
|---|---|---|---|
| A normal, sans grant | oui | 404 | 403 |
| B secretary, sans grant | oui | 404 | 200 |
| C avec relation seule | oui | 404 | 403 |
| C avec grant actif | oui | 200 | 403 |
| C après révocation grant | oui | 404 | 403 |
| B après révocation secretary | oui | 404 inchangé | 403 |

Les réponses protégées testées portent Cache-Control: no-store, y compris les refus. Navigation administrative absente chez A/C et chez B après révocation. Aucun changement de l’identité affichée dans les comptes et dashboards. Le header commun montre le nom du site, pas une identité connectée ; les fiches administratives désignent naturellement la Person cible.

## 18. Mail

Décision antérieure conservée : Mailpit local par défaut ; aucune configuration arbitraire d’un SMTP réel. L’option SMTP du fichier privé reste disponible et validée par le mailer existant. Aucun SMTP externe utilisé et aucune livraison à une vraie boîte testée. Le semis active les comptes par le domaine sans envoyer leurs codes ; l’approbation de A déjà activé ne nécessite pas d’email. Cette campagne vérifie les autorisations et les données partagées, pas la délivrabilité externe.

## 19–21. Bugs, corrections et tests ajoutés

Trois erreurs de compilation du préparateur reproduites par la suite initiale puis corrigées minimalement : retrait de l’import pgx inutilisé, netip.MustParseAddr pour HostIP, int(port.Num()) pour SMTP. Aucune correction de code métier nécessaire.

Deux tests Go ajoutés au préparateur : protection des fichiers privés (non-écrasement, refus des permissions publiques et symlinks), maintien des ports Docker sur loopback sans changement de port. La compilation du package couvre les adaptations de types.

Ajout du harnais navigateur spécifique `scripts/multi-user-campaign.py` pour rendre les scénarios et preuves reproductibles. Ses premières assertions de chargement CSS supposaient un max-width mobile et un display de bouton particuliers ; elles ont été corrigées pour vérifier la variable Bootstrap effectivement chargée. Les pages 404 texte n’ont pas de Bootstrap et sont exclues de cette assertion, mais restent contrôlées pour statut, confidentialité et overflow. Ces échecs étaient des hypothèses du harnais, pas des bugs applicatifs. Les scénarios métier déjà réussis ont été rejoués sur base fraîche.

## 22. Contrôle visuel

22 captures pleine page, viewports 390×900 et 1365×900 : A dashboard/compte/Membership ; B admin/Persons/fiche A/Membership A/Trials ; C dashboard/enfant autorisé/enfant révoqué. Inspection des captures et planches locales : hiérarchie et navigation lisibles, notes inertes côté bureau, états vides explicites, boutons et textes sans débordement. Aucun overflow de document sur les 22 rendus. Bootstrap chargé sur toutes les pages du layout normal. Parcours A complet sans JavaScript.

Le 404 familial reste une page texte anglaise minimale, sans navigation. C’est une limite UX constatée, cohérente avec le refus uniforme existant ; aucun changement UI ajouté à ce jalon. Pas d’audit WCAG formel ni matrice multi-navigateurs.

## 23. Validations techniques finales

- `gofmt -w cmd/multi-user/main.go cmd/multi-user/main_test.go` : réussi.
- `sqlc generate` : réussi ; empreintes SHA-256 de tous les fichiers générés identiques avant/après.
- `go test ./...` après correction : réussi, application 213,749 s ; dernière exécution incluant les nouveaux tests réussie (cache pour les packages inchangés).
- `go test -race ./cmd/multi-user` : réussi, 1,085 s. Aucun package métier modifié ; les contrôles race historiques étendus restent documentés dans le rapport administratif.
- `git diff --check` : réussi.
- Git ignore et permissions des fichiers privés vérifiés.

Passage navigateur final : **285 contrôles réussis**, après un premier passage complet à 254 contrôles. Les assertions supplémentaires portent sur /me propre après révocation, les IDs forgés en POST et la conservation des cookies de session.

Journaux techniques sans secret : `/tmp/club-campaign-final-tests.log`, `/tmp/club-campaign-race.log`. Les preuves navigateur et l’audit métier se trouvent uniquement sous le répertoire runtime privé indiqué ci-dessous.

## 24. État de transmission et limites

La campagne automatisée utilise de vraies connexions HTTP et trois sessions navigateur avec identités fictives. Elle ne prétend pas avoir été effectuée manuellement par trois personnes ni avec les vraies adresses de l’utilisateur. Leur injection locale reste possible avant un prochain start, sans modifier les fixtures versionnées.

Dernière campagne : `runtime/multi-user/run-4185820453/state.json`. Preuves : `runtime/multi-user/run-4185820453/browser/checks.json`, `domain-audit.json` et 22 captures PNG. Les 22 captures du passage complet précédent ont été inspectées en six planches dans `runtime/multi-user/run-1557032131/browser/` ; les captures finales fiche A desktop et refus enfant mobile ont également été revues, sans changement UI. Le serveur final reste démarré sur localhost:8090 pour une revue locale ; les contextes automatisés ont été fermés après validation. Les accès de connexion sont uniquement dans le fichier privé. Le serveur et sa base sont éphémères : si le processus est arrêté, relancer start pour une nouvelle campagne.

État métier final : A possède une adhésion active avec groupe et une demande pending de saison suivante ; B a de nouveau secretary ; C a conservé sa relation familiale mais son grant est révoqué. L’enfant n’a aucun compte. Les scénarios de majorité, concurrence et rollback restent couverts par les suites existantes, sans duplication de ces tests dans le harnais.

Pour les commandes de démonstration, remplacer STATE par le chemin privé annoncé :

```bash
go run ./cmd/multi-user grant STATE
go run ./cmd/multi-user revoke STATE
go run ./cmd/multi-user revoke-role STATE
go run ./cmd/multi-user grant-role STATE
go run ./cmd/multi-user audit STATE
```

Aucun travail fonctionnel hors périmètre commencé. La copie reste non committée pour revue manuelle.

## 25–26. Fichiers et état Git final

Conservé/modifié : `.gitignore` et `cmd/multi-user/main.go` préexistants localement. Créés : `cmd/multi-user/main_test.go`, `scripts/multi-user-campaign.py`, ce rapport. Aucun fichier métier, migration ou fichier sqlc modifié. Les fichiers runtime privés ne figurent pas dans Git.

```text
 M .gitignore
?? cmd/multi-user/
?? docs/reports/2026-09-13-multi-user-real-campaign.md
?? scripts/
```
