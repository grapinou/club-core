# Consolidation UX — 12 septembre 2026

## Diagnostic initial, établi avant les modifications d’interface

Dépôt initial propre ; HEAD `96c43ba Add public child registration`, également confirmé sur le distant par `git ls-remote`. Les trois premières commandes demandées ont été exécutées dans l’ordre ; `go test ./...` passait.

Inspection avec Chromium/Playwright sur le serveur Go réel, PostgreSQL 16 isolé, données fictives (administrateur, membre, responsable avec grant actif, enfant, adhésions pending, demande enfant avec responsable à résoudre). SMTP désactivé. Aucun template n’a été modifié avant cette inspection. Captures avant dans `/tmp/club-ux-before`, résultats de navigation dans `audit.json`.

Pages inspectées à 390 et 1365 px : `/`, `/club`, `/where`, `/when`, `/contact`, `/join`, `/join/child`, les deux pages submitted, `/registration/verify`, `/login`, `/activate` ; après connexion : `/`, `/login`, `/memberships`, `/memberships/2`, `/registration-reviews`, `/registration-reviews/1`, `/persons`, `/persons/new`, `/persons/3/edit`, `/persons/archived`. `/dashboard` répondait 404 : aucun tableau de bord existant.

| Priorité | Domaine | Observation réelle | Conséquence |
|---|---|---|---|
| Bloquant | Navigation mobile | Menu Bootstrap fermé, liens inaccessibles sans JavaScript | Adhérer et connexion ne sont plus accessibles depuis le menu |
| Bloquant | Personnes mobile | Table non responsive, débordement de la page à 390 px | Coordonnées/actions hors écran |
| Important | Navigation et hiérarchie | Liens publics et admin sur une seule ligne, Connexion persiste après login | Espace personnel absent, repères ambigus |
| Important | Revue admin | Titre « Résolue » et alerte verte avant une ambiguïté responsable encore ouverte | Mauvaise lecture de ce qui reste à traiter |
| Important | Densité des listes | Adhésions à 8 colonnes, vérifications à 6 ; action requise loin à droite | Lecture mobile difficile, priorité peu claire |
| Important | Formulaires | Responsable/enfant séparés mais emails intitulés simplement Email ; lien et urgence mélangés aux coordonnées | Charge cognitive et attribution de données moins évidente |
| Important | Largeurs | Formulaires administratifs et responsable étirés sur tout le conteneur desktop | Parcours de lecture trop large |
| Important | Vocabulaire/statuts | guardian, User, Person, Membership, awaiting_review, mother, strong visibles | Jargon inutile pour l’utilisateur |
| Important | États vides/succès | Tables vides ; submitted presque uniquement une alerte | Suite du parcours peu guidée |
| Important | Accessibilité | Erreurs responsable non associées aux champs ; aucune entrée d’évitement | Corrections et navigation clavier moins faciles |
| Polish | Cohérence | Titres, boutons créer/modifier, cartes auth et espacement variables | Harmonisation utile sans refonte graphique |
| À préserver | Sécurité/actions | CSRF et protection backend présents ; blocages Membership visibles | Ne pas affaiblir les contrôles en réorganisant l’interface |

Le défaut de photo de club (image de démonstration configurée) est éditorial, pas un problème à résoudre par une nouvelle illustration. Les descriptions de consentement doivent rester lisibles intégralement, avec acceptation/refus séparés. Aucune simplification métier n’est prévue.

## Diagnostic de reprise

La reprise a commencé par les six commandes demandées : status, diff stat, diff check, diff intégral, log, tests. Aucun reset/restore/checkout destructeur. État trouvé : 11 fichiers suivis modifiés et 7 nouveaux fichiers. Le diagnostic et toutes les captures initiales ont été conservés.

