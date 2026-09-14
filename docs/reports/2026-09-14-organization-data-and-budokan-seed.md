# Configuration métier en base et démonstration Budokan — 14 septembre 2026

## 1. État initial et audit

HEAD `e991f24 Add multi-user integration campaign`. Exécution préalable, dans l’ordre demandé : `git status --short` (vide), `git log --oneline -10`, `git diff --check` (vide), `go test ./...` (succès, cache). Aucun changement local à préserver. Rapports personnel/familial, gestion personnelle du compte, administration, campagne multi-utilisateurs et sécurité RBAC consultés. Inspection des migrations 0001–0025, requêtes sqlc, configuration JSON/runtime, assemblage application, handlers/vues, services Memberships, Trials et Administration. Aucun concept Organization ou Location référencé n’existait.

La configuration publique historique est `config/config.json`, avec une démonstration fictive TCR ; elle n’est pas un modèle métier. Les horaires opérationnels sont déjà en PostgreSQL. Le service Trials valide activité/groupe/créneau/date ; il n’implémente ni quota d’essais gratuits ni règle d’âge. Les groupes ont obligatoirement une seule activité et sont indépendants des saisons. Les créneaux portent la saison et un lieu texte facultatif. Les consentements sont versionnés, immuables, avec décisions append-only et exigences figées par adhésion.

## 2. Matrice configuration / base

| Donnée | Existe déjà ? | Source avant chantier | Action |
|---|---|---|---|
| Organisation | Non | `Config.SiteName`, titres/prose JSON | Ajouter `organizations` |
| Nom court / présentation | Partiellement, éditorial | JSON | Champs facultatifs typés de l’organisation |
| Coordonnées publiques | Oui, hors DB | `ContactConfig.EmailAddress`, `PhoneNumber` | Champs de l’organisation ; ne pas utiliser Persons |
| Adresse de correspondance | Pas de concept club | Adresse Person texte uniquement | Texte facultatif de l’organisation, absent du seed |
| Lieux | Texte seulement | `group_slots.location`, prose `Where` | Ajouter `locations`, FK du créneau, compatibilité texte historique |
| Activités | Oui | `activities` | Réutiliser |
| Groupes durables | Oui | `groups` | Réutiliser |
| Horaires saisonniers | Oui | `group_slots` | Réutiliser, ajouter référence lieu et désignation de séance |
| Saisons | Oui | `seasons` | Réutiliser |
| Types d’adhésion | Oui | `membership_types` | Réutiliser ; aucun champ tarif actuel |
| Adhésions / activités | Oui | `memberships`, `membership_activities` | Inchangées |
| Consentements | Oui | `consent_definitions`, exigences, décisions | Réutiliser, aucun booléen image ajouté |
| Réseaux publics | Non | Pas de stockage métier | Ajouter `organization_links` |
| Essais | Oui | `trial_registrations` et service Trials | Inchangés ; quota absent et non ajouté |
| Textes publics | Oui, éditorial | `PageConfig` | Présentation courte possible dans organization.description ; pas de CMS |
| Enseignants | Person + rôle coach disponibles | `persons`, `users`, `roles`, `user_roles` | Relation d’enseignement à décider, aucun import de personnes |
| Infrastructure / sécurité | Oui | `.env`, `Runtime`, paramètres serveur | Conserver hors DB |

Restent techniques : DATABASE_URL, APP_BASE_URL, SMTP/transport, cookies et secrets éventuels de session, délais de vérification/activation, APP_TIMEZONE, ports, timeouts et environnement. Aucun déplacement mécanique de ces paramètres.

## 3. Concepts et migration

Migration additive `0026_organization_data.sql` : trois tables explicites et deux colonnes sur `group_slots`. Aucun `settings`, JSON de configuration métier, duplication d’activités/planning ou identifiant d’organisation ajouté à toutes les tables historiques. Les nouvelles tables suivent les conventions INTEGER identity, `is_active`, timestamps.

