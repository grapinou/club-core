-- name: ResolveTrialQuotaSeason :many
-- Include inactive seasons: deactivation must not erase trial history.
SELECT s.id,s.name FROM seasons s
WHERE (sqlc.narg(slot_id)::integer IS NOT NULL AND s.id=(SELECT gs.season_id FROM group_slots gs WHERE gs.id=sqlc.narg(slot_id)))
 OR (sqlc.narg(slot_id)::integer IS NULL AND sqlc.arg(trial_date)::date BETWEEN s.starts_at AND s.ends_at)
ORDER BY s.starts_at DESC,s.id;

-- name: ListPersonTrialQuotaFacts :many
SELECT t.id,t.status,
 CASE WHEN t.group_slot_id IS NOT NULL THEN ARRAY[gs.season_id]::integer[]
 ELSE ARRAY(SELECT s.id FROM seasons s WHERE t.trial_date BETWEEN s.starts_at AND s.ends_at ORDER BY s.id)::integer[] END AS season_ids
FROM trial_registrations t LEFT JOIN group_slots gs ON gs.id=t.group_slot_id
WHERE t.person_id=$1 ORDER BY t.id;

-- name: ListTrialQuotaSeasons :many
SELECT id,name,is_active FROM seasons ORDER BY starts_at DESC,id DESC;

-- name: GetTrialQuotaLimit :one
SELECT max_trials_per_person_per_season FROM organizations WHERE is_active;

-- name: LockTrialQuotaPolicy :one
-- Configuration also locks this row before changing seasons or slots.
SELECT max_trials_per_person_per_season FROM organizations WHERE is_active FOR SHARE;