Déjà présents : navigation publique simplifiée, bandeau administratif séparé avec droits backend, accueil avec CTA, CSS partagé (largeurs, sections, listes mobiles, focus), mapping de statuts, dashboard en lecture seule et deux queries SQL de projection, sans migration. Dashboard revérifié au navigateur avec les comptes membre, responsable et administrateur, à 390 px et JavaScript désactivé : composition correcte, aucun lien admin pour le responsable/membre, enfant réellement gérable affiché.

Non encore terminés à la reprise : application des composants aux formulaires et listes/détails, traductions dans les templates de revue, écrans auth, correction de la liste Persons mobile, tests UX dédiés et contrôle visuel final. Le CSS responsive existait mais les listes n’utilisaient pas encore la classe partagée. Aucun test UX ajouté avant la reprise. Le rapport ne contenait que le diagnostic initial.

La suite de reprise compilait ; tous les packages hors application passaient. Les échecs application provenaient d’assertions de redirection restées sur `/` dans trois tests/helpers alors que la connexion vise désormais `/dashboard`. Ils bloquaient en cascade les tests administratifs. Les attentes d’affichage des statuts techniques ont également été adaptées à leur traduction française. Les assertions métier, de sécurité et les données contrôlées ne sont pas supprimées. Aucun échec préexistant n’a été identifié : le commit initial était testé vert avant le chantier.

## Navigation et dashboard terminés

Navigation publique : Accueil, Le club, Où / Quand (pages existantes reliées entre elles), Adhérer, Contact, Connexion. Le menu reste affiché et revient à la ligne sur mobile, y compris sans JavaScript. Connexion devient Mon espace après authentification. Le bandeau Administration est distinct et chaque entrée dépend des permissions calculées par le backend.

GET `/dashboard` compose les adhésions personnelles, les enfants gérables et les raccourcis administratifs. La connexion redirige vers ce tableau de bord. L’identité personnelle vient exclusivement de la session ; la liste enfant vient exclusivement de `GuardianAccess.ListManagedChildren`. Les liens historiques sans grant, grants révoqués, enfants étrangers et personnes devenues majeures sont exclus. Aucune route administrative de dossier n’est proposée aux membres ou responsables sans droit correspondant. Les sections se composent pour une personne simultanément membre, responsable et secrétaire.

Deux projections SQL de lecture ajoutées dans `dashboard.sql` : identité du compte courant et résumés d’adhésions (saison, type, statut, activités). Code sqlc généré. Aucune migration, aucune modification des services métier. Le tableau de bord ne crée ni compte, ni adhésion, ni permission.

## Formulaires publics et identité

`/join` et `/join/child` partagent les cartes d’orientation « Je m’inscris » / « J’inscris mon enfant », une largeur de lecture limitée, des sections numérotées, le récapitulatif et les actions Modifier / Envoyer ma demande.

Adulte : informations, adhésion, activités, consentements, récapitulatif. Enfant : responsable, enfant, lien, adhésion et activités, urgence, consentements, récapitulatif. L’email obligatoire du responsable et l’email facultatif de l’enfant ont des libellés explicites ; les sections d’autocomplétion sont distinctes. Les erreurs du responsable, du lien et du contact d’urgence sont associées aux champs. Les valeurs, noms des champs, contrôles, nonce et présentation signée sont conservés.

Chaque consentement garde le titre, la version, le texte intégral et deux réponses indépendantes. Le refus est explicitement autorisé ; aucune réponse globale n’est ajoutée. Les décisions restent visibles au récapitulatif.

Les deux pages submitted présentent « Demande reçue », les prochaines étapes, la messagerie à surveiller et le contact du club. Elles conservent une réponse générique sans résultat de matching ni information sur une relation existante.

Login, activation et vérification partagent une carte étroite et les styles d’alerte/actions. Les mécanismes de mot de passe, activation et vérification restent inchangés.

## Administration