`organizations` porte nom obligatoire non blanc, nom court, description facultative, email/téléphone publics, adresse de correspondance et URL du site. Email contrôlé structurellement, URLs HTTP(S) sans espaces ni identifiants incorporés. Pas de validation de délivrabilité ou d’existence des sites. Un index partiel autorise une seule organisation active. Plusieurs organisations archivées peuvent exister ; une deuxième association peut devenir active après désactivation de la première. Cela **ne constitue pas du multi-tenant** : les anciens référentiels restent à l’échelle de l’instance.

`locations` porte organisation propriétaire, nom et adresse texte obligatoires, état et timestamps. Unicité exacte `(organization_id, name)`. L’adresse n’est pas décomposée artificiellement en colonnes postales. La FK empêche de supprimer un lieu encore référencé. Une désactivation conserve ses références et son nom pour les lectures historiques.

`organization_links` porte organisation, kind extensible sous forme d’identifiant, label, URL, position non négative, état et timestamps. Unicité `(organization_id, url)`, plusieurs liens du même kind possibles. Pas de colonne par réseau. Site principal sur Organization, lien Instagram dans la collection, sans duplication du site principal.

`group_slots.location_id` est facultatif pour préserver les données existantes. La contrainte `group_slots_one_location` interdit de stocker simultanément la FK et le texte historique `location`. Aucun texte historique n’est interprété ou fusionné automatiquement. Les nouveaux seeds utilisent exclusivement la FK. `practice_label` est une désignation facultative de séance, pas un second planning : jours, heures et validité restent uniquement sur la même ligne `group_slots`.

Les mutations sqlc Create/Update des nouveaux concepts mettent à jour les timestamps ; Create/UpdateGroupSlot acceptent également les nouveaux champs. SetGroupSlotLocation remplace explicitement le texte historique par la référence. Les écritures SQL manuelles doivent maintenir updated_at elles-mêmes, comme ailleurs dans le projet. La migration Down est destinée aux environnements jetables : elle retire ces nouvelles données, elle ne sert pas à une réinitialisation de démonstration.

## 4. Lectures et administration

`internal/organization.Service` expose Identity, Schedule et Catalogue ; il est disponible dans `Application.Organization`. Catalogue compose l’organisation active, lieux/liens, saison explicite, activités, groupes, créneaux, types et consentements depuis PostgreSQL. La saison est demandée par nom au CLI, puis par ID au lecteur de planning. Le planning couvre la saison choisie et expose ses dates de validité ; il n’est pas réduit aux seules séances valides aujourd’hui.

Le planning de référence filtre saisons, activités, groupes, créneaux et lieux actifs. Identity retourne aussi les lieux/liens désactivés avec leur état pour l’inspection administrative ; un futur frontend public devra filtrer ces collections. Les méthodes composent plusieurs lectures sans snapshot transactionnel global : une édition concurrente peut apparaître entre deux lectures, limite acceptable pour cette inspection V1. Aucune donnée personnelle n’est sélectionnée.

Les projections existantes personnelles, administratives et des essais résolvent maintenant le nom du lieu référencé, avec repli sur le texte historique uniquement quand la FK est absente. Un renommage est donc visible sans recopier les horaires. Les lectures historiques continuent à nommer un lieu désactivé. Désactiver un lieu ne désactive pas automatiquement les créneaux ni les essais déjà enregistrés et ne change pas les règles Trials ; fermer les créneaux reste une opération métier distincte.

Pour chaque nouvelle table : lecture opérateur immédiate par `describe-club`, écritures typées sqlc pour les futurs services administratifs, contraintes DB dès maintenant. Pas de CRUD Web ajouté ni de nouveaux droits RBAC imaginés. L’accès CLI est celui d’un opérateur disposant de DATABASE_URL, comme les commandes de rôles existantes.

Le futur site public et sa refonte restent hors périmètre. Les pages publiques historiques continuent donc à rendre le JSON TCR : ce chantier ne prétend pas présenter déjà Budokan dans le navigateur. En revanche, créer et lire toute l’identité et la structure métier Budokan ne nécessite **aucun fichier de configuration Budokan**. Aucun contenu Budokan n’est ajouté aux handlers/templates, et le seed n’est jamais exécuté au démarrage.

## 5. Organisation, contacts et lieu importés

