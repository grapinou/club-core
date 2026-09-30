-- name: ResolveTrialQuotaSeason :many
-- Include inactive seasons: deactivation must not erase trial history.
SELECT s.id,s.name FROM seasons s
WHERE (sqlc.narg(slot_id) IS NOT NULL AND s.id=(SELECT gs.season_id FROM group_slots gs WHERE gs.id=sqlc.narg(slot_id)))
 OR (sqlc.narg(slot_id) IS NULL AND (sqlc.arg(trial_date)>=s.starts_at AND sqlc.arg(trial_date)<=s.ends_at))
ORDER BY s.starts_at DESC,s.id;

-- name: ListPersonTrialQuotaFacts :many
SELECT t.id,t.status,
 CAST((SELECT json_group_array(s.id) FROM seasons s WHERE (t.group_slot_id IS NOT NULL AND s.id=gs.season_id) OR (t.group_slot_id IS NULL AND t.trial_date BETWEEN s.starts_at AND s.ends_at)) AS JSON_TEXT_IDS) AS season_ids
FROM trial_registrations t LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE t.person_id=sqlc.arg(person_id) ORDER BY t.id;

-- name: ListTrialQuotaSeasons :many
SELECT id,name,is_active FROM seasons ORDER BY starts_at DESC,id DESC;

-- name: GetTrialQuotaLimit :one
SELECT max_trials_per_person_per_season FROM organizations WHERE is_active;

-- name: LockTrialQuotaPolicy :one
-- BEGIN IMMEDIATE excludes concurrent policy, season and slot changes.
SELECT max_trials_per_person_per_season FROM organizations WHERE is_active;