- **Liste des adhésions** : quatre colonnes Personne, Adhésion, État du dossier, Action requise ; statut principal unique, blocages et compte à activer lisibles. Les demandes en attente restent en tête selon l’ordre existant.
- **Détail adhésion** : blocages en tête, liens vers les sections, identité et adhésion, activités, responsables, urgence, consentements complets, compte et actions administratives. Les relations sont présentées en cartes ; l’approbation conserve ses contrôles et son état désactivé quand le dossier est incomplet.
- **Liste des vérifications** : personne, raison/type, ancienneté/date et action. Les dossiers encore ouverts sont distingués des vérifications email et de l’historique selon les informations existantes.
- **Détail vérification** : « Ce qui reste à vérifier » avant les identités, correspondances, responsable/relation, adhésion et consentements. L’historique de résolution vient en dernier. Une identité enfant résolue ne masque plus un responsable ambigu. Les actions de rattachement, création et confirmation restent près du problème. Les correspondances secondaires peuvent se replier avec `details` natif ; celles nécessitant une action sont ouvertes. Aucun contrôle backend n’est retiré.
- **Personnes** : liste responsive avec toutes les coordonnées conservées, formulaires de largeur limitée, archives avec état vide. Boutons d’écriture selon permission backend ; archivage visuellement distinct.

## Composants, vocabulaire et accessibilité

CSS partagé minimal au-dessus de Bootstrap : en-tête, section, largeur de lecture/authentification, cartes de choix, actions, état vide, focus, listes responsive. Aucun framework ou bibliothèque CSS supplémentaire. Mapping central `DisplayStatus` pour les statuts et helpers de traduction des relations, correspondances et résolutions. Les termes techniques ne doublent plus systématiquement le français.

États vides explicites pour adhésions personnelles, enfants gérables, demandes en attente, vérifications à traiter et personnes archivées. Aucun CTA vers un portail inexistant.

Lien d’évitement vers le contenu, focus visible, labels, fieldset/legend, hiérarchie de titres et erreurs associées aux champs. Les liens naviguent ; les formulaires POST restent des boutons avec CSRF. Sur mobile, les listes principales deviennent des blocs avec libellés de colonne ; les tables restantes disposent d’un conteneur responsive. Les textes longs peuvent revenir à la ligne. Desktop : largeur limitée des formulaires et des textes, données administratives réparties en sections.

Sécurité conservée : permissions backend, CSRF, PRG, no-store, CrossOriginProtection, limites de corps et échappement html/template. Aucun nouveau traitement métier de consentement, résolution, approbation ou sécurité des mineurs.

## Contrôle visuel final réel

Même méthode que l’audit initial : serveur Go et PostgreSQL de démonstration, Chromium via Playwright. Captures dans `/tmp/club-ux-after`, mesures dans `audit.json`, planches `sheet-390-*` et `sheet-1365-*` examinées visuellement. Les formulaires et détails longs ont également été découpés en segments pour contrôler la page entière, pas seulement le premier écran.

- À **390 et 1365 px** : accueil, club, où, quand, contact, adulte, enfant, deux confirmations, login, activation, vérification ; dashboard administrateur, membre et responsable ; adhésions et détail Arthur ; vérifications et détail enfant avec responsable ambigu ; personnes, création, modification et archives.
- À **768 px** : toutes les pages publiques ci-dessus.
- **58 navigations rendues**, réponses HTTP 200, aucun débordement de document détecté et aucun champ visible sans label détecté dans ce périmètre.
- Navigation mobile et récapitulatif enfant contrôlés sans JavaScript. Modification du récapitulatif et erreur de date future contrôlées ; aucun débordement. Une chaîne `<script>` déclarée est affichée comme texte, sans élément script injecté.
- Résultat visuel : menus accessibles, boutons entiers, listes mobiles lisibles, séparation des deux identités nette, texte de consentement intégral lisible, actions de résolution à proximité. Le détail adhésion montre immédiatement le contact d’urgence manquant.

## Tests