Une organisation canonique : Budokan Sud Oise, nom court BS.O. Les variantes de ponctuation ne deviennent pas des organisations distinctes. Email public `budokansud.oise@gmail.com`, téléphone public `06 21 03 21 61`, site `https://www.budokansudoise.com/`. Pas d’adresse de correspondance déduite de l’adresse sportive.

Un lieu : Gymnase La Mardelle, adresse texte `Rue des Marais, 60260 Lamorlaye`. Les seize créneaux référencent ce même lieu. Un lien Instagram normalisé depuis l’identifiant public `budokan_sud_oise` : `https://www.instagram.com/budokan_sud_oise/`. Cette normalisation ne prétend pas vérifier le compte auprès d’Instagram.

## 6. Activités et groupes

Trois activités : Jiu-Jitsu Brésilien ; Jiu-Jitsu Traditionnel / Combat ; Préparation physique. Ni No-Gi ni Libre ne deviennent des disciplines principales.

Cinq groupes directement soutenus par le planning : JJB enfants 7–10 ans ; JJB enfants 10–14 ans ; JJB Adolescents et Adultes ; Jiu-Jitsu Traditionnel / Combat enfants 7–10 ans ; Jiu-Jitsu Traditionnel / Combat enfants 10–14 ans. Aucun groupe ne porte une année.

Clarification utilisateur : les âges **7–10 et 10–14 sont des indications pédagogiques souples**. L’âge, la taille, la maturité et l’adéquation pédagogique guident la décision des enseignants, y compris aux frontières. Aucun min_age/max_age, CHECK ou refus automatique d’affectation n’est ajouté. L’écart avec la présentation publique 7–13 / adolescents 14–18 reste un écart de présentation, pas une règle logicielle à résoudre.

**Préparation physique / JJB** est une seule séance structurée : environ **45 minutes de renforcement musculaire et cardio, puis JJB**. Les créneaux du lundi et du vendredi conservent chacun une seule ligne et leur practice_label. Pas de cours simultanés indépendants, d’activité combinée fictive, de créneaux superposés ou de système de phases. L’activité Préparation physique reste proposée dans activities. Le vendredi, dont le public adolescents/adultes est publié, est rattaché à JJB Adolescents et Adultes.

**No-Gi**, le samedi 10:00–12:00, est une séance mixte adolescents et adultes du groupe **JJB Adolescents et Adultes**, avec practice_label `JJB No-Gi`. Ce n’est ni une activité ni un groupe autonome.

**JJB libre**, le dimanche 16:00–18:00, appartient au même groupe adolescents/adultes, avec practice_label `JJB libre`. Il s’agit de pratique libre surveillée, sans enseignement structuré ; la présence permet notamment d’intervenir en cas de blessure. La description du groupe rappelle ces modalités. Aucun domaine session_type/open_mat ajouté.

Le sixième groupe **JJB pratiques spécifiques** reste uniquement comme **rattachement technique de la séance du lundi 20:15–22:00**, dont le public demeure inconnu. Sa description indique expressément « lundi uniquement », « public à confirmer » et « pas un groupe public confirmé », ainsi que le déroulement de la séance. Le schéma exige un groupe pour chaque créneau : retirer ce rattachement imposerait de choisir arbitrairement un public ou de refondre cette relation. Il ne contient donc plus ni le vendredi, ni No-Gi, ni Libre. Le rattachement à l’activité JJB reflète la partie JJB confirmée de cette séance ; il ne crée pas une nouvelle discipline. Aucun membre ou essai n’est automatiquement affecté à ce groupe technique.

## 7. Saison et contrôle du planning

Saison `2026/2027`. Le titre HTML de la page reste « Horaires 2025/2026 », mais le contenu annonce explicitement 2026/2027 et le menu affiche également 2026/2027. Le contenu opérationnel prévaut.

Une seule **Season** est confirmée pour cette V1, du **1er septembre 2026 au 31 août 2027**. Elle sert aux adhésions, créneaux, essais via leur créneau et à l’historique ; aucune séparation saison sportive/administrative. Les dates de reprise, vacances et fermetures relèvent du calendrier et des créneaux, pas d’une deuxième saison. Les créneaux héritent des bornes annuelles ; aucune exception de calendrier n’est inventée. Aucun seed sur une base déjà peuplée, donc aucune saison historique modifiée. De futurs concepts réellement distincts, comme FiscalYear ou LicensePeriod, demanderaient un chantier séparé.

