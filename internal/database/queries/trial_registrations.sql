-- Scheduling writes are called through internal/trials.Service.
-- name: CreateTrial :one
INSERT INTO trial_registrations (person_id, activity_id, group_id, group_slot_id, trial_date, status, notes)
VALUES ($1, $2, $3, $4, $5, 'registered', $6) RETURNING *;

-- name: RescheduleTrial :one
UPDATE trial_registrations SET activity_id=$2, group_id=$3, group_slot_id=$4, trial_date=$5
WHERE id=$1 RETURNING *;

-- name: UpdateTrialStatus :one
UPDATE trial_registrations SET status=$2 WHERE id=$1 RETURNING *;

-- name: UpdateTrialNotes :one
UPDATE trial_registrations SET notes=$2 WHERE id=$1 RETURNING *;

-- name: GetTrial :one
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, gs.location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.id = $1
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- name: ListPersonTrials :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, gs.location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.person_id = $1
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- name: ListTrialsByDate :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, gs.location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.trial_date = $1
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- name: ListUpcomingTrials :many
SELECT t.id AS trial_id, t.trial_date, t.status, t.notes,
       p.id AS person_id, p.first_name, p.last_name, p.birth_date, p.phone_number,
       a.id AS activity_id, a.name AS activity_name,
       t.group_id, g.name AS group_name, t.group_slot_id,
       gs.weekday, gs.start_time, gs.end_time, gs.location
FROM trial_registrations t
JOIN persons p ON p.id = t.person_id
JOIN activities a ON a.id = t.activity_id
LEFT JOIN groups g ON g.id = t.group_id
LEFT JOIN group_slots gs ON gs.id = t.group_slot_id
WHERE t.trial_date >= sqlc.arg(from_date)::date
ORDER BY t.trial_date, gs.start_time NULLS LAST, p.last_name, p.first_name, t.id;

-- Locks prevent activation/calendar changes between validation and write.
-- name: LockTrialActivity :one
SELECT is_active FROM activities WHERE id=$1 FOR SHARE;

-- name: LockTrialGroup :one
SELECT activity_id, is_active FROM groups WHERE id=$1 FOR SHARE;

-- name: LockTrialSlot :one
SELECT gs.group_id, gs.is_active,
       (EXTRACT(ISODOW FROM sqlc.arg(trial_date)::date) = gs.weekday
        AND sqlc.arg(trial_date)::date >= gs.valid_from
        AND (gs.valid_until IS NULL OR sqlc.arg(trial_date)::date <= gs.valid_until)
        AND sqlc.arg(trial_date)::date BETWEEN s.starts_at AND s.ends_at)::boolean AS calendar_valid
FROM group_slots gs JOIN seasons s ON s.id=gs.season_id
WHERE gs.id=$1 FOR SHARE OF gs, s;