Nouveaux tests HTTP PostgreSQL/Testcontainers dans `ux_integration_test.go` : navigation anonyme/authentifiée, dashboard membre, responsable et administrateur composés, permissions après retrait du rôle, enfants sans grant/étrangers exclus, majorité et révocation, états vides, emails distincts et valeur invalide conservée, erreurs associées, priorité de la raison de vérification, CSRF et no-store. Test unitaire du mapping de statuts et distinction refus/absence de réponse dans `status_test.go`.

Tests historiques adaptés uniquement aux nouvelles redirections et aux libellés français (connexion vers dashboard, statuts, actions, titre de vérification). Les assertions métier restent en place. La première suite de reprise échouait sur ces attentes anciennes ; les tests ciblés après correction ont passé, puis la suite complète a été relancée.

## Limites volontairement conservées

Pas de portail membre complet ni d’actions nouvelles pour le responsable ; dashboard en lecture seule. Pas de pagination/refactor général des listes, ni nouvelle stratégie de chargement pour les familles très nombreuses (une lecture d’adhésions par enfant autorisé). Pas d’audit WCAG formel, lecteur d’écran ou matrice multi-navigateurs ; contrôles Chromium et clavier/structure évidente uniquement. Les contenus éditoriaux et l’image de démonstration du club restent à personnaliser. Quelques dates de staging et du récapitulatif conservent leur format ISO ; leur harmonisation fine reste du polish. Les captures et fixtures isolées sont temporaires, sans dépendance ajoutée au dépôt. Pas d’envoi SMTP réel pendant l’audit.

## Fichiers créés ou modifiés

- `docs/reports/2026-09-12-ux-consolidation.md`
- `internal/application/application.go`
- `internal/application/guardian_access_integration_test.go`
- `internal/application/integration_test.go`
- `internal/application/membership_ui_integration_test.go`
- `internal/application/public_registration_integration_test.go`
- `internal/application/rbac_integration_test.go`
- `internal/application/registration_integration_test.go`
- `internal/application/ux_integration_test.go`
- `internal/database/dbsqlc/dashboard.sql.go`
- `internal/database/queries/dashboard.sql`
- `internal/handlers/000_page.go`
- `internal/handlers/auth.go`
- `internal/handlers/authorization.go`
- `internal/handlers/dashboard.go`
- `internal/views/000_page.go`
- `internal/views/auth.go`
- `internal/views/dashboard.go`
- `internal/views/memberships.go`
- `internal/views/registration_reviews.go`
- `internal/views/security.go`
- `internal/views/status.go`
- `internal/views/status_test.go`
- `internal/views/templates/layouts/base.html`
- `internal/views/templates/pages/000_page.html`
- `internal/views/templates/pages/011_person_form.html`
- `internal/views/templates/pages/012_persons_list.html`
- `internal/views/templates/pages/013_person_update_form.html`
- `internal/views/templates/pages/017_archived_persons_list.html`
- `internal/views/templates/pages/auth.html`
- `internal/views/templates/pages/dashboard.html`
- `internal/views/templates/pages/join.html`
- `internal/views/templates/pages/membership_detail.html`
- `internal/views/templates/pages/memberships.html`
- `internal/views/templates/pages/registration_review_detail.html`
- `internal/views/templates/pages/registration_reviews.html`
- `internal/views/templates/pages/registration_verify.html`
- `static/css/style.css`

## Résultats finaux

- `sqlc generate` : succès ; aucune migration.
- `gofmt` : exécuté sur tous les fichiers Go concernés.
- `go test ./...` : succès, y compris application/PostgreSQL/Testcontainers et nouveaux tests UX. Dernière exécution après génération/formatage également verte (cache).
- `git diff --check` : succès, aucune erreur d’espacement.
- `git status --short` : 29 fichiers suivis modifiés et 9 fichiers nouveaux, conservés non committés ; inventaire ci-dessus.
- Aucun commit, aucun push, aucune commande de remise à zéro. Les changements locaux trouvés à la reprise ont été préservés et complétés.