| Jour | Créneaux et groupes/désignations |
|---|---|
| Lundi | 18:15–19:15 traditionnel/combat 7–10 ; 19:15–20:15 traditionnel/combat 10–14 ; 20:15–22:00 préparation physique / JJB |
| Mardi | 18:15–19:15 JJB 7–10 ; 19:15–20:15 JJB 10–14 ; 20:15–22:00 JJB adolescents/adultes |
| Mercredi | 18:15–19:15 JJB 7–10 ; 19:15–20:15 JJB 10–14 ; 20:15–22:00 JJB adolescents/adultes |
| Jeudi | 18:15–19:15 traditionnel/combat 7–10 ; 19:15–20:15 traditionnel/combat 10–14 ; 20:15–22:00 JJB adolescents/adultes |
| Vendredi | 20:15–22:00 préparation physique / JJB adolescents/adultes |
| Samedi | 10:00–12:00 JJB No-Gi ; 16:00–18:00 JJB adolescents/adultes |
| Dimanche | 16:00–18:00 JJB libre |

Une ligne publique = un GroupSlot, soit **16**, avec répartition **3/3/3/3/1/2/1**. Le test porte sur chaque jour, horaire et libellé, pas seulement sur un compte global. Pas de texte d’horaires parallèle stocké.

## 8. Essais et classification du règlement

Le règlement public décrit deux séances gratuites avec inscription préalable. Trials modélise une séance datée et son suivi, sans notion de forfait gratuit. Aucun quota ou trial_policy ajouté uniquement pour stocker ce chiffre inutilisé. La donnée est consignée ici ; un futur parcours d’essai gratuit devra définir le périmètre du quota (personne, saison, activité, exceptions) avant son implémentation. Aucune constante MaxTrials.

| Information publique | Classification / traitement |
|---|---|
| Adhésion pour la saison sportive | Déjà représentée par Membership → Season |
| Licence comprise et obligatoire | Future règle/offre métier ; aucune colonne improvisée |
| HelloAsso, chèques, plusieurs chèques | Information de paiement ; hors périmètre |
| Certificat médical / questionnaires | Future politique documentaire ; pas de stockage de document ni nouvelle obligation logicielle |
| Droit à l’image Oui/Non | Consentement existant, pas un attribut Person |
| Deux essais gratuits, inscription préalable | Future règle structurée lorsqu’un parcours l’utilisera |

La page Tarifs précise par ailleurs certificats et questionnaires selon situations ; le règlement plus général ne suffit donc pas à définir une politique documentaire exacte. Aucune lecture juridique ni nouvelle règle médicale déduite.

## 9. Types d’adhésion et prix

La page publique Tarifs consultée indique explicitement **2026/2027** et les catégories **Adulte, Adolescent, Enfant**. Ces trois noms sont importés dans membership_types ; aucun seuil d’âge logiciel n’est déduit. Montants publiés à la consultation : respectivement **280 €, 260 €, 250 €**. Ils sont documentés comme provenance, mais pas stockés en faux champs textuels : membership_types ne contient aujourd’hui que nom, état et date de création, sans tarif ni relation tarif/saison.

Aucune migration financière ajoutée, aucun paiement, licence ou remise calculée. Les noms sont des données de seed modifiables sans migration. Le stockage futur des montants saisonniers sera un chantier d’offres/tarification ; il ne faut pas confondre absence de colonne tarif et absence de source publique fiable. Pas de demande de confirmation artificielle des trois montants déjà publiés.

## 10. Consentements et contenu éditorial

Une définition `image_rights`, version 1, titre Droit à l’image, volontairement conservée telle quelle selon la clarification utilisateur pour cette V1. Le texte est une **formulation courte de démonstration explicitement étiquetée**, pas une transcription ni un texte validé par le bureau. Les décisions existantes granted/refused correspondent à Oui/Non ; withdrawn reste disponible. Aucun accord n’est créé automatiquement et aucune Person réelle n’est importée. Les triggers d’immutabilité, exigences par adhésion et snapshots sont inchangés. Le bureau fournira ultérieurement le texte définitif ; ce futur contenu ne bloque pas la V1 et n’est pas redemandé ici. Il nécessitera une nouvelle version et la désactivation de la version de démonstration, jamais sa réécriture ni celle des snapshots historiques.

