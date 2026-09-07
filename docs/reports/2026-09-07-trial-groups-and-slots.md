# Essais : groupes et créneaux — 7 septembre 2026

## Contexte

Permettre de programmer un essai avec une activité seule, une activité et un groupe, ou une activité avec groupe et créneau, dans le parcours Personne → contact → essai → adhésion.

Le modèle concerné comprend `Activity` (activité), `Group` (groupe rattaché à une activité), `GroupSlot` (créneau hebdomadaire rattaché à un groupe et une saison) et `TrialRegistration` (essai d’une personne à une date donnée).

## Décisions métier implémentées

- `activity_id` reste obligatoire ; `group_id` et `group_slot_id` sont facultatifs.
- Un créneau implique un groupe. Le groupe doit appartenir à l’activité de l’essai et le créneau au groupe de l’essai.
- La programmation vérifie l’existence et l’activation des objets utilisés. Une activité, un groupe ou un créneau inactif est refusé pour une création ou une reprogrammation.
- Avec un créneau, la date doit correspondre au weekday ISO (`1` lundi à `7` dimanche), être supérieure ou égale à `valid_from`, inférieure ou égale à `valid_until` si renseigné, et comprise dans les dates de la saison associée, bornes incluses.
- Les lectures historiques ne filtrent pas sur `is_active` ni sur la validité actuelle du créneau. Une désactivation ou clôture ultérieure ne rend pas l’essai illisible.
- Les statuts restent exactement `registered`, `attended`, `cancelled` et `no_show`. Leur modification ne suit pas de workflow rigide : une correction administrative reste possible.
- Une création démarre à `registered`. Une reprogrammation conserve la personne, le statut et la note.
- `trial_registrations.notes` concerne l’essai précis et peut être modifié ou effacé indépendamment de `persons.notes`, qui conserve la note générale de la personne.
- Plusieurs essais, y compris dans plusieurs activités, sont possibles pour une personne. Aucune contrainte de dédoublonnage supplémentaire n’est introduite.

## Migration

`migrations/0014_trial_groups_and_slots.sql` ajoute les colonnes nullable `group_id` et `group_slot_id` à `trial_registrations`, sans modifier les migrations 0001 à 0013.

Les contraintes déclaratives PostgreSQL ajoutées sont :

- `groups_id_activity_unique` : unicité de `(id, activity_id)` dans `groups` ;
- `group_slots_id_group_unique` : unicité de `(id, group_id)` dans `group_slots` ;
- `trial_slot_requires_group` : `CHECK` imposant un groupe lorsqu’un créneau est renseigné ;
- `trial_group_activity_fk` : clé étrangère composite `(group_id, activity_id)` vers `groups (id, activity_id)` ;
- `trial_slot_group_fk` : clé étrangère composite `(group_slot_id, group_id)` vers `group_slots (id, group_id)`.

La clé étrangère existante sur l’activité reste en place. Aucun trigger ni suppression en cascade des essais n’est ajouté. Les références empêchent la suppression des objets encore utilisés par les essais.

Des index couvrent `(person_id, trial_date)`, `trial_date`, `group_id` et `group_slot_id`. La migration fournit un retour arrière supprimant ses ajouts.

## Backend

`internal/trials/service.go` délimite le service métier de programmation :

- `Schedule` valide la cible et la date, puis crée l’essai ;
- `Reschedule` applique les mêmes validations à la nouvelle cible/date avant de modifier l’essai existant.

Les appels backend de programmation doivent passer par ce service. Validation et écriture sont réalisées dans une même transaction. Des verrous `FOR SHARE` protègent l’activité, le groupe, le créneau et la saison utilisés contre une modification concurrente entre validation et écriture. Le contrôle du calendrier est exécuté en SQL à partir de la date PostgreSQL.

Les lectures et modifications de statut ou de note restent disponibles via sqlc, indépendamment des règles de programmation. Aucune UI n’est ajoutée.

## SQL/sqlc

`internal/database/queries/trial_registrations.sql` ajoute :

- `CreateTrial` ;
- `GetTrial` ;
- `ListPersonTrials` ;
- `ListTrialsByDate` ;
- `ListUpcomingTrials` ;
- `UpdateTrialStatus` ;
- `RescheduleTrial` ;
- `UpdateTrialNotes` ;
- `LockTrialActivity`, `LockTrialGroup` et `LockTrialSlot` pour la validation et le verrouillage.

`ListUpcomingTrials` utilise une date de départ explicite et inclusive, sans exclure de statut. Les listes sont triées par date, horaire (valeurs absentes en dernier), nom, prénom puis identifiant d’essai.

Les fichiers Go sqlc ont été générés avec `sqlc generate`, sans modification manuelle.

## Données riches pour le frontend

Les lectures retournent directement l’identifiant et la date de l’essai, son statut et ses notes, ainsi que la personne (identifiant, prénom, nom, naissance facultative), son téléphone facultatif, l’activité, le groupe facultatif et le créneau facultatif avec jour ISO, horaires et lieu.

Les jointures permettent notamment d’afficher les essais d’une date sans requêtes supplémentaires pour ces informations. Le téléphone est celui de la personne ; aucune duplication ni logique supplémentaire de responsable/enfant n’est introduite.

## Tests

`internal/database/trials_integration_test.go` utilise PostgreSQL/Testcontainers et couvre :

- les trois niveaux de précision et plusieurs essais ou activités pour une personne ;
- les incohérences activité/groupe/créneau, y compris par écritures SQL directes, et la protection contre les suppressions ;
- le bon et le mauvais weekday, les dates hors validité, les bornes inclusives et les dates hors saison ;
- les objets absents ou inactifs et le refus d’une date manquante ;
- les reprogrammations valides et invalides, avec conservation de la personne, du statut et de la note ;
- les quatre statuts, leurs corrections et le refus d’un statut inconnu ;
- la création, modification et suppression de la note d’essai sans modification de la note générale de la personne ;
- les lectures riches, les champs facultatifs, le tri journalier et les listes par personne, date ou date de départ ;
- la lisibilité historique après désactivation et clôture, ainsi que la modification ultérieure du statut et de la note.

## Validation finale

Validation technique du chantier réalisée avant la rédaction de ce rapport :

```text
go test ./... : OK
git diff --check : OK
```

`sqlc generate` et `gofmt` ont également été exécutés. Aucun commit ni push n’a été réalisé.

## Points restant ouverts

Aucune décision bloquante restante pour ce chantier. L’UI, l’assemblage des coordonnées des responsables, la prévention UX des doublons et la conversion en adhésion restent hors périmètre.
