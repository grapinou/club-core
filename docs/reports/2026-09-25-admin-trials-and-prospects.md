# P2.1 — parcours administratif des prospects et essais

## État initial

Le dépôt était propre avant intervention (`git status --short` vide, HEAD `4a5af3c`). L’administration possédait déjà `/admin`, `/trials`, `/trials/{id}`, `/persons`, `/persons/{id}`, le suivi des adhésions et des demandes d’inscription, les actions de statut, notes et reprogrammation, ainsi qu’un lien essai → demande d’adhésion. Le dashboard affichait surtout trois compteurs et les dernières personnes créées. La fiche d’essai ne rappelait ni les coordonnées du responsable d’un mineur ni le `practice_label`. La liste des personnes ne distinguait pas un prospect d’un dossier d’adhésion. Le seed Budokan ne créait aucun compte utilisateur : `clubcore_demo` avait zéro utilisateur et zéro rôle attribué au début du test.

## Audit du modèle

Les migrations existantes fournissent `persons`, `person_guardians`, `trial_registrations`, `memberships`, `registration_applications`, groupes, créneaux et saisons. Les statuts d’essai sont `registered`, `attended`, `no_show`, `cancelled`. `trials.Service` valide la programmation et la reprogrammation ; `administration.Service` compose les lectures et mutations du bureau, protège aussi ses méthodes par permissions et journalise les écritures. La révision de l’essai empêche une modification concurrente silencieuse. Les requêtes `AdministrativeTrials`, `AdministrativeRelations`, `SearchAdministrativePersons` et `AdministrativeCounts` ont été enrichies et régénérées avec sqlc. Aucun nouveau champ ni migration n’a été nécessaire.

## Workflow administratif retenu

Réservation publique → prospect dans `persons` → essai dans `trial_registrations` → essai visible sur le dashboard et dans la liste → fiche de séance et fiche personne → statut humain (`Présent`, `Absent`, `Annulée`) → éventuelle demande d’adhésion existante. Les familles utilisent `person_guardians`, sans fusionner enfant et responsable. Le traitement demeure dans les routes administratives existantes.

## Tableau de bord

`/admin` met en premier les essais passés encore `registered`, puis les essais du jour et à venir. Chaque entrée mène directement à l’essai et indique date, heure, personne, activité, groupe, pratique éventuelle, mineur et présence d’une note. Les compteurs ouvrent les listes correspondantes. Une section secondaire mène aux adhésions en attente et à la recherche de personnes. La liste décorative des dernières personnes créées a quitté le dashboard.

## Essais

La liste `/trials` présente date et heure, personne, activité, groupe, pratique, lieu, statut et repère de date. Les filtres existants « Aujourd’hui » et « À venir » restent ; `/trials?pending=1` affiche les séances passées encore sans résultat. Le tri met les dates actuelles/futures proches avant les dates passées récentes. La fiche réunit séance, pratiquant, responsable si mineur, notes et demande de matériel lorsqu’elle figure dans les notes. Les actions rapides « Marquer présent », « Marquer absent » et « Annuler l’essai » réutilisent le POST de statut existant ; un statut déjà traité peut être corrigé. Reprogrammation et modification des notes restent disponibles dans des sections repliables. La confirmation visuelle après POST conserve le modèle PRG existant et affiche le nouveau statut.

## Personnes

La liste déduit « Prospect après essai », « Adhésion », « Responsable » ou « Autre dossier » des relations existantes, sans colonne `prospect`. La fiche personne présente identité, coordonnées, compte éventuel, famille, essais et adhésions. L’historique des essais bénéficie des informations et liens enrichis. Une personne peut avoir à la fois un essai et une adhésion ; le libellé de liste donne priorité au dossier d’adhésion et l’historique complet reste sur sa fiche.

## Mineurs

La fiche d’essai affiche le pratiquant séparément du responsable : nom, relation, contact principal, téléphone et email. La fiche personne montre aussi les coordonnées des responsables et, pour un responsable, les enfants associés. Les informations proviennent de `person_guardians`. Pour un adulte, la section « Responsable » est absente. Si un mineur n’a aucun responsable enregistré, la fiche le signale.

## Notes

`persons.notes` reste la note interne durable de la personne. `trial_registrations.notes` est la note de cette séance et porte aussi la demande générique de matériel issue du public. `memberships.admin_note` reste distincte pour le dossier d’adhésion. Les formulaires administratifs sont nommés selon leur portée ; aucune de ces notes internes n’a été ajoutée au site public ou à l’espace personnel.

## Authentification et permissions

Les GET du bureau passent par `PersonsRead` ; les changements de statut, reprogrammations et notes par `PersonsWrite`, avec CSRF. Le raccord vers une demande d’adhésion exige `MembershipsApprove`. Le service vérifie de nouveau les droits et l’activation du compte. Les rôles existants `president` et `secretary` disposent de ces permissions ; `treasurer` et `coach` n’en disposent actuellement d’aucune dans cette politique. Le RBAC plus fin reste hors P2.1. Les tests et le scénario manuel ont confirmé la redirection d’un anonyme et le refus 403 d’un compte sans rôle.