Présentation Organization.description disponible mais laissée absente : pas de prose promotionnelle inventée. Descriptions de groupes utilisées pour les modalités confirmées et le rattachement technique restant du lundi. Pas de table de contenu générique, CMS, média, asset Wix, logo téléchargé ou synchronisation. Les enseignants devraient probablement être des Persons reliées fonctionnellement à Activity/Group ; le rôle coach est un droit de User, pas une relation d’enseignement. Aucun domaine instructors ni import de noms de professeurs.

## 11. Seed reproductible et garde-fous

Depuis la racine, avec une **PostgreSQL jetable dédiée**, vide, et DATABASE_URL fourni localement. Le nom réel de la base doit se terminer par `_demo` (par exemple `club_core_demo`). Appliquer les migrations Goose du dépôt jusqu’à 0026, puis :

```bash
# Variables standard Goose, sans afficher la chaîne de connexion.
GOOSE_DRIVER=postgres GOOSE_DBSTRING="$DATABASE_URL" goose -dir migrations up

go run ./cmd/clubctl seed-budokan --confirm-empty-demo
go run ./cmd/clubctl describe-club 2026/2027
```

Le CLI exige DATABASE_URL, borne son contexte à 30 secondes et n’imprime pas la connexion. Le seed ne crée pas la base, ne migre pas automatiquement et n’envoie aucun mail. Le CLI describe-club est générique, en lecture seule, et produit un JSON des références ; il ne consulte aucun JSON de présentation.

`internal/demodata/budokan.sql` est un jeu de données versionné embarqué dans le binaire, exécuté par `SeedBudokan`. Il contient des références publiques et leur interprétation, jamais des secrets ou coordonnées privées. IDs issus des identity PostgreSQL et relations résolues par sélection des données insérées, pas d’IDs codés en dur. L’ordre d’insertion est stable ; timestamps et séquences ne sont pas des valeurs reproductibles octet pour octet. Une tentative avortée peut avancer une séquence PostgreSQL sans laisser de ligne.

Protection en plusieurs étapes : confirmation explicite ; contrôle de `current_database()` et du suffixe ; transaction complète ; verrou advisory transactionnel pour sérialiser deux seeds ; découverte et verrouillage des tables de l’application dans le schéma public ; refus si une seule contient des lignes. Seules `roles` (référentiel système initialisé par migration) et `goose_db_version` sont exclues du contrôle de vacuité. Même des données métier sans Organization font refuser le seed. Une table publique auxiliaire peuplée le fait également refuser volontairement.

Pas de reset, upsert, delete ou rapprochement par nom d’association. Une deuxième exécution échoue avec un message explicite et laisse toutes les lignes intactes. Toute erreur, y compris sur la dernière insertion, annule toute l’initialisation. Les noms de tables découverts sont échappés avec pgx.Identifier. Le suffixe et le drapeau sont un garde-fou d’opérateur, pas une authentification : les droits PostgreSQL restent la frontière d’accès. L’opérateur doit choisir une instance jetable et arrêter les écritures concurrentes ordinaires ; des verrous peuvent sinon conduire à un timeout, sans résultat partiel.

## 12. Provenance et certitude

Sources publiques consultées en **septembre 2026**, le 14 septembre pendant ce chantier :

