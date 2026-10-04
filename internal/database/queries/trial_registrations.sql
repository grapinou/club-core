-- Scheduling writes are called through internal/trials.Service.
-- name: CreateTrial :one
INSERT INTO trial_registrations (person_id, activity_id, group_id, group_slot_id, trial_date, status, notes)
VALUES (sqlc.arg(person_id), sqlc.arg(activity_id), sqlc.arg(group_id), sqlc.arg(group_slot_id), sqlc.arg(trial_date), 'registered', sqlc.arg(notes)) RETURNING *;

-- name: RescheduleTrial :one
UPDATE trial_registrations SET activity_id=sqlc.arg(activity_id), group_id=sqlc.arg(group_id), group_slot_id=sqlc.arg(group_slot_id), trial_date=sqlc.arg(trial_date),revision=revision+1
WHERE id=sqlc.arg(id) RETURNING *;

-- name: UpdateTrialStatus :one
UPDATE trial_registrations SET status=sqlc.arg(status),revision=revision+1 WHERE id=sqlc.arg(id) RETURNING *;

-- name: UpdateTrialNotes :one
UPDATE trial_registrations SET notes=sqlc.arg(notes),revision=revision+1 WHERE id=sqlc.arg(id) RETURNING *;

-- name: GetTrial :one
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, COALESCE((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location) AS location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.id = sqlc.arg(id)
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- name: ListPersonTrials :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, COALESCE((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location) AS location,
       CAST((SELECT json_group_array(s.id) FROM seasons s
             WHERE (t.group_slot_id IS NOT NULL AND s.id=gs.season_id)
                OR (t.group_slot_id IS NULL AND t.trial_date BETWEEN s.starts_at AND s.ends_at)) AS JSON_TEXT_IDS) AS season_ids
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.person_id = sqlc.arg(person_id)
ORDER BY t.trial_date DESC, gs.start_time DESC NULLS LAST, t.id DESC;

-- name: ListTrialsByDate :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, COALESCE((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location) AS location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.trial_date = sqlc.arg(trial_date)
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- name: ListUpcomingTrials :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, COALESCE((SELECT l.name FROM locations l WHERE l.id=gs.location_id),gs.location) AS location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.trial_date >= sqlc.arg(from_date)
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- Call inside BEGIN IMMEDIATE to keep activation/calendar valid until the write.
-- name: LockTrialActivity :one
SELECT is_active FROM activities WHERE id=sqlc.arg(id);

-- name: LockTrialGroup :one
SELECT activity_id, is_active FROM groups WHERE id=sqlc.arg(id);

-- name: LockTrialSlot :one
SELECT gs.group_id, gs.is_active, CAST((((CAST(strftime('%w',sqlc.arg(trial_date)) AS INTEGER)+6)%7+1) = gs.weekday
        AND sqlc.arg(trial_date) >= gs.valid_from
        AND (gs.valid_until IS NULL OR sqlc.arg(trial_date) <= gs.valid_until)
        AND (sqlc.arg(trial_date)>=s.starts_at AND sqlc.arg(trial_date)<=s.ends_at)
        AND s.is_active) AS BOOLEAN) AS calendar_valid
FROM group_slots gs JOIN seasons s ON s.id=gs.season_id
WHERE gs.id=sqlc.arg(id);