## Adhésion

Depuis un essai, le lien existant ouvre `/persons/{id}/memberships/new?trial={id}` et présélectionne l’activité de l’essai. Le service vérifie que cet essai appartient à la personne et que l’activité demandée correspond. Les décisions de consentement et la validation d’adhésion restent dans le workflow déjà présent. La source `trial` aide à ouvrir le formulaire mais ne crée pas aujourd’hui un lien persistant d’origine entre adhésion et essai ; cela reste à évaluer avec le cycle complet d’adhésion.

## UX

Les écrans du bureau sont orientés vers les actions : essais sans résultat, séances imminentes, fiche complète, puis résultat en un clic. Les informations importantes sont visibles avant les formulaires détaillés. Les identifiants internes ne sont pas affichés comme contenu. Les badges traduisent les statuts persistés sans les renommer en base. La navigation existante « Administration », « Adhésions », « Vérifications », « Essais », « Personnes » a été conservée ; aucune section vide n’a été ajoutée.

## Mobile

Chromium a été vérifié à 1280 × 900 et 390 × 844 sur dashboard, liste des essais, fiche d’essai et fiche personne. La largeur du document mobile est restée à 390 px, sans défilement horizontal. Les coordonnées du responsable, notes, actions de statut, famille et historique sont lisibles et les boutons restent utilisables. La navbar occupe plusieurs lignes sur mobile mais n’empêche pas les actions.

## Compte de démonstration local

La base `clubcore_demo` ne contenait aucun utilisateur. Pour le test manuel, deux comptes synthétiques ont été créés localement : un compte bureau et un compte sans rôle. Le rôle `secretary` a été attribué avec la commande existante `clubctl grant-role` et vérifié avec `list-roles`. Les mots de passe ont été générés aléatoirement et conservés uniquement dans `/tmp/club-core-p21-demo-credentials.json` (permissions `0600`), hors du dépôt ; aucune adresse personnelle ou secret n’a été ajouté au seed. Il n’existe toujours pas de bootstrap administrateur automatique sur une base neuve : il faut disposer d’un compte activé puis lui attribuer un rôle via `clubctl`.

## Tests

Un test d’intégration PostgreSQL/Testcontainers couvre dashboard imminent et passé sans résultat, liste et liens, détail adulte et mineur, responsable et matériel, statut présent/absent/annulé et persistance, disparition du filtre « à traiter » après résolution, fiches personne et famille, distinction prospect/adhésion/responsable, refus anonyme et compte non autorisé. Le test de réservation publique → administration a été étendu au cas mineur avec responsable. Les tests antérieurs continuent à couvrir conflits de révision, statut invalide, reprogrammation invalide, transaction et audit.

## Vérifications finales

- `go test ./...` : réussi (suite complète, dont intégration PostgreSQL/Testcontainers).
- `go vet ./...` : réussi.
- `git diff --check` : réussi.
- `scripts/run-dev.sh` : conservé, démarrage réel réussi ; aucun changement du script.

## Test manuel

Sur `clubcore_demo`, trois personnes synthétiques ont réservé publiquement (un adulte, un mineur avec responsable et matériel, un autre adulte). Le compte bureau s’est connecté et a retrouvé les essais dans le dashboard, les détails et les fiches personne. Les trois essais ont été reprogrammés par la route administrative vers des dates passées compatibles avec leurs créneaux pour simuler une séance terminée ; ils sont alors apparus dans « Essais passés sans résultat ». L’adulte et le mineur ont été marqués « Présent », le troisième « Absent ». Les trois statuts et révisions ont été vérifiés en PostgreSQL ; ils ont quitté le filtre des essais à traiter. Le lien essai → formulaire d’adhésion a été ouvert pour l’adulte et le mineur, sans créer d’adhésion. Les réservations, personnes et comptes synthétiques restent dans la base persistante pour inspection manuelle.

## Limites connues

Le dashboard et la liste bornent les résultats, sans pagination spécialisée des essais ; les filtres de date et de statut aident à retrouver un dossier. L’interface propose l’enregistrement d’un résultat même avant la date prévue, comme le service actuel ; P2.1 n’ajoute pas de règle temporelle nouvelle. Le rapprochement essai → adhésion n’est pas persisté comme relation métier dédiée. Le compte bureau local créé pour le test n’est pas seedé dans une base vierge. La gestion complète de l’organisation, des adhésions et un RBAC plus fin restent hors de cette phase.

## Suite recommandée

1. **P2.2 — administration de l’organisation** : activités, saisons, groupes, créneaux, lieux et contenus publics, avec protections adaptées.
2. **P3 — cycle complet de l’adhésion** : préciser le passage depuis un essai, les décisions nécessaires et les états jusqu’à l’adhésion active.
3. **RBAC avancé éventuel** : affiner les capacités du trésorier, des encadrants et des membres du bureau à partir des usages réels, sans introduire une matrice prématurée.