- [Accueil Budokan](https://www.budokansudoise.com/) : nom, coordonnées, lieu, disciplines.
- [Horaires](https://www.budokansudoise.com/horaires) : seize lignes et annonce 2026/2027 malgré le titre historique.
- [Quelques explications](https://www.budokansudoise.com/quelques-explications) : disciplines et présentations des publics.
- [Règlement](https://www.budokansudoise.com/r%C3%A8glement) : essais, modalités publiques, image, identifiant Instagram.
- [Tarifs](https://www.budokansudoise.com/tarifs) : catégories et montants annoncés 2026/2027.

Données certaines à cette consultation : identité canonisée, coordonnées publiques, lieu, trois activités, lignes horaires, noms des trois offres. Clarifications fournies directement par l’utilisateur lors de la reprise : déroulement des séances préparation physique puis JJB, public et rattachement No-Gi/Libre, souplesse des âges pédagogiques, Season unique et bornes annuelles, conservation du consentement de démonstration. Interprétations restantes : nom court, rattachement technique du lundi seulement, libellés normalisés, URL construite depuis le compte Instagram. La rédaction du consentement reste un texte de démonstration assumé. Absents : correspondance postale, textes éditoriaux définitifs, assets, enseignants, politiques de quota et documents. Aucun scraper ni requête réseau au démarrage ou à l’exécution du seed.

## Confirmations demandées à l'utilisateur

- Quel est le public exact de la séance du **lundi 20:15–22:00 Préparation physique / JJB** ? Peut-elle être rattachée au groupe JJB Adolescents et Adultes comme celle du vendredi ?

C’est la seule ambiguïté utile restante. Elle ne bloque pas le seed et justifie uniquement le rattachement technique documenté du lundi. Le fonctionnement général de la séance, No-Gi, Libre, les indications d’âge, Season et le consentement de démonstration sont désormais clarifiés ; aucune nouvelle confirmation de ces points n’est attendue.

## 13. Tests et contrôle fonctionnel

Tests réels PostgreSQL 16/Testcontainers dans `internal/database/organization_integration_test.go`, avec extension minimale du helper existant pour choisir le nom d’une base isolée. Pas de SQLite, mocks de DB ou modification des saisons des fixtures historiques.

Couverture : Organization/contacts et noms obligatoires, unicité de l’organisation active, passage à une deuxième organisation après désactivation, FK et unicité des lieux/liens, URLs et email invalides, désactivation, lieu référencé non supprimable, exclusion FK/texte simultanés, absence de FK cible, planning exact et rattachement au lieu, catalogue générique, trois types d’adhésion, consentement présent, absence de Persons/Users importés. Renommage du lieu visible dans les lectures catalogue et administratives ; lieu inactif absent du planning publié, organisation inactive introuvable comme organisation active.

Le test exécute réellement `go run ../../cmd/clubctl seed-budokan --confirm-empty-demo` sur une base vide migrée et inspecte ses lignes. Puis il exécute describe-club, relance le seed et teste un mauvais drapeau. Autres contrôles : mauvais nom de base, absence de confirmation, ancienne saison préservée et refusée, deux seeds concurrents (une réussite/un refus), trigger de test faisant échouer la dernière définition de consentement avec **zéro ligne dans les neuf tables métier concernées** après rollback. Aucun conteneur ni base de la campagne multi-utilisateur n’est réutilisé.

Réponses démontrées depuis la base seule : club Budokan Sud Oise ; lieu La Mardelle et adresse ; trois activités ; six groupes dont un rattachement technique réservé au lundi ; saison 2026/2027 ; seize créneaux et leur lieu ; trois types d’adhésion et une définition de droit à l’image. La commande ne charge pas config/config.json.

## 14. Résultats du chantier initial, avant clarifications

- `sqlc generate` : succès ; fichiers générés à jour.
- `gofmt -w` sur les fichiers Go concernés : effectué.
- `go test ./...` sur l’état final : succès ; database 31,640 s, application et autres packages testés en cache après leurs passages précédents réussis. Première exécution complète de ce chantier : application 212,072 s. Journal final : `/tmp/club-organization-final-tests.log`.
- `go test -race ./internal/database ./internal/organization ./internal/demodata ./internal/application ./cmd/clubctl` : succès, application 387,081 s, aucune course détectée. Journal : `/tmp/club-organization-race.log`.
- Après les dernières assertions de provenance et l’inclusion des types dans le rollback, contrôle race ciblé final des packages database/organization/demodata et cmd/clubctl : succès, database 47,271 s. Journal : `/tmp/club-organization-final-database-race.log`.
- Tests ciblés des nouveaux concepts et du seed, PostgreSQL réel, également exécutés sans cache avec succès ; les suites historiques restent vertes.
- `git diff --check` : succès. `git status --short` : 13 fichiers suivis modifiés et 8 nouveaux fichiers (les répertoires nouveaux sont regroupés par Git), aucun fichier indexé.

Le modèle générique, le jeu versionné et les lectures sont utilisables. Après clarifications, seul le public du lundi reste à confirmer ; le texte définitif du consentement est un futur contenu non bloquant. Pas de contrôle visuel ajouté : aucun handler/template/design public modifié. Aucun engagement de fonctionnement multi-tenant, de quotas d’essais, de tarification calculée ou de site public Budokan final.

## 15. Fichiers et état Git du chantier initial

Créés : migration 0026 ; requêtes et code sqlc organization_data ; package organization ; package demodata (Go et SQL embarqué) ; tests PostgreSQL organization_integration_test.go ; ce rapport.

Modifiés : README.md (accès à la documentation) ; cmd/clubctl/main.go (seed et lecture générique) ; internal/application/application.go (service exposé) ; requêtes group_slots/personal_space/administration/trial_registrations et leurs générations sqlc ; modèles générés ; helper de test PostgreSQL pour un nom de base explicite. Aucun template, configuration runtime, fixture de saison historique, domaine personnel ou fichier de données privées modifié.

Aucun commit, push, reset ou clean. Copie laissée pour revue manuelle.

État Git consigné lors du chantier initial (avant le commit 0741999) :

```text
 M README.md
 M cmd/clubctl/main.go
 M internal/application/application.go
 M internal/database/dbsqlc/administration.sql.go
 M internal/database/dbsqlc/group_slots.sql.go
 M internal/database/dbsqlc/models.go
 M internal/database/dbsqlc/personal_space.sql.go
 M internal/database/dbsqlc/trial_registrations.sql.go
 M internal/database/queries/administration.sql
 M internal/database/queries/group_slots.sql
 M internal/database/queries/personal_space.sql
 M internal/database/queries/trial_registrations.sql
 M internal/database/test_database_test.go
?? docs/reports/2026-09-14-organization-data-and-budokan-seed.md
?? internal/database/dbsqlc/organization_data.sql.go
?? internal/database/organization_integration_test.go
?? internal/database/queries/organization_data.sql
?? internal/demodata/
?? internal/organization/
?? migrations/0026_organization_data.sql
```

## 16. Reprise et intégration des clarifications métier

Les commandes demandées ont été exécutées avant modification : `git status --short`, `git diff --stat`, `git diff --check`, `git diff`. Toutes étaient vides. Contrairement à l’état annoncé dans la demande, le chantier initial avait déjà été committé sous **0741999 Add organization data and Budokan demo seed** ; ce commit et tous ses fichiers sont conservés. Aucun reset, clean, commit ou push effectué pendant cette reprise. Rapport lu intégralement, puis inspection du seed, des tests et du service d’affectation de groupe.

Ajustements limités à `internal/demodata/budokan.sql`, `internal/database/organization_integration_test.go` et ce rapport. Aucune migration, modification du domaine, frontend public ou asset. Trois FK de créneau changent : vendredi 20:15, samedi No-Gi et dimanche Libre vers JJB Adolescents et Adultes. Les descriptions explicitent les modalités confirmées et le seul rattachement technique restant. Les seize lignes, heures et practice_label restent inchangés. Season, activités, types et consentement version 1 sont conservés.

Le test PostgreSQL lance réellement le seed sur une base `_demo` vide et migrée, puis `go run ../../cmd/clubctl describe-club 2026/2027`. Le JSON est désormais décodé et contrôlé structurellement : Organization, Location, trois Activities, six Groups, Season, seize GroupSlots, trois MembershipTypes et une ConsentDefinition. Les rattachements sont vérifiés à la fois dans ce JSON et dans le catalogue lu directement en DB : aucun groupe No-Gi/Libre, modalités du vendredi/samedi/dimanche sur le groupe adolescents/adultes, groupe technique utilisé exactement une fois le lundi. Le contrôle existant du planning exact et de la répartition 3/3/3/3/1/2/1 est préservé.

Un sous-test transactionnel utilise le service Memberships existant pour affecter un enfant de 11 ans aux groupes 7–10 et un enfant de 9 ans aux groupes 10–14, dans les deux disciplines. Les quatre affectations doivent réussir ; les données personnelles sont fictives et la transaction est annulée à la fin. Cela vérifie concrètement l’absence de refus automatique basé sur les noms pédagogiques, sans ajouter de règle d’âge. Les tests de rollback, concurrence, relance, garde-fou, base non vide et saison historique conservée restent présents.

### Audit de continuité et travail restant constaté

Lors de la dernière demande de reprise, les cinq commandes imposées ont de nouveau été exécutées : status, diff stat, diff check, diff intégral, log sur huit commits, puis `go test ./...` avant toute modification. HEAD reste 0741999. Trois fichiers suivis étaient déjà modifiés, aucun fichier nouveau/non suivi : le seed, son test PostgreSQL et ce rapport. Le rapport a été relu intégralement et confronté aux modifications, aux assertions de tests et au garde-fou Go.

Le travail métier décrit ci-dessus était **déjà terminé et valide**. Les tests incluaient déjà les nouveaux rattachements, les affectations pédagogiques souples et le décodage JSON du CLI. La suite globale précédente avait réussi ; le contrôle race avait été lancé mais ses résultats n’étaient pas encore consignés. Seule la finalisation des preuves et du rapport restait à faire. Les modifications du seed et des tests ont été conservées telles quelles ; aucune nouvelle correction fonctionnelle ni réécriture ajoutée pendant cet audit de continuité.

### Résultats finaux des clarifications

- `sqlc generate` : succès, aucun changement de fichier généré.
- `gofmt -w internal/database/organization_integration_test.go` : effectué, aucun changement supplémentaire.
- `go test ./...` : succès ; passage des clarifications avec PostgreSQL/database en 31,535 s, puis contrôles de reprise et final réussis en cache. Journaux : `/tmp/club-budokan-clarifications-tests.log`, `/tmp/club-budokan-resume-tests.log`, `/tmp/club-budokan-resume-final-tests.log`.
- `go test -race ./internal/database ./internal/organization ./internal/demodata ./cmd/clubctl` : succès, aucune course détectée ; résultat database en cache après le passage race réel des clarifications. Journaux : `/tmp/club-budokan-clarifications-race.log`, `/tmp/club-budokan-resume-race.log`. Application n’a pas été modifiée pendant ces ajustements.
- Nouvelle exécution **sans cache** des trois tests PostgreSQL avec `go test ./internal/database -run 'TestBudokan|TestOrganization' -count=1 -v` : succès en 5,670 s. Journal : `/tmp/club-budokan-resume-real-seed.log`.
- Ce dernier passage a créé des PostgreSQL 16 isolées, appliqué les migrations, réellement lancé les commandes seed-budokan et describe-club, décodé et inspecté leur JSON, puis constaté le refus de la deuxième initialisation. Les scénarios de concurrence, rollback complet et garde-fous ont également réussi. Les conteneurs jetables ont été arrêtés après les tests.
- État du seed vérifié avant les fixtures pédagogiques transactionnelles : une organisation, un lieu, trois activités, six groupes dont le seul rattachement technique du lundi, une Season 2026/2027, seize créneaux avec leur lieu, trois types d’adhésion, image_rights version 1, **zéro Person et zéro User**. No-Gi et Libre appartiennent bien au groupe JJB Adolescents et Adultes ; aucun groupe ni activité autonome correspondant à ces modalités.
- `git diff --check` : succès. Trois fichiers modifiés, non indexés ; aucun fichier nouveau. Aucun commit, push, reset ou clean.

La seule ambiguïté restante concerne le public du lundi 20:15–22:00, sans bloquer cette V1. Aucun frontend public, asset ou refactor engagé. Le dépôt est prêt pour revue manuelle et commit par l’utilisateur.

État Git final de ces ajustements :

```text
 M docs/reports/2026-09-14-organization-data-and-budokan-seed.md
 M internal/database/organization_integration_test.go
 M internal/demodata/budokan.sql
```
