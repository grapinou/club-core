# Mon espace / Mon compte — portage sur P4.4 SQLite

## Origine et état final visé

La première passe UX a été réalisée sur `main` (`b9bc84a`) et enregistrée dans `f9a0e12 WIP personal account UX`. Elle est maintenant portée par cherry-pick sur **`p4.4-sqlite-migration`, base `cfeb266`**. P4.4, plus récent, constitue la base fonctionnelle et technique : les limitations de consultation d’urgence de l’ancien main ne décrivent plus le résultat de ce portage.

Audit avant résolution : `git status --short`, `git diff --cc`, `git diff --name-only --diff-filter=U`, `git show --stat f9a0e12`. Huit fichiers en conflit ; trois auto-fusionnés contrôlés manuellement. Aucun abort, commit, push ni `cherry-pick --continue`.

## Fusion sémantique

| Fichier en conflit | Décision |
|---|---|
| `personal_space_integration_test.go` | Tests SQLite P4.4 conservés, dont les quatre lectures groupées constantes ; aucune attente sur les booléens de main. |
| `self_service_account_integration_test.go` | API SQLite et protections P4.4 conservées ; injections prénom/nom/naissance/username ignorées, téléphone/adresse normalisés et audit maintenus. |
| `personalspace/service.go` | DTO et signatures P4.4 conservés intégralement ; contacts propres exposés depuis la lecture groupée déjà existante, uniquement selon le droit d’urgence actuel. |
| `layouts/base.html` | Rôles → Mon espace → fonctions de Bureau ; toutes les destinations P4.3/P4.4 préservées. Site du club et logout POST/CSRF dans le layout partagé. |
| `dashboard.html` | Demandes en attente, adhésion courante/historique, groupes, enfants, actions et gestion d’urgence conservés. Accès Mon compte ; modification de profil directe et anciennes actions de bas de page retirées. |
| `join.html` | Formulaire P4.4, dont urgence adulte obligatoire, conservé ; étoile sur le choix d’urgence enfant, sans modifier Oui/Non ni `required`. |
| `personal_space.html` | Fonctions remontées dans l’en-tête et actions Éditer ; sections urgence, enfant, adhésion et consentements éditables P4.4 préservées. |
| `style.css` | Styles calendrier, famille et navigation P4.4 conservés ; styles nouveaux des en-têtes limités aux cartes du compte. |

L’auto-fusion de `personal_space.sql` avait ajouté `ANY(...::integer[])`, incompatible avec SQLite, et une projection d’urgence limitée à des booléens. Ce bloc a été retiré : SQL source P4.4 conservé, puis **`sqlc generate`** avec `engine: sqlite` et `database/sql`. Son fichier généré revient également à la version P4.4. L’auto-fusion du handler personnel ne change que le titre navigateur du compte ; auth, CSRF et no-store sont inchangés.

## Identité : adaptation explicite

P4.4 permettait déjà de modifier prénom et nom dans le profil. La consigne de ce portage exige de garder l’identité non modifiable : le formulaire et le handler utilisent maintenant uniquement `UpdateContact` pour téléphone/adresse. L’écriture `UpdateSelfServiceProfile` et son API, devenues inutiles, ont été retirées du SQL source et régénérées normalement. Éditer dans Identité ouvre Contact ; le bloc explique la vérification par le club et l’identifiant fixe.

Cette restriction est volontaire, et non présentée comme une capacité P4.4 conservée : les noms normalisés participent au matching, les déclarations/candidats sont des preuves immuables, la naissance détermine majorité et accès familial, et l’audit actuel ne trace pas anciennes/nouvelles identités. Les autres capacités P4.4 restent intactes. La modification légitime d’un contact d’urgence dédié, non partagé, conserve ses règles propres.

## Garanties P4.4 conservées

SQLite, `database/sql`, `sql.Null*`, `dbtypes`, `civildate`, migrations et signatures sqlc actuelles ; dates de demande et groupes ; demandes familiales en staging et en attente ; actions responsable/enfant ; consentements personnels éditables et historiques ; GuardianAccess, majorité, archivage et révocations ; permissions réelles.

Les contacts autorisés restent consultables et gérables : compte adulte propre et enfants effectivement autorisés. Les contacts étrangers restent exclus, un rôle administratif n’élargit pas la vue personnelle et les mineurs ne gagnent pas d’accès autonome à leur urgence. Aucune régression vers les simples booléens de main. Étoiles ajoutées aux formulaires urgence P4.4 en suivant les `required` existants : téléphone partagé en lecture seule sans étoile, email facultatif et priorité inchangés.

Email vérifié séparément, mot de passe actuel, invalidation des demandes, audit transactionnel, rotation/révocation des sessions, auth, CSRF/origine, isolation utilisateur et no-store sont préservés. Aucune migration ni dépendance frontend ajoutée.

## Validation

Nouveaux tests UX adaptés à SQLite : navigation unique, rôles, logout/CSRF, compte/en-tête/actions, confidentialité des contacts étrangers, consultation autorisée, révocation et mineurs, étoiles/required et coordonnées partagées. Les tests P4.4 restent présents ; seules leurs attentes incompatibles avec la navigation désormais permanente et le gel explicite d’identité sont adaptées. Après révocation, le lien Mon espace reste visible mais les dossiers et actions enfant restent inaccessibles.

Validation finale sur SQLite : `sqlc generate`, `go test ./...`, `go vet ./...` et `git diff --check` réussis. Le SQL personnel et son code généré sont identiques à P4.4 ; aucune référence PostgreSQL (`pgx`, `pgtype`, `ANY(...::integer[])`) réintroduite.

Recette réelle Chromium sur une base SQLite isolée : 28 pages authentifiées vérifiées à **1280 × 900** et **390 × 900**, comme adhérent puis secrétaire/responsable. Dashboard, compte, formulaires coordonnées/email/mot de passe, urgence propre, ajout d’enfant et urgence enfant : réponses 200, aucun débordement horizontal. Édition des coordonnées, ajout d’un contact puis consultation depuis le dashboard, action Identité vers Contact, retour Site du club et déconnexion validés. Captures et mesures locales : `/tmp/clubcore-p44-personal-ux/`, journal de tests : `/tmp/clubcore-p44-personal-ux-tests.log`. Le serveur de recette temporaire et sa base ont été supprimés ; aucun outil de recette ni dépendance n’est ajouté au dépôt. Les captures PostgreSQL de la réalisation initiale ne servent pas de preuve de validation SQLite finale.

Hors périmètre : édition autonome de l’identité, migrations, changement des droits métier, modèle GuardianAccess et règles d’approbation. Aucun de ces changements n’est nécessaire au portage UX.

Le cherry-pick restera ouvert après résolution et mise à l’index, conformément à la demande.
